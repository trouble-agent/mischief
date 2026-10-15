package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/plane"
)

// plane_test.go — the `mischief plane` verb contract (MSF-021): the
// evidence CLI the bunker-plane driver delegates to.

// capturePlaneOutput runs fn capturing both streams (the plane verb
// writes records' human text to stdout and refusals to stderr).
func capturePlaneOutput(t *testing.T, fn func() int) (code int, stdout, stderr string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	defer func() {
		os.Stdout, os.Stderr = oldOut, oldErr
	}()
	code = fn()
	wOut.Close()
	wErr.Close()
	outBody := make(chan string, 1)
	errBody := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(rOut)
		outBody <- string(b)
	}()
	go func() {
		b, _ := io.ReadAll(rErr)
		errBody <- string(b)
	}()
	return code, <-outBody, <-errBody
}

func TestPlaneVerbDispatchAndRefusals(t *testing.T) {
	if code := run([]string{"plane"}); code != 2 {
		t.Fatalf("bare plane exit %d, want 2", code)
	}
	if code := run([]string{"plane", "teleport"}); code != 2 {
		t.Fatalf("unknown subcommand exit %d, want 2", code)
	}
	// record without required flags
	if code := run([]string{"plane", "record"}); code != 2 {
		t.Fatalf("flagless record exit %d, want 2", code)
	}
	// record with a bogus status is a usage refusal, not a bad row
	code, _, stderr := capturePlaneOutput(t, func() int {
		return run([]string{"plane", "record", "--file", filepath.Join(t.TempDir(), "ev.jsonl"), "--step", "spawn", "--status", "maybe"})
	})
	if code != 2 {
		t.Fatalf("bogus status exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "unknown --status") {
		t.Fatalf("stderr %q does not name the status refusal", stderr)
	}
	// selftest-verdict without --exit-code
	if code := run([]string{"plane", "selftest-verdict"}); code != 2 {
		t.Fatalf("flagless selftest-verdict exit %d, want 2", code)
	}
}

func TestPlaneParseAgentIDSubcommand(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "spawn.txt")
	if err := os.WriteFile(in, []byte("agent created\n  id: keys/cefd6920\nttl: 4h\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := capturePlaneOutput(t, func() int {
		return run([]string{"plane", "parse-agent-id", "--in", in})
	})
	if code != 0 || strings.TrimSpace(out) != "cefd6920" {
		t.Fatalf("parse-agent-id = (%d, %q), want (0, cefd6920)", code, out)
	}

	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, []byte("capacity exhausted (8/8)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "parse-agent-id", "--in", empty})
	})
	// Absence is DATA: exit 0 with an empty id line.
	if code != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("absent id = (%d, %q), want (0, \"\")", code, out)
	}

	// Missing input file is a real error (1), not silence.
	code, _, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "parse-agent-id", "--in", filepath.Join(dir, "nope.txt")})
	})
	if code != 1 {
		t.Fatalf("missing input exit %d, want 1", code)
	}
}

func TestPlaneRecordAppendsJSONLRows(t *testing.T) {
	ev := filepath.Join(t.TempDir(), "ev.jsonl")
	code, _, _ := capturePlaneOutput(t, func() int {
		return run([]string{"plane", "record", "--file", ev, "--step", "spawn", "--status", "skip", "--detail", "capacity exhausted (8/8)"})
	})
	if code != 0 {
		t.Fatalf("record exit %d", code)
	}
	// A second row must APPEND, not truncate.
	code, _, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "record", "--file", ev, "--step", "preflight", "--status", "pass", "--detail", "server reachable"})
	})
	if code != 0 {
		t.Fatalf("second record exit %d", code)
	}
	body, err := os.ReadFile(ev)
	if err != nil {
		t.Fatal(err)
	}
	steps, dropped, err := plane.ParseEvidence(string(body))
	if err != nil || dropped != 0 || len(steps) != 2 {
		t.Fatalf("evidence parse: %v steps=%d dropped=%d", err, len(steps), dropped)
	}
	if steps[0].Step != "spawn" || steps[0].Status != plane.StatusSkip || steps[0].Detail() != "capacity exhausted (8/8)" {
		t.Fatalf("row 0: %+v detail=%q", steps[0], steps[0].Detail())
	}
	if steps[1].Step != "preflight" || steps[1].TS == "" {
		t.Fatalf("row 1 missing fields: %+v", steps[1])
	}
}

func TestPlaneSelftestVerdictSubcommand(t *testing.T) {
	code, out, _ := capturePlaneOutput(t, func() int {
		return run([]string{"plane", "selftest-verdict", "--exit-code", "0"})
	})
	if code != 0 || strings.TrimSpace(out) != plane.StatusPass {
		t.Fatalf("exit 0 → (%d,%q), want pass", code, out)
	}
	code, out, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "selftest-verdict", "--exit-code", "137"})
	})
	if code != 0 || strings.TrimSpace(out) != plane.StatusFail {
		t.Fatalf("exit 137 → (%d,%q), want fail", code, out)
	}
}

func TestPlaneFoldSubcommand(t *testing.T) {
	dir := t.TempDir()

	// Empty file: named FAIL, exit 1.
	ev := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(ev, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := capturePlaneOutput(t, func() int {
		return run([]string{"plane", "fold", "--file", ev})
	})
	if code != 1 || !strings.Contains(out, "PLANE fail") || !strings.Contains(out, "empty evidence") {
		t.Fatalf("empty fold = (%d, %q)", code, out)
	}

	// Unreadable file: named FAIL, exit 1 — not a usage error.
	code, out, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "fold", "--file", filepath.Join(dir, "absent.jsonl")})
	})
	if code != 1 || !strings.Contains(out, "unreadable") {
		t.Fatalf("absent fold = (%d, %q)", code, out)
	}

	// A reasonless skip still grades skip (exit 3) but names the defect.
	evSkip := filepath.Join(dir, "skip.jsonl")
	code, _, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "record", "--file", evSkip, "--step", "spawn", "--status", "skip"})
	})
	if code != 0 {
		t.Fatalf("skip record exit %d", code)
	}
	code, out, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "fold", "--file", evSkip})
	})
	if code != 3 || !strings.Contains(out, "PLANE skip") || !strings.Contains(out, "plane defect") {
		t.Fatalf("reasonless-skip fold = (%d, %q)", code, out)
	}

	// The green pipeline: every stage pass → PLANE pass, exit 0.
	evGreen := filepath.Join(dir, "green.jsonl")
	for _, row := range [][2]string{
		{"preflight", "server reachable"},
		{"ship", "SYNC-OK"},
		{"selftest", "exit 0"},
		{"collect", "journal pulled"},
		{"destroy", "TEARDOWN-GONE"},
	} {
		code, _, _ = capturePlaneOutput(t, func() int {
			return run([]string{"plane", "record", "--file", evGreen, "--step", row[0], "--status", "pass", "--detail", row[1]})
		})
		if code != 0 {
			t.Fatalf("green row %v exit %d", row, code)
		}
	}
	code, out, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "fold", "--file", evGreen})
	})
	if code != 0 || !strings.Contains(out, "PLANE pass at destroy") {
		t.Fatalf("green fold = (%d, %q)", code, out)
	}

	// stdin path (--file -): the driver's pipe-shaped grading arm.
	stdinFile, err := os.Open(evGreen)
	if err != nil {
		t.Fatal(err)
	}
	defer stdinFile.Close()
	oldStdin := os.Stdin
	os.Stdin = stdinFile
	code, out, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "fold", "--file", "-"})
	})
	os.Stdin = oldStdin
	if code != 0 || !strings.Contains(out, "PLANE pass at destroy") {
		t.Fatalf("stdin fold = (%d, %q)", code, out)
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// MISCHIEF-PLANE-001 regression tests: record refuses an unknown --step at
// the flag surface (exit 2, nothing appended), and fold over a file with
// an alien-stage row never prints/returns PASS — the live repro at HEAD
// b3789e6 was `record --step nonsense --status pass` (rc=0, row appended)
// followed by `fold` printing "PLANE pass at nonsense".
// ═════════════════════════════════════════════════════════════════════════════

// ═════════════════════════════════════════════════════════════════════════════
// MISCHIEF-DF-21 regression tests: the fold's default (empty-final-detail)
// reason must name only what the evidence file actually records. The old
// behavior substituted "all stages green (agent obtained, selftest exit 0,
// evidence pulled, agent destroyed)" for ANY empty-detail final row — a
// spawn-only evidence file graded PASS with a claim about a selftest, a
// collect and a destroy that never ran, indistinguishable from a complete
// run; an empty-detail FAIL row was handed the same green claim.
// ═════════════════════════════════════════════════════════════════════════════

// TestPlaneFoldSpawnOnlyEmptyDetailPassNoOverclaim: a spawn-only evidence
// file (one pass row, no detail — exactly what `record --step spawn
// --status pass` appends) still folds PASS exit 0, but the reason must name
// the recorded stage and mark the pipeline incomplete — never the full
// "all stages green" claim.
func TestPlaneFoldSpawnOnlyEmptyDetailPassNoOverclaim(t *testing.T) {
	dir := t.TempDir()
	ev := filepath.Join(dir, "spawn-only.jsonl")
	code, _, _ := capturePlaneOutput(t, func() int {
		return run([]string{"plane", "record", "--file", ev, "--step", "spawn", "--status", "pass"})
	})
	if code != 0 {
		t.Fatalf("spawn-only record exit %d", code)
	}
	code, out, _ := capturePlaneOutput(t, func() int {
		return run([]string{"plane", "fold", "--file", ev})
	})
	if code != 0 || !strings.Contains(out, "PLANE pass") {
		t.Fatalf("spawn-only fold = (%d, %q), want exit 0 PLANE pass", code, out)
	}
	if strings.Contains(out, "all stages green") {
		t.Fatalf("spawn-only evidence file overclaims the full pipeline: %q", out)
	}
	if !strings.Contains(out, "spawn") {
		t.Fatalf("minimal reason %q must name the recorded stage", out)
	}
	if !strings.Contains(out, "incomplete") {
		t.Fatalf("minimal reason %q must mark the run incomplete (AC: incomplete distinguishable from complete)", out)
	}
}

// TestPlaneFoldCompletePipelineEmptyFinalDetailKeepsFullClaim: a genuinely
// complete evidence file (every vocabulary stage recorded pass, the final
// destroy row carrying no detail) keeps the full pipeline claim — the
// default reason is earned there and only there, so an incomplete run's
// minimal reason stays distinguishable from it.
func TestPlaneFoldCompletePipelineEmptyFinalDetailKeepsFullClaim(t *testing.T) {
	dir := t.TempDir()
	ev := filepath.Join(dir, "full.jsonl")
	var code int
	for _, stage := range plane.StageVocabulary[:len(plane.StageVocabulary)-1] {
		code, _, _ = capturePlaneOutput(t, func() int {
			return run([]string{"plane", "record", "--file", ev, "--step", stage, "--status", "pass", "--detail", "stage recorded"})
		})
		if code != 0 {
			t.Fatalf("record %s exit %d", stage, code)
		}
	}
	code, _, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "record", "--file", ev, "--step", "destroy", "--status", "pass"})
	})
	if code != 0 {
		t.Fatalf("final detail-less destroy record exit %d", code)
	}
	code, out, _ := capturePlaneOutput(t, func() int {
		return run([]string{"plane", "fold", "--file", ev})
	})
	if code != 0 || !strings.Contains(out, "PLANE pass at destroy") {
		t.Fatalf("complete-pipeline fold = (%d, %q), want exit 0 pass at destroy", code, out)
	}
	if !strings.Contains(out, "all stages green (agent obtained, selftest exit 0, evidence pulled, agent destroyed)") {
		t.Fatalf("complete pipeline lost the full claim: %q", out)
	}
}

// TestPlaneFoldEmptyDetailFailNeverClaimsGreen: the default-reason arm must
// never hand the green claim to a FAIL row — the old line overclaimed on
// fail rows too ("PLANE fail at ship: all stages green (…)").
func TestPlaneFoldEmptyDetailFailNeverClaimsGreen(t *testing.T) {
	dir := t.TempDir()
	ev := filepath.Join(dir, "fail.jsonl")
	code, _, _ := capturePlaneOutput(t, func() int {
		return run([]string{"plane", "record", "--file", ev, "--step", "ship", "--status", "fail"})
	})
	if code != 0 {
		t.Fatalf("fail record exit %d", code)
	}
	code, out, _ := capturePlaneOutput(t, func() int {
		return run([]string{"plane", "fold", "--file", ev})
	})
	if code != 1 || !strings.Contains(out, "PLANE fail") {
		t.Fatalf("empty-detail fail fold = (%d, %q), want exit 1 PLANE fail", code, out)
	}
	if strings.Contains(out, "all stages green") {
		t.Fatalf("fail row handed the green claim: %q", out)
	}
	if !strings.Contains(out, "ship") {
		t.Fatalf("minimal reason %q must name the recorded stage", out)
	}
}

// TestPlaneRecordRefusesUnknownStep: the record verb's refusal names the
// offending stage and the allowed set, exits 2 (usage), and appends
// NOTHING to the evidence file.
func TestPlaneRecordRefusesUnknownStep(t *testing.T) {
	ev := filepath.Join(t.TempDir(), "ev.jsonl")
	var code int
	var stderr string
	code, _, stderr = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "record", "--file", ev, "--step", "nonsense", "--status", "pass", "--detail", "bad-step"})
	})
	if code != 2 {
		t.Fatalf("alien-stage record exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "unknown stage") || !strings.Contains(stderr, "nonsense") {
		t.Fatalf("stderr %q must name the unknown stage", stderr)
	}
	if !strings.Contains(stderr, "destroy") {
		t.Fatalf("stderr %q must name the allowed set", stderr)
	}
	if body, err := os.ReadFile(ev); err == nil && len(body) > 0 {
		t.Fatalf("refused record still appended evidence: %q", body)
	}
	// Every canonical stage is still accepted (one probe per stage).
	for _, stage := range plane.StageVocabulary {
		code, _, _ = capturePlaneOutput(t, func() int {
			return run([]string{"plane", "record", "--file", ev, "--step", stage, "--status", "pass", "--detail", "ok"})
		})
		if code != 0 {
			t.Fatalf("canonical stage %q refused by record (exit %d)", stage, code)
		}
	}
}

// TestPlaneFoldAlienStageFileNeverPasses: the fold-level regression — the
// defect file (alien-stage pass rows + a green final row) folds to a named
// FAIL (exit 1) that names the alien stage, never "PLANE pass at
// nonsense".
func TestPlaneFoldAlienStageFileNeverPasses(t *testing.T) {
	dir := t.TempDir()
	// The exact live repro shape: `record --step nonsense --status pass
	// --detail bad-step` (base64 of "bad-step": YmFkLXN0ZXA=) plus a green
	// final destroy row.
	ev := filepath.Join(dir, "alien.jsonl")
	if err := os.WriteFile(ev, []byte(
		`{"step":"nonsense","status":"pass","detail_b64":"YmFkLXN0ZXA=","ts":"0"}`+"\n"+
			`{"step":"destroy","status":"pass","detail_b64":"","ts":"1"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := capturePlaneOutput(t, func() int {
		return run([]string{"plane", "fold", "--file", ev})
	})
	if code != 1 {
		t.Fatalf("alien-stage fold exit %d, want 1 (fail)", code)
	}
	if strings.Contains(out, "PLANE pass") {
		t.Fatalf("alien-stage file folded as PASS: %q", out)
	}
	if !strings.Contains(out, "PLANE fail") || !strings.Contains(out, "nonsense") {
		t.Fatalf("fold output %q must be a named FAIL citing the alien stage", out)
	}

	// Hand-built alien-stage evidence must also refuse at the source: the
	// same file written through `record` is impossible now — proven by the
	// record-refusal test above; here the green pipeline still passes so
	// the closure did not over-tighten.
	evGreen := filepath.Join(dir, "green.jsonl")
	for _, row := range [][2]string{
		{"preflight", "server reachable"},
		{"ship", "SYNC-OK"},
		{"selftest", "exit 0"},
		{"collect", "journal pulled"},
		{"destroy", "TEARDOWN-GONE"},
	} {
		code, _, _ = capturePlaneOutput(t, func() int {
			return run([]string{"plane", "record", "--file", evGreen, "--step", row[0], "--status", "pass", "--detail", row[1]})
		})
		if code != 0 {
			t.Fatalf("green row %v exit %d", row, code)
		}
	}
	code, out, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "fold", "--file", evGreen})
	})
	if code != 0 || !strings.Contains(out, "PLANE pass at destroy") {
		t.Fatalf("green pipeline = (%d, %q), want pass at destroy", code, out)
	}
}
