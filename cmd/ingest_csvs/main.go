package main

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// slugify cleans "Apple Inc." into "apple"
func slugify(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	// Remove common corporate suffixes
	suffixes := []string{
		" inc.", " inc", " corp.", " corp", " ltd.", " ltd",
		" llc", " group", " software", " limited", " pvt", " holdings",
	}
	for _, s := range suffixes {
		name = strings.ReplaceAll(name, s, "")
	}
	// Take first word or join with hyphens
	parts := strings.Fields(name)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func main() {
	// 1. Open Database
	db, err := sql.Open("sqlite", "jobs.db")
	if err != nil {
		fmt.Printf("[ERROR] Database connection failed: %v\n", err)
		return
	}
	defer db.Close()

	// 2. Target Folder
	dataFolder := "./csv_data"
	if _, err := os.Stat(dataFolder); os.IsNotExist(err) {
		fmt.Printf("[!] Folder '%s' not found. Create it and put your CSVs inside.\n", dataFolder)
		return
	}

	// 3. Load existing slugs to prevent duplicates
	existingSlugs := make(map[string]bool)
	rows, _ := db.Query("SELECT slug FROM companies")
	for rows.Next() {
		var s string
		rows.Scan(&s)
		existingSlugs[s] = true
	}
	fmt.Printf("[START] DB has %d companies. Scanning folder: %s\n", len(existingSlugs), dataFolder)

	// 4. Process all CSVs in the folder
	newTotal := 0
	err = filepath.Walk(dataFolder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".csv") {
			count := processFile(db, path, existingSlugs)
			newTotal += count
			fmt.Printf("Processed: %-30s | Added: %d\n", info.Name(), count)
		}
		return nil
	})

	if err != nil {
		fmt.Printf("[ERROR] Folder walk failed: %v\n", err)
	}

	fmt.Printf("\n[FINISH] Success! Added %d new companies to 'pending' list.\n", newTotal)
	fmt.Println("[INFO] Your background Hunter will now start verifying these companies automatically.")
}

func processFile(db *sql.DB, path string, existing map[string]bool) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	reader := csv.NewReader(f)
	// Some CSVs have weird quotes or characters; this makes it more robust
	reader.LazyQuotes = true

	header, err := reader.Read()
	if err != nil {
		return 0
	}

	// Identify columns
	nameIdx, countryIdx := -1, -1
	for i, h := range header {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "company" || h == "name" || h == "brand" {
			nameIdx = i
		}
		if h == "country" {
			countryIdx = i
		}
	}
	// Fallback if header detection fails
	if nameIdx == -1 {
		nameIdx = 1
	}

	// Start a transaction for this file (makes it much faster)
	tx, err := db.Begin()
	if err != nil {
		return 0
	}

	stmt, _ := tx.Prepare("INSERT OR IGNORE INTO companies (slug, platform, is_indian) VALUES (?, 'pending', ?)")
	defer stmt.Close()

	addedInFile := 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		// Clean name and create slug
		rawName := record[nameIdx]
		slug := slugify(rawName)

		if slug == "" || existing[slug] {
			continue
		}

		// Detect India
		isIndian := 0
		if countryIdx != -1 {
			if strings.Contains(strings.ToLower(record[countryIdx]), "india") {
				isIndian = 1
			}
		}

		_, err = stmt.Exec(slug, isIndian)
		if err == nil {
			existing[slug] = true // Don't add same slug twice in one run
			addedInFile++
		}
	}

	tx.Commit()
	return addedInFile
}
