package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type TokenData struct {
	Greenhouse      []string `json:"greenhouse"`
	Lever           []string `json:"lever"`
	SmartRecruiters []string `json:"smartrecruiters"`
	Zoho            []string `json:"zoho"`
	Ashby           []string `json:"ashby"`
	Darwinbox       []string `json:"darwinbox"`
	Workday         []string `json:"workday"`
	Indian          []string `json:"indian"`
	IndianDiscovered []string `json:"indian_discovered"`
}

// RunSync is the main entry point for the discovery engine
func RunSync(deepHunt bool) {
	fmt.Println("[SYNC] ══════════════════════════════════════════════")
	fmt.Println("[SYNC]  Discovery Engine: Scaling to 5,000+ Companies")
	fmt.Println("[SYNC] ══════════════════════════════════════════════")
	
	start := time.Now()

	// Load existing tokens
	var tokens TokenData
	file, err := os.ReadFile("tokens.json")
	if err != nil {
		fmt.Printf("[SYNC] tokens.json not found. Initializing a fresh database.\n")
	} else {
		json.Unmarshal(file, &tokens)
	}

	preCount := len(tokens.Greenhouse) + len(tokens.Lever) + len(tokens.SmartRecruiters)

	// Phase 0: Community Import - Load into memory array directly
	fetchGithubTokens(&tokens)

	// Result map for discovery (Company Slug -> IsIndiaHiring)
	discovered := make(map[string]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var validatingCount uint32
	sem := make(chan struct{}, 15) // 15 concurrent workers
	client := &http.Client{Timeout: 10 * time.Second}

	// Slug variations helper
	getVariations := func(base string) []string {
		base = strings.ToLower(strings.ReplaceAll(base, " ", ""))
		return []string{
			base,
			base + "-india",
			base + "-tech",
			base + "india",
			base + "tech",
			base + "global",
			base + "in",
		}
	}

	// 0.5 SERPER HUNTING PHASE
	newSlugs := huntNewSlugsWithSerper(&tokens)
	IndianStartups = append(IndianStartups, newSlugs...)

	// 0.6 THEIR STACK COMBING PHASE
	fmt.Println("[SYNC] Phase 0.6: Scraping TheirStack for ATS metadata...")
	tsGH := scrapeTheirStack("greenhouse")
	tsLV := scrapeTheirStack("lever")
	IndianStartups = append(IndianStartups, tsGH...)
	IndianStartups = append(IndianStartups, tsLV...)

	if deepHunt {
		fmt.Println("[SYNC] Phase 0.7: Deep Hunt enabled. Streaming 450k records...")
		csvBrands := processMassiveCSV(true)
		IndianStartups = append(IndianStartups, csvBrands...)
	}

	IndianStartups = uniqueStrings(IndianStartups)

	// 1. DISCOVERY PHASE: Brute-force Indian Startup slugs
	fmt.Println("[SYNC] Phase 1: Brute-forcing slugs for Indian Startups...")
	for _, base := range IndianStartups {
		for _, slug := range getVariations(base) {
			// Skip if already a known token
			isKnown := false
			for _, t := range tokens.Greenhouse { if t == slug { isKnown = true; break } }
			for _, t := range tokens.Lever { if t == slug { isKnown = true; break } }
			if isKnown { continue }

			wg.Add(1)
			go func(s string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				// Test Greenhouse
				ghUrl := fmt.Sprintf("https://boards-api.greenhouse.io/v1/boards/%s/jobs", s)
				resp, err := client.Get(ghUrl)
				if err == nil && resp.StatusCode == 200 {
					defer resp.Body.Close()
					mu.Lock()
					discovered[s] = true
					tokens.Greenhouse = append(tokens.Greenhouse, s)
					mu.Unlock()

					atomic.AddUint32(&validatingCount, 1)
					fmt.Printf("[SYNC] Validating slug %d/3000... (Greenhouse)\n", atomic.LoadUint32(&validatingCount))
					return
				}

				// Test Lever
				lvUrl := fmt.Sprintf("https://api.lever.co/v0/postings/%s", s)
				resp, err = client.Get(lvUrl)
				if err == nil && resp.StatusCode == 200 {
					defer resp.Body.Close()
					mu.Lock()
					discovered[s] = true
					tokens.Lever = append(tokens.Lever, s)
					mu.Unlock()

					atomic.AddUint32(&validatingCount, 1)
					fmt.Printf("[SYNC] Validating slug %d/3000... (Lever)\n", atomic.LoadUint32(&validatingCount))
					return
				}

				// Test SmartRecruiters
				srUrl := fmt.Sprintf("https://api.smartrecruiters.com/v1/companies/%s/postings", s)
				resp, err = client.Get(srUrl)
				if err == nil && resp.StatusCode == 200 {
					defer resp.Body.Close()
					mu.Lock()
					discovered[s] = true
					tokens.SmartRecruiters = append(tokens.SmartRecruiters, s)
					mu.Unlock()

					atomic.AddUint32(&validatingCount, 1)
					fmt.Printf("[SYNC] Validating slug %d/3000... (SmartRecruiters)\n", atomic.LoadUint32(&validatingCount))
					return
				}
			}(slug)
		}
	}

	wg.Wait()

	// Dedup tokens.json before saving
	tokens.Greenhouse = uniqueStrings(tokens.Greenhouse)
	tokens.Lever = uniqueStrings(tokens.Lever)
	tokens.SmartRecruiters = uniqueStrings(tokens.SmartRecruiters)
	tokens.Indian = uniqueStrings(tokens.Indian)

	finalData, _ := json.MarshalIndent(tokens, "", "    ")
	os.WriteFile("tokens.json", finalData, 0644)

	postCount := len(tokens.Greenhouse) + len(tokens.Lever) + len(tokens.SmartRecruiters)
	RunGrowthTest(preCount, postCount)

	fmt.Printf("[SYNC] Total time: %s\n", time.Since(start).Round(time.Second))
}

func RunGrowthTest(prev, new int) {
	growth := 0.0
	if prev > 0 {
		growth = float64(new-prev) / float64(prev) * 100
	}
	fmt.Println("\n[TEST] ══════════════════════════════════════")
	fmt.Println("[TEST]  DISCOVERY GROWTH RESULTS")
	fmt.Printf("[TEST] Previous Count : %d Companies\n", prev)
	fmt.Printf("[TEST] New Scale      : %d Companies\n", new)
	fmt.Printf("[TEST] Growth %%       : +%.0f%%\n", growth)
	fmt.Println("[TEST] ══════════════════════════════════════")
}

func uniqueStrings(s []string) []string {
	keys := make(map[string]bool)
	list := []string{}
	for _, entry := range s {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			if entry != "" {
				list = append(list, entry)
			}
		}
	}
	return list
}

func fetchGithubTokens(tokens *TokenData) {
	fmt.Println("[SYNC] Phase 0: Importing Master Lists from SimplifyJobs...")
	client := &http.Client{Timeout: 15 * time.Second}

	// Greenhouse
	fmt.Println("[SYNC] Fetching community Greenhouse list...")
	ghURL := "https://raw.githubusercontent.com/simplify-jobs/v2/main/data/greenhouse.json"
	resp, err := client.Get(ghURL)
	if err == nil && resp.StatusCode == 200 {
		var ghSlugs []string
		if err := json.NewDecoder(resp.Body).Decode(&ghSlugs); err == nil {
			fmt.Printf("[SYNC] Imported %d Greenhouse slugs\n", len(ghSlugs))
			tokens.Greenhouse = append(tokens.Greenhouse, ghSlugs...)
			tokens.Greenhouse = uniqueStrings(tokens.Greenhouse)
		}
		resp.Body.Close()
	}

	// Lever
	fmt.Println("[SYNC] Fetching community Lever list...")
	lvURL := "https://raw.githubusercontent.com/simplify-jobs/v2/main/data/lever.json"
	resp, err = client.Get(lvURL)
	if err == nil && resp.StatusCode == 200 {
		var lvSlugs []string
		if err := json.NewDecoder(resp.Body).Decode(&lvSlugs); err == nil {
			fmt.Printf("[SYNC] Imported %d Lever slugs\n", len(lvSlugs))
			tokens.Lever = append(tokens.Lever, lvSlugs...)
			tokens.Lever = uniqueStrings(tokens.Lever)
		}
		resp.Body.Close()
	}
}

func scrapeTheirStack(atsName string) []string {
	client := &http.Client{Timeout: 15 * time.Second}
	url := fmt.Sprintf("https://theirstack.com/en/technology/%s/in", atsName)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/91.0.4472.124 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	
	re := regexp.MustCompile(`\\"children\\":\\"([^\\"]+)\\"`)
	matches := re.FindAllStringSubmatch(string(body), -1)
	
	var names []string
	for _, m := range matches {
		name := m[1]
		
		name = strings.Split(name, "(")[0]
		name = strings.ReplaceAll(name, "India", "")
		name = strings.TrimSpace(name)
		
		lower := strings.ToLower(name)
		if lower == "" || lower == "jobs" || strings.Contains(lower, "theirstack") || strings.Contains(lower, "technographics") || lower == "0" || lower == "directories" || len(lower) < 3 {
			continue
		}
		names = append(names, name)
	}
	fmt.Printf("[HUNTER] TheirStack extracted %d clean company names for %s\n", len(names), atsName)
	return uniqueStrings(names)
}

func processMassiveCSV(deepHunt bool) []string {
	if !deepHunt {
		return nil
	}
	url := "https://raw.githubusercontent.com/mratanusarkar/Dataset-Indian-Companies/master/dataset/List_of_companies_in_India.csv"
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("[ERROR] Failed to fetch massive CSV: %v\n", err)
		return nil
	}
	defer resp.Body.Close()

	reader := csv.NewReader(resp.Body)
	// Skip header
	reader.Read()

	validTechKeywords := []string{"SOFTWARE", "TECH", "DIGITAL", "SYSTEMS", "LABS", "SOLUTIONS", "INFOTECH", "DATA", "CONSULTANCY"}
	var brands []string
	count := 0
	matched := 0

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		count++

		if len(record) < 2 {
			continue
		}

		name := record[1]
		upperName := strings.ToUpper(name)
		isTech := false
		for _, kw := range validTechKeywords {
			if strings.Contains(upperName, kw) {
				isTech = true
				break
			}
		}

		if isTech {
			brand := cleanCompanyName(name)
			if brand != "" {
				brands = append(brands, brand)
				matched++
			}
		}
	}

	fmt.Printf("[HUNTER] Scanned %d records. Found %d tech-aligned brands.\n", count, matched)
	return uniqueStrings(brands)
}

func cleanCompanyName(name string) string {
	// Strip junk as per specific regex: (?i)( PVT LTD| PRIVATE LIMITED| LIMITED| LTD| LLP| INC| CORP| SOLUTIONS| TECHNOLOGIES| SOFTWARE| SERVICES| INDIA)
	re := regexp.MustCompile(`(?i)( PVT LTD| PRIVATE LIMITED| LIMITED| LTD| LLP| INC| CORP| SOLUTIONS| TECHNOLOGIES| SOFTWARE| SERVICES| INDIA)`)
	clean := re.ReplaceAllString(name, "")
	clean = strings.ReplaceAll(clean, ".", "")
	clean = strings.ReplaceAll(clean, ",", "")
	parts := strings.Fields(clean)
	if len(parts) > 0 {
		return strings.ToLower(parts[0])
	}
	return ""
}

func huntNewSlugsWithSerper(tokens *TokenData) []string {
	fmt.Println("[HUNTER] Initiating Broad-Spectrum Search via Serper API...")
	serperKey := os.Getenv("SERPER_API_KEY")
	if serperKey == "" {
		fmt.Println("[HUNTER] ❌ SERPER_API_KEY missing. Skipping Serper hunt.")
		return nil
	}

	queries := []string{
		`site:boards.greenhouse.io "India" "careers"`,
		`site:jobs.lever.co "India" "Software"`,
		`site:ashbyhq.com "India" "Engineering"`,
		`site:boards.greenhouse.io "Bengaluru" OR "Hyderabad"`,
	}

	client := &http.Client{Timeout: 15 * time.Second}
	var newSlugs []string

	for _, q := range queries {
		payload := fmt.Sprintf(`{"q":"%s"}`, strings.ReplaceAll(q, `"`, `\"`))
		req, _ := http.NewRequest("POST", "https://google.serper.dev/search", strings.NewReader(payload))
		req.Header.Set("X-API-KEY", serperKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil { resp.Body.Close() }
			continue
		}

		var data struct {
			Organic []struct {
				Link string `json:"link"`
			} `json:"organic"`
		}
		json.NewDecoder(resp.Body).Decode(&data)
		resp.Body.Close()

		for _, item := range data.Organic {
			link := item.Link
			var slug string

			if strings.Contains(link, "boards.greenhouse.io/") {
				slug = strings.Split(strings.Split(link, "boards.greenhouse.io/")[1], "/")[0]
			} else if strings.Contains(link, "jobs.lever.co/") {
				slug = strings.Split(strings.Split(link, "jobs.lever.co/")[1], "/")[0]
			} else if strings.Contains(link, "ashbyhq.com/") {
				slug = strings.Split(strings.Split(link, "ashbyhq.com/")[1], "/")[0]
			}

			if slug != "" {
				slug = strings.ToLower(strings.Split(slug, "?")[0])
				if slug == "jobs" || slug == "search" || slug == "embed" || slug == "companies" {
					continue
				}

				isNew := true
				if tokens != nil {
					for _, t := range tokens.Greenhouse { if t == slug { isNew = false; break } }
					for _, t := range tokens.Lever { if t == slug { isNew = false; break } }
					for _, t := range tokens.Ashby { if t == slug { isNew = false; break } }
					for _, t := range tokens.Indian { if t == slug { isNew = false; break } }
				}
				
				if isNew {
					fmt.Printf("[HUNTER] Serper extracted new slug: %s\n", slug)
					newSlugs = append(newSlugs, slug)
				}
			}
		}
	}

	newSlugs = uniqueStrings(newSlugs)
	fmt.Printf("[HUNTER] Found %d potential new Indian-hiring companies via Serper.\n", len(newSlugs))
	return newSlugs
}
