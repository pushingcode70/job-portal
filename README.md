# Job Aggregator

A Go-based tool that concurrently searches Greenhouse and Lever APIs for job postings. The project features a Go backend utilizing `sync.WaitGroup` for fast, parallel fetching, and an HTML frontend for displaying the aggregated job listings.

## Features
- **Concurrent API Fetching:** Quickly scrapes Greenhouse and Lever job boards.
- **Data Aggregation:** Collects and standardizes job data (saved locally to `master_jobs.json`).
- **Web Interface:** Serves an HTML UI from the `frontend/` directory for easy browsing, searching, and filtering of job listings.

## Prerequisites
- [Go (1.18+ recommended)](https://go.dev/dl/)

## Setup Instructions

1. **Clone the repository:**
   ```bash
   git clone <your-repo-url>
   cd "test go"
   ```

2. **Configure Environment Variables:**
   Create a `.env` file in the root directory to store any required configuration variables or tokens.
   ```bash
   touch .env
   ```

3. **Download Dependencies:**
   ```bash
   go mod download
   ```

4. **Run the Application locally:**
   ```bash
   go run .
   ```
   *Alternatively, you can build and run the executable:*
   ```bash
   go build -o testgo.exe .
   ./testgo.exe
   ```

5. **View the User Interface:**
   Once the application is running, open your web browser and navigate to the local server address (usually `http://localhost:8080`, depending on the configuration in `main.go`).

## Project Structure
- `main.go`: Application entry point and server setup.
- `sync.go`: Concurrency logic and API fetching implementation.
- `models.go`: Data structures and JSON mappings.
- `constants.go`: Core application constants and configurations.
- `frontend/`: Contains the `index.html` and other web assets for the user interface.
