package main

import (
	"testing"
)

func TestExpandSearchTerms(t *testing.T) {
	py := expandSearchTerms("python")
	if !containsTerm(py, "python") || !containsTerm(py, "django") {
		t.Fatalf("python expansion too narrow: %v", py)
	}
	if containsTerm(py, "data") && !containsTerm(py, "data engineer") {
		// "data" alone should not be in python expansion anymore
		for _, term := range py {
			if term == "data" {
				t.Fatalf("python should not expand to broad 'data', got %v", py)
			}
		}
	}

	cpp := expandSearchTerms("c++")
	if !containsTerm(cpp, "c++") {
		t.Fatalf("c++ expansion: %v", cpp)
	}
}

func containsTerm(terms []string, want string) bool {
	for _, t := range terms {
		if t == want {
			return true
		}
	}
	return false
}

func TestJobMatchesRoleQuery(t *testing.T) {
	if !jobMatchesRoleQuery("Senior Python Engineer", "Acme", "python") {
		t.Error("expected python engineer match")
	}
	if !jobMatchesRoleQuery("Django Backend Developer", "Labs", "python") {
		t.Error("expected django match for python")
	}
	if jobMatchesRoleQuery("Senior Data Engineer", "Stripe", "python") {
		t.Error("data engineer should not match python-only search")
	}
	if jobMatchesRoleQuery("Java Developer", "Oracle", "python") {
		t.Error("java should not match python")
	}

	if !jobMatchesRoleQuery("C++ Software Engineer", "GameCo", "c++") {
		t.Error("expected c++ match")
	}
	if jobMatchesRoleQuery("Embedded Systems Engineer", "Chip", "c++") {
		t.Error("embedded-only title should not match c++ without c++/cpp")
	}

	if !jobMatchesRoleQuery("Python Developer", "Co", "python developer") {
		t.Error("expected python developer match")
	}
	if jobMatchesRoleQuery("Python Specialist", "Co", "python developer") {
		t.Error("python specialist should not match python developer (missing developer)")
	}

	if jobMatchesRoleQuery("Senior JavaScript Engineer", "X", "java") {
		t.Error("javascript should not match java boundary search")
	}
	if !jobMatchesRoleQuery("Senior Java Engineer", "X", "java") {
		t.Error("java engineer should match java")
	}

	if !jobMatchesRoleQuery("Golang Developer", "X", "golang") {
		t.Error("golang match")
	}
}

func TestTitleContainsTermJavaScript(t *testing.T) {
	if titleContainsTerm("Senior JavaScript Developer", "java") {
		t.Error("java must not match inside javascript")
	}
	if !titleContainsTerm("Senior Java Developer", "java") {
		t.Error("java should match java developer")
	}
}

func TestSearchMasterIndex(t *testing.T) {
	RAMCache = []CompanyResponse{
		{
			Name:     "PyCorp",
			IsIndian: true,
			Jobs: []Job{
				{Title: "Python Developer", Company: "PyCorp", URL: "http://a"},
				{Title: "Java Engineer", Company: "PyCorp", URL: "http://b"},
				{Title: "Data Engineer", Company: "PyCorp", URL: "http://c"},
			},
		},
		{
			Name:     "CppInc",
			IsIndian: false,
			Jobs: []Job{
				{Title: "C++ Systems Engineer", Company: "CppInc", URL: "http://d"},
			},
		},
	}

	pyResults := searchMasterIndex("python")
	if len(pyResults) != 1 || len(pyResults[0].Jobs) != 1 {
		t.Fatalf("python search: want 1 job, got %+v", pyResults)
	}
	if pyResults[0].Jobs[0].Title != "Python Developer" {
		t.Fatalf("wrong job matched: %s", pyResults[0].Jobs[0].Title)
	}

	cppResults := searchMasterIndex("c++")
	if len(cppResults) != 1 || cppResults[0].Name != "CppInc" {
		t.Fatalf("c++ search: got %+v", cppResults)
	}
}

func TestSearchRAMCachePythonRole(t *testing.T) {
	RAMCache = []CompanyResponse{
		{
			Name: "BigCo",
			Jobs: []Job{
				{Title: "Python Engineer", Company: "BigCo"},
				{Title: "Python Developer", Company: "BigCo"},
				{Title: "Java Engineer", Company: "BigCo"},
			},
		},
		{
			Name: "SmallCo",
			Jobs: []Job{
				{Title: "Python Intern", Company: "SmallCo"},
			},
		},
		{
			Name: "NoPy",
			Jobs: []Job{
				{Title: "Ruby Developer", Company: "NoPy"},
			},
		},
	}

	results := searchMasterIndex("python")
	if len(results) != 2 {
		t.Fatalf("want 2 companies, got %d", len(results))
	}
	if results[0].Name != "BigCo" || len(results[0].Jobs) != 2 {
		t.Fatalf("BigCo should rank first with 2 python jobs, got %+v", results[0])
	}
	if len(results[1].Jobs) != 1 || results[1].Name != "SmallCo" {
		t.Fatalf("SmallCo second: %+v", results[1])
	}
}

func TestCompanyCacheKeyNormalization(t *testing.T) {
	if companyCacheKey(" Google ") != companyCacheKey("google") {
		t.Fatal("cache keys should merge Google/google")
	}
}

func TestSearchRankingIndianFirst(t *testing.T) {
	RAMCache = []CompanyResponse{
		{Name: "GlobalBig", IsIndian: false, Jobs: []Job{{Title: "Python Dev", Company: "GlobalBig"}}},
		{Name: "GlobalHuge", IsIndian: false, Jobs: []Job{
			{Title: "Python Eng", Company: "GlobalHuge"},
			{Title: "Python Lead", Company: "GlobalHuge"},
		}},
		{Name: "IndiaCo", IsIndian: true, Jobs: []Job{{Title: "Python Intern", Company: "IndiaCo"}}},
	}
	results := searchMasterIndex("python")
	if len(results) < 2 {
		t.Fatalf("expected results, got %d", len(results))
	}
	if !results[0].IsIndian {
		t.Fatalf("Indian company should rank first, got %s", results[0].Name)
	}
}
