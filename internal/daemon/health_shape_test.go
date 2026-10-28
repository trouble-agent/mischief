package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServeHealthHoldsExplicitCollection(t *testing.T) {
	dir := localTempDir(t)
	d, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	healthPath := filepath.Join(dir, HealthFileName)
	var raw struct {
		Holds json.RawMessage `json:"holds"`
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b, readErr := os.ReadFile(healthPath)
		if readErr == nil {
			if err := json.Unmarshal(b, &raw); err == nil {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	if err := <-done; err != nil && err != context.Canceled {
		t.Fatalf("daemon run: %v", err)
	}

	b, err := os.ReadFile(healthPath)
	if err != nil {
		t.Fatalf("health.json: %v", err)
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("health parse: %v", err)
	}
	if string(raw.Holds) != "[]" {
		t.Fatalf("health holds = %s, want explicit []", raw.Holds)
	}
}

// ch:trace row=MISCHIEF-RV-20261027-HEALTH-SHAPE spec=none:review-row wave=mischief-foreman-2026-10-28-19-31-37.json#MISCHIEF-RV-20261027-HEALTH-SHAPE test=internal/daemon/health_shape_test.go::TestServeHealthHoldsExplicitCollection doc=none:health-json-contract prompt=/tmp/mf-20261028-1931-MISCHIEF-RV-20261027-HEALTH-SHAPE-brief.txt evidence=commit witness=none:serve-probe verdict=none:pending-foreman-judge commit=self memory=none:board-row
// STATUS: complete
// TIER: T2 (render fix + file-reading regression test + package suite + live serve probe invoked and observed)
// EVIDENCE: commit
