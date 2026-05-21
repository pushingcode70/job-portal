package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/ledongthuc/pdf"
)

// ProcessResumeUpload reads the file into memory (max 2MB), validates MIME type, and extracts plain text.
func ProcessResumeUpload(r io.Reader) (string, error) {
	limitReader := io.LimitReader(r, 2*1024*1024)
	data, err := io.ReadAll(limitReader)
	if err != nil && err != io.EOF {
		return "", err
	}

	contentType := http.DetectContentType(data)
	if contentType != "application/pdf" {
		return "", fmt.Errorf("Invalid file type. Please upload a genuine PDF.")
	}

	pdfReader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	b, err := pdfReader.GetPlainText()
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(b); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func containsWithWordBoundary(text, term string) bool {
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return false
	}
	if strings.Contains(term, " ") {
		return strings.Contains(text, term)
	}

	needsBoundary := len(term) <= 3 || term == "java" || term == "go" || term == "js" || term == "ts" || term == "ruby" || term == "php"
	if !needsBoundary {
		return strings.Contains(text, term)
	}

	for i := 0; i <= len(text)-len(term); i++ {
		if text[i:i+len(term)] != term {
			continue
		}
		before := i == 0 || !isAlnum(rune(text[i-1]))
		after := i+len(term) >= len(text) || !isAlnum(rune(text[i+len(term)]))
		if before && after {
			return true
		}
	}
	return false
}

func countWithWordBoundary(text, term string) int {
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return 0
	}
	
	count := 0
	needsBoundary := len(term) <= 3 || term == "java" || term == "go" || term == "js" || term == "ts" || term == "ruby" || term == "php"
	if !needsBoundary {
		return strings.Count(text, term)
	}

	for i := 0; i <= len(text)-len(term); i++ {
		if text[i:i+len(term)] != term {
			continue
		}
		before := i == 0 || !isAlnum(rune(text[i-1]))
		after := i+len(term) >= len(text) || !isAlnum(rune(text[i+len(term)]))
		if before && after {
			count++
		}
	}
	return count
}

// ExtractKeywordsWeighted returns skill weights and the primary skill
func ExtractKeywordsWeighted(text string) (map[string]int, string) {
	lines := strings.Split(text, "\n")
	skillWeights := make(map[string]int)
	
	for _, line := range lines {
		lowerLine := strings.ToLower(line)
		
		multiplier := 1
		if strings.Contains(lowerLine, "present") || strings.Contains(lowerLine, "current") {
			multiplier = 2
		}
		
		for _, syns := range roleSynonyms {
			for _, skill := range syns {
				count := countWithWordBoundary(lowerLine, skill)
				if count > 0 {
					skillWeights[skill] += count * multiplier
					expanded := expandSearchTerms(skill)
					for _, exp := range expanded {
						skillWeights[exp] += count * multiplier
					}
				}
			}
		}
	}
	
	primarySkill := ""
	maxWeight := 0
	for skill, weight := range skillWeights {
		if weight > maxWeight {
			maxWeight = weight
			primarySkill = skill
		}
	}
	
	return skillWeights, primarySkill
}

// ExtractJobSkills gets unique skills from job title
func ExtractJobSkills(title string) []string {
	lower := strings.ToLower(title)
	var skills []string
	for _, syns := range roleSynonyms {
		for _, skill := range syns {
			if containsWithWordBoundary(lower, skill) {
				skills = append(skills, skill)
			}
		}
	}
	return uniqueTerms(skills)
}

// FindCompaniesForResume calculates confidence scores based on Broad Union Search logic.
func FindCompaniesForResume(resumeText string, remoteOnly, recentOnly, indiaOnly bool) []CompanyResponse {
	skillWeights, primarySkill := ExtractKeywordsWeighted(resumeText)
	if len(skillWeights) == 0 {
		return []CompanyResponse{}
	}

	RAMCacheMutex.RLock()
	cache := RAMCache
	RAMCacheMutex.RUnlock()

	var results []CompanyResponse

	for _, cr := range cache {
		var matchedJobs []Job
		companyBestScore := 0
		uniqueMatchedSkillsMap := make(map[string]bool)

		for _, j := range cr.Jobs {
			if remoteOnly && !j.IsRemote {
				continue
			}
			if recentOnly && !j.IsNew {
				continue
			}
			if indiaOnly && !j.IsIndia {
				continue
			}
			
			jobSkills := ExtractJobSkills(j.Title)
			intersectCount := 0
			
			for _, js := range jobSkills {
				if skillWeights[js] > 0 {
					intersectCount++
					uniqueMatchedSkillsMap[js] = true
				}
			}
			
			if intersectCount > 0 {
				jobScore := 0
				
				if primarySkill != "" && titleContainsTerm(j.Title, primarySkill) {
					jobScore = 100 // Tier 1
				} else if intersectCount >= 2 {
					jobScore = 75 // Tier 2
				} else {
					jobScore = 50 // Tier 3
				}

				matchedJobs = append(matchedJobs, j)
				if jobScore > companyBestScore {
					companyBestScore = jobScore
				}
			}
		}

		if companyBestScore > 0 {
			sort.SliceStable(matchedJobs, func(i, j int) bool {
				if matchedJobs[i].IsNew != matchedJobs[j].IsNew {
					return matchedJobs[i].IsNew
				}
				return matchedJobs[i].IsIndia && !matchedJobs[j].IsIndia
			})
			crCopy := cr
			crCopy.Jobs = matchedJobs
			crCopy.ConfidenceScore = companyBestScore
			
			var matchedSkills []string
			for s := range uniqueMatchedSkillsMap {
				matchedSkills = append(matchedSkills, s)
			}
			crCopy.MatchedSkills = matchedSkills
			
			results = append(results, crCopy)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].ConfidenceScore != results[j].ConfidenceScore {
			return results[i].ConfidenceScore > results[j].ConfidenceScore
		}
		if results[i].IsIndian != results[j].IsIndian {
			return results[i].IsIndian
		}
		return len(results[i].Jobs) > len(results[j].Jobs)
	})

	return results
}
