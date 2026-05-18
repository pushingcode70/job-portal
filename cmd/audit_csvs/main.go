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

// This matches your main app's slug logic
func slugify(name string) string {
	name = strings.ToLower(name)
	// Remove common junk
	replaces := []string{" inc.", " inc", " corp.", " corp", " ltd.", " ltd", " llc", " group", " software"}
	for _, r := range replaces {
		name = strings.ReplaceAll(name, r, "")
	}
	// Take the first word only for broad matching
	parts := strings.Fields(name)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func main() {
	// 1. Open your existing database
	db, err := sql.Open("sqlite", "jobs.db")
	if err != nil {
		fmt.Printf("Error opening DB: %v\n", err)
		return
	}
	defer db.Close()

	// 2. Load all current slugs into a map for fast lookup
	existingSlugs := make(map[string]bool)
	rows, err := db.Query("SELECT slug FROM companies")
	if err != nil {
		fmt.Printf("Error querying DB: %v\n", err)
		return
	}
	for rows.Next() {
		var s string
		rows.Scan(&s)
		existingSlugs[strings.ToLower(s)] = true
	}
	fmt.Printf("[INFO] Database currently has %d companies.\n\n", len(existingSlugs))

	// 3. Define the folder where your CSVs are kept
	csvFolder := "./csv_data" // Change this to your folder name!

	// Create the folder if it doesn't exist
	if _, err := os.Stat(csvFolder); os.IsNotExist(err) {
		os.Mkdir(csvFolder, 0755)
		fmt.Printf("[!] Folder '%s' not found. I created it. Put your CSVs there and run again.\n", csvFolder)
		return
	}

	// 4. Walk through the folder
	err = filepath.Walk(csvFolder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".csv") {
			processCSV(path, existingSlugs)
		}
		return nil
	})

	if err != nil {
		fmt.Printf("Error walking folder: %v\n", err)
	}
}

func processCSV(path string, existingSlugs map[string]bool) {
	file, err := os.Open(path)
	if err != nil {
		fmt.Printf("Could not open %s: %v\n", path, err)
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// Try to find the "Company" column index
	header, err := reader.Read()
	if err != nil {
		return
	}

	companyIdx := -1
	for i, head := range header {
		h := strings.ToLower(head)
		if h == "company" || h == "name" || h == "brand" {
			companyIdx = i
			break
		}
	}
	// Fallback to column 1 if header doesn't match
	if companyIdx == -1 {
		companyIdx = 1
	}

	newCount := 0
	totalCount := 0

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		totalCount++
		rawName := record[companyIdx]
		slug := slugify(rawName)

		if slug != "" && !existingSlugs[slug] {
			newCount++
		}
	}

	fmt.Printf("File: %-25s | Total: %-5d | New to DB: %-5d\n", filepath.Base(path), totalCount, newCount)
}
