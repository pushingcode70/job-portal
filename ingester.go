package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-rod/rod"
)

const (
	startupIndiaSearchURL      = "https://api.startupindia.gov.in/sih/1.0/search/startups"
	startupIndiaProfilesURL    = "https://api.startupindia.gov.in/sih/api/noauth/search/profiles"
	startupIndiaFedoraUA       = "Mozilla/5.0 (X11; Fedora; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36"
	startupIndiaOrigin         = "https://www.startupindia.gov.in"
	startupIndiaReferer        = "https://www.startupindia.gov.in/content/sih/en/search.html"
	startupIndiaPageSize       = 20
	startupIndiaMaxOffset      = 200000
	startupIndiaProfilesPerPage     = 9
	defaultStartupIndiaWorkers      = 32
	maxStartupIndiaWorkers          = 50
	startupIndiaProgressInterval    = 30 * time.Second
	startupIndiaPageLogEvery        = 25
	startupIndiaCheckpointFile      = ".startup-india-page"
	startupIndiaMaxEmptyPages       = 5
	defaultStartupIndiaAPIDelay     = 500 * time.Millisecond
	minStartupIndiaAPIDelay         = 400 * time.Millisecond
	maxStartupIndiaAPIDelay         = 3 * time.Second
	defaultStartupIndiaATSHostGap   = 100 * time.Millisecond
	defaultStartupIndiaAPIFetchers  = 2
	maxStartupIndiaAPIFetchers      = 3
)

var (
	startupIndiaAPIRateMu   sync.Mutex
	startupIndiaAPIRateLast time.Time
	startupIndiaAPIRateGap   = defaultStartupIndiaAPIDelay
	startupIndiaATSHostGap   = newHostThrottler(defaultStartupIndiaATSHostGap)
)

type startupIndiaPlatformCounters struct {
	greenhouse      atomic.Uint64
	lever           atomic.Uint64
	ashby           atomic.Uint64
	smartrecruiters atomic.Uint64
	freshteam       atomic.Uint64
	zohorecruit     atomic.Uint64
	bamboohr        atomic.Uint64
	recruitee       atomic.Uint64
	workable        atomic.Uint64
}

func (c *startupIndiaPlatformCounters) inc(platform string) {
	switch platform {
	case "greenhouse":
		c.greenhouse.Add(1)
	case "lever":
		c.lever.Add(1)
	case "ashby":
		c.ashby.Add(1)
	case "smartrecruiters":
		c.smartrecruiters.Add(1)
	case "freshteam":
		c.freshteam.Add(1)
	case "zohorecruit":
		c.zohorecruit.Add(1)
	case "bamboohr":
		c.bamboohr.Add(1)
	case "recruitee":
		c.recruitee.Add(1)
	case "workable":
		c.workable.Add(1)
	}
}

func (c *startupIndiaPlatformCounters) totalNew() uint64 {
	return c.greenhouse.Load() + c.lever.Load() + c.ashby.Load() +
		c.smartrecruiters.Load() + c.freshteam.Load() + c.zohorecruit.Load() +
		c.bamboohr.Load() + c.recruitee.Load() + c.workable.Load()
}

type atsEndpoint struct {
	platform string
	host     string
	url      string
	verify   func([]byte) bool
}

func atsEndpointsForSlug(slug string) []atsEndpoint {
	return []atsEndpoint{
		{"greenhouse", "boards-api.greenhouse.io", fmt.Sprintf("https://boards-api.greenhouse.io/v1/boards/%s/jobs", slug), verifyGreenhouseBody},
		{"lever", "api.lever.co", fmt.Sprintf("https://api.lever.co/v0/postings/%s", slug), verifyLeverBody},
		{"ashby", "api.ashbyhq.com", fmt.Sprintf("https://api.ashbyhq.com/posting-api/job-board/%s", slug), verifyAshbyBody},
		{"smartrecruiters", "api.smartrecruiters.com", fmt.Sprintf("https://api.smartrecruiters.com/v1/companies/%s/postings", slug), verifySmartRecruitersBody},
		{"freshteam", slug + ".freshteam.com", fmt.Sprintf("https://%s.freshteam.com/jobs", slug), verifyFreshteamBody},
		{"zohorecruit", slug + ".zohorecruit.com", fmt.Sprintf("https://%s.zohorecruit.com/jobs", slug), verifyZohoRecruitBody},
		{"bamboohr", slug + ".bamboohr.com", fmt.Sprintf("https://%s.bamboohr.com/careers", slug), verifyBambooHRBody},
		{"recruitee", slug + ".recruitee.com", fmt.Sprintf("https://%s.recruitee.com/api/offers", slug), verifyRecruiteeBody},
		{"workable", "apply.workable.com", fmt.Sprintf("https://apply.workable.com/api/v1/widget/accounts/%s", slug), verifyWorkableBody},
	}
}

func startupIndiaAPIDelay() time.Duration {
	if v := os.Getenv("STARTUP_INDIA_API_DELAY_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return defaultStartupIndiaAPIDelay
}

func startupIndiaAPIFetchers() int {
	n := defaultStartupIndiaAPIFetchers
	if v := os.Getenv("STARTUP_INDIA_API_FETCHERS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}
	if n > maxStartupIndiaAPIFetchers {
		n = maxStartupIndiaAPIFetchers
	}
	return n
}

func startupIndiaWaitAPI() {
	startupIndiaAPIRateMu.Lock()
	gap := startupIndiaAPIRateGap
	if gap < minStartupIndiaAPIDelay {
		gap = minStartupIndiaAPIDelay
	}
	if since := time.Since(startupIndiaAPIRateLast); since < gap {
		time.Sleep(gap - since)
	}
	startupIndiaAPIRateLast = time.Now()
	startupIndiaAPIRateMu.Unlock()
}

func startupIndiaAPIBackoff() {
	startupIndiaAPIRateMu.Lock()
	if startupIndiaAPIRateGap < maxStartupIndiaAPIDelay {
		startupIndiaAPIRateGap += 250 * time.Millisecond
		fmt.Printf("[STARTUP INDIA] API throttle increased to %s (rate-limit protection)\n", startupIndiaAPIRateGap.Round(time.Millisecond))
	}
	startupIndiaAPIRateMu.Unlock()
}

func startupIndiaAPIRelax() {
	startupIndiaAPIRateMu.Lock()
	if startupIndiaAPIRateGap > minStartupIndiaAPIDelay {
		startupIndiaAPIRateGap -= 25 * time.Millisecond
	}
	startupIndiaAPIRateMu.Unlock()
}

func startupIndiaInitAPIDelay() {
	startupIndiaAPIRateMu.Lock()
	startupIndiaAPIRateGap = startupIndiaAPIDelay()
	startupIndiaAPIRateLast = time.Time{}
	startupIndiaAPIRateMu.Unlock()
	gap := defaultStartupIndiaATSHostGap
	if v := os.Getenv("STARTUP_INDIA_ATS_HOST_GAP_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			gap = time.Duration(ms) * time.Millisecond
		}
	}
	startupIndiaATSHostGap.gap = gap
}

func brandSlugVariations(brand string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, v := range []string{brand, brand + "-india", brand + "-tech", brand + "india"} {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func probeATSVerified(client *http.Client, ep atsEndpoint) bool {
	if ep.verify == nil {
		return false
	}
	startupIndiaATSHostGap.wait(ep.host)

	req, err := http.NewRequest(http.MethodGet, ep.url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", startupIndiaFedoraUA)
	req.Header.Set("Accept", "application/json, text/html, */*")

	resp, err := client.Do(req)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return false
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		return false
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	resp.Body.Close()
	if err != nil || isEffectivelyEmptyBody(body) {
		return false
	}
	return ep.verify(body)
}

func probeSlugATSParallel(client *http.Client, slug string) (platform string, ok bool) {
	probes := atsEndpointsForSlug(slug)
	found := make(map[string]struct{}, len(probes))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, ep := range probes {
		ep := ep
		wg.Add(1)
		go func() {
			defer wg.Done()
			if probeATSVerified(client, ep) {
				mu.Lock()
				found[ep.platform] = struct{}{}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	for _, p := range platformProbePriority {
		if _, hit := found[p]; hit {
			return p, true
		}
	}
	return "", false
}

func detectATSPlatform(client *http.Client, brand string) (platform, winningSlug string, ok bool) {
	for _, slug := range brandSlugVariations(brand) {
		if platform, hit := probeSlugATSParallel(client, slug); hit {
			return platform, slug, true
		}
	}
	return "", "", false
}

func companySlugExists(slug string) bool {
	var exists int
	err := DB.QueryRow("SELECT 1 FROM companies WHERE slug = ? LIMIT 1", slug).Scan(&exists)
	return err == nil && exists == 1
}

func parseStartupIndiaCompanyNames(body []byte) []string {
	addName := func(names *[]string, n string) {
		n = strings.TrimSpace(n)
		if n != "" {
			*names = append(*names, n)
		}
	}

	var names []string
	var envelope struct {
		Content []struct {
			CompanyName             string `json:"companyName"`
			Name                    string `json:"name"`
			DippRecognitionStatus   string `json:"dippRecognitionStatus"`
		} `json:"content"`
		Hits []struct {
			CompanyName string `json:"companyName"`
			Name        string `json:"name"`
			Source      struct {
				CompanyName string `json:"companyName"`
			} `json:"_source"`
		} `json:"hits"`
		Results []struct {
			CompanyName string `json:"companyName"`
		} `json:"results"`
		Data struct {
			Hits []struct {
				CompanyName string `json:"companyName"`
			} `json:"hits"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil
	}
	for _, h := range envelope.Content {
		if status := strings.ToUpper(strings.TrimSpace(h.DippRecognitionStatus)); status != "" && status != "RECOGNISED" {
			continue
		}
		n := h.CompanyName
		if n == "" {
			n = h.Name
		}
		addName(&names, n)
	}
	for _, h := range envelope.Hits {
		n := h.CompanyName
		if n == "" {
			n = h.Source.CompanyName
		}
		if n == "" {
			n = h.Name
		}
		addName(&names, n)
	}
	for _, h := range envelope.Results {
		addName(&names, h.CompanyName)
	}
	for _, h := range envelope.Data.Hits {
		addName(&names, h.CompanyName)
	}
	return names
}

func startupIndiaPOST(client *http.Client, url string, payload []byte) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(payload)))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", startupIndiaFedoraUA)
	req.Header.Set("Origin", startupIndiaOrigin)
	req.Header.Set("Referer", startupIndiaReferer)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		startupIndiaAPIBackoff()
		return body, resp.StatusCode, fmt.Errorf("rate limited (429)")
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode >= 500 {
			startupIndiaAPIBackoff()
		}
		return body, resp.StatusCode, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	startupIndiaAPIRelax()
	return body, resp.StatusCode, nil
}

func fetchStartupIndiaV1Page(client *http.Client, offset int) ([]string, int, error) {
	payload := fmt.Sprintf(
		`{"from":%d,"size":%d,"status":"Recognised","appliedFacets":{},"sort":"desc"}`,
		offset, startupIndiaPageSize,
	)
	body, status, err := startupIndiaPOST(client, startupIndiaSearchURL, []byte(payload))
	if err != nil {
		return nil, status, err
	}
	return parseStartupIndiaCompanyNames(body), status, nil
}

func startupIndiaWorkerCount() int {
	n := defaultStartupIndiaWorkers
	if v := os.Getenv("STARTUP_INDIA_WORKERS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}
	if n > maxStartupIndiaWorkers {
		n = maxStartupIndiaWorkers
	}
	return n
}

func fetchStartupIndiaProfilesPage(client *http.Client, page int) ([]string, int, int, error) {
	payload, err := json.Marshal(map[string]interface{}{
		"query":              "",
		"focusSector":        false,
		"industries":         []string{},
		"sectors":            []string{},
		"states":             []string{},
		"cities":             []string{},
		"stages":             []string{},
		"badges":             []string{},
		"roles":              []string{"Startup"},
		"page":               page,
		"sort":               map[string]interface{}{"orders": []map[string]string{{"field": "registeredOn", "direction": "DESC"}}},
		"dpiitRecogniseUser": true,
		"internationalUser":  false,
	})
	if err != nil {
		return nil, 0, 0, err
	}
	body, status, err := startupIndiaPOST(client, startupIndiaProfilesURL, payload)
	if err != nil {
		return nil, status, 0, err
	}
	var meta struct {
		TotalElements int `json:"totalElements"`
	}
	_ = json.Unmarshal(body, &meta)
	return parseStartupIndiaCompanyNames(body), status, meta.TotalElements, nil
}

// IngestStartupIndia scrapes recognised startups via the official API and validates ATS boards.
func IngestStartupIndia() {
	workers := startupIndiaWorkerCount()
	apiFetchers := startupIndiaAPIFetchers()
	startupIndiaInitAPIDelay()
	fmt.Println("[TITAN] ══════════════════════════════════════════════════════")
	fmt.Println("[TITAN]  Startup India API Discovery + 9-ATS Validation (turbo)")
	fmt.Printf("[TITAN]  ATS workers: %d | API fetchers: %d | API gap: %s | ATS host gap: %s\n",
		workers, apiFetchers, startupIndiaAPIRateGap, startupIndiaATSHostGap.gap)
	fmt.Println("[TITAN]  Tune: STARTUP_INDIA_API_DELAY_MS STARTUP_INDIA_API_FETCHERS STARTUP_INDIA_ATS_HOST_GAP_MS")
	fmt.Println("[TITAN]  Progress: [STARTUP INDIA] every 25 pages + [TITAN] heartbeat every 30s")
	fmt.Println("[TITAN] ══════════════════════════════════════════════════════")

	if DB == nil {
		fmt.Println("[TITAN] Database not initialized. Aborting.")
		return
	}

	var totalScanned atomic.Uint64
	var counters startupIndiaPlatformCounters
	var catalogPage atomic.Int64
	var catalogTotal atomic.Int64
	var extractionDone atomic.Bool

	brandChan := make(chan string, 5000)
	transport := &http.Transport{
		MaxIdleConns:        workers * 4,
		MaxIdleConnsPerHost: workers * 2,
		IdleConnTimeout:     90 * time.Second,
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: transport}

	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)

	printStartupIndiaStatus := func(label string) {
		page := catalogPage.Load()
		total := catalogTotal.Load()
		queued := len(brandChan)
		scanned := totalScanned.Load()
		boards := counters.totalNew()
		pct := ""
		if total > 0 && page >= 0 {
			pct = fmt.Sprintf(" (~%.1f%% of catalogue)", float64(page*startupIndiaProfilesPerPage)/float64(total)*100)
		}
		fmt.Printf("\n[TITAN] --- %s %s ---\n", label, time.Now().Format("15:04:05"))
		fmt.Printf("[TITAN] API page          : %d%s\n", page, pct)
		if total > 0 {
			fmt.Printf("[TITAN] Catalogue total   : ~%d recognised startups\n", total)
		}
		fmt.Printf("[TITAN] Names scanned     : %d\n", scanned)
		fmt.Printf("[TITAN] Queue depth       : %d (brandChan)\n", queued)
		fmt.Printf("[TITAN] New ATS boards    : %d\n", boards)
		fmt.Printf("[TITAN] Workers active    : %d\n", workers)
		if extractionDone.Load() {
			fmt.Println("[TITAN] API extraction    : finished (draining validation queue)")
		} else {
			fmt.Println("[TITAN] API extraction    : running")
		}
		fmt.Println("[TITAN] -------------------------")
	}

	progressDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(startupIndiaProgressInterval)
		defer ticker.Stop()
		printStartupIndiaStatus("heartbeat")
		for {
			select {
			case <-progressDone:
				return
			case <-ticker.C:
				printStartupIndiaStatus("heartbeat")
			}
		}
	}()

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range brandChan {
				sem <- struct{}{}
				brand := cleanBrand(name)
				if brand == "" {
					<-sem
					continue
				}

				skip := false
				for _, v := range brandSlugVariations(brand) {
					if companySlugExists(v) {
						skip = true
						break
					}
				}
				if skip {
					<-sem
					continue
				}

				platform, winningSlug, found := detectATSPlatform(client, brand)
				<-sem
				if !found {
					continue
				}

				err := batchInsertCompanies([]CompanyRecord{{
					Slug:        winningSlug,
					Name:        name,
					Platform:    platform,
					IsIndian:    true,
					Industry:    "Startup India",
					LastChecked: time.Now(),
				}})
				if err != nil {
					fmt.Printf("[TITAN] Insert error for %s: %v\n", winningSlug, err)
					continue
				}
				counters.inc(platform)
				if os.Getenv("STARTUP_INDIA_VERBOSE") == "1" {
					fmt.Printf("[TITAN] +1 board: %s (%s)\n", winningSlug, platform)
				}
			}
		}()
	}

	go func() {
		defer close(brandChan)
		defer extractionDone.Store(true)
		fmt.Println("[STARTUP INDIA] Starting internal API extractor...")
		CurrentStartupIndiaKeyword.Store("API:Recognised")

		tryV1 := func() bool {
			names, status, err := fetchStartupIndiaV1Page(client, 0)
			if err != nil {
				fmt.Printf("[STARTUP INDIA] v1 API unavailable (status=%d): %v\n", status, err)
				fmt.Println("[STARTUP INDIA] Skipping /sih/1.0/search/startups — switching to profiles API.")
				return false
			}
			for _, name := range names {
				totalScanned.Add(1)
				brandChan <- name
			}
			fmt.Printf("[STARTUP INDIA] v1 API returned %d names at offset 0; continuing v1 pagination.\n", len(names))
			for offset := startupIndiaPageSize; offset <= startupIndiaMaxOffset; offset += startupIndiaPageSize {
				names, status, err := fetchStartupIndiaV1Page(client, offset)
				if err != nil {
					fmt.Printf("[STARTUP INDIA] offset=%d status=%d error: %v\n", offset, status, err)
					break
				}
				if len(names) == 0 {
					break
				}
				for _, name := range names {
					totalScanned.Add(1)
					brandChan <- name
				}
				if offset%1000 == 0 {
					fmt.Printf("[STARTUP INDIA] Progress offset=%d scanned=%d new_boards=%d\n",
						offset, totalScanned.Load(), counters.totalNew())
				}
				if len(names) < startupIndiaPageSize {
					return true
				}
				startupIndiaWaitAPI()
			}
			return true
		}

		paginateProfiles := func() {
			fmt.Println("[STARTUP INDIA] Using portal profiles API (noauth/search/profiles).")
			startPage := 0
			if data, err := os.ReadFile(startupIndiaCheckpointFile); err == nil {
				if p, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && p > 0 {
					startPage = p
					fmt.Printf("[STARTUP INDIA] Resuming from checkpoint page %d\n", startPage)
				}
			}

			var (
				pageMu            sync.Mutex
				nextPage          = startPage
				consecutiveErrors int
				emptyPages        int
				stopFetch         atomic.Bool
			)

			fetchPage := func(page int) {
				catalogPage.Store(int64(page))
				startupIndiaWaitAPI()
				names, status, totalElts, err := fetchStartupIndiaProfilesPage(client, page)
				if err != nil {
					pageMu.Lock()
					consecutiveErrors++
					errCount := consecutiveErrors
					pageMu.Unlock()
					fmt.Printf("[STARTUP INDIA] page=%d status=%d error: %v\n", page, status, err)
					if errCount >= 5 {
						stopFetch.Store(true)
					}
					return
				}
				pageMu.Lock()
				consecutiveErrors = 0
				pageMu.Unlock()

				if totalElts > 0 {
					catalogTotal.Store(int64(totalElts))
				}

				maxPage := int64(0)
				if total := catalogTotal.Load(); total > 0 {
					maxPage = (total + int64(startupIndiaProfilesPerPage) - 1) / int64(startupIndiaProfilesPerPage)
				}

				if len(names) == 0 {
					pageMu.Lock()
					emptyPages++
					streak := emptyPages
					pageMu.Unlock()
					fmt.Printf("[STARTUP INDIA] Empty page %d (%d/%d empty streak)\n", page, streak, startupIndiaMaxEmptyPages)
					if maxPage > 0 && int64(page) >= maxPage {
						stopFetch.Store(true)
						return
					}
					if streak >= startupIndiaMaxEmptyPages {
						stopFetch.Store(true)
					}
					return
				}
				pageMu.Lock()
				emptyPages = 0
				pageMu.Unlock()

				for _, name := range names {
					totalScanned.Add(1)
					brandChan <- name
				}

				if page == startPage || page%startupIndiaPageLogEvery == 0 {
					startupIndiaAPIRateMu.Lock()
					gap := startupIndiaAPIRateGap
					startupIndiaAPIRateMu.Unlock()
					total := catalogTotal.Load()
					eta := ""
					if total > 0 {
						pagesLeft := (total / int64(startupIndiaProfilesPerPage)) - int64(page)
						if pagesLeft > 0 {
							eta = fmt.Sprintf(" | ETA ~%s", (time.Duration(pagesLeft) * gap).Round(time.Minute))
						}
					}
					fmt.Printf("[STARTUP INDIA] page=%d scanned=%d queued=%d new_boards=%d%s\n",
						page, totalScanned.Load(), len(brandChan), counters.totalNew(), eta)
				}

				if page%startupIndiaPageLogEvery == 0 {
					_ = os.WriteFile(startupIndiaCheckpointFile, []byte(fmt.Sprintf("%d", page)), 0644)
				}

				if maxPage > 0 && int64(page) >= maxPage-1 {
					fmt.Printf("[STARTUP INDIA] Completed catalogue at page %d (~%d startups).\n", page, totalElts)
					_ = os.Remove(startupIndiaCheckpointFile)
					stopFetch.Store(true)
				}
			}

			var fetchWg sync.WaitGroup
			for f := 0; f < apiFetchers; f++ {
				fetchWg.Add(1)
				go func() {
					defer fetchWg.Done()
					for !stopFetch.Load() {
						pageMu.Lock()
						if stopFetch.Load() {
							pageMu.Unlock()
							return
						}
						page := nextPage
						nextPage++
						pageMu.Unlock()

						if page >= 200000 {
							return
						}
						fetchPage(page)
					}
				}()
			}
			fetchWg.Wait()
		}

		if !tryV1() {
			paginateProfiles()
		}

		CurrentStartupIndiaKeyword.Store("")
		fmt.Println("[STARTUP INDIA] API extraction finished.")
	}()

	wg.Wait()
	close(progressDone)
	printStartupIndiaStatus("final")

	scanned := totalScanned.Load()
	totalNew := counters.totalNew()
	fmt.Println()
	fmt.Println("[TITAN] STARTUP INDIA DISCOVERY COMPLETE")
	fmt.Println("-------------------------------------------")
	fmt.Printf("Total Startups Scanned from Portal : %d\n", scanned)
	fmt.Printf("Total New Valid Boards Added       : %d\n", totalNew)
	fmt.Println("-------------------------------------------")
	fmt.Printf("Greenhouse      -> %-5d    Lever           -> %d\n", counters.greenhouse.Load(), counters.lever.Load())
	fmt.Printf("Ashby           -> %-5d    SmartRecruiters -> %d\n", counters.ashby.Load(), counters.smartrecruiters.Load())
	fmt.Printf("Freshteam       -> %-5d    Zoho Recruit    -> %d\n", counters.freshteam.Load(), counters.zohorecruit.Load())
	fmt.Printf("BambooHR        -> %-5d    Recruitee       -> %d\n", counters.bamboohr.Load(), counters.recruitee.Load())
	fmt.Printf("Workable        -> %d\n", counters.workable.Load())
	fmt.Println("-------------------------------------------")
	fmt.Println()
}

var (
	techKeywords = []string{
		"SOFTWARE", "TECH", "DIGITAL", "SYSTEMS", "LABS", "SOLUTIONS", "INFOTECH", "DATA", "CONSULTANCY",
	}
	brandRegex = regexp.MustCompile(`(?i)( PVT LTD| PRIVATE LIMITED| LIMITED| LTD| LLP| INC| CORP| SOLUTIONS| TECHNOLOGIES| SOFTWARE| SERVICES| INDIA)`)
)

func cleanBrand(name string) string {
	clean := brandRegex.ReplaceAllString(name, "")
	clean = strings.ReplaceAll(clean, ".", "")
	clean = strings.ReplaceAll(clean, ",", "")
	parts := strings.Fields(clean)
	if len(parts) > 0 {
		return strings.ToLower(parts[0])
	}
	return ""
}

func IngestAllSeeds(deepHunt bool) {
	fmt.Println("[INGEST] ══════════════════════════════════════════════")
	fmt.Println("[INGEST]  Streaming Engine: Processing /seeds/ Directory")
	fmt.Println("[INGEST] ══════════════════════════════════════════════")

	start := time.Now()
	var totalProcessed uint64

	files, err := os.ReadDir("seeds")
	if err != nil {
		fmt.Printf("[INGEST] Error reading seeds directory: %v\n", err)
		return
	}

	var validCompanies []CompanyRecord

	flushCompanies := func() {
		if len(validCompanies) == 0 {
			return
		}
		err := batchInsertCompanies(validCompanies)
		if err != nil {
			fmt.Printf("[DB] Batch insert error: %v\n", err)
		} else {
			fmt.Printf("[DB] Successfully inserted batch of %d companies.\n", len(validCompanies))
		}
		validCompanies = nil // Reset
	}

	for _, file := range files {
		if filepath.Ext(file.Name()) == ".csv" {
			filePath := filepath.Join("seeds", file.Name())
			f, err := os.Open(filePath)
			if err != nil {
				continue
			}

			reader := csv.NewReader(f)
			reader.ReuseRecord = true 

			// Find header indices
			header, err := reader.Read()
			if err != nil {
				f.Close()
				continue
			}
			nameIdx := -1
			industryIdx := -1
			for i, val := range header {
				lowerVal := strings.ToLower(val)
				if strings.Contains(lowerVal, "name") {
					nameIdx = i
				}
				if strings.Contains(lowerVal, "industry") || strings.Contains(lowerVal, "domain") {
					industryIdx = i
				}
			}

			if nameIdx == -1 {
				nameIdx = 0 
			}

			var fileRowCount uint64
			var newLeadsFound uint64

			for {
				record, err := reader.Read()
				if err == io.EOF {
					break
				}
				if err != nil {
					continue
				}
				totalProcessed++
				fileRowCount++

				if fileRowCount%10000 == 0 {
					fmt.Printf("[INGEST] File: %s | Row: %d | New Tech Leads Found: %d\n", file.Name(), fileRowCount, newLeadsFound)
				}

				name := record[nameIdx]
				industry := ""
				if industryIdx != -1 && industryIdx < len(record) {
					industry = record[industryIdx]
				}

				upperName := strings.ToUpper(name)
				upperIndustry := strings.ToUpper(industry)
				
				isTech := false
				for _, kw := range techKeywords {
					if strings.Contains(upperName, kw) || strings.Contains(upperIndustry, kw) {
						isTech = true
						break
					}
				}

				if isTech {
					brand := cleanBrand(name)
					if brand != "" {
						var exists int
						err := DB.QueryRow("SELECT 1 FROM companies WHERE slug = ?", brand).Scan(&exists)
						if err == nil && exists == 1 {
							continue
						}

						newLeadsFound++
						validCompanies = append(validCompanies, CompanyRecord{
							Slug:        brand,
							Name:        name,
							Platform:    "pending",
							IsIndian:    strings.Contains(strings.ToLower(name), "india") || strings.Contains(brand, "india"),
							Industry:    industry,
							LastChecked: time.Now(),
						})

						if len(validCompanies) >= 10000 {
							flushCompanies()
						}
					}
				}
			}
			f.Close()
			fmt.Printf("[DONE] Finished processing %s\n", file.Name())
		}
	}

	// Final flush
	flushCompanies()

	fmt.Printf("\n[REPORT] ══════════════════════════════════════\n")
	fmt.Printf("[REPORT]  GROWTH AGGREGATOR RESULTS\n")
	fmt.Printf("[REPORT] Total CSV Records : %d\n", totalProcessed)
	fmt.Printf("[REPORT] Total Run Time    : %s\n", time.Since(start).Round(time.Second))
	fmt.Printf("[REPORT] ══════════════════════════════════════\n\n")
}

// ─────────────────────────────────────────────────────────────────────────────
// Indian Seed Pipeline (Startup India API & NASSCOM Scraper)
// ─────────────────────────────────────────────────────────────────────────────

var (
	CurrentStartupIndiaKeyword atomic.Value // string
	CurrentNasscomKeyword      atomic.Value // string
)

func IngestIndianSeeds() {
	fmt.Println("[INGEST] ══════════════════════════════════════════════")
	fmt.Println("[INGEST]  Indian Seed Pipeline: Startup India & NASSCOM")
	fmt.Println("[INGEST] ══════════════════════════════════════════════")

	brandChan := make(chan string, 5000)
	done := make(chan struct{})

	// Normalization & Database Insertion Loop
	go func() {
		var validCompanies []CompanyRecord
		var totalProcessed uint64
		var newLeadsFound uint64

		flushCompanies := func() {
			if len(validCompanies) == 0 {
				return
			}
			err := batchInsertCompanies(validCompanies)
			if err != nil {
				fmt.Printf("[DB] Batch insert error: %v\n", err)
			} else {
				fmt.Printf("[DB] Successfully inserted batch of %d Indian companies.\n", len(validCompanies))
			}
			validCompanies = nil
		}

		for name := range brandChan {
			totalProcessed++
			brand := cleanBrand(name)
			if brand == "" {
				continue
			}

			// Database Guard: Discard if already in DB
			var exists int
			err := DB.QueryRow("SELECT 1 FROM companies WHERE slug = ? LIMIT 1", brand).Scan(&exists)
			if err == nil && exists == 1 {
				continue
			}

			newLeadsFound++
			validCompanies = append(validCompanies, CompanyRecord{
				Slug:        brand,
				Name:        strings.Title(strings.ToLower(brand)),
				Platform:    "pending",
				IsIndian:    true,
				Industry:    "Indian Startup",
				LastChecked: time.Now(),
			})

			if len(validCompanies) >= 1000 {
				flushCompanies()
			}
		}

		// Final flush
		flushCompanies()
		fmt.Printf("\n[REPORT] ══════════════════════════════════════\n")
		fmt.Printf("[REPORT]  INDIAN PIPELINE RESULTS\n")
		fmt.Printf("[REPORT] Total Names Processed : %d\n", totalProcessed)
		fmt.Printf("[REPORT] New Indian Leads Added: %d\n", newLeadsFound)
		fmt.Printf("[REPORT] ══════════════════════════════════════\n\n")
		close(done)
	}()

	// Run fetchers sequentially
	fetchStartupIndia(brandChan)
	fetchNASSCOM(brandChan)

	close(brandChan)
	<-done
}

func fetchStartupIndia(brandChan chan<- string) {
	fmt.Println("[STARTUP INDIA] Starting Serper API Extractor...")
	serperKey := os.Getenv("SERPER_API_KEY")
	if serperKey == "" {
		fmt.Println("[STARTUP INDIA] SERPER_API_KEY is missing. Skipping.")
		return
	}

	keywords := []string{
		"AI", "Software", "Tech", "Data", "Cloud", "SaaS", "Fintech", "Healthtech", "Edtech", "E-commerce",
		"Web3", "Blockchain", "Cybersecurity", "IoT", "Robotics", "Agritech", "Proptech", "Logistics", "Deeptech", "Machine Learning",
		"Mobile Apps", "B2B", "B2C", "D2C", "Gaming", "AR/VR", "Cleantech", "EV", "Semiconductor", "Hardware",
		"Retailtech", "Insurtech", "Legaltech", "HRtech", "Traveltech", "Spacetech", "Medtech", "Biotech", "Analytics", "DevOps",
		"IT Services", "Consulting", "Enterprise Software", "Open Source", "Design", "Automation", "Marketplace", "CRM", "ERP",
		"Bangalore", "Pune", "Hyderabad", "Gurgaon", "Noida", "Chennai", "Mumbai", "Delhi",
	}
	client := &http.Client{Timeout: 15 * time.Second}

	for _, kw := range keywords {
		CurrentStartupIndiaKeyword.Store(kw)
		dork := fmt.Sprintf(`startup india recognized %s companies`, kw)
		payload := fmt.Sprintf(`{"q":"%s","num":20}`, dork)

		req, err := http.NewRequest("POST", "https://google.serper.dev/search", strings.NewReader(payload))
		if err != nil {
			continue
		}
		req.Header.Set("X-API-KEY", serperKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("[STARTUP INDIA] Network error: %v\n", err)
			continue
		}

		if resp.StatusCode != 200 {
			fmt.Printf("[STARTUP INDIA] Unexpected status: %d. Stopping.\n", resp.StatusCode)
			resp.Body.Close()
			break
		}

		var data struct {
			Organic []struct {
				Title string `json:"title"`
			} `json:"organic"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		for _, item := range data.Organic {
			title := item.Title
			// Clean common Google search title suffixes
			title = strings.Split(title, "| Startup India")[0]
			title = strings.Split(title, "- Startup India")[0]
			title = strings.TrimSpace(title)
			if title != "" {
				brandChan <- title
			}
		}
		time.Sleep(1 * time.Second)
	}
	CurrentStartupIndiaKeyword.Store("")
	fmt.Println("[STARTUP INDIA] Pipeline Finished.")
}

func fetchNASSCOM(brandChan chan<- string) {
	fmt.Println("[NASSCOM] Starting Serper API Extractor...")
	serperKey := os.Getenv("SERPER_API_KEY")
	if serperKey == "" {
		fmt.Println("[NASSCOM] SERPER_API_KEY is missing. Skipping.")
		return
	}

	keywords := []string{
		"AI", "Software", "Tech", "Data", "Cloud", "SaaS", "Fintech", "Healthtech", "Edtech", "E-commerce",
		"Web3", "Blockchain", "Cybersecurity", "IoT", "Robotics", "Agritech", "Proptech", "Logistics", "Deeptech", "Machine Learning",
		"Mobile Apps", "B2B", "B2C", "D2C", "Gaming", "AR/VR", "Cleantech", "EV", "Semiconductor", "Hardware",
		"Retailtech", "Insurtech", "Legaltech", "HRtech", "Traveltech", "Spacetech", "Medtech", "Biotech", "Analytics", "DevOps",
		"IT Services", "Consulting", "Enterprise Software", "Open Source", "Design", "Automation", "Marketplace", "CRM", "ERP",
		"Bangalore", "Pune", "Hyderabad", "Gurgaon", "Noida", "Chennai", "Mumbai", "Delhi",
	}
	client := &http.Client{Timeout: 15 * time.Second}

	for _, kw := range keywords {
		CurrentNasscomKeyword.Store(kw)
		dork := fmt.Sprintf(`nasscom members %s companies`, kw)
		payload := fmt.Sprintf(`{"q":"%s","num":20}`, dork)

		req, err := http.NewRequest("POST", "https://google.serper.dev/search", strings.NewReader(payload))
		if err != nil {
			continue
		}
		req.Header.Set("X-API-KEY", serperKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("[NASSCOM] Network error: %v\n", err)
			continue
		}

		if resp.StatusCode != 200 {
			fmt.Printf("[NASSCOM] Unexpected status: %d. Stopping.\n", resp.StatusCode)
			resp.Body.Close()
			break
		}

		var data struct {
			Organic []struct {
				Title string `json:"title"`
			} `json:"organic"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		for _, item := range data.Organic {
			title := item.Title
			// Clean common Google search title suffixes
			title = strings.Split(title, "| NASSCOM")[0]
			title = strings.Split(title, "- NASSCOM")[0]
			title = strings.TrimSpace(title)
			if title != "" {
				brandChan <- title
			}
		}
		time.Sleep(1 * time.Second)
	}
	CurrentNasscomKeyword.Store("")
	fmt.Println("[NASSCOM] Pipeline Finished.")
}

func IngestManualList() {
	manualList := []string{
		"Binance", "Bread Financial", "Credit Agricole Bank Polska", "ING Bank Śląski", "Millennium Bank",
		"NuBank", "PKO BP – PKO Junior App", "Skandia", "Société Générale", "SoFi", "Solflare",
		"Ueno Bank", "Virgin Money", "TAB Australia", "Tide", "Cheddar", "BMW", "MOIA (Volkswagen Group)",
		"Toyota", "1KOMMA5°", "Polarium", "Viessmann Climate Solutions", "Monta", "LG Electronics",
		"Sonova", "Whirlpool", "Xiaomi", "Universal Studios", "teamLab", "AlloFresh", "Burger King Finland",
		"Delivery Hero", "Taco Bell Finland", "Google Earth", "Google One", "CZ", "GEICO", "Bauhaus",
		"Bricomarché", "eBay Motors", "Coop Norge", "Etsy", "MediaMarktSaturn", "Sizeer", "Żabka / Żappka",
		"Moi Mobiili Finland", "NOS", "Red Bull MOBILE", "Couchsurfing", "easyJet", "Lufthansa Group – Miles & More",
		"Michelin / ViaMichelin", "MGM Resorts", "SAS Airlines", "Agapé", "Headspace", "Welliba", "Caribou Coffee",
		"Wedel", "Tata Neu", "Subex", "Syrma SGS", "Sasken Tech.", "Sahana System", "Sagility India",
		"RS Software", "Route Mobile", "R Systems Intl.", "Quick Heal Tech.", "Quadrant Future", "Prime Focus",
		"Persistent Sys.", "Oracle Fin.Serv.", "Onward Tech.", "Nucleus Soft.", "Newgen Software", "Niyogin Fintech",
		"Netweb Tech.", "Nazara Tech.", "Mphasis", "Mindteck", "Mastek", "MPS Ltd", "Mold-Tek Tech.", "LTI Mindtree",
	}

	fmt.Println("[MANUAL] Starting manual ingestion pipeline...")
	var totalProcessed, newLeadsFound int

	var validCompanies []CompanyRecord
	flushCompanies := func() {
		if len(validCompanies) == 0 {
			return
		}
		err := batchInsertCompanies(validCompanies)
		if err != nil {
			fmt.Printf("[DB] Batch insert error: %v\n", err)
		} else {
			fmt.Printf("[DB] Successfully inserted batch of %d manual companies (including variations).\n", len(validCompanies))
		}
		validCompanies = nil
	}

	for _, name := range manualList {
		totalProcessed++
		brand := cleanBrand(name)
		if brand == "" {
			continue
		}

		variations := []string{brand, brand + "-india", brand + "-tech"}

		for _, v := range variations {
			// Database Guard: Discard if already in DB
			var exists int
			err := DB.QueryRow("SELECT 1 FROM companies WHERE slug = ? LIMIT 1", v).Scan(&exists)
			if err == nil && exists == 1 {
				continue
			}

			newLeadsFound++
			validCompanies = append(validCompanies, CompanyRecord{
				Slug:        v,
				Name:        strings.Title(strings.ToLower(brand)),
				Platform:    "pending",
				IsIndian:    true, // requested by prompt
				Industry:    "Manual Seed",
				LastChecked: time.Now(),
			})

			if len(validCompanies) >= 1000 {
				flushCompanies()
			}
		}
	}
	flushCompanies()

	fmt.Printf("\n[REPORT] ══════════════════════════════════════\n")
	fmt.Printf("[REPORT] MANUAL PIPELINE RESULTS\n")
	fmt.Printf("[REPORT] Total Names Processed : %d\n", totalProcessed)
	fmt.Printf("[REPORT] New Variations Queued : %d\n", newLeadsFound)
	fmt.Printf("[REPORT] Run 'go run . --build-index' to validate ATS.\n")
	fmt.Printf("[REPORT] ══════════════════════════════════════\n\n")
}

func IngestDeepDive() {
	const checkpointFile = ".deep-dive-checkpoint"
	// Load last saved page index for resumption
	var pageIdx int
	if data, err := os.ReadFile(checkpointFile); err == nil {
		fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pageIdx)
		fmt.Printf("[DEEP-DIVE] Resuming from page %d (checkpoint found)\n", pageIdx)
	} else {
		fmt.Println("[DEEP-DIVE] Starting fresh from page 0.")
	}

	saveCheckpoint := func(idx int) {
		os.WriteFile(checkpointFile, []byte(fmt.Sprintf("%d", idx)), 0644)
	}

	fmt.Println("[DEEP-DIVE] Starting 200,000+ Startup India Headless Exhaustion Pipeline...")
	var consecutiveErrors int
	var totalScanned, newBrands, boardsValidated int

	var preCount int
	if DB != nil {
		DB.QueryRow("SELECT count(*) FROM companies").Scan(&preCount)
	}

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fmt.Printf("\n[STATUS] --- %s ---\n", time.Now().Format("03:04 PM"))
				fmt.Printf("Total Pages Scanned   : %d\n", pageIdx)
				fmt.Printf("Total Records Scanned : %d / ~200000\n", totalScanned)
				fmt.Printf("New Brands Extracted  : %d\n", newBrands)
				fmt.Printf("Boards Validated Today: %d\n", boardsValidated)
				if consecutiveErrors > 0 {
					fmt.Printf("Status                : Error (%d consecutive)\n", consecutiveErrors)
				} else {
					fmt.Printf("Status                : Active (Headless OK)\n")
				}
				fmt.Printf("-------------------------\n")
			case <-done:
				return
			}
		}
	}()

	var browser *rod.Browser
	newBrowser := func() *rod.Browser {
		if browser != nil {
			browser.Close()
		}
		b := rod.New().MustConnect()
		browser = b
		fmt.Println("[DEEP-DIVE] Browser (re)started.")
		return b
	}
	browser = newBrowser()
	defer func() {
		if browser != nil {
			browser.Close()
		}
	}()

	client := &http.Client{Timeout: 10 * time.Second}

	// safePage opens a URL in the browser but recovers from panics (e.g. closed WebSocket)
	safePage := func(url string) (page *rod.Page, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("browser panic: %v", r)
				page = nil
			}
		}()
		page = browser.MustPage(url)
		return page, nil
	}

	for {
		time.Sleep(1500 * time.Millisecond)

		// Restart browser every 50 pages to prevent Chrome WebSocket exhaustion
		if pageIdx > 0 && pageIdx%50 == 0 {
			fmt.Printf("[DEEP-DIVE] Restarting browser at page %d to prevent memory leak...\n", pageIdx)
			browser = newBrowser()
		}

		url := fmt.Sprintf("https://www.startupindia.gov.in/content/sih/en/search.html?roles=Startup&page=%d", pageIdx)
		page, pageErr := safePage(url)
		if pageErr != nil || page == nil {
			fmt.Printf("[DEEP-DIVE] Browser error on page %d: %v. Restarting browser...\n", pageIdx, pageErr)
			browser = newBrowser()
			consecutiveErrors++
			if consecutiveErrors >= 10 {
				fmt.Println("[DEEP-DIVE] Hit 10 consecutive errors. Stopping pipeline.")
				break
			}
			continue
		}

		loadErr := page.WaitLoad()
		if loadErr != nil {
			page.Close()
			consecutiveErrors++
			continue
		}

		time.Sleep(2 * time.Second) // Wait for dynamic content to render

		elements, elemErr := page.Elements("h3")
		if elemErr != nil || len(elements) < 3 {
			consecutiveErrors++
			page.Close()
			if consecutiveErrors >= 5 {
				fmt.Println("[DEEP-DIVE] Empty pages or missing elements. Likely reached the end. Stopping pipeline.")
				break
			}
			continue
		}

		consecutiveErrors = 0
		var foundOnPage int

		for i, el := range elements {
			if i == 0 {
				continue // Skip the first h3 which is usually "Menu" or generic
			}
			
			text, err := el.Text()
			if err != nil || strings.TrimSpace(text) == "" {
				continue
			}

			// Skip known false positives in their template
			if strings.Contains(text, "Please Login") || strings.Contains(text, "Error") || strings.Contains(text, "logout") {
				continue
			}

			totalScanned++
			foundOnPage++
			brand := cleanBrand(text)
			if brand == "" {
				continue
			}

			var exists int
			err = DB.QueryRow("SELECT 1 FROM companies WHERE slug = ? LIMIT 1", brand).Scan(&exists)
			if err == nil && exists == 1 {
				continue
			}

			newBrands++
			variations := []string{brand, brand + "-india"}
			foundATS := false
			var detectedPlatform string
			var winningSlug string
			for _, v := range variations {
				platform, ok := probeCompanyPlatform(client, v)
				if ok {
					foundATS = true
					detectedPlatform = platform
					winningSlug = v
					break
				}
			}

			if foundATS {
				boardsValidated++
				batchInsertCompanies([]CompanyRecord{{
					Slug:        winningSlug,
					Name:        strings.Title(strings.ToLower(brand)),
					Platform:    detectedPlatform,
					IsIndian:    true,
					Industry:    "Startup India",
					LastChecked: time.Now(),
				}})
			}
		}

		page.Close()
		
		if foundOnPage == 0 {
			fmt.Println("[DEEP-DIVE] No valid companies found on this page. Stopping.")
			break
		}

		pageIdx++
		saveCheckpoint(pageIdx) // Save progress after every page
	}

	close(done)
	var postCount int
	if DB != nil {
		DB.QueryRow("SELECT count(*) FROM companies").Scan(&postCount)
	}
	RunGrowthTest(preCount, postCount)
}
