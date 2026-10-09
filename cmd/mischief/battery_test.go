package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/battery"
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
	writeSanctionMarker(t) // the MSF-032 gate runs before --dry-run (plan's posture: a host-posture check is not a landing act)
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
	writeSanctionMarker(t) // the MSF-032 gate runs before the live matrix
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
			Type  string `json:"type"`
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
	writeSanctionMarker(t) // the MSF-032 gate runs before catalog validation
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

// TestBatteryAC9HopsCarryRunIDAndVerdict (AC-9 completion, MSF-031): a
// live quick run prints the aggregate verdict + run id on the CLI hop,
// and the SAME run id + verdict values appear in the journal records and
// the emitted findings row — the full journal → CLI output → findings
// chain over one run. The live harness grades the five quick cells
// pass-or-skip on the sanctioned scratch host, so the aggregate verdict
// here is recovered; the adverse-value sweep lives in the battery
// package's AC-9 tests (internal/battery/ac9_test.go).
func TestBatteryAC9HopsCarryRunIDAndVerdict(t *testing.T) {
	writeSanctionMarker(t) // the MSF-032 gate runs before the live matrix
	dir := t.TempDir()
	outDir := filepath.Join(dir, "art")
	code, out := captureStdout(t, func() int {
		return run([]string{"battery", "--quick", "--file-rows", "--dir", dir, "--out", outDir})
	})
	if code != 0 {
		t.Fatalf("quick run exit %d\nstdout:\n%s", code, out)
	}

	// The CLI hop: the verdict line names the aggregate verdict and the
	// run id on one line.
	var line string
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "verdict: ") {
			line = ln
		}
	}
	if line == "" {
		t.Fatalf("no verdict line in the output:\n%s", out)
	}
	m := regexp.MustCompile(`^verdict: ([a-z_]+) \(run ([0-9a-f]{16})\)$`).FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("verdict line does not match the run-id + verdict shape: %q", line)
	}
	cliVerdict, runID := m[1], m[2]

	// Hop 1: the journal — every record carries the run id and the
	// verdict record carries the SAME verdict value the CLI printed.
	jb, err := os.ReadFile(filepath.Join(dir, "battery.jsonl"))
	if err != nil {
		t.Fatalf("journal: %v", err)
	}
	var journalVerdict string
	journalRunIDs := map[string]bool{}
	for i, ln := range strings.Split(strings.TrimRight(string(jb), "\n"), "\n") {
		var rec struct {
			Type    string `json:"type"`
			RunID   string `json:"run_id"`
			Verdict string `json:"verdict"`
		}
		if err := json.Unmarshal([]byte(ln), &rec); err != nil {
			t.Fatalf("journal line %d: %v", i, err)
		}
		journalRunIDs[rec.RunID] = true
		if rec.Type == "verdict" {
			journalVerdict = rec.Verdict
		}
	}
	if journalVerdict != cliVerdict {
		t.Errorf("AC-9 violated: journal verdict %q, CLI printed %q", journalVerdict, cliVerdict)
	}
	if len(journalRunIDs) != 1 || !journalRunIDs[runID] {
		t.Errorf("journal run ids %v, want exactly {%s}", journalRunIDs, runID)
	}

	// Hop 3: the findings row — on a green-or-skip run there are none
	// (an empty file is the honest shape).
	fb, err := os.ReadFile(filepath.Join(outDir, "findings.jsonl"))
	if err != nil {
		t.Fatalf("findings: %v", err)
	}
	if len(fb) != 0 {
		t.Errorf("green-or-skip run filed rows: %s", fb)
	}
}

// TestBatteryAC9AdverseChainLive (AC-9 completion, MSF-031): ONE live run
// whose cells include a measured FAILURE so all three hops carry an
// adverse verdict value end to end — the journal's verdict record, the
// CLI's verdict line, and an emitted findings row all name the same run
// id and agree on the verdict values.
//
// The failure is forced honestly at the harness's own entry: the
// land+prove+revert loop starts at a scratch dir under TMPDIR, so a
// TMPDIR pointing at a non-directory makes scratch creation fail — the
// loop grades fail with an empty land proof, the engine grades no_op
// (AC-3: armed-but-never-proved is the run FAILING), and the cell files
// a real findings row. P-001 (green on every Linux host) is the
// execution vehicle; the CLI already inherited the poisoned TMPDIR from
// the test process, and it is restored by t.Setenv's cleanup.
func TestBatteryAC9AdverseChainLive(t *testing.T) {
	writeSanctionMarker(t)              // the MSF-032 gate runs before the live cell
	t.Setenv("TMPDIR", "/etc/hostname") // a file: MkdirTemp under it fails
	dir := t.TempDir()                  // unaffected (created before the swap)
	code, out := captureStdout(t, func() int {
		return run([]string{"battery", "--project", "scratch", "--primitive", "P-001", "--file-rows", "--dir", dir})
	})
	if code != 0 {
		t.Fatalf("run exit %d\nstdout:\n%s", code, out)
	}

	// The CLI hop.
	m := regexp.MustCompile(`^verdict: ([a-z_]+) \(run ([0-9a-f]{16})\)$`).
		FindStringSubmatch(lastLinePrefixed(t, out, "verdict: "))
	if m == nil {
		t.Fatalf("verdict line does not match the run-id + verdict shape:\n%s", out)
	}
	cliVerdict, runID := m[1], m[2]

	// Hop 1: the journal — the verdict record agrees with the CLI, and
	// the fault cell's own line printed the no_op verdict too.
	jb, err := os.ReadFile(filepath.Join(dir, "battery.jsonl"))
	if err != nil {
		t.Fatalf("journal: %v", err)
	}
	var journalVerdict string
	sawCellNoOp := false
	for i, ln := range strings.Split(strings.TrimRight(string(jb), "\n"), "\n") {
		var rec struct {
			Type    string            `json:"type"`
			RunID   string            `json:"run_id"`
			Verdict string            `json:"verdict"`
			Fields  map[string]string `json:"fields"`
		}
		if err := json.Unmarshal([]byte(ln), &rec); err != nil {
			t.Fatalf("journal line %d: %v", i, err)
		}
		if rec.RunID != runID {
			t.Errorf("journal record %d run_id %q, want %q", i, rec.RunID, runID)
		}
		if rec.Type == "verdict" {
			journalVerdict = rec.Verdict
		}
	}
	if journalVerdict != cliVerdict {
		t.Errorf("AC-9 violated: journal verdict %q, CLI printed %q", journalVerdict, cliVerdict)
	}
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "P-001") && strings.Contains(ln, "no_op") {
			sawCellNoOp = true
		}
	}
	if !sawCellNoOp {
		t.Errorf("the failed cell's CLI line does not carry its no_op verdict:\n%s", out)
	}

	// Hop 3: the findings row — the emitted row carries the run id and
	// the cell's no_op verdict, in the board's vocabulary.
	fb, err := os.ReadFile(filepath.Join(dir, "findings.jsonl"))
	if err != nil {
		t.Fatalf("findings: %v", err)
	}
	if err := batteryValidateRowsForTest(fb); err != nil {
		t.Fatalf("emitted findings failed the row validator: %v", err)
	}
	var row struct {
		ID      string `json:"id"`
		RunID   string `json:"run_id"`
		Verdict string `json:"verdict"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(fb[:len(fb)-1], &row); err != nil {
		t.Fatalf("findings row: %v (%s)", err, fb)
	}
	if row.RunID != runID {
		t.Errorf("AC-9 hop 3: findings run_id %q, want %q", row.RunID, runID)
	}
	if row.Verdict != "no_op" {
		t.Errorf("AC-9 hop 3: findings verdict %q, want no_op", row.Verdict)
	}
	if row.Status != "pending" {
		t.Errorf("findings row status %q, want pending", row.Status)
	}
	// The aggregate on an all-no_op run is no_op: the journal hop and the
	// CLI hop must agree on it (asserted above via journalVerdict).
	if cliVerdict != "no_op" {
		t.Errorf("aggregate verdict %q, want no_op (single failed cell)", cliVerdict)
	}
}

// lastLinePrefixed returns the LAST line of out starting with prefix.
func lastLinePrefixed(t *testing.T, out, prefix string) string {
	t.Helper()
	var line string
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, prefix) {
			line = ln
		}
	}
	if line == "" {
		t.Fatalf("no line starting %q in:\n%s", prefix, out)
	}
	return line
}

// batteryValidateRowsForTest runs the battery package's row validator on
// emitted findings bytes (the same self-check the CLI verb runs).
func batteryValidateRowsForTest(b []byte) error {
	return battery.ValidateRows(b)
}
