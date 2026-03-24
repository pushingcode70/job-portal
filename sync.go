package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type TokenData struct {
	Greenhouse []string `json:"greenhouse"`
	Lever      []string `json:"lever"`
	Zoho       []string `json:"zoho"`
	Ashby      []string `json:"ashby"`
	Darwinbox  []string `json:"darwinbox"`
	Workday    []string `json:"workday"`
	Indian     []string `json:"indian"`
	IndianDiscovered []string `json:"indian_discovered"`
}

// RunSync is the main entry point for the discovery engine
func RunSync() {
	fmt.Println("[SYNC] ══════════════════════════════════════════════")
	fmt.Println("[SYNC]  Discovery Engine: Scaling to 5,000+ Companies")
	fmt.Println("[SYNC] ══════════════════════════════════════════════")
	
	// Phase 0: Community Import
	importFromSimplify()
	
	start := time.Now()

	// Load existing tokens
	file, err := os.ReadFile("tokens.json")
	if err != nil {
		fmt.Printf("[SYNC] Error reading tokens.json: %v\n", err)
		return
	}
	var tokens TokenData
	json.Unmarshal(file, &tokens)

	// Result map for discovery (Company Slug -> IsIndiaHiring)
	discovered := make(map[string]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 15) // 15 concurrent workers
	client := &http.Client{Timeout: 10 * time.Second}

	// Slug variations helper
	getVariations := func(base string) []string {
		base = strings.ToLower(strings.ReplaceAll(base, " ", ""))
		return []string{
			base,
			base + "india",
			base + "tech",
			base + "global",
			base + "in",
		}
	}

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
					var data struct {
						Jobs []struct {
							Location struct {
								Name string `json:"name"`
							} `json:"location"`
						} `json:"jobs"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&data); err == nil && len(data.Jobs) > 0 {
						hasIndia := false
						for _, j := range data.Jobs {
							if _, ok := GetLocationContext(j.Location.Name); ok {
								hasIndia = true
								break
							}
						}
						mu.Lock()
						discovered[s] = true
						tokens.Greenhouse = append(tokens.Greenhouse, s)
						
						// Dynamic Indian Tagging (Regex \bIndia\b)
						if hasIndia {
							tokens.Indian = append(tokens.Indian, s)
							fmt.Printf("[DISCOVERY] Added Greenhouse: %s (India ✅)\n", s)
						} else {
							fmt.Printf("[DISCOVERY] Added Greenhouse: %s (Global 🌍)\n", s)
						}
						mu.Unlock()
					}
					return
				}

				// Test Lever
				lvUrl := fmt.Sprintf("https://api.lever.co/v0/postings/%s", s)
				resp, err = client.Get(lvUrl)
				if err == nil && resp.StatusCode == 200 {
					defer resp.Body.Close()
					var data []struct {
						Categories struct {
							Location string `json:"location"`
						} `json:"categories"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&data); err == nil && len(data) > 0 {
						hasIndia := false
						for _, j := range data {
							if _, ok := GetLocationContext(j.Categories.Location); ok {
								hasIndia = true
								break
							}
						}
						mu.Lock()
						discovered[s] = true
						tokens.Lever = append(tokens.Lever, s)
						
						// Dynamic Indian Tagging (Regex \bIndia\b)
						if hasIndia {
							tokens.Indian = append(tokens.Indian, s)
							fmt.Printf("[DISCOVERY] Added Lever: %s (India ✅)\n", s)
						} else {
							fmt.Printf("[DISCOVERY] Added Lever: %s (Global 🌍)\n", s)
						}
						mu.Unlock()
					}
				}
			}(slug)
		}
	}

	// 2. RE-VALIDATION PHASE: Check existing tokens for India hiring
	fmt.Println("[SYNC] Phase 2: Re-validating existing tokens...")
	allCompanyTokens := append(tokens.Greenhouse, tokens.Lever...)
	for _, t := range allCompanyTokens {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Check if already tagged as Indian (to avoid duplicates)
			isAlreadyIndian := false
			mu.Lock()
			for _, it := range tokens.Indian { if it == token { isAlreadyIndian = true; break } }
			mu.Unlock()
			if isAlreadyIndian { return }

			// We use scrapeJobs to check current status (efficiently)
			// But for a sync tool, a direct lightweight check is better
			// Greenhouse
			ghUrl := fmt.Sprintf("https://boards-api.greenhouse.io/v1/boards/%s/jobs", token)
			resp, err := client.Get(ghUrl)
			if err == nil && resp.StatusCode == 200 {
				defer resp.Body.Close()
				var data struct {
					Jobs []struct {
						Location struct {
							Name string `json:"name"`
						} `json:"location"`
					} `json:"jobs"`
				}
				json.NewDecoder(resp.Body).Decode(&data)
				for _, j := range data.Jobs {
					if _, ok := GetLocationContext(j.Location.Name); ok {
						mu.Lock()
						tokens.Indian = append(tokens.Indian, token)
						mu.Unlock()
						fmt.Printf("[TAG] %s tagged as Indian-hiring (Greenhouse)\n", token)
						return
					}
				}
			}

			// Lever
			lvUrl := fmt.Sprintf("https://api.lever.co/v0/postings/%s", token)
			resp, err = client.Get(lvUrl)
			if err == nil && resp.StatusCode == 200 {
				defer resp.Body.Close()
				var data []struct {
					Categories struct {
						Location string `json:"location"`
					} `json:"categories"`
				}
				json.NewDecoder(resp.Body).Decode(&data)
				for _, j := range data {
					if _, ok := GetLocationContext(j.Categories.Location); ok {
						mu.Lock()
						tokens.Indian = append(tokens.Indian, token)
						mu.Unlock()
						fmt.Printf("[TAG] %s tagged as Indian-hiring (Lever)\n", token)
						return
					}
				}
			}
		}(t)
	}

	wg.Wait()

	// Dedup tokens.json before saving
	tokens.Greenhouse = uniqueStrings(tokens.Greenhouse)
	tokens.Lever = uniqueStrings(tokens.Lever)
	tokens.Indian = uniqueStrings(tokens.Indian)

	finalData, _ := json.MarshalIndent(tokens, "", "    ")
	os.WriteFile("tokens.json", finalData, 0644)

	fmt.Printf("[SYNC] ✅ Sync complete. Total Greenhouse: %d, Lever: %d, Indian-hiring: %d\n", 
		len(tokens.Greenhouse), len(tokens.Lever), len(tokens.Indian))
	fmt.Printf("[SYNC] Total time: %s\n", time.Since(start).Round(time.Second))
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

func importFromSimplify() {
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
			updateTokens("greenhouse", ghSlugs)
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
			updateTokens("lever", lvSlugs)
		}
		resp.Body.Close()
	}
}

func updateTokens(boardType string, newSlugs []string) {
	file, err := os.ReadFile("tokens.json")
	if err != nil {
		return
	}
	var tokens TokenData
	json.Unmarshal(file, &tokens)

	switch boardType {
	case "greenhouse":
		tokens.Greenhouse = append(tokens.Greenhouse, newSlugs...)
		tokens.Greenhouse = uniqueStrings(tokens.Greenhouse)
	case "lever":
		tokens.Lever = append(tokens.Lever, newSlugs...)
		tokens.Lever = uniqueStrings(tokens.Lever)
	}

	data, _ := json.MarshalIndent(tokens, "", "    ")
	os.WriteFile("tokens.json", data, 0644)
}
