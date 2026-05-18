package main

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// searchJobsDB queries SQLite when the RAM cache is still warming up.
func searchJobsDB(query string) ([]CompanyResponse, error) {
	q := normalizeSearchQuery(query)
	plan := buildSearchPlan(q)

	var rows *sql.Rows
	var err error

	indianSlugs := loadIndianCompanySlugs()

	if q == "" {
		rows, err = DB.Query(`
			SELECT title, company, location, location_tag, url, is_india
			FROM jobs
			LIMIT 3000
		`)
	} else {
		// SQL prefilter on title using primary skill terms (strict filter applied after)
		likeParts := make([]string, 0)
		args := make([]any, 0)
		for _, group := range plan.skillGroups {
			if len(group) == 0 {
				continue
			}
			primary := group[0]
			likeParts = append(likeParts, "LOWER(title) LIKE ?")
			args = append(args, "%"+strings.ToLower(primary)+"%")
		}
		if len(likeParts) == 0 {
			likeParts = append(likeParts, "LOWER(title) LIKE ?")
			args = append(args, "%"+q+"%")
		}
		sqlStmt := fmt.Sprintf(`
			SELECT title, company, location, location_tag, url, is_india
			FROM jobs
			WHERE %s
			LIMIT 5000
		`, strings.Join(likeParts, " OR "))
		rows, err = DB.Query(sqlStmt, args...)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	companyMap := make(map[string]*CompanyResponse)
	for rows.Next() {
		var title, companySlug, location, locationTag, url string
		var isIndia bool

		if err := rows.Scan(&title, &companySlug, &location, &locationTag, &url, &isIndia); err != nil {
			continue
		}
		if q != "" && !jobMatchesRoleQuery(title, companySlug, q) {
			continue
		}

		key := strings.Title(companySlug)
		if _, exists := companyMap[key]; !exists {
			companyMap[key] = &CompanyResponse{
				Name:     key,
				IsIndian: indianSlugs[strings.ToLower(companySlug)],
				Jobs:     []Job{},
			}
		}
		data := companyMap[key]
		if isIndia {
			data.IsIndian = true
		}
		data.Jobs = append(data.Jobs, Job{
			Title:       title,
			Company:     key,
			Location:    location,
			LocationTag: locationTag,
			URL:         url,
			IsIndia:     isIndia,
		})
	}

	var results []CompanyResponse
	for _, cr := range companyMap {
		if len(cr.Jobs) > 0 {
			sort.Slice(cr.Jobs, func(i, j int) bool {
				return cr.Jobs[i].IsIndia && !cr.Jobs[j].IsIndia
			})
			results = append(results, *cr)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].IsIndian != results[j].IsIndian {
			return results[i].IsIndian
		}
		return len(results[i].Jobs) > len(results[j].Jobs)
	})

	const maxCompanies = 1000
	if len(results) > maxCompanies {
		results = results[:maxCompanies]
	}

	return results, nil
}
