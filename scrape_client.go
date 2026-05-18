package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	scraperHTTPTimeout   = 12 * time.Second
	scraperJitterMinMS   = 10
	scraperJitterMaxMS   = 30
	scraper429Cooldown   = 60 * time.Second
	scraperChromeUA      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36"
)

var (
	scraperHTTPClient = &http.Client{Timeout: scraperHTTPTimeout}
	scraperRand       = rand.New(rand.NewSource(time.Now().UnixNano()))

	scraperCooldownMu    sync.Mutex
	scraperCooldownUntil time.Time
)

func scraperUserAgent() string {
	if v := os.Getenv("SCRAPER_USER_AGENT"); v != "" {
		return v
	}
	return scraperChromeUA
}

func scraperJitter() {
	ms := scraperJitterMinMS + scraperRand.Intn(scraperJitterMaxMS-scraperJitterMinMS+1)
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

func scraperWaitIfRateLimited() {
	scraperCooldownMu.Lock()
	until := scraperCooldownUntil
	scraperCooldownMu.Unlock()
	if wait := time.Until(until); wait > 0 {
		time.Sleep(wait)
	}
}

func scraperTrigger429Cooldown() {
	scraperCooldownMu.Lock()
	if time.Now().Before(scraperCooldownUntil) {
		scraperCooldownMu.Unlock()
		scraperWaitIfRateLimited()
		return
	}
	scraperCooldownUntil = time.Now().Add(scraper429Cooldown)
	scraperCooldownMu.Unlock()
	fmt.Printf("⚠️ [CRITICAL] Scraper HTTP 429 — circuit breaker open for %s\n", scraper429Cooldown)
	time.Sleep(scraper429Cooldown)
}

// scraperGET performs a stealthy GET with jitter, browser UA, and 429 circuit breaker.
func scraperGET(url string) (*http.Response, error) {
	scraperWaitIfRateLimited()
	scraperJitter()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("scraper request: %w", err)
	}
	if req == nil {
		return nil, fmt.Errorf("scraper request is nil")
	}
	req.Header.Set("User-Agent", scraperUserAgent())
	req.Header.Set("Accept", "application/json")

	resp, err := scraperHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("scraper response is nil")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		scraperTrigger429Cooldown()
		return nil, fmt.Errorf("scraper rate limited (429)")
	}
	return resp, nil
}
