package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// IngestIndiaAI scrapes the IndiaAI directory, verifies platforms, and inserts them.
func IngestIndiaAI() {
	fmt.Println("[INDIA-AI] Starting ingestion of IndiaAI startups...")

	client := &http.Client{Timeout: 15 * time.Second}

	var processedCount int32
	var verifiedCount int32
	var pendingCount int32
	var totalScraped int32

	// Channel for startups to verify
	startupChan := make(chan string, 1000)

	var wg sync.WaitGroup

	// Worker pool size for verification
	workerCount := 30

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range startupChan {
				slug := cleanBrand(name)
				if slug == "" {
					continue
				}

				platform, ok := probeCompanyPlatform(hunterHTTPClient, slug)

				now := time.Now().Format(time.RFC3339)

				if ok {
					// Insert/Update verified
					_, err := DB.Exec(`INSERT INTO companies (slug, name, platform, is_indian, industry, last_checked) 
					VALUES (?, ?, ?, 1, 'AI/ML', ?) 
					ON CONFLICT(slug) DO UPDATE SET platform=excluded.platform, is_indian=1, industry='AI/ML', last_checked=excluded.last_checked`,
						slug, name, platform, now)
					if err == nil {
						atomic.AddInt32(&verifiedCount, 1)
					} else {
						fmt.Printf("[INDIA-AI] DB Error (verified): %v\n", err)
					}
				} else {
					// Insert pending
					_, err := DB.Exec(`INSERT INTO companies (slug, name, platform, industry, last_checked) 
					VALUES (?, ?, 'pending', 'AI/ML', ?) 
					ON CONFLICT(slug) DO UPDATE SET industry='AI/ML', last_checked=excluded.last_checked`,
						slug, name, now)
					if err == nil {
						atomic.AddInt32(&pendingCount, 1)
					} else {
						fmt.Printf("[INDIA-AI] DB Error (pending): %v\n", err)
					}
				}

				// The "Every 20" Rule
				currentProcessed := atomic.AddInt32(&processedCount, 1)
				if currentProcessed%20 == 0 {
					total := atomic.LoadInt32(&totalScraped)
					verified := atomic.LoadInt32(&verifiedCount)
					pending := atomic.LoadInt32(&pendingCount)
					fmt.Printf("\033[1;36m[INDIA-AI]\033[0m Processed: %d | Verified Found: %d | Moved to Pending: %d | Total Scraped: %d\n",
						currentProcessed, verified, pending, total)
				}
			}
		}()
	}

	page := 1
	for {
		url := fmt.Sprintf("https://indiaai.gov.in/api/startups?page=%d", page)
		resp, err := client.Get(url)
		if err != nil {
			fmt.Printf("[INDIA-AI] Request failed on page %d: %v\n", page, err)
			break
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
		resp.Body.Close()

		if err != nil || resp.StatusCode != 200 {
			fmt.Printf("[INDIA-AI] Bad response on page %d: %d\n", page, resp.StatusCode)
			break
		}

		// Try unmarshaling robustly
		// Look for anything that could be an array of objects
		var root interface{}
		if err := json.Unmarshal(body, &root); err != nil {
			fmt.Printf("[INDIA-AI] 🚨 Error: JSON structure from indiaai.gov.in is not as expected on page %d: %v\n", page, err)
			break
		}

		names := extractNamesFromIndiaAI(root)
		if len(names) == 0 {
			// End of pagination or unexpected structure
			fmt.Printf("[INDIA-AI] No names extracted from page %d. Stopping API fetch.\n", page)
			break
		}

		for _, name := range names {
			atomic.AddInt32(&totalScraped, 1)
			startupChan <- name
		}

		page++
		// Respect government server
		time.Sleep(250 * time.Millisecond)
	}

	close(startupChan)
	wg.Wait()

	fmt.Printf("\n[INDIA-AI] ✅ Ingestion complete. Final Stats -> Processed: %d | Verified: %d | Pending: %d\n", 
		processedCount, verifiedCount, pendingCount)
}

// extractNamesFromIndiaAI searches deeply for "name", "title", or "company_name" in the JSON.
func extractNamesFromIndiaAI(data interface{}) []string {
	var names []string

	var walk func(v interface{})
	walk = func(v interface{}) {
		switch node := v.(type) {
		case []interface{}:
			for _, child := range node {
				walk(child)
			}
		case map[string]interface{}:
			// Check if this map represents a startup
			found := false
			for _, key := range []string{"title", "name", "company_name", "startup_name"} {
				if val, ok := node[key]; ok && val != nil {
					if strVal, isStr := val.(string); isStr {
						strVal = strings.TrimSpace(strVal)
						if strVal != "" {
							names = append(names, strVal)
							found = true
							break
						}
					}
				}
			}
			// If not found, keep walking values
			if !found {
				for _, child := range node {
					walk(child)
				}
			}
		}
	}

	walk(data)

	// Deduplicate names to avoid duplicates in the same page payload
	seen := make(map[string]bool)
	var unique []string
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			unique = append(unique, n)
		}
	}

	return unique
}
