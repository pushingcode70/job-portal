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
	"strings"
	"sync/atomic"
	"time"
)

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
