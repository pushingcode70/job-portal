package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
