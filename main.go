package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/joho/godotenv"
)

var (
	// Pre-Scraped Master Index
	MasterJobList []Job
	MasterMutex   sync.RWMutex
	MasterFile    = "master_jobs.json"
	MasterReady   = false

	// Failure Tracking
	FailureCounts = make(map[string]int)
	FailureMutex  sync.Mutex
)

// loadMasterIndex loads a previously saved master index from disk
func loadMasterIndex() {
	file, err := os.ReadFile(MasterFile)
	if err != nil {
		fmt.Println("[MASTER] No existing master_jobs.json found, will build fresh")
		return
	}
	var idx MasterIndex
	if err := json.Unmarshal(file, &idx); err != nil {
		return
	}
	fmt.Printf("[DEBUG] Loading index from disk: %d jobs found. Delete master_jobs.json to force re-scrape.\n", len(idx.Jobs))
	// Only use if less than 6 hours old
	if time.Since(idx.Timestamp) < 6*time.Hour {
		MasterMutex.Lock()
		MasterJobList = idx.Jobs
		MasterReady = true
		MasterMutex.Unlock()
		fmt.Printf("[MASTER] Loaded %d jobs from disk (age: %s)\n", len(idx.Jobs), time.Since(idx.Timestamp).Round(time.Minute))
	}
}

// saveMasterIndex persists the master list to disk
func saveMasterIndex() {
	MasterMutex.RLock()
	idx := MasterIndex{Jobs: MasterJobList, Timestamp: time.Now()}
	MasterMutex.RUnlock()
	data, _ := json.Marshal(idx)
	os.WriteFile(MasterFile, data, 0644)
	fmt.Printf("[MASTER] Saved %d jobs to %s\n", len(idx.Jobs), MasterFile)
}

// buildMasterIndex scrapes ALL tokens and populates MasterJobList
func buildMasterIndex() {
	fmt.Println("[MASTER] ══════════════════════════════════════════════")
	fmt.Println("[MASTER]  Building Master Index — scraping all tokens")
	fmt.Println("[MASTER] ══════════════════════════════════════════════")
	start := time.Now()

	extGH, extLV, extZH, extAS, extDB, extWD := loadTokensFromFile("tokens.json")
	allGH := append(GlobalGreenhouse, extGH...)
	allLV := append(GlobalLever, extLV...)

	// Scrape with a broad empty query to get ALL jobs
	allJobs := scrapeJobs(allGH, allLV, extZH, extAS, extDB, extWD, "")

	MasterMutex.Lock()
	MasterJobList = allJobs
	MasterReady = true
	MasterMutex.Unlock()

	saveMasterIndex()
	fmt.Printf("[MASTER] ✅ Index built: %d jobs from %d tokens in %s\n",
		len(allJobs), len(allGH)+len(allLV)+len(extZH)+len(extAS)+len(extDB)+len(extWD),
		time.Since(start).Round(time.Millisecond))
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

// expandSearchTerms returns a list of synonym terms for tech stacks
func expandSearchTerms(query string) []string {
	switch query {
	case "mern":
		return []string{"react", "node", "express", "mongodb", "fullstack", "mern", "javascript"}
	case "golang", "go":
		return []string{"golang", " go ", "backend", "developer", "engineer", "api", "microservice"}
	case "javascript", "js":
		return []string{"javascript", "typescript", "node", "react", "vue", "angular", "nextjs", "express", "frontend", "fullstack", "js"}
	case "react", "reactjs":
		return []string{"react", "reactjs", "nextjs", "frontend", "javascript", "typescript", "redux", "ui engineer"}
	case "mongodb", "mongo":
		return []string{"mongodb", "mongo", "nosql", "database", "backend", "node", "express"}
	case "java":
		return []string{"java", "spring", "springboot", "jvm", "backend", "microservice", "kubernetes", "kafka"}
	case "c++", "cpp":
		return []string{"c++", "cpp", "systems", "embedded", "graphics", "game", "performance", "low-latency", "hft"}
	case "frontend", "front-end":
		return []string{"frontend", "front-end", "react", "vue", "angular", "javascript", "typescript", "ui", "css", "html", "nextjs"}
	case "backend", "back-end":
		return []string{"backend", "back-end", "api", "node", "java", "python", "golang", "ruby", "microservice", "server", "rest", "graphql"}
	case "engineer", "sde", "software", "developer", "dev":
		return []string{"engineer", "sde", "software", "developer", "dev", "backend", "frontend", "fullstack", "systems", "platform", "infrastructure", "java", "python", "react", "node", "golang", "typescript", "c++", "ruby", "rust", "php", "mobile", "ios", "android", "cloud", "devops", "qa", "test", "security", "data", "ml", "ai"}
	default:
		return []string{query}
	}
}

// searchMasterIndex filters the MasterJobList in-memory (instant)
func searchMasterIndex(query string) []CompanyResponse {
	MasterMutex.RLock()
	jobs := MasterJobList
	MasterMutex.RUnlock()

	q := strings.ToLower(query)
	
	// Tech-Stack Search Expansion
	searchTerms := expandSearchTerms(q)

	// Weight-based tracking
	companyMap := make(map[string]*struct {
		Response *CompanyResponse
		Score    int
	})
	var indiaCount, globalCount int

	for _, job := range jobs {
		lowerTitle := strings.ToLower(job.Title)
		lowerCompany := strings.ToLower(job.Company)
		score := 0

		// Weight-Based Matching
		if q == "" {
			score = 1
		} else {
			// Exact match (10 points)
			if strings.Contains(lowerTitle, q) || strings.Contains(lowerCompany, q) {
				score = 10
			} else {
				// Synonym match (5 points)
				for _, term := range searchTerms {
					if strings.Contains(lowerTitle, term) || strings.Contains(lowerCompany, term) {
						score = 5
						break
					}
				}
			}
		}

		if score > 0 {
			if _, exists := companyMap[job.Company]; !exists {
				companyMap[job.Company] = &struct {
					Response *CompanyResponse
					Score    int
				}{
					Response: &CompanyResponse{
						Name:     job.Company,
						IsIndian: false,
						Jobs:     []Job{},
					},
					Score: score,
				}
			}
			data := companyMap[job.Company]
			data.Response.Jobs = append(data.Response.Jobs, job)
			
			// Update company-level score if higher
			if score > data.Score {
				data.Score = score
			}

			if job.IsIndia {
				data.Response.IsIndian = true
			}
		}
	}

	// Final pass to build and sort results
	type scoredCompany struct {
		Response CompanyResponse
		Score    int
	}
	var scoredList []scoredCompany

	for _, data := range companyMap {
		comp := data.Response
		if comp.IsIndian {
			indiaCount += len(comp.Jobs)
		} else {
			globalCount += len(comp.Jobs)
		}
		
		// Internal Job Sorting: India-based positions first
		sort.Slice(comp.Jobs, func(i, j int) bool {
			if comp.Jobs[i].IsIndia != comp.Jobs[j].IsIndia {
				return comp.Jobs[i].IsIndia // True (India) comes first
			}
			return false
		})

		// Truncate to top 8 (though frontend only shows 3)
		if len(comp.Jobs) > 8 {
			comp.Jobs = comp.Jobs[:8]
		}
		scoredList = append(scoredList, scoredCompany{Response: *comp, Score: data.Score})
	}

	// Global Sorting: IsIndian > Match Score > Job Count
	sort.Slice(scoredList, func(i, j int) bool {
		if scoredList[i].Response.IsIndian != scoredList[j].Response.IsIndian {
			return scoredList[i].Response.IsIndian
		}
		if scoredList[i].Score != scoredList[j].Score {
			return scoredList[i].Score > scoredList[j].Score
		}
		return len(scoredList[i].Response.Jobs) > len(scoredList[j].Response.Jobs)
	})

	var results []CompanyResponse
	for _, sc := range scoredList {
		results = append(results, sc.Response)
	}

	if q != "" {
		fmt.Printf("[SEARCH] Query: \"%s\" | Found: %d Indian Jobs, %d Global Jobs\n", query, indiaCount, globalCount)
	}

	return results
}

func main() {
	if len(os.Args) > 1 {
		if os.Args[1] == "--sync" {
			RunSync()
			return
		}
		if os.Args[1] == "--build-index" {
			buildMasterIndex()
			return
		}
	}
	fmt.Println("[STARTUP] Loading environment and master index...")
	godotenv.Load()
	loadMasterIndex()

	fmt.Println("[STARTUP] Configuring Fiber app...")
	app := fiber.New(fiber.Config{
		DisableStartupMessage: false,
	})
	app.Use(logger.New())

	fmt.Println("[STARTUP] Starting background master indexer...")
	// Build master index immediately on startup (in background)
	go func() {
		if !MasterReady {
			fmt.Println("[INDEXER] Master not ready, starting build...")
			buildMasterIndex()
		}
		// Then refresh every 6 hours
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			fmt.Println("[INDEXER] 6-hour refresh triggered...")
			buildMasterIndex()
		}
	}()

	app.Get("/health", func(c *fiber.Ctx) error {
		MasterMutex.RLock()
		count := len(MasterJobList)
		ready := MasterReady
		MasterMutex.RUnlock()
		return c.JSON(map[string]interface{}{
			"status":      "healthy",
			"masterJobs":  count,
			"masterReady": ready,
			"globalGH":    len(GlobalGreenhouse),
			"globalLV":    len(GlobalLever),
		})
	})

	app.Get("/api/companies", func(c *fiber.Ctx) error {
		query := strings.ToLower(c.Query("search", ""))

		// If master index is ready, use it for INSTANT results
		MasterMutex.RLock()
		ready := MasterReady
		MasterMutex.RUnlock()

		if ready {
			fmt.Printf("[API] Instant search from MasterIndex for: %s\n", query)
			results := searchMasterIndex(query)
			return c.JSON(results)
		}

		// Master not ready fallback: return error instead of live scraping
		fmt.Printf("[API] Master not ready, rejecting live scrape for: %s\n", query)
		return c.Status(http.StatusServiceUnavailable).JSON(map[string]string{
			"error": "System Initializing - Master Index is actively building. Please try again soon.",
		})
	})

	app.Get("/api/company", func(c *fiber.Ctx) error {
		name := strings.ToLower(c.Query("name", ""))
		searchQuery := strings.ToLower(c.Query("search", ""))

		token := ""
		isGreenhouse, isLever, isZoho, isAshby, isDarwinbox, isWorkday := false, false, false, false, false, false
		extGH, extLV, extZH, extAS, extDB, extWD := loadTokensFromFile("tokens.json")

		allGH := append(GlobalGreenhouse, extGH...)
		for _, t := range allGH {
			if strings.Contains(strings.ToLower(t), name) {
				token = t
				isGreenhouse = true
				break
			}
		}
		if token == "" {
			allLV := append(GlobalLever, extLV...)
			for _, t := range allLV {
				if strings.Contains(strings.ToLower(t), name) {
					token = t
					isLever = true
					break
				}
			}
		}
		if token == "" {
			for _, t := range extZH {
				if strings.Contains(strings.ToLower(t), name) {
					token = t
					isZoho = true
					break
				}
			}
		}
		if token == "" {
			for _, t := range extAS {
				if strings.Contains(strings.ToLower(t), name) {
					token = t
					isAshby = true
					break
				}
			}
		}
		if token == "" {
			for _, t := range extDB {
				if strings.Contains(strings.ToLower(t), name) {
					token = t
					isDarwinbox = true
					break
				}
			}
		}
		if token == "" {
			for _, t := range extWD {
				if strings.Contains(strings.ToLower(t), name) {
					token = t
					isWorkday = true
					break
				}
			}
		}

		if token == "" {
			return c.Status(404).JSON(map[string]string{"error": "Company not found"})
		}

		var ghTokens, lvTokens, zhTokens, asTokens, dbTokens, wdTokens []string
		if isGreenhouse {
			ghTokens = []string{token}
		} else if isLever {
			lvTokens = []string{token}
		} else if isZoho {
			zhTokens = []string{token}
		} else if isAshby {
			asTokens = []string{token}
		} else if isDarwinbox {
			dbTokens = []string{token}
		} else if isWorkday {
			wdTokens = []string{token}
		}
		// Declare results slice
		var results []Job
		// Build synonym search terms (same logic as searchMasterIndex)
		expandedTerms := expandSearchTerms(searchQuery)


		matchesQuery := func(j Job) bool {
			if searchQuery == "" {
				return true
			}
			lowerTitle := strings.ToLower(j.Title)
			for _, term := range expandedTerms {
				if strings.Contains(lowerTitle, term) {
					return true
				}
			}
			return false
		}

		// Strategy 1: Check Master Index first (instant, synonym-aware)
		fmt.Printf("[API] Checking MasterIndex for %s (query: %s)...\n", token, searchQuery)
		MasterMutex.RLock()
		for _, j := range MasterJobList {
			if strings.EqualFold(j.Company, token) || strings.EqualFold(j.Company, strings.Title(token)) {
				if matchesQuery(j) {
					results = append(results, j)
				}
			}
		}
		MasterMutex.RUnlock()
		fmt.Printf("[API] MasterIndex found %d jobs for %s\n", len(results), token)

		// Strategy 2: Live scrape with EMPTY query (get all), then filter client-side
		if len(results) == 0 {
			fmt.Printf("[API] MasterIndex miss, falling back to live scrape for: %s\n", token)
			rawResults := scrapeJobs(ghTokens, lvTokens, zhTokens, asTokens, dbTokens, wdTokens, "")
			for _, j := range rawResults {
				if matchesQuery(j) {
					results = append(results, j)
				}
			}
			fmt.Printf("[API] Live scrape found %d matching jobs for %s\n", len(results), token)
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

	app.Get("/company/:name", func(c *fiber.Ctx) error {
		return c.SendFile("./frontend/details.html")
	})

	app.Static("/", "./frontend")

	app.Use(func(c *fiber.Ctx) error {
		if strings.HasPrefix(c.Path(), "/company") {
			return c.SendFile("./frontend/details.html")
		}
		return c.Next()
	})

	app.Get("/api/india-jobs", func(c *fiber.Ctx) error {
		query := strings.ToLower(c.Query("query", ""))
		ghTokensStr := c.Query("ghTokens", "")
		lvTokensStr := c.Query("lvTokens", "")
		zhTokensStr := c.Query("zhTokens", "")
		asTokensStr := c.Query("asTokens", "")
		dbTokensStr := c.Query("dbTokens", "")
		wdTokensStr := c.Query("wdTokens", "")
		var ghTokens, lvTokens, zhTokens, asTokens, dbTokens, wdTokens []string
		if ghTokensStr != "" {
			ghTokens = strings.Split(ghTokensStr, ",")
		}
		if lvTokensStr != "" {
			lvTokens = strings.Split(lvTokensStr, ",")
		}
		if zhTokensStr != "" {
			zhTokens = strings.Split(zhTokensStr, ",")
		}
		if asTokensStr != "" {
			asTokens = strings.Split(asTokensStr, ",")
		}
		if dbTokensStr != "" {
			dbTokens = strings.Split(dbTokensStr, ",")
		}
		if wdTokensStr != "" {
			wdTokens = strings.Split(wdTokensStr, ",")
		}

		if len(ghTokens) == 0 && len(lvTokens) == 0 && len(zhTokens) == 0 && len(asTokens) == 0 && len(dbTokens) == 0 && len(wdTokens) == 0 {
			extGH, extLV, extZH, extAS, extDB, extWD := loadTokensFromFile("tokens.json")
			ghTokens = append(GlobalGreenhouse, extGH...)
			lvTokens = append(GlobalLever, extLV...)
			zhTokens, asTokens, dbTokens, wdTokens = extZH, extAS, extDB, extWD
			results := scrapeJobs(ghTokens, lvTokens, zhTokens, asTokens, dbTokens, wdTokens, query)
			
			// Group by company and filter out zero-result companies
			companyMap := make(map[string][]Job)
			for _, j := range results {
				companyMap[j.Company] = append(companyMap[j.Company], j)
			}
			
			var finalResults []Job
			for _, jobs := range companyMap {
				if len(jobs) > 0 {
					finalResults = append(finalResults, jobs...)
				}
			}
			return c.JSON(finalResults)
		}
		results := scrapeJobs(ghTokens, lvTokens, zhTokens, asTokens, dbTokens, wdTokens, query)
		
		// Filter out zero-result companies (if grouped)
		// For this specific call, results are usually already filtered by scrapeJobs
		return c.JSON(results)
	})

	fmt.Println("[STARTUP] Starting Fiber server on :3000...")
	if err := app.Listen(":3000"); err != nil {
		fmt.Printf("[CRITICAL] Failed to start server: %v\n", err)
		fmt.Println("[CRITICAL] Port 3000 likely in use. Kill with: taskkill /f /im go.exe")
		os.Exit(1)
	}
}

func scrapeJobs(ghTokens, lvTokens, zhTokens, asTokens, dbTokens, wdTokens []string, query string) []Job {
	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	var results []Job
	var mu sync.Mutex
	var wg sync.WaitGroup
	client := &http.Client{Timeout: 12 * time.Second}

	// 15 workers pool
	sem := make(chan struct{}, 15)

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
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("User-Agent", userAgent)

		resp, err := client.Do(req)
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
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("User-Agent", userAgent)

		resp, err := client.Do(req)
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

	// Zoho
	processTokens(zhTokens, "ZOHO", func(token string) {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()

		url := fmt.Sprintf("https://recruit.zoho.in/recruit/v2/public/Job_Openings?digest=%s", token)
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("User-Agent", userAgent)

		resp, err := client.Do(req)
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
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("User-Agent", userAgent)

		resp, err := client.Do(req)
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

func loadTokensFromFile(path string) ([]string, []string, []string, []string, []string, []string) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, nil, nil, nil
	}
	var data struct {
		Greenhouse []string `json:"greenhouse"`
		Lever      []string `json:"lever"`
		Zoho       []string `json:"zoho"`
		Ashby      []string `json:"ashby"`
		Darwinbox  []string `json:"darwinbox"`
		Workday    []string `json:"workday"`
	}
	json.Unmarshal(file, &data)
	return data.Greenhouse, data.Lever, data.Zoho, data.Ashby, data.Darwinbox, data.Workday
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
