package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// battery_test.go — CLI-level tests for the `mischief battery` verb:
// usage refusals, --dry-run, the live --quick run's artefacts, and the
// findings self-check.

// TestBatteryVerbRequiresMode: bare battery is the usage refusal (there is
// no default matrix — an unpointed battery is not a pass).
func TestBatteryVerbRequiresMode(t *testing.T) {
	code, stderr := captureStderr(t, func() int { return run([]string{"battery"}) })
	if code != 2 {
		t.Fatalf("bare battery exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "--quick") {
		t.Fatalf("refusal does not name --quick: %s", stderr)
	}
}

// TestBatteryDryRunPrintsMatrix: --dry-run prints the resolved parity
// matrix (all five legacy cells, named) and executes nothing (no run dir
// is created).
func TestBatteryDryRunPrintsMatrix(t *testing.T) {
	dir := t.TempDir()
	code, out := captureStdout(t, func() int {
		return run([]string{"battery", "--quick", "--dry-run", "--dir", dir})
	})
	if code != 0 {
		t.Fatalf("dry-run exit %d, want 0", code)
	}
	for _, cell := range []string{"chaos-disconnect", "chaos-shutdown", "chaos-corruption", "chaos-resource", "chaos-errorpath"} {
		if !strings.Contains(out, cell) {
			t.Errorf("dry-run output missing legacy cell %q:\n%s", cell, out)
		}
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("dry-run wrote into the run dir: %v", ents)
	}
}

// TestBatteryQuickLiveRunsAndEmitsArtefacts is the AC-18/S-5 wiring test:
// a live --quick run exits 0 (every cell pass-or-skip on the sanctioned
// scratch host), writes battery.jsonl (valid journal records), the matrix
// artefact (JSON naming every legacy cell + the detection note), and —
// under --file-rows — findings.jsonl that passes the in-package validator.
func TestBatteryQuickLiveRunsAndEmitsArtefacts(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "art")
	code, out := captureStdout(t, func() int {
		return run([]string{"battery", "--quick", "--file-rows", "--dir", dir, "--out", outDir})
	})
	if code != 0 {
		t.Fatalf("quick run exit %d\nstdout:\n%s", code, out)
	}
	if !strings.Contains(out, "5 cells") {
		t.Errorf("summary missing the cell count:\n%s", out)
	}

	// journal: every line reads back as a valid record
	jb, err := os.ReadFile(filepath.Join(dir, "battery.jsonl"))
	if err != nil {
		t.Fatalf("journal: %v", err)
	}
	for i, ln := range strings.Split(strings.TrimRight(string(jb), "\n"), "\n") {
		var rec struct {
			Type string `json:"type"`
			RunID string `json:"run_id"`
		}
		if err := json.Unmarshal([]byte(ln), &rec); err != nil || rec.Type == "" || rec.RunID == "" {
			t.Errorf("journal line %d not a valid record (err=%v, %+v)", i, err, rec)
		}
	}

	// artefact JSON: parity table + detection note present
	ab, err := os.ReadFile(filepath.Join(outDir, "matrix.json"))
	if err != nil {
		t.Fatalf("artefact: %v", err)
	}
	art := string(ab)
	for _, cell := range []string{"chaos-disconnect", "chaos-shutdown", "chaos-corruption", "chaos-resource", "chaos-errorpath"} {
		if !strings.Contains(art, cell) {
			t.Errorf("artefact missing legacy cell %q", cell)
		}
	}
	if !strings.Contains(art, "not_wired") || !strings.Contains(art, "future work") {
		t.Errorf("artefact does not state the AC-14 posture honestly")
	}
	if _, err := os.Stat(filepath.Join(outDir, "matrix.md")); err != nil {
		t.Errorf("markdown artefact missing: %v", err)
	}

	// findings: on a green-or-skip scratch run there are no adverse
	// verdicts, so findings.jsonl is EMPTY — a file, not an absence
	fb, err := os.ReadFile(filepath.Join(outDir, "findings.jsonl"))
	if err != nil {
		t.Fatalf("findings: %v", err)
	}
	if len(fb) != 0 {
		t.Errorf("green-or-skip run filed rows: %s", fb)
	}
	if !strings.Contains(out, "findings: none") {
		t.Errorf("output does not state the findings posture:\n%s", out)
	}
}

// TestBatteryFullMatrixUnknownPrimitiveRefuses: a --primitive id outside
// the catalog refuses with exit 2 BEFORE anything executes.
func TestBatteryFullMatrixUnknownPrimitiveRefuses(t *testing.T) {
	dir := t.TempDir()
	code, stderr := captureStderr(t, func() int {
		return run([]string{"battery", "--primitive", "ZZZ-999", "--dir", dir})
	})
	if code != 2 {
		t.Fatalf("unknown primitive exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "not in the loaded catalog") {
		t.Fatalf("refusal does not name the catalog: %s", stderr)
	}
}
