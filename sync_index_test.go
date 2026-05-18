package main

import (
	"testing"
	"time"
)

func TestSweepStaleJobsGraceLogic(t *testing.T) {
	syncStart := time.Now()
	grace := 72 * time.Hour
	graceCutoff := time.Now().Add(-grace)

	stale := syncStart.Add(-96 * time.Hour)
	if !stale.Before(syncStart) || !stale.Before(graceCutoff) {
		t.Fatal("96h old job should be eligible for sweep")
	}

	recent := syncStart.Add(-12 * time.Hour)
	if recent.Before(syncStart) && recent.Before(graceCutoff) {
		t.Fatal("12h old job should survive grace buffer")
	}
}
