package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// profile_test.go — the `profile run` CLI contract: usage refusals land
// nothing, --json puts ONE parseable document on stdout and every human note on
// stderr, and the process exit code is the graded outcome (0/1/3) with
// refusals at 2.

// sanctionEnv sanctions the test process and points the marker at a temp file
// carrying a reason, which is exactly what an ephemeral host does.
func sanctionEnv(t *testing.T) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "sanction")
	if err := os.WriteFile(marker, []byte("reason: unit test — profile run on an L0 scratch target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MISCHIEF_SANCTION", "1")
	t.Setenv("MISCHIEF_SANCTION_FILE", marker)
}

// unsanctionEnv guarantees the sanction gate refuses: no env, no marker.
func unsanctionEnv(t *testing.T) {
	t.Helper()
	t.Setenv("MISCHIEF_SANCTION", "")
	t.Setenv("MISCHIEF_SANCTION_FILE", filepath.Join(t.TempDir(), "absent-marker"))
}

func TestProfileUsageRefusals(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no subcommand", []string{"profile"}, "profile — apply a testing profile"},
		{"unknown subcommand", []string{"profile", "nope"}, `unknown subcommand "nope"`},
		{"run without id", []string{"profile", "run"}, "missing profile id"},
		{"run without target", []string{"profile", "run", "storage/short-write"}, "missing --target"},
		{"run with a malformed id", []string{"profile", "run", "short-write", "--target", "scratch:/tmp/x"}, "must be <class>/<name>"},
		{"run with two positionals", []string{"profile", "run", "a/b", "c/d", "--target", "scratch:/tmp/x"}, "unexpected argument"},
		{"run with an unknown flag", []string{"profile", "run", "storage/short-write", "--target", "scratch:/tmp/x", "--turbo"}, "flag provided but not defined"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			unsanctionEnv(t)
			code, stderr := captureStderr(t, func() int { return run(c.args) })
			if code != 2 {
				t.Fatalf("exit = %d (want 2); stderr=%s", code, stderr)
			}
			if !strings.Contains(stderr, c.want) {
				t.Fatalf("stderr %q does not name %q", stderr, c.want)
			}
		})
	}
}

func TestProfileRunRefusesWithoutSanction(t *testing.T) {
	unsanctionEnv(t)
	code, stderr := captureStderr(t, func() int {
		return run([]string{"profile", "run", "storage/short-write", "--target", "scratch:/tmp/x", "--json", "--dir", t.TempDir()})
	})
	if code != 2 {
		t.Fatalf("exit = %d (want 2: the sanction gate refuses)", code)
	}
	if !strings.Contains(stderr, "rails:") && !strings.Contains(stderr, "sanction") {
		t.Fatalf("refusal does not name the sanction rail: %s", stderr)
	}
}

func TestProfileRunMissingProfileRefuses(t *testing.T) {
	sanctionEnv(t)
	code, stderr := captureStderr(t, func() int {
		return run([]string{"profile", "run", "storage/nope", "--target", "scratch:/tmp/x", "--json", "--dir", t.TempDir()})
	})
	if code != 2 {
		t.Fatalf("exit = %d (want 2)", code)
	}
	if !strings.Contains(stderr, "profile not found") {
		t.Fatalf("stderr = %s", stderr)
	}
}

// TestProfileRunJSONIsTheOnlyStdoutByte is the acceptance-critical CLI test:
// stdout must be one JSON document and nothing else, and the exit code must be
// the graded outcome.
func TestProfileRunJSONIsTheOnlyStdoutByte(t *testing.T) {
	sanctionEnv(t)
	dir := t.TempDir()
	target := filepath.Join(os.TempDir(), "mischief-profile-target-cli")
	code, stdout := captureStdout(t, func() int {
		return run([]string{"profile", "run", "storage/short-write", "--target", "scratch:" + target, "--json", "--seed", "7", "--dir", dir})
	})
	if code != 0 {
		t.Fatalf("exit = %d (want 0 passed); stdout=%s", code, stdout)
	}
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not one JSON document:\n%s", stdout)
	}
	var f struct {
		Schema  string `json:"schema"`
		Profile string `json:"profile"`
		Outcome string `json:"outcome"`
		Seed    int64  `json:"seed"`
		RunID   string `json:"run_id"`
		RunDir  string `json:"run_dir"`
		Steps   []struct {
			ID      string `json:"id"`
			Landed  bool   `json:"landed"`
			Outcome string `json:"outcome"`
			Probe   *struct {
				Passed bool `json:"passed"`
			} `json:"probe"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(stdout), &f); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.Schema != "mischief.finding/v1" || f.Profile != "storage/short-write" || f.Outcome != "passed" {
		t.Fatalf("finding = %+v", f)
	}
	if f.Seed != 7 || f.RunID == "" {
		t.Fatalf("seed/run id = %d / %q", f.Seed, f.RunID)
	}
	if len(f.Steps) != 1 || !f.Steps[0].Landed || f.Steps[0].Probe == nil || !f.Steps[0].Probe.Passed {
		t.Fatalf("steps = %+v", f.Steps)
	}
	// The run dir the finding names must be the one that exists, and it must
	// carry the journal + finding + injection log.
	if _, err := os.Stat(filepath.Join(f.RunDir, "profile.jsonl")); err != nil {
		t.Fatalf("journal missing from %s: %v", f.RunDir, err)
	}
	if _, err := os.Stat(filepath.Join(f.RunDir, "finding.json")); err != nil {
		t.Fatalf("finding.json missing from %s: %v", f.RunDir, err)
	}
	if _, err := os.Stat(filepath.Join(f.RunDir, "shim", "libshortwrite.c")); err != nil {
		t.Fatalf("injection log missing from %s: %v", f.RunDir, err)
	}
	// Every human note went to stderr: nothing outside the JSON document.
	if !json.Valid([]byte(stdout)) || strings.HasPrefix(strings.TrimSpace(stdout), "profile") {
		t.Fatalf("stdout carries non-JSON text:\n%s", stdout)
	}
}

// TestProfileRunUnknownPrimitiveIsInconclusiveAsJSON pins the negative case end
// to end: rc=3, stdout is one valid JSON finding, and the reason names the
// missing primitive.
func TestProfileRunUnknownPrimitiveIsInconclusiveAsJSON(t *testing.T) {
	sanctionEnv(t)
	pdir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(pdir, "storage"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `schema: mischief.profile/v1
name: missing-primitive
class: storage
version: 1.0.0
steps:
  - id: s1
    fault: Z-999
    probe: payload-complete
`
	if err := os.WriteFile(filepath.Join(pdir, "storage", "missing-primitive.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout := captureStdout(t, func() int {
		return run([]string{"profile", "run", "storage/missing-primitive",
			"--target", "scratch:/tmp/mischief-profile-target-neg", "--json",
			"--dir", t.TempDir(), "--profiles-dir", pdir})
	})
	if code != 3 {
		t.Fatalf("exit = %d (want 3 inconclusive); stdout=%s", code, stdout)
	}
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON:\n%s", stdout)
	}
	var f struct {
		Outcome string `json:"outcome"`
		Reason  string `json:"reason"`
		Steps   []struct {
			Outcome string `json:"outcome"`
			Reason  string `json:"reason"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(stdout), &f); err != nil {
		t.Fatal(err)
	}
	if f.Outcome != "inconclusive" {
		t.Fatalf("outcome = %q", f.Outcome)
	}
	if !strings.Contains(f.Reason, "Z-999") || !strings.Contains(f.Reason, "not in the loaded catalog") {
		t.Fatalf("reason = %q", f.Reason)
	}
	if len(f.Steps) != 1 || f.Steps[0].Outcome != "inconclusive" {
		t.Fatalf("steps = %+v", f.Steps)
	}
}

// TestProfileRunUnattachableTargetIsInconclusive pins that a process selector
// (which this build cannot attach to) is a graded inconclusive, not a refusal
// and not a pass.
func TestProfileRunUnattachableTargetIsInconclusive(t *testing.T) {
	sanctionEnv(t)
	code, stdout := captureStdout(t, func() int {
		return run([]string{"profile", "run", "storage/short-write", "--target", "process:1", "--json", "--dir", t.TempDir()})
	})
	if code != 3 {
		t.Fatalf("exit = %d (want 3); stdout=%s", code, stdout)
	}
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON:\n%s", stdout)
	}
}

// TestProfileRunTextModeWritesNoJSON pins the non --json path: a human report
// that still names the outcome, and no JSON document on stdout.
func TestProfileRunTextMode(t *testing.T) {
	sanctionEnv(t)
	target := filepath.Join(os.TempDir(), "mischief-profile-target-text")
	code, stdout := captureStdout(t, func() int {
		return run([]string{"profile", "run", "storage/short-write", "--target", "scratch:" + target, "--dir", t.TempDir()})
	})
	if code != 0 {
		t.Fatalf("exit = %d; stdout=%s", code, stdout)
	}
	if json.Valid([]byte(stdout)) {
		t.Fatalf("text mode emitted a JSON document:\n%s", stdout)
	}
	if !strings.Contains(stdout, "finding: passed") {
		t.Fatalf("text report does not name the outcome:\n%s", stdout)
	}
	if !strings.Contains(stdout, "profile: storage/short-write") {
		t.Fatalf("text report does not name the profile:\n%s", stdout)
	}
}

func TestProfileList(t *testing.T) {
	code, stdout := captureStdout(t, func() int { return run([]string{"profile", "list"}) })
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "storage/short-write") {
		t.Fatalf("list output = %q", stdout)
	}
}

func TestProfileHelp(t *testing.T) {
	for _, args := range [][]string{{"profile", "--help"}, {"profile", "help"}, {"profile", "run", "--help"}} {
		code, out := captureStdout(t, func() int { return run(args) })
		if code != 0 {
			t.Fatalf("%v exit = %d", args, code)
		}
		if out != "" && !strings.Contains(out, "mischief profile") && !strings.Contains(out, "usage: mischief profile run") {
			t.Fatalf("%v output = %q", args, out)
		}
	}
}

// findingHeader is the shape a CLI test needs from a finding.json/stdout doc.
type findingHeader struct {
	Seed   int64  `json:"seed"`
	RunID  string `json:"run_id"`
	RunDir string `json:"run_dir"`
}

// TestProfileCompareTwoRunsSameSeedIsDeterministic is the PROF-031
// acceptance, end to end against the REAL actuator:
//
//  1. the same profile + same seed, run into two different --dir roots,
//     writes byte-identical journals (the diff between the two injection logs
//     is empty — the seed alone decides injection order/timing);
//  2. `profile compare <run_dir_a> <run_dir_b>` on the two dirs reports the
//     transitions (here: unchanged, both runs pass on an unchanged target).
func TestProfileCompareTwoRunsSameSeedIsDeterministic(t *testing.T) {
	sanctionEnv(t)
	target := filepath.Join(os.TempDir(), fmt.Sprintf("mischief-profile-target-cmp-%d", os.Getpid()))
	root := t.TempDir()
	runOnce := func(dir string) findingHeader {
		t.Helper()
		code, stdout := captureStdout(t, func() int {
			return run([]string{"profile", "run", "storage/short-write",
				"--target", "scratch:" + target, "--json", "--seed", "21", "--dir", dir})
		})
		if code != 0 {
			t.Fatalf("exit = %d (want 0 passed); stdout=%s", code, stdout)
		}
		var f findingHeader
		if err := json.Unmarshal([]byte(stdout), &f); err != nil {
			t.Fatalf("decode finding: %v\n%s", err, stdout)
		}
		if f.RunID == "" || f.RunDir == "" {
			t.Fatalf("finding header = %+v", f)
		}
		return f
	}
	dirA := filepath.Join(root, "before")
	dirB := filepath.Join(root, "after")
	fA := runOnce(dirA)
	fB := runOnce(dirB)
	if fA.RunID != fB.RunID {
		t.Fatalf("same bytes + seed must derive the same run id: %q vs %q", fA.RunID, fB.RunID)
	}

	// The injection log byte-diff between the two run dirs must be EMPTY:
	// the journal IS the diffed artifact (finding.json is not compared — it
	// carries the wall-clock bounds and the absolute run_dir, which SHOULD
	// differ between two --dir roots).
	for _, rel := range []string{"profile.jsonl", "shim/writer.c", "shim/libshortwrite.c", "shim/shortwrite.counter"} {
		ba, err := os.ReadFile(filepath.Join(fA.RunDir, rel))
		if err != nil {
			t.Fatalf("run a %s: %v", rel, err)
		}
		bb, err := os.ReadFile(filepath.Join(fB.RunDir, rel))
		if err != nil {
			t.Fatalf("run b %s: %v", rel, err)
		}
		if string(ba) != string(bb) {
			t.Fatalf("%s differs between the two same-seed runs (a=%d bytes, b=%d bytes)\nA: %s\nB: %s",
				rel, len(ba), len(bb), ba, bb)
		}
	}

	// Compare reports the transitions (both passed here: unchanged).
	code, stdout := captureStdout(t, func() int {
		return run([]string{"profile", "compare", fA.RunDir, fB.RunDir})
	})
	if code != 0 {
		t.Fatalf("compare exit = %d; stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "unchanged (passed)") || !strings.Contains(stdout, "payload-complete") {
		t.Fatalf("compare output does not name the unchanged transition:\n%s", stdout)
	}
	if !strings.Contains(stdout, "0 changed") {
		t.Fatalf("compare counts must say 0 changed:\n%s", stdout)
	}

	// --json mode: ONE parseable document on stdout, the run ids it compared.
	code, stdout = captureStdout(t, func() int {
		return run([]string{"profile", "compare", fA.RunDir, fB.RunDir, "--json"})
	})
	if code != 0 {
		t.Fatalf("compare --json exit = %d; stdout=%s", code, stdout)
	}
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("compare --json stdout is not one JSON document:\n%s", stdout)
	}
	var rep struct {
		Schema  string `json:"schema"`
		Profile string `json:"profile"`
		RunIDA  string `json:"run_id_a"`
		RunIDB  string `json:"run_id_b"`
		Steps   []struct {
			Step       string `json:"step"`
			Probe      string `json:"probe"`
			Transition string `json:"transition"`
			Changed    bool   `json:"changed"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Schema != "mischief.compare/v1" || rep.Profile != "storage/short-write" ||
		rep.RunIDA != fA.RunID || rep.RunIDB != fB.RunID {
		t.Fatalf("compare report = %+v", rep)
	}
	if len(rep.Steps) != 1 || rep.Steps[0].Probe != "payload-complete" || rep.Steps[0].Transition != "unchanged" || rep.Steps[0].Changed {
		t.Fatalf("steps = %+v", rep.Steps)
	}

	// The compare verdict record: the journal disclosed the deterministic
	// clock, so the two runs' run_planned records name it.
	if !strings.Contains(string(mustRead(t, fA.RunDir, "profile.jsonl")), "journal_clock") {
		t.Fatal("run_planned must disclose the seed-derived journal clock")
	}
}

func mustRead(t *testing.T, dir, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestProfileCompareRefusals pins the compare verb's refusal arms: bad
// invocation and unreadable run dirs land nothing and exit 2.
func TestProfileCompareRefusals(t *testing.T) {
	unsanctionEnv(t) // the compare does NOT gate on sanction: it reads run dirs only
	code, stderr := captureStderr(t, func() int {
		return run([]string{"profile", "compare"})
	})
	if code != 2 {
		t.Fatalf("exit = %d (want 2, usage refusal); stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "want exactly two run dirs") {
		t.Fatalf("stderr = %q", stderr)
	}
	code, stderr = captureStderr(t, func() int {
		return run([]string{"profile", "compare", filepath.Join(t.TempDir(), "absent-a"), filepath.Join(t.TempDir(), "absent-b")})
	})
	if code != 2 {
		t.Fatalf("exit = %d (want 2, unreadable run dirs); stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "journal") {
		t.Fatalf("stderr must name the journal read failure: %q", stderr)
	}
}
