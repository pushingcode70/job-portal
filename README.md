Job Aggregator: High-Speed Concurrency with Go
A high-performance job scraping tool built with Go and React/HTML. This aggregator leverages Go’s concurrency primitives to fetch listings from major job boards like Greenhouse and Lever simultaneously, reducing wait times and API overhead.

Key Features
Parallel Processing: Uses sync.WaitGroup to execute API requests in parallel rather than sequentially.
Data Standardization: Automatically cleans and maps varied JSON responses into a single master_jobs.json file.
Modular Frontend: Serves a dedicated frontend/ directory for searching, filtering, and viewing job listings.
Scalable Architecture: Designed with a modular structure to allow for easy integration of additional job boards.

Tech Stack
Backend: Go (Golang)
Frontend: HTML5 / CSS3 (React-ready)
Data Storage: Local JSON

Quick Start
1. Clone and Enter Directory
Bash
git clone https://github.com/pushingcode70/job-portal.git
cd job-portal
2. Prepare Environment
Create a local environment file for your configuration:

HOW TO RUN
Bas->
go mod tidy(if theres any problem change go version in go mod file to 1.25.1)
go run .
