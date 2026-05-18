package main

import (
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// largeBoardJSON pads JSON past the 500-byte minimum for GH/Lever/Ashby verification.
func largeBoardJSON(core string) string {
	if len(core) > 500 {
		return core
	}
	if strings.HasPrefix(strings.TrimSpace(core), "[") {
		inner := strings.TrimSpace(core)
		inner = strings.TrimSuffix(inner, "]")
		pad := strings.Repeat("x", 520-len(inner))
		return inner + `,{"_pad":"` + pad + `"}]`
	}
	pad := strings.Repeat("x", 501-len(core))
	return core[:len(core)-1] + `,"_pad":"` + pad + `"}`
}

type mockHunterClient struct {
	mu       sync.Mutex
	handlers map[string]func(*http.Request) (*http.Response, error)
	calls    atomic.Int32
}

func (m *mockHunterClient) Do(req *http.Request) (*http.Response, error) {
	m.calls.Add(1)
	m.mu.Lock()
	h := m.handlers[req.URL.Host]
	m.mu.Unlock()
	if h == nil {
		return mockHTTPResponse(404, `{}`), nil
	}
	return h(req)
}

func TestVerifySmartRecruitersBody(t *testing.T) {
	emptyOK := `{"content":[],"totalFound":0}`
	if verifySmartRecruitersBodyExported([]byte(emptyOK)) {
		t.Fatal("empty SR response must not verify")
	}

	withTotal := `{"content":[],"totalFound":3}`
	if !verifySmartRecruitersBodyExported([]byte(withTotal)) {
		t.Fatal("totalFound > 0 should verify")
	}

	contentOnly := `{"content":[{"uuid":"abc","name":"Engineer"}],"totalFound":0}`
	if verifySmartRecruitersBodyExported([]byte(contentOnly)) {
		t.Fatal("content without totalFound > 0 must not verify")
	}
}

func TestTitleContainsTermGoNotGoogle(t *testing.T) {
	if titleContainsTerm("Google Software Engineer", "go") {
		t.Error("'go' must not match inside Google")
	}
	if titleContainsTerm("Golang Backend Engineer", "go") {
		t.Error("'go' must not substring-match inside Golang")
	}
	if !titleContainsTerm("Go Developer", "go") {
		t.Error("'go' should match Go Developer with word boundaries")
	}
	if !jobMatchesRoleQuery("Golang Backend Engineer", "Co", "golang") {
		t.Error("golang query should match Golang title via roleSynonyms")
	}
}

func TestVerifyGreenhouseBody(t *testing.T) {
	if verifyGreenhouseBody([]byte(`{"jobs":[]}`)) {
		t.Fatal("empty jobs array should fail")
	}
	if !verifyGreenhouseBody([]byte(largeBoardJSON(`{"jobs":[{"title":"Eng","absolute_url":"https://x"}]}`))) {
		t.Fatal("non-empty jobs with body >500 bytes should pass")
	}
	if verifyGreenhouseBody([]byte(`{}`)) {
		t.Fatal("tiny/invalid body should fail")
	}
}

func TestSmartRecruitersFiveJobsVerifies(t *testing.T) {
	body := `{"content":[{"uuid":"1","name":"Role A"},{"uuid":"2","name":"Role B"},{"uuid":"3","name":"Role C"},{"uuid":"4","name":"Role D"},{"uuid":"5","name":"Role E"}],"totalFound":5}`
	if !verifySmartRecruitersBodyExported([]byte(body)) {
		t.Fatal("200 OK with 5 jobs (totalFound=5) must verify")
	}

	mock := &mockHunterClient{handlers: map[string]func(*http.Request) (*http.Response, error){
		"boards-api.greenhouse.io": func(*http.Request) (*http.Response, error) {
			return mockHTTPResponse(404, `{}`), nil
		},
		"api.lever.co": func(*http.Request) (*http.Response, error) {
			return mockHTTPResponse(404, `{}`), nil
		},
		"api.ashbyhq.com": func(*http.Request) (*http.Response, error) {
			return mockHTTPResponse(404, `{}`), nil
		},
		"api.smartrecruiters.com": func(*http.Request) (*http.Response, error) {
			return mockHTTPResponse(200, body), nil
		},
	}}
	platform, ok := probeCompanyPlatformExported(mock, "realco")
	if !ok || platform != "smartrecruiters" {
		t.Fatalf("want smartrecruiters with 5 jobs, got ok=%v platform=%q", ok, platform)
	}
}

func TestProbeSmartRecruitersFalsePositive(t *testing.T) {
	slug := "fakeco"
	mock := &mockHunterClient{handlers: map[string]func(*http.Request) (*http.Response, error){
		"boards-api.greenhouse.io": func(*http.Request) (*http.Response, error) {
			return mockHTTPResponse(404, `{}`), nil
		},
		"api.lever.co": func(*http.Request) (*http.Response, error) {
			return mockHTTPResponse(404, `{}`), nil
		},
		"api.ashbyhq.com": func(*http.Request) (*http.Response, error) {
			return mockHTTPResponse(404, `{}`), nil
		},
		"api.smartrecruiters.com": func(*http.Request) (*http.Response, error) {
			// Classic false positive: 200 but no jobs
			return mockHTTPResponse(200, `{"content":[],"totalFound":0}`), nil
		},
	}}

	platform, ok := probeCompanyPlatformExported(mock, slug)
	if ok {
		t.Fatalf("expected no platform for empty SR, got %q", platform)
	}
}

func TestProbeGreenhouseWithJobs(t *testing.T) {
	mock := &mockHunterClient{handlers: map[string]func(*http.Request) (*http.Response, error){
		"boards-api.greenhouse.io": func(*http.Request) (*http.Response, error) {
			body := largeBoardJSON(`{"jobs":[{"title":"Backend","absolute_url":"https://boards.example/j/1","location":{"name":"Remote"}}]}`)
			return mockHTTPResponse(200, body), nil
		},
	}}

	platform, ok := probeCompanyPlatformExported(mock, "acme")
	if !ok || platform != "greenhouse" {
		t.Fatalf("want greenhouse, got ok=%v platform=%q", ok, platform)
	}
}

func TestHunterWorkerCountInSafeRange(t *testing.T) {
	n := hunterWorkerCount()
	if n < minHunterWorkers || n > maxHunterWorkers {
		t.Fatalf("workers %d outside safe range %d-%d", n, minHunterWorkers, maxHunterWorkers)
	}
}

func TestHunterWorkerPoolConcurrency(t *testing.T) {
	const workers = 50
	const tasks = 100
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var ran atomic.Int32

	for i := 0; i < tasks; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			ran.Add(1)
		}()
	}
	wg.Wait()

	if ran.Load() != tasks {
		t.Fatalf("ran %d want %d", ran.Load(), tasks)
	}
}

func TestVerifyLeverBody(t *testing.T) {
	leverBody := largeBoardJSON(`[{"text":"Eng","hostedUrl":"https://jobs.lever.co/x"}]`)
	if !verifyLeverBody([]byte(leverBody)) {
		t.Fatal("lever posting should verify")
	}
	if verifyLeverBody([]byte(`[]`)) {
		t.Fatal("empty lever should not verify")
	}
}

func TestIsEffectivelyEmptyBody(t *testing.T) {
	if !isEffectivelyEmptyBody(nil) {
		t.Fatal("nil body should be empty")
	}
	if !isEffectivelyEmptyBody([]byte(`{"content":[],"totalFound":0}`)) {
		t.Fatal("SR empty shell should be empty")
	}
	if isEffectivelyEmptyBody([]byte(`{"jobs":[{"title":"x"}]}`)) {
		t.Fatal("non-empty JSON should not be empty")
	}
}

func TestHunterGETMalformedURLNoPanic(t *testing.T) {
	client := &http.Client{}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("hunterGET panicked: %v", r)
		}
	}()
	_, _, err := hunterGET(client, "example.com", "://bad-url")
	if err == nil {
		t.Fatal("expected error for malformed URL")
	}
}

func TestHunterMalformedSlugNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("processHunterSlug panicked: %v", r)
		}
	}()
	r := probeHunterSlug(&mockHunterClient{handlers: map[string]func(*http.Request) (*http.Response, error){}}, "bad slug/with spaces")
	if r.platform != "" {
		t.Fatal("malformed slug should be invalid")
	}
}

func TestHunterMarksInvalidOnEmptySRHTTP(t *testing.T) {
	body := `{"content":[],"totalFound":0}`
	mock := &mockHunterClient{handlers: map[string]func(*http.Request) (*http.Response, error){
		"boards-api.greenhouse.io": func(*http.Request) (*http.Response, error) {
			return mockHTTPResponse(404, `{}`), nil
		},
		"api.lever.co": func(*http.Request) (*http.Response, error) {
			return mockHTTPResponse(404, `{}`), nil
		},
		"api.ashbyhq.com": func(*http.Request) (*http.Response, error) {
			return mockHTTPResponse(404, `{}`), nil
		},
		"api.smartrecruiters.com": func(*http.Request) (*http.Response, error) {
			return mockHTTPResponse(200, body), nil
		},
	}}
	_, ok := probeCompanyPlatformExported(mock, "emptyco")
	if ok {
		t.Fatal("hunter must not verify SR with totalFound 0 and empty content")
	}
}
