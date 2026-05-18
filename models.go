package main

import "time"

// Job represents an individual job posting
type Job struct {
	Title       string `json:"title"`
	Company     string `json:"company"`
	Location    string `json:"location"`
	LocationTag string `json:"locationTag"`
	URL         string `json:"url"`
	IsIndia     bool   `json:"isIndia"`
}

// CompanyResponse represents the API response for /api/company
type CompanyResponse struct {
	Name      string `json:"name"`
	IsIndian  bool   `json:"isIndian"`
	Jobs      []Job  `json:"jobs"`
}

// MasterIndex represents the full job cache
type MasterIndex struct {
	Jobs      []Job     `json:"jobs"`
	Timestamp time.Time `json:"timestamp"`
	CompanyMap map[string]bool `json:"companyMap"`
}

// CompanyRecord maps to the SQLite companies table
type CompanyRecord struct {
	Slug        string
	Name        string
	Platform    string
	IsIndian    bool
	Industry    string
	LastChecked time.Time
}

// JobRecord maps to the SQLite jobs table
type JobRecord struct {
	URL         string
	Title       string
	Company     string
	Location    string
	LocationTag string
	IsIndia     bool
	Timestamp   time.Time
}
