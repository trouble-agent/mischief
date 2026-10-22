package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVersionVerbPrintsStampedSha: `mischief version` prints exactly the
// stamped sha (the Makefile's -X main.gitSha value). An unstamped build
// prints "unknown" verbatim — never a fabricated hash.
func TestVersionVerbPrintsStampedSha(t *testing.T) {
	gitSha = "abc123def456"
	code, out := captureStdout(t, func() int { return run([]string{"version"}) })
	if code != 0 {
		t.Fatalf("version exit: %d", code)
	}
	if got := strings.TrimSpace(out); got != "mischief abc123def456" {
		t.Fatalf("version output: %q", got)
	}
	// --version alias
	code, out = captureStdout(t, func() int { return run([]string{"--version"}) })
	if code != 0 || strings.TrimSpace(out) != "mischief abc123def456" {
		t.Fatalf("--version: rc=%d out=%q", code, out)
	}
}

// TestUnknownVerbExit2: an unknown verb exits 2 with usage on stderr.
func TestUnknownVerbExit2(t *testing.T) {
	if code := run([]string{"frobnicate"}); code != 2 {
		t.Fatalf("unknown verb exit: %d, want 2", code)
	}
	if code := run(nil); code != 2 {
		t.Fatalf("no verb exit: %d, want 2", code)
	}
}

// TestRevertAllPerHoldLineLeadsWithOutcome (MISCHIEF-DF-51-R2): the FIRST
// word of a per-hold revert line is what an on-call human and a log scraper
// read, so it must come from the measured outcome — never a hardcoded
// success verb. A revert_failed hold leads with "not-reverted" (the old
// contract printed "reverted … outcome=revert_failed", the failure word
// after the success word); a green hold still leads with "reverted".
func TestRevertAllPerHoldLineLeadsWithOutcome(t *testing.T) {
	dir := t.TempDir()
	good, _, _ := scratchCLIHold(t, "P-CLI-WORDGOOD")
	bad, _, badBackup := scratchCLIHold(t, "P-CLI-WORDBAD")
	armCLIHolds(t, dir, good, bad)
	if err := os.Remove(badBackup); err != nil {
		t.Fatalf("sabotage: %v", err)
	}
	code, out := captureStdout(t, func() int { return run([]string{"revert", "--all", "--dir", dir}) })
	if code != 1 {
		t.Fatalf("revert --all with a revert_failed proof: exit %d, want 1\nstdout=%s", code, out)
	}
	goodLine := holdLine(t, out, "fault=P-CLI-WORDGOOD")
	if !strings.HasPrefix(goodLine, "reverted ") {
		t.Fatalf("green hold's line does not lead with the success word: %q", goodLine)
	}
	badLine := holdLine(t, out, "fault=P-CLI-WORDBAD")
	if !strings.HasPrefix(badLine, "not-reverted ") {
		t.Fatalf("failed hold's line still claims the success word: %q", badLine)
	}
	if !strings.Contains(badLine, "outcome=revert_failed") {
		t.Fatalf("failed hold's line lost its outcome field: %q", badLine)
	}
}

// TestRevertHoldSingleLineLeadsWithOutcome (MISCHIEF-DF-51-R2): the same
// first-word rule on the --hold path. The retry path already led with
// "retried"; the plain path hardcoded "reverted" for every outcome.
func TestRevertHoldSingleLineLeadsWithOutcome(t *testing.T) {
	dir := t.TempDir()
	bad, _, badBackup := scratchCLIHold(t, "P-CLI-HOLDWORD")
	id := armCLIHolds(t, dir, bad)[0]
	if err := os.Remove(badBackup); err != nil {
		t.Fatalf("sabotage: %v", err)
	}
	code, out := captureStdout(t, func() int { return run([]string{"revert", "--hold", id, "--dir", dir}) })
	if code != 1 {
		t.Fatalf("revert --hold on a failing hold: exit %d, want 1\nstdout=%s", code, out)
	}
	line := holdLine(t, out, "P-CLI-HOLDWORD")
	if !strings.HasPrefix(line, "not-reverted ") {
		t.Fatalf("--hold failed line still claims the success word: %q", line)
	}
	if !strings.Contains(line, "outcome=revert_failed") {
		t.Fatalf("--hold failed line lost its outcome field: %q", line)
	}
}

// holdLine returns the per-hold revert line naming the given fault.
func holdLine(t *testing.T, out, fault string) string {
	t.Helper()
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, fault) && strings.Contains(ln, "outcome=") {
			return ln
		}
	}
	t.Fatalf("no per-hold revert line for %s:\n%s", fault, out)
	return ""
}

// unsanctioned test host (no marker, no env), `mischief plan` exits 2
// fail-closed and prints the refusal naming the host — nothing landed.
func TestPlanRefusesUnsanctionedHost(t *testing.T) {
	exp := filepath.Join(t.TempDir(), "exp.yaml")
	if err := os.WriteFile(exp, []byte("id: x\ntarget: {selector: \"host:.\", scratch: true}\nfaults:\n  - primitive: P\n    params: {}\n    ttl: 1s\n    landed_proof: {kind: k, check: c}\nprobe:\n  - {name: p, cmd: \"true\", expect: \"0\"}\nbudget: {recover_within: 1s}\nseed: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// the worker's test env named MISCHIEF_SANCTION_MARKER/HOST; the judged
	// SPEC-13 package (7b29502) owns MISCHIEF_SANCTION_FILE — forced to a
	// nonexistent file, the env value is empty in the test process.
	t.Setenv("MISCHIEF_SANCTION_FILE", "/nonexistent/sanction-marker")
	code, stderr := captureStderr(t, func() int { return run([]string{"plan", "-f", exp}) })
	if code != 2 {
		t.Fatalf("plan on unsanctioned host: exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "not sanctioned") || !strings.Contains(stderr, "/nonexistent/sanction-marker") {
		t.Fatalf("refusal does not name the missing marker: %s", stderr)
	}
}

// TestDoctorFailsOnMissingCatalog (MSF-035): doctor is a health check —
// a catalog it cannot load is a FAILED check, so the verb must exit
// NONZERO and its report must name the failure (regresses the dogfood
// 2026-10-08 false-green: 'catalog load FAILED' graded exit 0). A loaded
// catalog is exit 0 regardless of how many primitives are merely
// capability_unavailable (see cmdDoctor's exit contract).
func TestDoctorFailsOnMissingCatalog(t *testing.T) {
	code, out := captureStdout(t, func() int {
		return run([]string{"doctor", "--catalog-dir", filepath.Join(t.TempDir(), "no-such-catalog")})
	})
	if code == 0 {
		t.Fatal("doctor with a missing catalog exited 0 — failed check graded success")
	}
	if !strings.Contains(out, "catalog load FAILED") {
		t.Fatalf("doctor output does not name the failed check: %s", out)
	}
}

// TestDoctorGreenOnLoadedCatalog (MSF-035 positive control): with a real
// catalog the doctor reports it loaded and exits 0 — the nonzero exit is
// reserved for genuine failures, not for capability_unavailable rows.
func TestDoctorGreenOnLoadedCatalog(t *testing.T) {
	repoCatalog := "../../../catalog/faults"
	if st, err := os.Stat(repoCatalog); err != nil || !st.IsDir() {
		t.Skipf("catalog fixtures not present (%s) — run from a full checkout", repoCatalog)
	}
	code, out := captureStdout(t, func() int {
		return run([]string{"doctor", "--catalog-dir", repoCatalog})
	})
	if code != 0 {
		t.Fatalf("doctor on the repo catalog: exit %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "catalog ok loaded") {
		t.Fatalf("doctor output does not report a loaded catalog: %s", out)
	}
}

// planScratchDirExperiment is the clean-plan experiment for the
// --scratch-dir regression (MISCHIEF-CLI-002): one L0 primitive
// (capability: none) that plans clean on this host — no refusals, exit 0.
const planScratchDirExperiment = `id: scratch-dir-plan
target: {selector: "host:.", scratch: true}
faults:
  - primitive: N-012
    params: {code: "429", rate: "0.5"}
    ttl: 20s
    landed_proof: {kind: http, check: "responses carry 429"}
    inverse: {action: restore-proxy}
probe:
  - {name: probe-ok, cmd: "true", expect: "0"}
budget: {recover_within: 30s}
seed: 7
`

// planScratchDirDescriptor is the minimal N-012 descriptor the experiment
// resolves against (self-contained: the test builds its own catalog dir in
// a TempDir, so the primary acceptance arm never depends on repo fixture
// paths that break under worktree nesting).
const planScratchDirDescriptor = `id: N-012
name: http-error-injection
what: inject HTTP error codes at a proxy
breaks: clients that do not back off
params: {code: "429", rate: "0..1"}
landed_proof: {kind: http, check: "responses carry the code"}
inverse: {action: restore-proxy, verify: "responses are 200 again"}
capability: none
tier: L0
backend: proxy
maturity: planned
`

// planScratchDirSanctioned pins the SPEC-13 env-sanction seam for the
// clean-plan controls (the process env is otherwise unsanctioned; the
// sanctioned() helper's own writes stay untouched).
func planScratchDirSanctioned(t *testing.T) {
	t.Helper()
	t.Setenv("MISCHIEF_SANCTION", "1")
}

// TestPlanScratchDirPathValuedAccepted (MISCHIEF-CLI-002, primary arm): the
// exact path-valued form the shipped usage/docs advertise — plan
// --scratch-dir D — is ACCEPTED and the declared dir reaches the plan
// output verbatim. Plan stays side-effect free (AC-1): the dir must NOT
// exist afterwards (nothing was created, nothing was read — the render is
// the declaration's preview; run owns the actual dir).
func TestPlanScratchDirPathValuedAccepted(t *testing.T) {
	planScratchDirSanctioned(t)
	catalogDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(catalogDir, "n-012.yaml"), []byte(planScratchDirDescriptor), 0o644); err != nil {
		t.Fatal(err)
	}
	exp := filepath.Join(t.TempDir(), "exp.yaml")
	if err := os.WriteFile(exp, []byte(planScratchDirExperiment), 0o644); err != nil {
		t.Fatal(err)
	}
	// The declared dir does not exist and plan must not create it.
	scratch := filepath.Join(t.TempDir(), "declared-scratch")
	code, out := captureStdout(t, func() int {
		return run([]string{"plan", "-f", exp, "--catalog-dir", catalogDir, "--scratch-dir", scratch})
	})
	if code != 0 {
		t.Fatalf("plan --scratch-dir D: exit %d, want 0", code)
	}
	if !strings.Contains(out, "scratch_dir: "+scratch) {
		t.Fatalf("plan output does not record the declared scratch dir verbatim:\n%s", out)
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Fatalf("plan created or touched the declared scratch dir %s (AC-1 violated)", scratch)
	}
}

// TestPlanScratchDirBooleanControl (MISCHIEF-CLI-002, boolean control arm):
// the old boolean spelling — plan --scratch-dir with no value — exits 2
// with a usage error NAMING the flag. It must never again die with the
// type confusion "invalid boolean value ... for -scratch-dir" that the
// shipped docs' own command produced.
func TestPlanScratchDirBooleanControl(t *testing.T) {
	exp := filepath.Join(t.TempDir(), "exp.yaml")
	if err := os.WriteFile(exp, []byte(planScratchDirExperiment), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MISCHIEF_SANCTION_FILE", "/nonexistent/sanction-marker")
	code, stderr := captureStderr(t, func() int {
		return run([]string{"plan", "-f", exp, "--scratch-dir"})
	})
	if code != 2 {
		t.Fatalf("valueless --scratch-dir: exit %d, want 2", code)
	}
	if strings.Contains(stderr, "invalid boolean value") {
		t.Fatalf("valueless --scratch-dir still fails as a boolean (MISCHIEF-CLI-002 unfixed): %s", stderr)
	}
	if !strings.Contains(stderr, "scratch-dir") || !strings.Contains(stderr, "flag needs an argument") {
		t.Fatalf("usage error does not name the flag needing a value: %s", stderr)
	}
}

// captureStdout swaps os.Stdout for a pipe around fn.
func captureStdout(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	return capture(t, &os.Stdout, fn)
}

// captureStderr swaps os.Stderr for a pipe around fn.
func captureStderr(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	return capture(t, &os.Stderr, fn)
}

func capture(t *testing.T, target **os.File, fn func() int) (int, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := *target
	*target = w
	code := fn()
	*target = saved
	w.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 512)
	for {
		n, err := r.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	r.Close()
	return code, string(buf)
}
