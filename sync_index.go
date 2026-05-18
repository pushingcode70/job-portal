package main

import (
	"os"
	"strconv"
	"sync"
	"time"
)

// syncSweepGrace returns how old a stale row must be before mark-and-sweep deletes it.
func syncSweepGrace() time.Duration {
	hours := 72
	if v := os.Getenv("SYNC_SWEEP_GRACE_HOURS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			hours = n
		}
	}
	return time.Duration(hours) * time.Hour
}

// ramRefreshMinInterval throttles full-table RAM rebuilds during long syncs.
func ramRefreshMinInterval() time.Duration {
	sec := 600 // 10 minutes — balanced default vs per-batch refresh
	if v := os.Getenv("RAM_REFRESH_INTERVAL_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			sec = n
		}
	}
	return time.Duration(sec) * time.Second
}

var (
	ramRefreshMu       sync.Mutex
	lastRAMFullRefresh time.Time
)

// MaybeRefreshRAMCache rebuilds RAM from SQLite at most once per interval unless forced.
func MaybeRefreshRAMCache(force bool) {
	ramRefreshMu.Lock()
	defer ramRefreshMu.Unlock()

	if !force && !lastRAMFullRefresh.IsZero() && time.Since(lastRAMFullRefresh) < ramRefreshMinInterval() {
		return
	}
	lastRAMFullRefresh = time.Now()
	RefreshRAMCache()
}
