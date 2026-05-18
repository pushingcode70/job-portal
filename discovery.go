package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultHunterBatchSize = 2000
	turboHunterBatchSize   = 5000
	hunterMinBodyBytes     = 500
	hunterHTTPTimeout      = 3 * time.Second
	defaultHunterWorkers   = 65
	minHunterWorkers       = 50
	maxHunterWorkers       = 80
	defaultHostGap         = 120 * time.Millisecond
	turboHostGap           = 80 * time.Millisecond
	hunterJitterMinMS      = 10
	hunterJitterMaxMS      = 50
	hunter429Cooldown      = 60 * time.Second
	hunterChromeUserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36"
)

var platformProbePriority = []string{"greenhouse", "lever", "ashby", "smartrecruiters"}

// HTTPDoer is used by the hunter (mockable in tests).
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

var (
	hunterHTTPClient HTTPDoer = &http.Client{Timeout: hunterHTTPTimeout}
	hostThrottle     = newHostThrottler(defaultHostGap)

	priorityScrapeCh   chan priorityScrape
	priorityScrapeOnce sync.Once

	hunterStats struct {
		probed       atomic.Uint64
		verified     atomic.Uint64
		invalid      atomic.Uint64
		scraped      atomic.Uint64
		lastBatchPerSec atomic.Uint64 // companies/sec × 100 (fixed point)
	}

	hunterCooldownMu     sync.Mutex
	hunterCooldownUntil  time.Time
	hunterRand           = rand.New(rand.NewSource(time.Now().UnixNano()))
)

type priorityScrape struct {
	slug     string
	platform string
}

type hostThrottler struct {
	mu   sync.Mutex
	last map[string]time.Time
	gap  time.Duration
}

func newHostThrottler(gap time.Duration) *hostThrottler {
	return &hostThrottler{last: make(map[string]time.Time), gap: gap}
}

func (t *hostThrottler) wait(host string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if last, ok := t.last[host]; ok {
		if d := time.Since(last); d < t.gap {
			time.Sleep(t.gap - d)
		}
	}
	t.last[host] = time.Now()
}

func hunterTurboEnabled() bool {
	return os.Getenv("HUNTER_TURBO") == "1" || os.Getenv("HUNTER_TURBO") == "true"
}

func hunterSkipPriorityScrape() bool {
	return os.Getenv("HUNTER_SKIP_SCRAPE") == "1" || os.Getenv("HUNTER_SKIP_SCRAPE") == "true"
}

func applyTurboHunterDefaults() {
	if os.Getenv("HUNTER_TURBO") == "" {
		_ = os.Setenv("HUNTER_TURBO", "1")
	}
	if os.Getenv("HUNTER_WORKERS") == "" {
		_ = os.Setenv("HUNTER_WORKERS", strconv.Itoa(defaultHunterWorkers))
	}
	if os.Getenv("HUNTER_BATCH_SIZE") == "" {
		_ = os.Setenv("HUNTER_BATCH_SIZE", strconv.Itoa(turboHunterBatchSize))
	}
	if os.Getenv("HUNTER_HOST_GAP_MS") == "" {
		_ = os.Setenv("HUNTER_HOST_GAP_MS", "80")
	}
	if os.Getenv("HUNTER_SKIP_SCRAPE") == "" {
		_ = os.Setenv("HUNTER_SKIP_SCRAPE", "1")
	}
}

func hunterWorkerCount() int {
	n := defaultHunterWorkers
	if v := os.Getenv("HUNTER_WORKERS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			n = parsed
		}
	}
	if n < minHunterWorkers {
		n = minHunterWorkers
	}
	if n > maxHunterWorkers {
		n = maxHunterWorkers
	}
	return n
}

func hunterBatchSize() int {
	n := defaultHunterBatchSize
	if hunterTurboEnabled() {
		n = turboHunterBatchSize
	}
	if v := os.Getenv("HUNTER_BATCH_SIZE"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}
	return n
}

func hunterHostGap() time.Duration {
	if v := os.Getenv("HUNTER_HOST_GAP_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	if hunterTurboEnabled() {
		return turboHostGap
	}
	return defaultHostGap
}

// runResetData resets false-positive SR tags and purges jobs for unverified companies.
func runResetData() error {
	nSR, err := ResetSmartRecruitersToPending()
	if err != nil {
		return fmt.Errorf("smartrecruiters reset: %w", err)
	}
	nJobs, err := CleanupJobsForUnverifiedCompanies()
	if err != nil {
		return fmt.Errorf("job cleanup: %w", err)
	}
	fmt.Printf("[RESET] smartrecruiters→pending: %d | jobs removed: %d\n", nSR, nJobs)
	go RefreshRAMCache()
	return nil
}

// runDiscoveryMaintenance runs one-time data fixes (SR reset + invalid job cleanup).
func runDiscoveryMaintenance(resetSR, cleanupJobs bool) {
	if resetSR {
		n, err := ResetSmartRecruitersToPending()
		if err != nil {
			fmt.Printf("[MAINT] SR reset error: %v\n", err)
		} else {
			fmt.Printf("[MAINT] Reset %d smartrecruiters → pending\n", n)
		}
	}
	if cleanupJobs {
		n, err := CleanupJobsForUnverifiedCompanies()
		if err != nil {
			fmt.Printf("[MAINT] Job cleanup error: %v\n", err)
		} else {
			fmt.Printf("[MAINT] Removed %d jobs for unverified/invalid companies\n", n)
			go RefreshRAMCache()
		}
	}
}

// ResetSmartRecruitersToPending fixes false-positive SmartRecruiters platform tags.
func ResetSmartRecruitersToPending() (int64, error) {
	res, err := DB.Exec(
		`UPDATE companies SET platform = 'pending', last_checked = CURRENT_TIMESTAMP WHERE platform = 'smartrecruiters'`,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func fetchPendingCompanySlugs(limit int) ([]string, error) {
	rows, err := DB.Query(
		`SELECT slug FROM companies INDEXED BY idx_companies_platform
		 WHERE platform = 'pending' ORDER BY slug LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var slugs []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			continue
		}
		slugs = append(slugs, slug)
	}
	return slugs, rows.Err()
}

func updateCompanyPlatform(slug, platform string) error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := DB.Exec(
		`UPDATE companies SET platform = ?, last_checked = CURRENT_TIMESTAMP WHERE slug = ?`,
		platform, strings.ToLower(slug),
	)
	return err
}

func readResponseBody(resp *http.Response, maxBytes int64) ([]byte, error) {
	if resp == nil {
		return nil, fmt.Errorf("nil response")
	}
	defer resp.Body.Close()
	if maxBytes <= 0 {
		maxBytes = 512 * 1024
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBytes))
}

// isEffectivelyEmptyBody rejects empty HTTP bodies and trivial empty JSON shells.
func isEffectivelyEmptyBody(body []byte) bool {
	if len(body) == 0 {
		return true
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed == "{}" || trimmed == "[]" {
		return true
	}
	if strings.Contains(trimmed, `"content":[]`) && !strings.Contains(trimmed, `"uuid"`) {
		if strings.Contains(trimmed, `"totalFound":0`) || strings.Contains(trimmed, `"totalFound": 0`) {
			return true
		}
	}
	return false
}

func boardBodyLargeEnough(body []byte) bool {
	return len(body) > hunterMinBodyBytes
}

func verifyGreenhouseBody(body []byte) bool {
	if !boardBodyLargeEnough(body) {
		return false
	}
	var data struct {
		Jobs []json.RawMessage `json:"jobs"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return false
	}
	return len(data.Jobs) > 0
}

func verifyLeverBody(body []byte) bool {
	if !boardBodyLargeEnough(body) {
		return false
	}
	var data []struct {
		HostedURL string `json:"hostedUrl"`
		Text      string `json:"text"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return false
	}
	for _, j := range data {
		if j.HostedURL != "" || j.Text != "" {
			return true
		}
	}
	return false
}

func verifyAshbyBody(body []byte) bool {
	if !boardBodyLargeEnough(body) {
		return false
	}
	var data struct {
		Jobs []json.RawMessage `json:"jobs"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return false
	}
	return len(data.Jobs) > 0
}

// verifySmartRecruitersBody accepts only boards reporting totalFound > 0.
func verifySmartRecruitersBody(body []byte) bool {
	var data struct {
		TotalFound int `json:"totalFound"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return false
	}
	return data.TotalFound > 0
}

func hunterRequestJitter() {
	ms := hunterJitterMinMS + hunterRand.Intn(hunterJitterMaxMS-hunterJitterMinMS+1)
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

func hunterWaitIfRateLimited() {
	hunterCooldownMu.Lock()
	until := hunterCooldownUntil
	hunterCooldownMu.Unlock()
	if wait := time.Until(until); wait > 0 {
		time.Sleep(wait)
	}
}

func hunterTrigger429Cooldown() {
	hunterCooldownMu.Lock()
	if time.Now().Before(hunterCooldownUntil) {
		hunterCooldownMu.Unlock()
		hunterWaitIfRateLimited()
		return
	}
	hunterCooldownUntil = time.Now().Add(hunter429Cooldown)
	hunterCooldownMu.Unlock()
	fmt.Printf("⚠️ [CRITICAL] HTTP 429 Rate Limited! Hunter sleeping %s before resuming.\n", hunter429Cooldown)
	time.Sleep(hunter429Cooldown)
}

func hunterGET(client HTTPDoer, host, url string) ([]byte, int, error) {
	if client == nil {
		return nil, 0, fmt.Errorf("nil http client")
	}
	hunterWaitIfRateLimited()
	hunterRequestJitter()
	hostThrottle.wait(host)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid request: %w", err)
	}
	if req == nil {
		return nil, 0, fmt.Errorf("nil request from http.NewRequest")
	}
	req.Header.Set("User-Agent", hunterUserAgent())
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp == nil {
		return nil, 0, fmt.Errorf("nil response from client")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		hunterTrigger429Cooldown()
		return nil, http.StatusTooManyRequests, fmt.Errorf("rate limited (429)")
	}
	body, err := readResponseBody(resp, 512*1024)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if isEffectivelyEmptyBody(body) {
		return body, resp.StatusCode, fmt.Errorf("empty job payload")
	}
	return body, resp.StatusCode, nil
}

func hunterUserAgent() string {
	if v := os.Getenv("HUNTER_USER_AGENT"); v != "" {
		return v
	}
	return hunterChromeUserAgent
}

type atsProbe struct {
	platform string
	host     string
	url      string
	verify   func([]byte) bool
}

func probesForSlug(slug string) []atsProbe {
	return []atsProbe{
		{
			platform: "greenhouse",
			host:     "boards-api.greenhouse.io",
			url:      fmt.Sprintf("https://boards-api.greenhouse.io/v1/boards/%s/jobs", slug),
			verify:   verifyGreenhouseBody,
		},
		{
			platform: "lever",
			host:     "api.lever.co",
			url:      fmt.Sprintf("https://api.lever.co/v0/postings/%s", slug),
			verify:   verifyLeverBody,
		},
		{
			platform: "ashby",
			host:     "api.ashbyhq.com",
			url:      fmt.Sprintf("https://api.ashbyhq.com/posting-api/job-board/%s", slug),
			verify:   verifyAshbyBody,
		},
		{
			platform: "smartrecruiters",
			host:     "api.smartrecruiters.com",
			url:      fmt.Sprintf("https://api.smartrecruiters.com/v1/companies/%s/postings", slug),
			verify:   verifySmartRecruitersBody,
		},
	}
}

func pickPlatformByPriority(found map[string]struct{}) (string, bool) {
	for _, p := range platformProbePriority {
		if _, ok := found[p]; ok {
			return p, true
		}
	}
	return "", false
}

// probeCompanyPlatform hits all ATS endpoints in parallel and picks the highest-priority match.
func probeCompanyPlatform(client HTTPDoer, slug string) (platform string, ok bool) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return "", false
	}

	probes := probesForSlug(slug)
	found := make(map[string]struct{}, 4)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, p := range probes {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, status, err := hunterGET(client, p.host, p.url)
			if err != nil || status != http.StatusOK || !p.verify(body) {
				return
			}
			mu.Lock()
			found[p.platform] = struct{}{}
			mu.Unlock()
		}()
	}
	wg.Wait()

	return pickPlatformByPriority(found)
}

func enqueuePriorityScrape(slug, platform string) {
	priorityScrapeOnce.Do(func() {
		priorityScrapeCh = make(chan priorityScrape, 4096)
		go runPriorityScrapeWorker()
	})
	select {
	case priorityScrapeCh <- priorityScrape{slug: slug, platform: platform}:
	default:
		// Channel full — drop rather than block hunter workers
	}
}

func runPriorityScrapeWorker() {
	const flushEvery = 32
	buf := make([]priorityScrape, 0, flushEvery)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	flush := func() {
		if len(buf) == 0 {
			return
		}
		scrapeAndIngestPriority(buf)
		buf = buf[:0]
	}

	for {
		select {
		case item, ok := <-priorityScrapeCh:
			if !ok {
				flush()
				return
			}
			buf = append(buf, item)
			if len(buf) >= flushEvery {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func scrapeAndIngestPriority(items []priorityScrape) {
	byPlatform := make(map[string][]string)
	for _, it := range items {
		byPlatform[it.platform] = append(byPlatform[it.platform], it.slug)
	}

	var gh, lv, sr, zh, as, db, wd []string
	gh = byPlatform["greenhouse"]
	lv = byPlatform["lever"]
	sr = byPlatform["smartrecruiters"]
	zh = byPlatform["zoho"]
	as = byPlatform["ashby"]
	db = byPlatform["darwinbox"]
	wd = byPlatform["workday"]

	if len(gh)+len(lv)+len(sr)+len(zh)+len(as)+len(db)+len(wd) == 0 {
		return
	}

	jobs := scrapeJobs(gh, lv, sr, zh, as, db, wd, "")
	if len(jobs) == 0 {
		return
	}

	var records []JobRecord
	now := time.Now()
	for _, j := range jobs {
		if j.URL == "" {
			continue
		}
		records = append(records, JobRecord{
			URL:         j.URL,
			Title:       j.Title,
			Company:     strings.ToLower(j.Company),
			Location:    j.Location,
			LocationTag: j.LocationTag,
			IsIndia:     j.IsIndia,
			Timestamp:   now,
		})
	}
	if err := batchInsertJobs(records); err != nil {
		fmt.Printf("[HUNTER] Priority scrape insert error: %v\n", err)
		return
	}
	hunterStats.scraped.Add(uint64(len(records)))
	fmt.Printf("[HUNTER] Priority scrape ingested %d jobs from %d companies\n", len(records), len(items))

	// Quick cache refresh so UI picks up new jobs; full refresh runs after syncJobsToDB
	go RefreshRAMCacheQuick()
}

type hunterResult struct {
	slug     string
	platform string
}

func probeHunterSlug(client HTTPDoer, slug string) hunterResult {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[HUNTER] Recovered panic for slug %q: %v\n", slug, r)
		}
	}()

	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" || strings.ContainsAny(slug, " \t\n\r/\\") {
		return hunterResult{slug: slug, platform: ""}
	}

	platform, ok := probeCompanyPlatform(client, slug)
	if ok {
		return hunterResult{slug: slug, platform: platform}
	}
	return hunterResult{slug: slug, platform: ""}
}

func flushHunterResults(results []hunterResult) {
	if len(results) == 0 {
		return
	}

	updates := make([]CompanyPlatformUpdate, 0, len(results))
	var scrapeQueue []priorityScrape

	for _, r := range results {
		hunterStats.probed.Add(1)
		if r.platform != "" {
			updates = append(updates, CompanyPlatformUpdate{Slug: r.slug, Platform: r.platform})
			if !hunterSkipPriorityScrape() {
				scrapeQueue = append(scrapeQueue, priorityScrape{slug: r.slug, platform: r.platform})
			}
		} else {
			updates = append(updates, CompanyPlatformUpdate{Slug: r.slug, Platform: ""})
		}
	}

	verified, invalid, err := BatchUpdateCompanyPlatforms(updates)
	if err != nil {
		fmt.Printf("[HUNTER] Batch DB update error: %v\n", err)
		return
	}
	hunterStats.verified.Add(uint64(verified))
	hunterStats.invalid.Add(uint64(invalid))

	for _, item := range scrapeQueue {
		enqueuePriorityScrape(item.slug, item.platform)
	}
}

func runHunterBatch(client HTTPDoer, slugs []string, workers int) {
	sem := make(chan struct{}, workers)
	results := make([]hunterResult, len(slugs))
	var wg sync.WaitGroup

	for i, slug := range slugs {
		i, slug := i, slug
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = probeHunterSlug(client, slug)
		}()
	}
	wg.Wait()
	flushHunterResults(results)
}

// logStats prints the 60s heartbeat: backlog counts, job totals, and hunter speed.
func logStats() {
	if DB == nil {
		return
	}
	var pending, verified, invalid, dbJobs int
	_ = DB.QueryRow(`SELECT count(*) FROM companies WHERE platform = 'pending'`).Scan(&pending)
	_ = DB.QueryRow(`SELECT count(*) FROM companies WHERE platform = 'invalid'`).Scan(&invalid)
	_ = DB.QueryRow(`SELECT count(*) FROM companies WHERE platform IN ('greenhouse','lever','smartrecruiters','ashby','zoho','darwinbox','workday')`).Scan(&verified)
	_ = DB.QueryRow(`SELECT count(*) FROM jobs`).Scan(&dbJobs)

	RAMCacheMutex.RLock()
	ramJobs := 0
	for _, cr := range RAMCache {
		ramJobs += len(cr.Jobs)
	}
	RAMCacheMutex.RUnlock()

	speed := float64(hunterStats.lastBatchPerSec.Load()) / 100.0
	fmt.Printf(
		"[STATS] Pending: %d | Verified: %d | Invalid: %d | Total Jobs: %d (RAM: %d)\n",
		pending, verified, invalid, dbJobs, ramJobs,
	)
	fmt.Printf("[SPEED] Current Rate: ~%.1f companies/sec\n", speed)
}

func startStatsLogger() {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		logStats()
		for range ticker.C {
			logStats()
		}
	}()
}

// backgroundHunter discovers ATS platforms for pending companies with a worker pool.
func backgroundHunter() {
	hostThrottle.gap = hunterHostGap()
	workers := hunterWorkerCount()
	batchSize := hunterBatchSize()
	hunterHTTPClient = &http.Client{Timeout: hunterHTTPTimeout}
	client := hunterHTTPClient

	turbo := hunterTurboEnabled()
	fmt.Printf("[HUNTER] Started turbo=%v workers=%d batch=%d host_gap=%s skip_scrape=%v\n",
		turbo, workers, batchSize, hostThrottle.gap, hunterSkipPriorityScrape())

	var pendingStart int
	if DB != nil {
		_ = DB.QueryRow(`SELECT count(*) FROM companies WHERE platform = 'pending'`).Scan(&pendingStart)
		if pendingStart > 0 {
			fmt.Printf("[HUNTER] Backlog: %d pending companies to classify\n", pendingStart)
		}
	}

	for {
		slugs, err := fetchPendingCompanySlugs(batchSize)
		if err != nil {
			fmt.Printf("[HUNTER] Fetch pending error: %v\n", err)
			time.Sleep(30 * time.Second)
			continue
		}
		if len(slugs) == 0 {
			time.Sleep(1 * time.Minute)
			continue
		}

		start := time.Now()
		runHunterBatch(client, slugs, workers)
		elapsed := time.Since(start)
		rate := float64(len(slugs)) / elapsed.Seconds()
		hunterStats.lastBatchPerSec.Store(uint64(rate * 100))

		var pendingLeft int
		if DB != nil {
			_ = DB.QueryRow(`SELECT count(*) FROM companies WHERE platform = 'pending'`).Scan(&pendingLeft)
		}
		eta := ""
		if pendingLeft > 0 && rate > 0 {
			remaining := time.Duration(float64(pendingLeft)/rate) * time.Second
			eta = fmt.Sprintf(" | ETA ~%s", remaining.Round(time.Minute))
		}

		fmt.Printf(
			"[HUNTER] Batch %d slugs in %s (%.0f/s) pending_left=%d%s | total probed=%d verified=%d invalid=%d\n",
			len(slugs), elapsed.Round(time.Millisecond), rate, pendingLeft, eta,
			hunterStats.probed.Load(), hunterStats.verified.Load(), hunterStats.invalid.Load(),
		)
	}
}

// runProcessPendingCLI runs Safe Turbo discovery without starting the web server.
func runProcessPendingCLI() {
	applyTurboHunterDefaults()
	fmt.Println("[HUNTER] Safe Turbo — 50-80 workers, jittered requests, 429 backoff, strict ATS verification.")
	fmt.Println("[HUNTER] Tip: sqlite3 jobs.db \"UPDATE companies SET platform='pending' WHERE platform='smartrecruiters';\"")
	fmt.Println("[HUNTER] After run: ./job-portal --build-index to ingest jobs for verified companies.")
	startStatsLogger()
	backgroundHunter()
}

// SetHunterHTTPClient replaces the hunter HTTP client (tests only).
func SetHunterHTTPClient(c HTTPDoer) {
	hunterHTTPClient = c
}

// probeCompanyPlatformExported exposes probing for tests.
func probeCompanyPlatformExported(client HTTPDoer, slug string) (string, bool) {
	return probeCompanyPlatform(client, slug)
}

// verifySmartRecruitersBodyExported exposes SR verification for tests.
func verifySmartRecruitersBodyExported(body []byte) bool {
	return verifySmartRecruitersBody(body)
}

// mockResponse helper for tests
func mockHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
		Header:     make(http.Header),
	}
}

var (
	serperThrottle sync.Map

	SerperDiscoveredCompaniesCount int64
	SerperDiscoveredJobsCount      int64
	SerperStatsMu                  sync.Mutex
	SerperQueryCompanyCounts       = make(map[string]int)
	SerperQueryJobCounts           = make(map[string]int)
)

func TriggerRuntimeDiscovery(query string) {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" || len(query) <= 2 {
		return
	}

	if os.Getenv("BYPASS_SERPER_THROTTLE") != "1" {
		lastSearched, loaded := serperThrottle.Load(query)
		if loaded {
			if time.Since(lastSearched.(time.Time)) < 60*time.Minute {
				fmt.Printf("[HUNTER] Query %s searched recently. Throttling.\n", query)
				return
			}
		}
		serperThrottle.Store(query, time.Now())
	}

	serperKey := os.Getenv("SERPER_API_KEY")
	if serperKey == "" {
		fmt.Println("[HUNTER] ❌ SERPER_API_KEY missing. Skipping Serper hunt.")
		return
	}

	fmt.Printf("[HUNTER] Triggering Runtime Discovery for query: %s\n", query)
	q := fmt.Sprintf(`site:boards.greenhouse.io OR site:jobs.lever.co OR site:api.ashbyhq.com "%s"`, query)
	payload := fmt.Sprintf(`{"q":"%s"}`, strings.ReplaceAll(q, `"`, `\"`))
	req, err := http.NewRequest("POST", "https://google.serper.dev/search", strings.NewReader(payload))
	if err != nil {
		fmt.Printf("[HUNTER] Failed to create Serper request: %v\n", err)
		return
	}
	req.Header.Set("X-API-KEY", serperKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[HUNTER] Serper request failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var data struct {
		Organic []struct {
			Link string `json:"link"`
		} `json:"organic"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		fmt.Printf("[HUNTER] Failed to decode Serper response: %v\n", err)
		return
	}

	var newJobsIngested bool

	for idx, item := range data.Organic {
		if idx >= 10 {
			break
		}
		link := item.Link
		var slug string
		var platform string

		if strings.Contains(link, "boards.greenhouse.io/") {
			platform = "greenhouse"
			if strings.Contains(link, "for=") {
				slug = strings.Split(strings.Split(link, "for=")[1], "&")[0]
			} else {
				slug = strings.Split(strings.Split(link, "boards.greenhouse.io/")[1], "/")[0]
			}
		} else if strings.Contains(link, "jobs.lever.co/") {
			slug = strings.Split(strings.Split(link, "jobs.lever.co/")[1], "/")[0]
			platform = "lever"
		} else if strings.Contains(link, "api.ashbyhq.com/posting-api/job-board/") {
			slug = strings.Split(strings.Split(link, "api.ashbyhq.com/posting-api/job-board/")[1], "/")[0]
			platform = "ashby"
		} else if strings.Contains(link, "jobs.ashbyhq.com/") {
			slug = strings.Split(strings.Split(link, "jobs.ashbyhq.com/")[1], "/")[0]
			platform = "ashby"
		} else if strings.Contains(link, "ashbyhq.com/") {
			parts := strings.Split(strings.Split(link, "ashbyhq.com/")[1], "/")
			if len(parts) > 0 {
				if parts[0] == "posting-api" && len(parts) > 2 {
					slug = parts[2]
				} else {
					slug = parts[0]
				}
			}
			platform = "ashby"
		}

		if slug != "" {
			slug = strings.ToLower(strings.Split(slug, "?")[0])
			if slug == "jobs" || slug == "search" || slug == "embed" || slug == "companies" || slug == "" {
				continue
			}

			var count int
			err := DB.QueryRow("SELECT COUNT(*) FROM companies WHERE slug = ?", slug).Scan(&count)
			if err == nil && count == 0 {
				_, err = DB.Exec("INSERT INTO companies (slug, name, platform, is_indian, last_checked) VALUES (?, ?, ?, ?, ?)", slug, slug, platform, true, time.Now())
				if err != nil {
					fmt.Printf("[HUNTER] Failed to insert company %s: %v\n", slug, err)
					continue
				}
				fmt.Printf("[HUNTER] Discovered new company: %s (%s)\n", slug, platform)
				
				SerperStatsMu.Lock()
				SerperDiscoveredCompaniesCount++
				SerperQueryCompanyCounts[query]++
				SerperStatsMu.Unlock()
			}

			var rawJobs []Job
			if platform == "greenhouse" {
				rawJobs = scrapeJobs([]string{slug}, nil, nil, nil, nil, nil, nil, "")
			} else if platform == "lever" {
				rawJobs = scrapeJobs(nil, []string{slug}, nil, nil, nil, nil, nil, "")
			} else if platform == "ashby" {
				rawJobs = scrapeJobs(nil, nil, nil, nil, []string{slug}, nil, nil, "")
			}

			if len(rawJobs) > 0 {
				var dbJobs []JobRecord
				now := time.Now()
				for _, j := range rawJobs {
					if j.URL == "" {
						continue
					}
					dbJobs = append(dbJobs, JobRecord{
						URL:         j.URL,
						Title:       j.Title,
						Company:     strings.ToLower(j.Company),
						Location:    j.Location,
						LocationTag: j.LocationTag,
						IsIndia:     j.IsIndia,
						Timestamp:   now,
					})
				}
				if err := batchUpsertJobs(dbJobs); err != nil {
					fmt.Printf("[HUNTER] Error upserting jobs for %s: %v\n", slug, err)
				} else {
					fmt.Printf("[HUNTER] Successfully ingested %d jobs for discovered company %s\n", len(dbJobs), slug)
					newJobsIngested = true
					
					SerperStatsMu.Lock()
					SerperDiscoveredJobsCount += int64(len(dbJobs))
					SerperQueryJobCounts[query] += len(dbJobs)
					SerperStatsMu.Unlock()
				}
			}
		}
	}

	if newJobsIngested {
		fmt.Println("[HUNTER] New jobs ingested. Refreshing RAM cache.")
		RefreshRAMCache()
	}
}
