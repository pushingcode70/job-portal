package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/joho/godotenv"
)

var (
	// Failure Tracking
	FailureCounts = make(map[string]int)
	FailureMutex  sync.Mutex

	// RAM Cache
	RAMCache      []CompanyResponse
	RAMCacheMutex sync.RWMutex

	// Third Party Cache
	ThirdPartyCache      = make(map[string][]Job)
	ThirdPartyCacheMutex sync.RWMutex
)

func RefreshRAMCache() {
	refreshRAMCacheFromQuery("")
}

// RefreshRAMCacheQuick loads a subset so search is responsive during cold start.
func RefreshRAMCacheQuick() {
	refreshRAMCacheFromQuery("LIMIT 200000")
}

func loadIndianCompanySlugs() map[string]bool {
	indian := make(map[string]bool)
	rows, err := DB.Query(`SELECT slug FROM companies WHERE is_indian = 1`)
	if err != nil {
		return indian
	}
	defer rows.Close()
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err == nil {
			indian[strings.ToLower(slug)] = true
		}
	}
	return indian
}

// fetchFromJooble calls the Jooble API and returns standardized Job slice
func fetchFromJooble(query string, page int) []Job {
	var jobs []Job
	apiKey := os.Getenv("JOOBLE_API_KEY")
	if apiKey == "" {
		return jobs
	}

	url := "https://jooble.org/api/" + apiKey
	payload := map[string]interface{}{
		"keywords": query,
		"location": "India",
		"page":     fmt.Sprintf("%d", page),
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", url, strings.NewReader(string(body)))
	if err != nil {
		return jobs
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		return jobs
	}
	defer resp.Body.Close()

	var data struct {
		Jobs []struct {
			Title    string `json:"title"`
			Company  string `json:"company"`
			Location string `json:"location"`
			URL      string `json:"link"`
		} `json:"jobs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return jobs
	}

	for _, j := range data.Jobs {
		tag, isIndia := GetLocationContext(j.Location)
		jobs = append(jobs, Job{
			Title:       j.Title,
			Company:     j.Company,
			Location:    j.Location,
			LocationTag: tag,
			URL:         j.URL,
			IsIndia:     isIndia,
		})
	}
	return jobs
}

// fetchFromCareerjet calls the Careerjet API and returns standardized Job slice
func fetchFromCareerjet(query string) []Job {
	var jobs []Job
	url := "http://public.api.careerjet.net/search?locale_code=en_IN"
	if query != "" {
		url += "&keywords=" + query
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		return jobs
	}
	defer resp.Body.Close()

	var data struct {
		Jobs []struct {
			Title    string `json:"title"`
			Company  string `json:"company"`
			Location string `json:"locations"`
			URL      string `json:"url"`
		} `json:"jobs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return jobs
	}

	for _, j := range data.Jobs {
		tag, isIndia := GetLocationContext(j.Location)
		jobs = append(jobs, Job{
			Title:       j.Title,
			Company:     j.Company,
			Location:    j.Location,
			LocationTag: tag,
			URL:         j.URL,
			IsIndia:     isIndia,
		})
	}
	return jobs
}

// VerifyAPIIntegrations performs a diagnostic test of the JOOBLE and SERPER APIs
func VerifyAPIIntegrations() {
	joobleKey := os.Getenv("JOOBLE_API_KEY")
	serperKey := os.Getenv("SERPER_API_KEY")

	if joobleKey == "" || serperKey == "" {
		fmt.Println("================================================================================")
		fmt.Println("  [CRITICAL WARNING] API KEYS MISSING")
		fmt.Println("  Please ensure both JOOBLE_API_KEY and SERPER_API_KEY are present in .env")
		fmt.Println("================================================================================")
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}

	// Test Serper
	reqSerper, _ := http.NewRequest("POST", "https://google.serper.dev/search", strings.NewReader(`{"q":"test"}`))
	reqSerper.Header.Set("X-API-KEY", serperKey)
	reqSerper.Header.Set("Content-Type", "application/json")
	respS, errS := client.Do(reqSerper)
	if errS == nil && respS.StatusCode == 200 {
		fmt.Println("[HEALTH] ✅ Serper API: Connected (Credits: Active)")
	} else {
		fmt.Println("[HEALTH] ❌ Serper API: Invalid Key or No Credits")
	}
	if respS != nil {
		respS.Body.Close()
	}

	// Test Jooble
	urlJooble := "https://jooble.org/api/" + joobleKey
	payloadJooble := `{"keywords": "Software", "location": "India"}`
	reqJooble, _ := http.NewRequest("POST", urlJooble, strings.NewReader(payloadJooble))
	reqJooble.Header.Set("Content-Type", "application/json")
	respJ, errJ := client.Do(reqJooble)
	if errJ == nil && respJ.StatusCode == 200 {
		fmt.Println("[HEALTH] ✅ Jooble API: Authorized")
	} else {
		fmt.Println("[HEALTH] ❌ Jooble API: Unauthorized or Error")
	}
	if respJ != nil {
		respJ.Body.Close()
	}
}

type scrapeTarget struct {
	slug     string
	platform string
}

const syncBatchSize = 50

// syncJobsToDB scrapes verified companies with UPSERT + mark-and-sweep expiration.
func syncJobsToDB() {
	fmt.Println("[SYNC] ══════════════════════════════════════════════")
	fmt.Println("[SYNC]  Persistent sync (UPSERT + mark-and-sweep)...")
	fmt.Println("[SYNC] ══════════════════════════════════════════════")

	syncStart := time.Now()
	syncCompleted := false
	defer func() {
		if syncCompleted {
			if n, err := SweepStaleJobsAfterSync(syncStart, syncSweepGrace()); err != nil {
				fmt.Printf("[SYNC] Sweep error: %v\n", err)
			} else if n > 0 {
				fmt.Printf("[SYNC] Mark-and-sweep removed %d stale jobs (grace=%s)\n", n, syncSweepGrace())
			}
		} else {
			fmt.Println("[SYNC] Mark-and-sweep SKIPPED — sync did not finish (crash-safe)")
		}
		MaybeRefreshRAMCache(true)
	}()

	targets := loadVerifiedScrapeTargets()
	if len(targets) == 0 {
		extGH, extLV, extSR, extZH, extAS, extDB, extWD := loadTokensFromFile("tokens.json")
		for _, s := range append(GlobalGreenhouse, extGH...) {
			targets = append(targets, scrapeTarget{slug: s, platform: "greenhouse"})
		}
		for _, s := range append(GlobalLever, extLV...) {
			targets = append(targets, scrapeTarget{slug: s, platform: "lever"})
		}
		for _, s := range extSR {
			targets = append(targets, scrapeTarget{slug: s, platform: "smartrecruiters"})
		}
		for _, s := range extAS {
			targets = append(targets, scrapeTarget{slug: s, platform: "ashby"})
		}
		for _, s := range extZH {
			targets = append(targets, scrapeTarget{slug: s, platform: "zoho"})
		}
		for _, s := range extDB {
			targets = append(targets, scrapeTarget{slug: s, platform: "darwinbox"})
		}
		for _, s := range extWD {
			targets = append(targets, scrapeTarget{slug: s, platform: "workday"})
		}
	}

	var totalJobs int
	uniqueCompanies := make(map[string]bool)

	for i := 0; i < len(targets); i += syncBatchSize {
		end := i + syncBatchSize
		if end > len(targets) {
			end = len(targets)
		}
		batch := targets[i:end]
		gh, lv, sr, zh, as, db, wd := splitScrapeTargets(batch)
		rawJobs := scrapeJobs(gh, lv, sr, zh, as, db, wd, "")

		var dbJobs []JobRecord
		now := time.Now()
		for _, j := range rawJobs {
			if j.URL == "" {
				continue
			}
			key := strings.ToLower(j.Company)
			uniqueCompanies[key] = true
			dbJobs = append(dbJobs, JobRecord{
				URL:         j.URL,
				Title:       j.Title,
				Company:     key,
				Location:    j.Location,
				LocationTag: j.LocationTag,
				IsIndia:     j.IsIndia,
				Timestamp:   now,
			})
		}
		if err := batchUpsertJobs(dbJobs); err != nil {
			fmt.Printf("[SYNC] Batch upsert error: %v\n", err)
		}
		totalJobs += len(dbJobs)
		fmt.Printf("[SYNC] Batch %d–%d: upserted %d jobs from %d companies\n", i+1, end, len(dbJobs), len(batch))
	}

	if n, err := CleanupUnverifiedJobsWithPolicy(); err != nil {
		fmt.Printf("[SYNC] Unverified job cleanup error: %v\n", err)
	} else if n > 0 {
		fmt.Printf("[SYNC] Processed %d jobs for unverified companies (quarantine=%v)\n", n, os.Getenv("SYNC_QUARANTINE") == "1")
	}

	syncCompleted = true
	fmt.Printf("[SYNC] Complete: %d jobs upserted across %d companies.\n", totalJobs, len(uniqueCompanies))
	
	RefreshRAMCache()
}

func loadVerifiedScrapeTargets() []scrapeTarget {
	rows, err := DB.Query(`
		SELECT slug, platform FROM companies 
		WHERE platform IN ('greenhouse','lever','smartrecruiters','ashby','zoho','darwinbox','workday')
		ORDER BY (SELECT COUNT(*) FROM jobs WHERE company = companies.slug) ASC`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var targets []scrapeTarget
	for rows.Next() {
		var slug, platform string
		if err := rows.Scan(&slug, &platform); err != nil {
			continue
		}
		targets = append(targets, scrapeTarget{slug: slug, platform: platform})
	}
	return targets
}

func splitScrapeTargets(batch []scrapeTarget) (gh, lv, sr, zh, as, db, wd []string) {
	for _, t := range batch {
		switch t.platform {
		case "greenhouse":
			gh = append(gh, t.slug)
		case "lever":
			lv = append(lv, t.slug)
		case "smartrecruiters":
			sr = append(sr, t.slug)
		case "zoho":
			zh = append(zh, t.slug)
		case "ashby":
			as = append(as, t.slug)
		case "darwinbox":
			db = append(db, t.slug)
		case "workday":
			wd = append(wd, t.slug)
		}
	}
	return
}

func GetLocationContext(loc string) (string, bool) {
	if loc == "" {
		return "Global", false
	}
	lowerLoc := strings.ToLower(loc)

	// Robust word boundary check for "India" vs "Indiana"
	reIndia := regexp.MustCompile(`(?i)\bindia\b`)
	isIndiaKeyword := reIndia.MatchString(lowerLoc)

	// Filter out "Indiana" or "Indianapolis" explicitly
	if strings.Contains(lowerLoc, "indiana") || strings.Contains(lowerLoc, "indianapolis") {
		return "Global", false
	}

	// Check for International Hubs first (Explicit Exclusion)
	for _, hub := range InternationalHubKeywords {
		if strings.Contains(lowerLoc, hub) {
			return "Global", false
		}
	}

	// Check for Indian Cities
	for _, city := range IndiaCityKeywords {
		if strings.Contains(lowerLoc, city) {
			// Title case for the tag
			cityTag := strings.Title(city)
			if cityTag == "India" {
				return "India", true
			}
			return cityTag + ", India", true
		}
	}

	if isIndiaKeyword {
		return "India", true
	}

	return "Global", false
}

func searchMasterIndex(query string) []CompanyResponse {
	q := strings.ToLower(query)
	var results []CompanyResponse
	start := time.Now()

	RAMCacheMutex.RLock()
	cache := RAMCache
	RAMCacheMutex.RUnlock()

	if len(cache) == 0 {
		fmt.Printf("[PERF] RAM cache cold (%dms) — warm cache for instant search.\n", time.Since(start).Milliseconds())
		return nil
	}

	if q == "" {
		if len(cache) > 1000 {
			results = append(results, cache[:1000]...)
		} else {
			results = append(results, cache...)
		}
		return results
	}

	for _, cr := range cache {
		var matchedJobs []Job
		for _, j := range cr.Jobs {
			if jobMatchesRoleQuery(j.Title, j.Company, q) {
				matchedJobs = append(matchedJobs, j)
			}
		}

		if len(matchedJobs) > 0 {
			crCopy := cr
			crCopy.Jobs = matchedJobs
			results = append(results, crCopy)
		}
	}

	sortCompaniesByJobCount(results)

	const maxCompanies = 1000
	if len(results) > maxCompanies {
		results = results[:maxCompanies]
	}

	fmt.Printf("[PERF] RAM Cache Search: %dms | Found %d companies.\n", time.Since(start).Milliseconds(), len(results))

	return results
}

// searchIndiaJobsFromRAM filters India roles exclusively from RAM cache.
func searchIndiaJobsFromRAM(query string) []Job {
	q := strings.ToLower(strings.TrimSpace(query))
	var out []Job

	RAMCacheMutex.RLock()
	defer RAMCacheMutex.RUnlock()

	for _, cr := range RAMCache {
		for _, j := range cr.Jobs {
			if !j.IsIndia {
				continue
			}
			if q == "" || jobMatchesRoleQuery(j.Title, j.Company, q) {
				out = append(out, j)
			}
		}
	}
	if len(out) > 1000 {
		out = out[:1000]
	}
	return out
}

func asyncEnrich(query string) {
	if query == "" { return }
	
	ThirdPartyCacheMutex.RLock()
	_, exists := ThirdPartyCache[query]
	ThirdPartyCacheMutex.RUnlock()
	if exists { return }

	var wg sync.WaitGroup
	var joobleJobs []Job
	var careerjetJobs []Job

	wg.Add(2)
	go func() {
		defer wg.Done()
		joobleJobs = fetchFromJooble(query, 1)
	}()
	go func() {
		defer wg.Done()
		careerjetJobs = fetchFromCareerjet(query)
	}()
	wg.Wait()

	ThirdPartyCacheMutex.Lock()
	ThirdPartyCache[query] = append(joobleJobs, careerjetJobs...)
	ThirdPartyCacheMutex.Unlock()
}

func main() {
	godotenv.Load()
	InitDB()

	if len(os.Args) > 1 {
		if os.Args[1] == "--sync" {
			deepHunt := false
			if len(os.Args) > 2 && os.Args[2] == "--deep-hunt" {
				deepHunt = true
			}
			RunSync(deepHunt)
			return
		}
		if os.Args[1] == "--ingest-seeds" {
			deepHunt := false
			if len(os.Args) > 2 && os.Args[2] == "--deep-hunt" {
				deepHunt = true
			}
			IngestAllSeeds(deepHunt)
			return
		}
		if os.Args[1] == "--build-index" {
			syncJobsToDB()
			return
		}
		if os.Args[1] == "--reset-smartrecruiters" {
			n, err := ResetSmartRecruitersToPending()
			if err != nil {
				fmt.Printf("[MIGRATE] Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("[MIGRATE] Reset %d companies from smartrecruiters -> pending\n", n)
			return
		}
		if os.Args[1] == "--cleanup-invalid-jobs" {
			n, err := CleanupJobsForInvalidCompanies()
			if err != nil {
				fmt.Printf("[MIGRATE] Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("[MIGRATE] Removed %d jobs tied to invalid companies\n", n)
			return
		}
		if os.Args[1] == "--maintenance" {
			runDiscoveryMaintenance(true, true)
			return
		}
		if os.Args[1] == "--reset-data" {
			if err := runResetData(); err != nil {
				fmt.Printf("[RESET] Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		if os.Args[1] == "--process-pending" {
			runProcessPendingCLI()
			return
		}
	}
	fmt.Println("[STARTUP] Loading environment...")
	if os.Getenv("RUN_DISCOVERY_MAINTENANCE") == "1" {
		go runDiscoveryMaintenance(true, true)
	}
	VerifyAPIIntegrations()

	fmt.Println("[STARTUP] Configuring Fiber app...")
	app := fiber.New(fiber.Config{
		DisableStartupMessage: false,
	})
	app.Use(logger.New())

	fmt.Println("[STARTUP] Warming search index in background...")
	go func() {
		RefreshRAMCacheQuick()
		RefreshRAMCache()
	}()

	fmt.Println("[STARTUP] Starting background workers...")
	startStatsLogger()
	if os.Getenv("HUNTER_TURBO") == "1" || os.Getenv("HUNTER_TURBO") == "true" {
		applyTurboHunterDefaults()
	}
	go backgroundHunter()
	
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			fmt.Println("[INDEXER] 1-hour refresh triggered...")
			syncJobsToDB()
		}
	}()

	app.Get("/health", func(c *fiber.Ctx) error {
		var jobCount int
		DB.QueryRow("SELECT count(*) FROM jobs").Scan(&jobCount)
		return c.JSON(map[string]interface{}{
			"status":      "healthy",
			"masterJobs":  jobCount,
			"globalGH":    len(GlobalGreenhouse),
			"globalLV":    len(GlobalLever),
		})
	})

	app.Get("/api/companies", func(c *fiber.Ctx) error {
		query := strings.ToLower(strings.TrimSpace(c.Query("search", "")))

		fmt.Printf("[API] Searching RAM Cache for: %s\n", query)
		results := searchMasterIndex(query)
		
		if len(results) == 0 && len(query) > 2 {
			fmt.Printf("[API] 0 local results for '%s'. Running synchronous Serper hunt...\n", query)
			TriggerRuntimeDiscovery(query)
			results = searchMasterIndex(query)
		} else if len(query) > 2 {
			go TriggerRuntimeDiscovery(query)
		}
		
		go asyncEnrich(query)
		
		ThirdPartyCacheMutex.RLock()
		tpJobs, exists := ThirdPartyCache[query]
		ThirdPartyCacheMutex.RUnlock()
		if exists && len(tpJobs) > 0 {
			results = append(results, CompanyResponse{
				Name: "Global Partner Networks",
				IsIndian: true,
				Jobs: tpJobs,
			})
		}
		
		return c.JSON(results)
	})

	app.Get("/api/company", func(c *fiber.Ctx) error {
		name := strings.ToLower(strings.TrimSpace(c.Query("name", "")))
		searchQuery := strings.ToLower(strings.TrimSpace(c.Query("search", "")))

		if name == "" {
			return c.Status(404).JSON(map[string]string{"error": "Company not found"})
		}

		token := name
		if searchQuery == token {
			searchQuery = "" // Clear search if identical to company name
		}

		// Fetch directly from RAM Cache
		var results []Job
		matchesQuery := func(j Job) bool {
			if searchQuery == "" {
				return true
			}
			return jobMatchesRoleQuery(j.Title, j.Company, searchQuery)
		}

		fmt.Printf("[API] Checking RAM for %s (query: %s)...\n", token, searchQuery)
		
		RAMCacheMutex.RLock()
		var companyJobs []Job
		for _, cr := range RAMCache {
			if strings.ToLower(cr.Name) == token || strings.ToLower(strings.ReplaceAll(cr.Name, " ", "-")) == token {
				companyJobs = cr.Jobs
				break
			}
		}
		RAMCacheMutex.RUnlock()
		
		for _, j := range companyJobs {
			if matchesQuery(j) {
				results = append(results, j)
			}
		}

		fmt.Printf("[API] RAM found %d jobs for %s\n", len(results), token)

		if len(results) == 0 {
			// fallback check to make sure it exists
			var count int
			DB.QueryRow("SELECT count(*) FROM companies WHERE slug = LOWER(?)", token).Scan(&count)
			if count == 0 {
				return c.Status(404).JSON(map[string]string{"error": "Company not found"})
			}
		}

		// Dynamic DetectOrigin for specific company view
		isIndian := false
		for _, j := range results {
			if j.IsIndia {
				isIndian = true
				break
			}
		}

		return c.JSON(CompanyResponse{
			Name:     token,
			IsIndian: isIndian,
			Jobs:     results,
		})
	})

	// SPA fallback for Next static export (company pages, client routing)
	app.Get("/company/*", func(c *fiber.Ctx) error {
		if _, err := os.Stat("./frontend/company.html"); err == nil {
			return c.SendFile("./frontend/company.html")
		}
		return c.SendFile("./frontend/details.html")
	})

	// Cache-Control middleware to prevent aggressive browser/CDNs caching of static assets
	app.Use(func(c *fiber.Ctx) error {
		path := c.Path()
		if !strings.HasPrefix(path, "/api") && !strings.HasPrefix(path, "/health") {
			c.Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate, max-age=0")
			c.Set("Pragma", "no-cache")
			c.Set("Expires", "0")
		}
		return c.Next()
	})

	app.Static("/", "./frontend", fiber.Static{
		Index:  "index.html",
		Browse: false,
	})

	app.Use(func(c *fiber.Ctx) error {
		path := c.Path()
		if strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/health") {
			return c.Next()
		}
		if strings.Contains(path, ".") {
			return c.Next()
		}
		if strings.HasPrefix(path, "/company") {
			if _, err := os.Stat("./frontend/company.html"); err == nil {
				return c.SendFile("./frontend/company.html")
			}
			return c.SendFile("./frontend/details.html")
		}
		return c.SendFile("./frontend/index.html")
	})

	app.Get("/api/serper-stats", func(c *fiber.Ctx) error {
		SerperStatsMu.Lock()
		defer SerperStatsMu.Unlock()
		
		queries := make(map[string]map[string]interface{})
		for q, compCount := range SerperQueryCompanyCounts {
			jobCount := SerperQueryJobCounts[q]
			queries[q] = map[string]interface{}{
				"companies": compCount,
				"jobs":      jobCount,
			}
		}
		
		return c.JSON(map[string]interface{}{
			"total_companies": SerperDiscoveredCompaniesCount,
			"total_jobs":      SerperDiscoveredJobsCount,
			"queries":          queries,
		})
	})

	app.Get("/api/india-jobs", func(c *fiber.Ctx) error {
		query := strings.ToLower(c.Query("query", ""))

		fmt.Printf("[API] Searching RAM Cache (India only) for: %s\n", query)
		finalResults := searchIndiaJobsFromRAM(query)

		go asyncEnrich(query)
		
		ThirdPartyCacheMutex.RLock()
		tpJobs, exists := ThirdPartyCache[query]
		ThirdPartyCacheMutex.RUnlock()
		if exists && len(tpJobs) > 0 {
			for _, j := range tpJobs {
				if j.IsIndia {
					finalResults = append(finalResults, j)
				}
			}
		}
		
		// Limit results to avoid massive payload
		if len(finalResults) > 1000 {
			finalResults = finalResults[:1000]
		}
		
		return c.JSON(finalResults)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	fmt.Printf("[STARTUP] Starting Fiber server on :%s...\n", port)
	if err := app.Listen(":" + port); err != nil {
		fmt.Printf("[CRITICAL] Failed to start server: %v\n", err)
		fmt.Println("[CRITICAL] Port likely in use.")
		os.Exit(1)
	}
}

func scrapeJobs(ghTokens, lvTokens, srTokens, zhTokens, asTokens, dbTokens, wdTokens []string, query string) []Job {
	var results []Job
	var mu sync.Mutex
	var wg sync.WaitGroup

	// 50 workers pool
	sem := make(chan struct{}, 50)

	processTokens := func(tokens []string, platform string, fetcher func(string)) {
		for _, t := range tokens {
			// Skip companies with >3 failures
			FailureMutex.Lock()
			count := FailureCounts[t]
			FailureMutex.Unlock()
			if count > 3 {
				fmt.Printf("[%s] Skipping %s due to excessive failures (%d)\n", platform, t, count)
				continue
			}

			wg.Add(1)
			go fetcher(t)
		}
	}

	trackFailure := func(token string) {
		FailureMutex.Lock()
		FailureCounts[token]++
		FailureMutex.Unlock()
	}

	resetFailure := func(token string) {
		FailureMutex.Lock()
		FailureCounts[token] = 0
		FailureMutex.Unlock()
	}

	// Greenhouse
	processTokens(ghTokens, "GREENHOUSE", func(token string) {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()

		url := fmt.Sprintf("https://boards-api.greenhouse.io/v1/boards/%s/jobs", token)
		resp, err := scraperGET(url)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			trackFailure(token)
			return
		}
		defer resp.Body.Close()

		var data struct {
			Jobs []struct {
				Title    string `json:"title"`
				URL      string `json:"absolute_url"`
				Location struct {
					Name string `json:"name"`
				} `json:"location"`
			} `json:"jobs"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			trackFailure(token)
			return
		}

		resetFailure(token)
		lowerQuery := strings.ToLower(query)
		count := 0
		indiaCount := 0
		for _, j := range data.Jobs {
			if j.URL == "" {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(j.Title), lowerQuery) {
				continue
			}

			tag, isIndia := GetLocationContext(j.Location.Name)
			if isIndia {
				indiaCount++
			}
			count++
			mu.Lock()
			results = append(results, Job{
				Title:       j.Title,
				Company:     strings.Title(token),
				Location:    j.Location.Name,
				LocationTag: tag,
				URL:         j.URL,
				IsIndia:     isIndia,
			})
			mu.Unlock()
		}
		if count > 0 {
			fmt.Printf("[SCRAPE] Scraping %s... Found %d jobs. %d tagged as India.\n", strings.Title(token), count, indiaCount)
		}
	})

	// Lever
	processTokens(lvTokens, "LEVER", func(token string) {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()

		url := fmt.Sprintf("https://api.lever.co/v0/postings/%s", token)
		resp, err := scraperGET(url)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			trackFailure(token)
			return
		}
		defer resp.Body.Close()

		var data []struct {
			Title      string `json:"text"`
			URL        string `json:"hostedUrl"`
			Categories struct {
				Location string `json:"location"`
			} `json:"categories"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			trackFailure(token)
			return
		}

		resetFailure(token)
		lowerQuery := strings.ToLower(query)
		count := 0
		indiaCount := 0
		for _, j := range data {
			if query != "" && !strings.Contains(strings.ToLower(j.Title), lowerQuery) {
				continue
			}

			tag, isIndia := GetLocationContext(j.Categories.Location)
			if isIndia {
				indiaCount++
			}
			count++
			mu.Lock()
			results = append(results, Job{
				Title:       j.Title,
				Company:     strings.Title(token),
				Location:    j.Categories.Location,
				LocationTag: tag,
				URL:         j.URL,
				IsIndia:     isIndia,
			})
			mu.Unlock()
		}
		if count > 0 {
			fmt.Printf("[SCRAPE] Scraping %s... Found %d jobs. %d tagged as India.\n", strings.Title(token), count, indiaCount)
		}
	})

	// SmartRecruiters
	processTokens(srTokens, "SMARTRECRUITERS", func(token string) {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()

		url := fmt.Sprintf("https://api.smartrecruiters.com/v1/companies/%s/postings", token)
		resp, err := scraperGET(url)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			trackFailure(token)
			return
		}
		defer resp.Body.Close()

		var data struct {
			Content []struct {
				Name     string `json:"name"`
				Location struct {
					City    string `json:"city"`
					Country string `json:"country"`
				} `json:"location"`
				UUID string `json:"uuid"`
			} `json:"content"`
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		if err != nil || !verifySmartRecruitersBody(body) {
			trackFailure(token)
			return
		}
		if err := json.Unmarshal(body, &data); err != nil {
			trackFailure(token)
			return
		}

		resetFailure(token)
		lowerQuery := strings.ToLower(query)
		count := 0
		indiaCount := 0
		for _, j := range data.Content {
			if query != "" && !strings.Contains(strings.ToLower(j.Name), lowerQuery) {
				continue
			}

			locStr := j.Location.City + ", " + j.Location.Country
			tag, isIndia := GetLocationContext(locStr)
			if isIndia {
				indiaCount++
			}
			count++
			mu.Lock()
			results = append(results, Job{
				Title:       j.Name,
				Company:     strings.Title(token),
				Location:    locStr,
				LocationTag: tag,
				URL:         fmt.Sprintf("https://jobs.smartrecruiters.com/%s/%s", token, j.UUID),
				IsIndia:     isIndia,
			})
			mu.Unlock()
		}
		if count > 0 {
			fmt.Printf("[SCRAPE] Scraping %s (SmartRecruiters)... Found %d jobs. %d tagged as India.\n", strings.Title(token), count, indiaCount)
		}
	})

	// Zoho
	processTokens(zhTokens, "ZOHO", func(token string) {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()

		url := fmt.Sprintf("https://recruit.zoho.in/recruit/v2/public/Job_Openings?digest=%s", token)
		resp, err := scraperGET(url)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			trackFailure(token)
			return
		}
		defer resp.Body.Close()

		var data struct {
			Data []map[string]interface{} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			trackFailure(token)
			return
		}

		resetFailure(token)
		lowerQuery := strings.ToLower(query)
		count := 0
		indiaCount := 0
		for _, j := range data.Data {
			title, _ := j["Job_Title"].(string)
			id, _ := j["id"].(string)
			city, _ := j["City"].(string)
			if title == "" || id == "" {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(title), lowerQuery) {
				continue
			}

			applyURL := fmt.Sprintf("https://recruit.zoho.in/recruit/Portal.na?digest=%s&iframe=false&jobid=%s", token, id)
			tag, isIndia := GetLocationContext(city)
			if isIndia {
				indiaCount++
			}
			count++
			mu.Lock()
			results = append(results, Job{
				Title:       title,
				Company:     strings.Title(token),
				Location:    city,
				LocationTag: tag,
				URL:         applyURL,
				IsIndia:     isIndia,
			})
			mu.Unlock()
		}
		if count > 0 {
			fmt.Printf("[SCRAPE] Scraping %s... Found %d jobs. %d tagged as India.\n", strings.Title(token), count, indiaCount)
		}
	})

	// Ashby
	processTokens(asTokens, "ASHBY", func(token string) {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()

		url := fmt.Sprintf("https://api.ashbyhq.com/v1/jobBoard/%s/jobs", token)
		resp, err := scraperGET(url)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			trackFailure(token)
			return
		}
		defer resp.Body.Close()

		var data struct {
			Jobs []struct {
				Title    string `json:"jobTitle"`
				Location string `json:"location"`
				JobURL   string `json:"jobUrl"`
			} `json:"jobs"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			trackFailure(token)
			return
		}

		resetFailure(token)
		lowerQuery := strings.ToLower(query)
		count := 0
		indiaCount := 0
		for _, j := range data.Jobs {
			if j.JobURL == "" {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(j.Title), lowerQuery) {
				continue
			}

			tag, isIndia := GetLocationContext(j.Location)
			if isIndia {
				indiaCount++
			}
			count++
			mu.Lock()
			results = append(results, Job{
				Title:       j.Title,
				Company:     strings.Title(token),
				Location:    j.Location,
				LocationTag: tag,
				URL:         j.JobURL,
				IsIndia:     isIndia,
			})
			mu.Unlock()
		}
		if count > 0 {
			fmt.Printf("[SCRAPE] Scraping %s... Found %d jobs. %d tagged as India.\n", strings.Title(token), count, indiaCount)
		}
	})

	// Darwinbox is currently a no-op / disabled

	// Workday
	processTokens(wdTokens, "WORKDAY", func(token string) {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()
		wJobs := scrapeWorkdayJobs(token, query)
		mu.Lock()
		results = append(results, wJobs...)
		mu.Unlock()
	})

	wg.Wait()
	return results
}

func loadTokensFromFile(path string) ([]string, []string, []string, []string, []string, []string, []string) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil
	}
	var data struct {
		Greenhouse      []string `json:"greenhouse"`
		Lever           []string `json:"lever"`
		SmartRecruiters []string `json:"smartrecruiters"`
		Zoho            []string `json:"zoho"`
		Ashby           []string `json:"ashby"`
		Darwinbox       []string `json:"darwinbox"`
		Workday         []string `json:"workday"`
	}
	json.Unmarshal(file, &data)
	return data.Greenhouse, data.Lever, data.SmartRecruiters, data.Zoho, data.Ashby, data.Darwinbox, data.Workday
}

func scrapeWorkdayJobs(token string, query string) []Job {
	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	parts := strings.Split(token, "/")
	hostPart := parts[0]
	siteID := "External"
	if len(parts) > 1 {
		siteID = parts[1]
	}

	tenant := hostPart
	if idx := strings.Index(hostPart, "."); idx > 0 {
		tenant = hostPart[:idx]
	}

	if !strings.Contains(hostPart, ".") {
		hostPart = hostPart + ".wd5"
	}

	url := fmt.Sprintf("https://%s.myworkdayjobs.com/wday/cxs/%s/%s/jobs", hostPart, tenant, siteID)
	payload := map[string]interface{}{
		"appliedFacets": map[string]interface{}{},
		"limit":         20,
		"offset":        0,
		"searchText":    query,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", url, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		FailureMutex.Lock()
		FailureCounts[token]++
		FailureMutex.Unlock()
		return nil
	}
	defer resp.Body.Close()

	FailureMutex.Lock()
	FailureCounts[token] = 0
	FailureMutex.Unlock()

	var data struct {
		JobPostings []struct {
			Title         string `json:"title"`
			ExternalPath  string `json:"externalPath"`
			LocationsText string `json:"locationsText"`
		} `json:"jobPostings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil
	}

	var results []Job
	count := 0
	indiaCount := 0
	for _, j := range data.JobPostings {
		if j.ExternalPath == "" {
			continue
		}
		tag, isIndia := GetLocationContext(j.LocationsText)
		if isIndia {
			indiaCount++
		}
		count++
		results = append(results, Job{
			Title:       j.Title,
			Company:     strings.Title(tenant),
			Location:    j.LocationsText,
			LocationTag: tag,
			URL:         fmt.Sprintf("https://%s.myworkdayjobs.com/en-US/%s%s", hostPart, siteID, j.ExternalPath),
			IsIndia:     isIndia,
		})
	}
	if count > 0 {
		fmt.Printf("[SCRAPE] Scraping %s (Workday)... Found %d jobs. %d tagged as India.\n", strings.Title(tenant), count, indiaCount)
	}
	return results
}
