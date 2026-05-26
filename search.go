package main

import (
	"strings"
	"unicode"
)

// roleSynonyms: tight title-only synonyms per role/stack (no broad "data", "backend", etc.)
var roleSynonyms = map[string][]string{
	"python":     {"python", "django", "flask", "fastapi", "pytorch", "tensorflow", "pandas", "airflow", "celery"},
	"c++":        {"c++", "cpp", "c/c++", "c plus plus"},
	"cpp":        {"c++", "cpp", "c/c++"},
	"java":       {"java", "spring boot", "springboot", "hibernate", "jvm"},
	"javascript": {"javascript", "typescript", "node.js", "nodejs", "react", "vue", "angular", "next.js", "nextjs"},
	"js":         {"javascript", "typescript", "node.js", "nodejs", "react"},
	"typescript": {"typescript", "ts ", " ts/", "tsx"},
	"ts":         {"typescript", "tsx"},
	"react":      {"react", "reactjs", "next.js", "nextjs", "redux"},
	"reactjs":    {"react", "reactjs", "nextjs"},
	"golang":     {"golang"},
	"go":         {"golang", "go"},
	"rust":       {"rust", "rustlang"},
	"ruby":       {"ruby", "rails", "ruby on rails"},
	"php":        {"php", "laravel", "symfony"},
	"swift":      {"swift", "ios developer", "ios engineer"},
	"kotlin":     {"kotlin", "android developer", "android engineer"},
	"c#":         {"c#", "csharp", ".net", "dotnet", "asp.net"},
	"csharp":     {"c#", "csharp", ".net", "dotnet"},
	"mern":       {"react", "node", "express", "mongodb", "mern", "full stack", "fullstack"},
	"mongodb":    {"mongodb", "mongo"},
	"mongo":      {"mongodb", "mongo"},
	"linux":      {"linux", "ubuntu", "sysadmin", "bash", "shell script"},
	"devops":     {"devops", "sre", "kubernetes", "k8s", "terraform", "ci/cd", "docker"},
	"aws":        {"aws", "amazon web services"},
	"frontend":   {"frontend", "front-end", "front end", "ui engineer", "ux engineer"},
	"backend":    {"backend", "back-end", "back end", "api engineer"},
	"ai":         {"ai", "artificial intelligence", "openai", "anthropic", "llm", "langchain", "generative ai", "nlp"},
	"ml":         {"machine learning", "ml engineer", "deep learning", "nlp", "computer vision", "pytorch", "tensorflow", "data science"},
	"data":       {"data engineer", "data scientist", "data analyst", "etl", "spark", "databricks"},
}

// roleWords are optional second tokens in queries like "python developer"
var roleWords = map[string]bool{
	"developer": true, "engineer": true, "dev": true, "sde": true,
	"architect": true, "lead": true, "manager": true, "intern": true,
	"analyst": true, "consultant": true, "programmer": true,
	"specialist": true, "associate": true, "fellow": true,
}

func normalizeSearchQuery(query string) string {
	q := strings.TrimSpace(strings.ToLower(query))
	q = strings.ReplaceAll(q, "c plus plus", "c++")
	q = strings.ReplaceAll(q, "cplusplus", "c++")
	return q
}

// searchPlan drives strict role matching on job titles.
type searchPlan struct {
	skillGroups [][]string // each group: match ANY synonym in title (AND across groups)
	roleTerms   []string   // if non-empty, title must contain at least one
	rawQuery    string
}

func buildSearchPlan(query string) searchPlan {
	q := normalizeSearchQuery(query)
	if q == "" {
		return searchPlan{}
	}

	tokens := strings.Fields(q)
	var skillGroups [][]string
	var roleTerms []string

	for _, tok := range tokens {
		if roleWords[tok] {
			roleTerms = append(roleTerms, tok)
			continue
		}
		if syns, ok := roleSynonyms[tok]; ok {
			skillGroups = append(skillGroups, uniqueTerms(syns))
			continue
		}
		// Unknown token: treat as literal skill (strict)
		skillGroups = append(skillGroups, []string{tok})
	}

	// Single phrase not split (e.g. "machine learning") — check full query
	if len(skillGroups) == 0 && len(roleTerms) == 0 {
		if syns, ok := roleSynonyms[q]; ok {
			skillGroups = append(skillGroups, uniqueTerms(syns))
		} else {
			skillGroups = append(skillGroups, []string{q})
		}
	}

	// Plain "python" with no role word: only skill filter
	return searchPlan{
		skillGroups: skillGroups,
		roleTerms:   roleTerms,
		rawQuery:    q,
	}
}

// expandSearchTerms kept for API compatibility; returns all title synonyms for SQL prefilter.
func expandSearchTerms(query string) []string {
	plan := buildSearchPlan(query)
	seen := make(map[string]struct{})
	var out []string
	add := func(t string) {
		t = strings.TrimSpace(t)
		if t == "" {
			return
		}
		if _, ok := seen[t]; ok {
			return
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	for _, group := range plan.skillGroups {
		for _, s := range group {
			add(s)
		}
	}
	for _, r := range plan.roleTerms {
		add(r)
	}
	if len(out) == 0 && plan.rawQuery != "" {
		add(plan.rawQuery)
	}
	return out
}

func uniqueTerms(terms []string) []string {
	seen := make(map[string]struct{}, len(terms))
	var out []string
	for _, t := range terms {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

func isAlnum(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// titleContainsTerm matches a term in the job title with word boundaries for short/ambiguous tokens.
func titleContainsTerm(title, term string) bool {
	lower := strings.ToLower(title)
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return false
	}

	// Phrases with spaces: substring match
	if strings.Contains(term, " ") {
		return strings.Contains(lower, term)
	}

	// Short or ambiguous tokens need word boundaries
	needsBoundary := len(term) <= 3 || term == "java" || term == "go" || term == "js" || term == "ts"
	if !needsBoundary {
		return strings.Contains(lower, term)
	}

	for i := 0; i <= len(lower)-len(term); i++ {
		if lower[i:i+len(term)] != term {
			continue
		}
		before := i == 0 || !isAlnum(rune(lower[i-1]))
		after := i+len(term) >= len(lower) || !isAlnum(rune(lower[i+len(term)]))
		if before && after {
			return true
		}
	}
	return false
}

func titleMatchesAny(title string, terms []string) bool {
	for _, t := range terms {
		if titleContainsTerm(title, t) {
			return true
		}
	}
	return false
}

// jobMatchesTerms strict role filter: skills must appear in title; role words enforced when present.
func jobMatchesTerms(title, company string, terms []string) bool {
	// Legacy path: rebuild plan from terms isn't ideal; use title-only OR with boundary rules
	_ = company
	if len(terms) == 0 {
		return true
	}
	for _, term := range terms {
		if titleContainsTerm(title, term) {
			return true
		}
	}
	return false
}

// jobMatchesRoleQuery is the strict matcher used by search.
func jobMatchesRoleQuery(title, company string, query string) bool {
	plan := buildSearchPlan(query)
	if plan.rawQuery == "" {
		return true
	}

	cLower := strings.ToLower(company)
	if cLower == plan.rawQuery || strings.HasPrefix(cLower, plan.rawQuery+" ") || strings.Contains(cLower, " "+plan.rawQuery) || strings.HasPrefix(cLower, plan.rawQuery+"-") {
		return true
	}


	for _, group := range plan.skillGroups {
		if !titleMatchesAny(title, group) {
			return false
		}
	}

	if len(plan.roleTerms) > 0 && !titleMatchesAny(title, plan.roleTerms) {
		return false
	}

	return len(plan.skillGroups) > 0 || len(plan.roleTerms) > 0
}
