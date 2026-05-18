package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func InitDB() {
	var err error
	DB, err = sql.Open("sqlite", "jobs.db?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	// Create tables
	schema := `
	CREATE TABLE IF NOT EXISTS companies (
		slug TEXT PRIMARY KEY,
		name TEXT,
		platform TEXT,
		is_indian BOOLEAN,
		industry TEXT,
		last_checked DATETIME
	);

	CREATE TABLE IF NOT EXISTS jobs (
		url TEXT PRIMARY KEY,
		title TEXT,
		company TEXT,
		location TEXT,
		location_tag TEXT,
		is_india BOOLEAN,
		timestamp DATETIME
	);

	CREATE VIRTUAL TABLE IF NOT EXISTS jobs_fts USING fts5(
		title,
		company,
		location,
		content=jobs,
		content_rowid=rowid
	);

	-- Triggers to automatically update jobs_fts on insert, update, and delete
	CREATE TRIGGER IF NOT EXISTS jobs_ai AFTER INSERT ON jobs BEGIN
	  INSERT INTO jobs_fts(rowid, title, company, location) VALUES (new.rowid, new.title, new.company, new.location);
	END;
	CREATE TRIGGER IF NOT EXISTS jobs_ad AFTER DELETE ON jobs BEGIN
	  INSERT INTO jobs_fts(jobs_fts, rowid, title, company, location) VALUES('delete', old.rowid, old.title, old.company, old.location);
	END;
	CREATE TRIGGER IF NOT EXISTS jobs_au AFTER UPDATE ON jobs BEGIN
	  INSERT INTO jobs_fts(jobs_fts, rowid, title, company, location) VALUES('delete', old.rowid, old.title, old.company, old.location);
	  INSERT INTO jobs_fts(rowid, title, company, location) VALUES (new.rowid, new.title, new.company, new.location);
	END;

	CREATE INDEX IF NOT EXISTS idx_jobs_company ON jobs(company);
	CREATE INDEX IF NOT EXISTS idx_companies_slug ON companies(slug);
	CREATE INDEX IF NOT EXISTS idx_companies_platform ON companies(platform);
	CREATE INDEX IF NOT EXISTS idx_companies_platform_slug ON companies(platform, slug);
	CREATE INDEX IF NOT EXISTS idx_jobs_title_lower ON jobs(title COLLATE NOCASE);
	CREATE INDEX IF NOT EXISTS idx_jobs_timestamp ON jobs(timestamp);

	CREATE TABLE IF NOT EXISTS jobs_quarantine (
		url TEXT PRIMARY KEY,
		title TEXT,
		company TEXT,
		location TEXT,
		location_tag TEXT,
		is_india BOOLEAN,
		timestamp DATETIME,
		quarantined_at DATETIME,
		reason TEXT
	);
	`

	_, err = DB.Exec(schema)
	if err != nil {
		log.Fatalf("Failed to execute schema: %v", err)
	}
	
	// Optimize setting for SQLite (WAL allows hunter writes during API reads)
	DB.Exec("PRAGMA synchronous=NORMAL;")
	DB.Exec("PRAGMA cache_size=-64000;") // 64MB cache
	DB.Exec("PRAGMA temp_store=MEMORY;")
	DB.SetMaxOpenConns(25)
	DB.SetMaxIdleConns(10)

	fmt.Println("[DB] SQLite database and FTS5 indices initialized successfully.")
}

// CleanupJobsForInvalidCompanies removes jobs whose company slug is marked invalid.
func CleanupJobsForInvalidCompanies() (int64, error) {
	res, err := DB.Exec(`
		DELETE FROM jobs
		WHERE LOWER(company) IN (SELECT slug FROM companies WHERE platform = 'invalid')
	`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// CleanupJobsForUnverifiedCompanies removes jobs tied to pending, invalid, or non-ATS companies.
func CleanupJobsForUnverifiedCompanies() (int64, error) {
	res, err := DB.Exec(`
		DELETE FROM jobs
		WHERE LOWER(company) IN (
			SELECT slug FROM companies
			WHERE platform IN ('invalid', 'pending')
			   OR platform NOT IN ('greenhouse','lever','smartrecruiters','ashby','zoho','darwinbox','workday')
		)
	`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// CompanyPlatformUpdate is a single hunter result for batch DB writes.
type CompanyPlatformUpdate struct {
	Slug     string
	Platform string // empty means mark invalid
}

// BatchUpdateCompanyPlatforms applies hunter results in one transaction.
func BatchUpdateCompanyPlatforms(updates []CompanyPlatformUpdate) (verified, invalid int, err error) {
	if DB == nil {
		return 0, 0, fmt.Errorf("database not initialized")
	}
	if len(updates) == 0 {
		return 0, 0, nil
	}

	tx, err := DB.Begin()
	if err != nil {
		return 0, 0, err
	}

	verifiedStmt, err := tx.Prepare(
		`UPDATE companies SET platform = ?, last_checked = CURRENT_TIMESTAMP WHERE slug = ?`,
	)
	if err != nil {
		tx.Rollback()
		return 0, 0, err
	}
	defer verifiedStmt.Close()

	invalidStmt, err := tx.Prepare(
		`UPDATE companies SET platform = 'invalid', last_checked = CURRENT_TIMESTAMP WHERE slug = ?`,
	)
	if err != nil {
		tx.Rollback()
		return 0, 0, err
	}
	defer invalidStmt.Close()

	for _, u := range updates {
		slug := strings.ToLower(strings.TrimSpace(u.Slug))
		if slug == "" {
			continue
		}
		if u.Platform != "" {
			if _, err := verifiedStmt.Exec(u.Platform, slug); err != nil {
				tx.Rollback()
				return verified, invalid, err
			}
			verified++
		} else {
			if _, err := invalidStmt.Exec(slug); err != nil {
				tx.Rollback()
				return verified, invalid, err
			}
			invalid++
		}
	}

	if err := tx.Commit(); err != nil {
		return verified, invalid, err
	}
	return verified, invalid, nil
}

const dbInsertChunkSize = 5000

func batchInsertCompanies(companies []CompanyRecord) error {
	for i := 0; i < len(companies); i += dbInsertChunkSize {
		end := i + dbInsertChunkSize
		if end > len(companies) {
			end = len(companies)
		}
		if err := batchInsertCompaniesChunk(companies[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func batchInsertCompaniesChunk(companies []CompanyRecord) error {
	if len(companies) == 0 {
		return nil
	}
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO companies (slug, name, platform, is_indian, industry, last_checked) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, c := range companies {
		_, err = stmt.Exec(strings.ToLower(c.Slug), strings.ToLower(c.Name), c.Platform, c.IsIndian, c.Industry, c.LastChecked)
		if err != nil {
			fmt.Printf("[DB] Error inserting company %s: %v\n", c.Name, err)
		}
	}
	return tx.Commit()
}

const jobUpsertSQL = `
INSERT INTO jobs (url, title, company, location, location_tag, is_india, timestamp)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(url) DO UPDATE SET
	timestamp = excluded.timestamp,
	title = excluded.title,
	location = excluded.location,
	location_tag = excluded.location_tag,
	is_india = excluded.is_india,
	company = excluded.company
`

// batchUpsertJobs inserts or updates jobs in chunks (persistent index growth).
func batchUpsertJobs(jobs []JobRecord) error {
	for i := 0; i < len(jobs); i += dbInsertChunkSize {
		end := i + dbInsertChunkSize
		if end > len(jobs) {
			end = len(jobs)
		}
		if err := batchUpsertJobsChunk(jobs[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func batchUpsertJobsChunk(jobs []JobRecord) error {
	if len(jobs) == 0 {
		return nil
	}
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(jobUpsertSQL)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, j := range jobs {
		_, err = stmt.Exec(
			j.URL, j.Title, strings.ToLower(j.Company),
			j.Location, j.LocationTag, j.IsIndia, j.Timestamp,
		)
		if err != nil {
			fmt.Printf("[DB] Error upserting job %v: %v\n", j.Title, err)
		}
	}
	return tx.Commit()
}

// batchInsertJobs is an alias for batchUpsertJobs (persistent indexing).
func batchInsertJobs(jobs []JobRecord) error {
	return batchUpsertJobs(jobs)
}

// SweepStaleJobsAfterSync removes jobs not refreshed during a completed sync run.
// Safety: only deletes rows older than syncStart AND older than the grace window.
func SweepStaleJobsAfterSync(syncStart time.Time, grace time.Duration) (int64, error) {
	if grace <= 0 {
		grace = 168 * time.Hour
	}
	graceCutoff := time.Now().Add(-grace)
	res, err := DB.Exec(`
		DELETE FROM jobs
		WHERE timestamp < ?
		  AND timestamp < ?
	`, syncStart, graceCutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// QuarantineJobsForUnverified moves jobs to jobs_quarantine instead of hard-deleting.
func QuarantineJobsForUnverified(reason string) (int64, error) {
	if reason == "" {
		reason = "unverified_company"
	}
	tx, err := DB.Begin()
	if err != nil {
		return 0, err
	}

	moveSQL := `
		INSERT OR REPLACE INTO jobs_quarantine
			(url, title, company, location, location_tag, is_india, timestamp, quarantined_at, reason)
		SELECT j.url, j.title, j.company, j.location, j.location_tag, j.is_india, j.timestamp, CURRENT_TIMESTAMP, ?
		FROM jobs j
		WHERE LOWER(j.company) IN (
			SELECT slug FROM companies
			WHERE platform IN ('invalid', 'pending')
			   OR platform NOT IN ('greenhouse','lever','smartrecruiters','ashby','zoho','darwinbox','workday')
		)
	`
	if _, err := tx.Exec(moveSQL, reason); err != nil {
		tx.Rollback()
		return 0, err
	}

	del, err := tx.Exec(`
		DELETE FROM jobs
		WHERE LOWER(company) IN (
			SELECT slug FROM companies
			WHERE platform IN ('invalid', 'pending')
			   OR platform NOT IN ('greenhouse','lever','smartrecruiters','ashby','zoho','darwinbox','workday')
		)
	`)
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	n, _ := del.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// CleanupUnverifiedJobsWithPolicy deletes or quarantines jobs for non-verified companies.
func CleanupUnverifiedJobsWithPolicy() (int64, error) {
	if os.Getenv("SYNC_QUARANTINE") == "1" || os.Getenv("SYNC_QUARANTINE") == "true" {
		return QuarantineJobsForUnverified("platform_unverified")
	}
	return CleanupJobsForUnverifiedCompanies()
}
