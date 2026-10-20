package main

// ch:trace row=MISCHIEF-DF-37 spec=docs/prd/mischief-v0.1.md (AC-1) test=cmd/mischief/plan_journal_test.go evidence=cmd/mischief/plan.go witness=none:temp-root-plan-runs-no-live-target

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/journal"
)

// planDF37Fixture builds the self-contained catalog + experiment the plan
// journal arms use: one L0 primitive (capability: none) that plans clean on
// this host, so the acceptance arm never depends on repo fixture paths.
func planDF37Fixture(t *testing.T) (catalogDir, exp string) {
	t.Helper()
	catalogDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(catalogDir, "n-012.yaml"), []byte(planScratchDirDescriptor), 0o644); err != nil {
		t.Fatal(err)
	}
	exp = filepath.Join(t.TempDir(), "exp.yaml")
	if err := os.WriteFile(exp, []byte(planScratchDirExperiment), 0o644); err != nil {
		t.Fatal(err)
	}
	return catalogDir, exp
}

// runIDFromRender reads the run_id line the plan render printed — the id
// MISCHIEF-DF-37 found NOWHERE on disk.
func runIDFromRender(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if id, ok := strings.CutPrefix(line, "run_id: "); ok {
			return strings.TrimSpace(id)
		}
	}
	t.Fatalf("plan render printed no run_id line:\n%s", out)
	return ""
}

// readPlanJournalLines reads <dir>/plan.jsonl and returns its non-empty
// lines, failing when the file is missing (the AC-1 evidence half).
func readPlanJournalLines(t *testing.T, dir string) []string {
	t.Helper()
	path := filepath.Join(dir, planJournalFile)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("plan journal %s: %v (plan wrote no journal entry)", path, err)
	}
	lines := []string{}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// TestPlanJournalDirRecordsPlanAction (MISCHIEF-DF-37, AC-1 + AC-2): with
// --journal-dir D the verb writes a journal entry into the run dir, and the
// entry RECORDS THE PLAN ACTION — the run_id the render printed, the
// experiment, the resolved fault set, the target, and the refusal tally —
// read back through the journal package's own validation.
func TestPlanJournalDirRecordsPlanAction(t *testing.T) {
	planScratchDirSanctioned(t)
	catalogDir, exp := planDF37Fixture(t)
	dir := t.TempDir()

	code, out := captureStdout(t, func() int {
		return run([]string{"plan", "-f", exp, "--catalog-dir", catalogDir, "--journal-dir", dir})
	})
	if code != 0 {
		t.Fatalf("plan --journal-dir D: exit %d, want 0\n%s", code, out)
	}
	printed := runIDFromRender(t, out)

	lines := readPlanJournalLines(t, dir)
	if len(lines) != 1 {
		t.Fatalf("plan journal lines = %d, want 1 (one plan, one record)\n%v", len(lines), lines)
	}
	// The journal's own read-back validation: an unknown type, a missing
	// run id or an out-of-vocabulary verdict fails here, so a record that
	// passes is a record a run journal reader can read unchanged.
	var rec journal.Record
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("plan journal line does not read back as a journal record: %v\n%s", err, lines[0])
	}
	if rec.Type != journal.RecordRunPlanned {
		t.Errorf("record type = %q, want %q (the journal's own type for the resolved plan)", rec.Type, journal.RecordRunPlanned)
	}
	if rec.RunID != printed {
		t.Errorf("record run_id = %q, want the printed run_id %q (the DF's complaint: the id appeared nowhere on disk)", rec.RunID, printed)
	}
	if rec.Seq != 0 {
		t.Errorf("record seq = %d, want 0 (one act, one record)", rec.Seq)
	}
	if rec.TS == 0 {
		t.Error("record ts = 0, want the write time")
	}
	if rec.Verdict != "" {
		t.Errorf("record carries verdict %q — a plan makes no verdict claim", rec.Verdict)
	}
	wantFields := map[string]string{
		"kind":            "plan",
		"mode":            "preview",
		"experiment":      "scratch-dir-plan",
		"target_selector": "host:.",
		"faults":          "N-012",
		"faults_count":    "1",
		"refusals":        "0",
	}
	for k, want := range wantFields {
		if got := rec.Fields[k]; got != want {
			t.Errorf("record field %s = %q, want %q (fields=%v)", k, got, want, rec.Fields)
		}
	}
	if rec.Fields["seed"] != "7" {
		t.Errorf("record seed = %q, want \"7\"", rec.Fields["seed"])
	}
	if rec.Fields["catalog_version"] == "" {
		t.Errorf("record carries no catalog_version (the plan resolved against one): %v", rec.Fields)
	}
	// The verb names the file it wrote, so an operator can find it.
	if !strings.Contains(out, "journal: "+filepath.Join(dir, planJournalFile)) {
		t.Errorf("plan output does not name the journal it wrote:\n%s", out)
	}
	if !strings.Contains(out, printed) {
		t.Errorf("plan output does not carry the recorded run id %s:\n%s", printed, out)
	}
}

// TestPlanJournalDirJSONKeepsStdoutMachineReadable: --json is a machine
// surface — the journal note must NOT be appended to the JSON payload (it
// goes to stderr there; the run_id is already in the JSON).
func TestPlanJournalDirJSONKeepsStdoutMachineReadable(t *testing.T) {
	planScratchDirSanctioned(t)
	catalogDir, exp := planDF37Fixture(t)
	dir := t.TempDir()

	var code int
	code, out, errOut := captureBoth(t, func() int {
		return run([]string{"plan", "-f", exp, "--catalog-dir", catalogDir, "--journal-dir", dir, "--json"})
	})
	if code != 0 {
		t.Fatalf("plan --json --journal-dir D: exit %d, want 0\n%s", code, errOut)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("plan --json stdout is not pure JSON (%v):\n%s", err, out)
	}
	if got, _ := payload["run_id"].(string); got == "" {
		t.Fatalf("plan --json payload carries no run_id: %s", out)
	}
	if !strings.Contains(errOut, "journal: ") {
		t.Errorf("the journal note did not reach stderr in --json mode: %q", errOut)
	}
	if len(readPlanJournalLines(t, dir)) != 1 {
		t.Error("--json mode wrote no journal entry")
	}
}

// TestPlanJournalDirAppendsAcrossRuns: the journal is append-only — a second
// plan of the same experiment adds a record rather than truncating the file,
// and the content-derived run id is identical across the two replays (AC-10
// reaches the plan surface too).
func TestPlanJournalDirAppendsAcrossRuns(t *testing.T) {
	planScratchDirSanctioned(t)
	catalogDir, exp := planDF37Fixture(t)
	dir := t.TempDir()

	var ids []string
	for i := 0; i < 2; i++ {
		code, out := captureStdout(t, func() int {
			return run([]string{"plan", "-f", exp, "--catalog-dir", catalogDir, "--journal-dir", dir})
		})
		if code != 0 {
			t.Fatalf("plan run %d: exit %d, want 0\n%s", i+1, code, out)
		}
		ids = append(ids, runIDFromRender(t, out))
	}
	lines := readPlanJournalLines(t, dir)
	if len(lines) != 2 {
		t.Fatalf("plan journal lines after two runs = %d, want 2 (append-only, never truncate)", len(lines))
	}
	for i, l := range lines {
		var rec journal.Record
		if err := json.Unmarshal([]byte(l), &rec); err != nil {
			t.Fatalf("line %d does not read back: %v", i, err)
		}
		if rec.RunID != ids[i] {
			t.Errorf("line %d run_id = %q, want the run's printed id %q", i, rec.RunID, ids[i])
		}
	}
	if ids[0] != ids[1] {
		t.Errorf("run ids differ across identical replays: %q vs %q", ids[0], ids[1])
	}
}

// TestPlanJournalRecordOnRefusedPlan: a plan the rails refuse still renders,
// still exits 2, and — when a journal dir was requested — RECORDS the refused
// preview (refusal count + the closed taxonomy reasons), so an operator can
// audit a refusal the same way they audit a clean plan.
func TestPlanJournalRecordOnRefusedPlan(t *testing.T) {
	planScratchDirSanctioned(t)
	catalogDir, exp := planDF37Fixture(t)
	// The experiment declares a primitive the catalog does not hold: the
	// plan renders the typed unknown-primitive row and a rails refusal.
	refused := strings.Replace(planScratchDirExperiment, "primitive: N-012", "primitive: Z-999", 1)
	exp = filepath.Join(t.TempDir(), "refused.yaml")
	if err := os.WriteFile(exp, []byte(refused), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	code, out := captureStdout(t, func() int {
		return run([]string{"plan", "-f", exp, "--catalog-dir", catalogDir, "--journal-dir", dir})
	})
	if code != 2 {
		t.Fatalf("refused plan: exit %d, want 2 (the rails exit contract)\n%s", code, out)
	}
	lines := readPlanJournalLines(t, dir)
	if len(lines) != 1 {
		t.Fatalf("refused plan journal lines = %d, want 1", len(lines))
	}
	var rec journal.Record
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("refused-plan record does not read back: %v", err)
	}
	if rec.Type != journal.RecordRunPlanned {
		t.Errorf("record type = %q, want %q", rec.Type, journal.RecordRunPlanned)
	}
	if rec.Fields["refusals"] != "1" {
		t.Errorf("record refusals = %q, want \"1\" (fields=%v)", rec.Fields["refusals"], rec.Fields)
	}
	if rec.Fields["refusal_reasons"] == "" {
		t.Errorf("record carries no refusal_reasons: %v", rec.Fields)
	}
	if rec.Fields["faults"] != "Z-999" {
		t.Errorf("record faults = %q, want the declared primitive \"Z-999\"", rec.Fields["faults"])
	}
}

// TestPlanWithoutJournalDirWritesNothing (AC-1's shipped-invocation half, and
// the README claim made testable): without --journal-dir the verb is
// side-effect free — no run dir, no journal, nothing under HOME. This is the
// regression arm for MISCHIEF-DF-37's fix direction: the record is opt-in by
// construction, so a future change that makes `plan` journal unconditionally
// fails here instead of silently falsifying AC-1.
func TestPlanWithoutJournalDirWritesNothing(t *testing.T) {
	planScratchDirSanctioned(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	catalogDir, exp := planDF37Fixture(t)

	code, out := captureStdout(t, func() int {
		return run([]string{"plan", "-f", exp, "--catalog-dir", catalogDir})
	})
	if code != 0 {
		t.Fatalf("plan without --journal-dir: exit %d, want 0\n%s", code, out)
	}
	if strings.Contains(out, "journal: ") {
		t.Errorf("plan without --journal-dir claims it wrote a journal:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".mischief")); !os.IsNotExist(err) {
		t.Errorf("plan created %s without --journal-dir (AC-1 violated)", filepath.Join(home, ".mischief"))
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("plan wrote %v under HOME without --journal-dir (AC-1: no run dir, no journal)", names)
	}
}

// TestPlanJournalWriteFailureIsLoud: a requested record that cannot be
// written is a FAILED write, not a footnote — the verb names the failure on
// stderr and exits non-zero rather than reporting a clean plan whose
// evidence is missing.
func TestPlanJournalWriteFailureIsLoud(t *testing.T) {
	planScratchDirSanctioned(t)
	catalogDir, exp := planDF37Fixture(t)
	// A regular file where the run dir should be: MkdirAll fails, so the
	// write cannot proceed and MAX_PATH/EACCES shape is deterministic.
	blocked := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	var code int
	code, _, errOut := captureBoth(t, func() int {
		return run([]string{"plan", "-f", exp, "--catalog-dir", catalogDir, "--journal-dir", blocked})
	})
	if code == 0 {
		t.Fatalf("plan --journal-dir <unwritable>: exit 0, want non-zero (the requested record is missing)")
	}
	if !strings.Contains(errOut, "journal write failed") {
		t.Errorf("plan did not name the failed journal write on stderr: %q", errOut)
	}
}
