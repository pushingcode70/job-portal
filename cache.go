package main

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// sqliteTimestampLayouts lists the formats SQLite stores DATETIME in.
var sqliteTimestampLayouts = []string{
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func parseSQLiteTimestamp(s string) time.Time {
	for _, layout := range sqliteTimestampLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Now()
}

// companyCacheKey normalizes slugs so "Google" and "google" merge in RAM.
func companyCacheKey(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}

func companyDisplayName(slug string) string {
	k := companyCacheKey(slug)
	if k == "" {
		return ""
	}
	return strings.Title(k)
}

func refreshRAMCacheFromQuery(limitClause string) {
	fmt.Println("[CACHE] Refreshing global RAM cache...")
	start := time.Now()

	sqlStmt := `
		SELECT title, company, COALESCE(location, ''), COALESCE(location_tag, ''), url, 
		       COALESCE(is_india, 0) AS is_india, timestamp
		FROM jobs`
	if limitClause != "" {
		sqlStmt += " " + limitClause
	}

	rows, err := DB.Query(sqlStmt)
	if err != nil {
		fmt.Printf("[CACHE] Error pulling jobs: %v\n", err)
		return
	}
	defer rows.Close()

	companyMap := make(map[string]*CompanyResponse)
	var jobCount int

	for rows.Next() {
		var companySlug, jobTitle, location, locationTag, url string
		var jobIndian bool
		var tsStr sql.NullString

		if err := rows.Scan(&jobTitle, &companySlug, &location, &locationTag, &url, &jobIndian, &tsStr); err != nil {
			continue
		}

		var timestamp time.Time
		if tsStr.Valid && tsStr.String != "" {
			timestamp = parseSQLiteTimestamp(tsStr.String)
		} else {
			timestamp = time.Now()
		}

		isNew := time.Since(timestamp) <= 24*time.Hour
		
		isRemote := false
		lowerTitle := strings.ToLower(jobTitle)
		lowerLoc := strings.ToLower(location)
		remoteKeywords := []string{"remote", "wfh", "work from home", "anywhere", "distributed"}
		for _, kw := range remoteKeywords {
			if strings.Contains(lowerTitle, kw) || strings.Contains(lowerLoc, kw) {
				isRemote = true
				break
			}
		}
		
		key := companyCacheKey(companySlug)
		if key == "" {
			continue
		}
		jobCount++
		if _, exists := companyMap[key]; !exists {
			companyMap[key] = &CompanyResponse{
				Name:     companyDisplayName(companySlug),
				IsIndian: false,
				Jobs:     []Job{},
			}
		}
		data := companyMap[key]
		display := companyDisplayName(companySlug)
		data.Jobs = append(data.Jobs, Job{
			Title:       jobTitle,
			Company:     display,
			Location:    location,
			LocationTag: locationTag,
			URL:         url,
			IsIndia:     jobIndian,
			IsNew:       isNew,
			IsRemote:    isRemote,
		})
	}

	RAMCacheMutex.RLock()
	currentCount := 0
	for _, c := range RAMCache {
		currentCount += len(c.Jobs)
	}
	RAMCacheMutex.RUnlock()

	if currentCount > 0 && jobCount < int(float64(currentCount)*0.8) {
		fmt.Printf("[CACHE] WARNING: jobCount (%d) is < 80%% of current (%d). Aborting refresh.\n", jobCount, currentCount)
		return
	}

	if jobCount == 0 {
		fmt.Printf("[CACHE] WARNING: 0 jobs returned. Aborting RAM cache overwrite to prevent blank UI.\n")
		return
	}

	sortedResults := buildSortedCompanyList(companyMap)

	RAMCacheMutex.Lock()
	RAMCache = sortedResults
	RAMCacheMutex.Unlock()

	fmt.Printf("[CACHE] RAM cache updated in %dms. %d jobs → %d companies.\n",
		time.Since(start).Milliseconds(), jobCount, len(sortedResults))
}

func buildSortedCompanyList(companyMap map[string]*CompanyResponse) []CompanyResponse {
	var results []CompanyResponse
	for _, cr := range companyMap {
		if len(cr.Jobs) > 0 {
			hasIndiaJob := false
			for _, j := range cr.Jobs {
				if j.IsIndia {
					hasIndiaJob = true
					break
				}
			}
			cr.IsIndian = hasIndiaJob

			sort.Slice(cr.Jobs, func(i, j int) bool {
				return cr.Jobs[i].IsIndia && !cr.Jobs[j].IsIndia
			})
			results = append(results, *cr)
		}
	}
	sortCompaniesByJobCount(results)
	return results
}

// sortCompaniesByJobCount sorts in-place: Indian companies first, then most jobs.
func sortCompaniesByJobCount(results []CompanyResponse) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].IsIndian != results[j].IsIndian {
			return results[i].IsIndian
		}
		
		if results[i].IsIndian {
			cI, cJ := 0, 0
			for _, job := range results[i].Jobs { if job.IsIndia { cI++ } }
			for _, job := range results[j].Jobs { if job.IsIndia { cJ++ } }
			if cI != cJ { return cI > cJ }
		}
		
		ni, nj := len(results[i].Jobs), len(results[j].Jobs)
		if ni != nj {
			return ni > nj
		}
		return strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
	})
}
