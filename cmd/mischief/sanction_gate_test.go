package main

import (
	"os"
	"strings"
	"testing"
)

// SPEC-13 verb-coverage tests (MSF-032, found by MSF-022): the sanction
// gate must cover EVERY fault-landing verb, not only plan. On this
// unsanctioned test host (no marker, no env) selftest and battery refuse
// fail-closed (exit 2) and write NOTHING into the run dir — battery's gate
// runs before EVERYTHING including --dry-run (plan's posture: a
// host-posture check is not a landing act).
//
// RED discipline: before the gates existed, both tests failed with exit 0
// and a landed matrix (measured on the scheduler host, 2026-10-06). The
// ADMITTED path is covered by the existing verb tests via
// writeSanctionMarker (selftest_test.go) — the plane's own shape.

func TestSelftestRefusesUnsanctionedHost(t *testing.T) {
	t.Setenv("MISCHIEF_SANCTION_FILE", "/nonexistent/sanction-marker")
	dir := t.TempDir()
	code, stderr := captureStderr(t, func() int {
		return run([]string{"selftest", "--all", "--dir", dir})
	})
	if code != 2 {
		t.Fatalf("selftest on unsanctioned host: exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "not sanctioned") {
		t.Fatalf("refusal does not name the sanction check: %s", stderr)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("refused selftest wrote into the run dir: %v", ents)
	}
}

func TestBatteryRefusesUnsanctionedHost(t *testing.T) {
	t.Setenv("MISCHIEF_SANCTION_FILE", "/nonexistent/sanction-marker")
	dir := t.TempDir()
	code, stderr := captureStderr(t, func() int {
		return run([]string{"battery", "--quick", "--dry-run", "--dir", dir})
	})
	if code != 2 {
		t.Fatalf("battery on unsanctioned host: exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "not sanctioned") {
		t.Fatalf("refusal does not name the sanction check: %s", stderr)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("refused battery wrote into the run dir: %v", ents)
	}
}
