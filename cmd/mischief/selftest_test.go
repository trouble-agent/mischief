package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// selftest-verb tests (MSF-015): usage refusals, catalog validation, the
// journal write, and the AC-19 wiring line. The live land+revert loop is
// covered by internal/selftest's own suite (TestLiveSelftestAllGreenOrSkip).

// TestSelftestVerbRequiresSelector: neither --all nor --primitive is the
// usage refusal (exit 2, there is no default primitive); both together is
// a mutual-exclusion refusal (exit 2).
func TestSelftestVerbRequiresSelector(t *testing.T) {
	code, stderr := captureStderr(t, func() int { return run([]string{"selftest"}) })
	if code != 2 {
		t.Fatalf("bare selftest exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "--all or --primitive") {
		t.Fatalf("refusal does not name the selectors: %s", stderr)
	}
	code, stderr = captureStderr(t, func() int { return run([]string{"selftest", "--all", "--primitive", "P-001"}) })
	if code != 2 {
		t.Fatalf("both-selectors exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "mutually exclusive") {
		t.Fatalf("refusal does not name the exclusivity: %s", stderr)
	}
}

// TestSelftestVerbUnknownPrimitiveRefuses: a primitive id outside the
// catalog refuses with exit 2 naming the catalog (the unknown-primitive
// shape — never a silent pass, never a skip).
func TestSelftestVerbUnknownPrimitiveRefuses(t *testing.T) {
	dir := t.TempDir()
	code, stderr := captureStderr(t, func() int {
		return run([]string{"selftest", "--primitive", "ZZZ-999", "--dir", dir})
	})
	if code != 2 {
		t.Fatalf("unknown primitive exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "not in the catalog") {
		t.Fatalf("refusal does not name the catalog: %s", stderr)
	}
}

// TestSelftestVerbWritesJournalAndWiring: a --primitive run of a green
// primitive exits 0, prints the pass line with BOTH measured proofs,
// writes the journal file (JSONL, in the journal package's record
// format), and demonstrates the AC-19 wiring for the primitive.
func TestSelftestVerbWritesJournalAndWiring(t *testing.T) {
	dir := t.TempDir()
	code, out := captureStdout(t, func() int {
		return run([]string{"selftest", "--primitive", "P-001", "--dir", dir})
	})
	if code != 0 {
		t.Fatalf("P-001 selftest exit %d, want 0", code)
	}
	if !strings.Contains(out, "PASS") || !strings.Contains(out, "land:") || !strings.Contains(out, "revert:") {
		t.Fatalf("pass line does not carry both proofs: %s", out)
	}
	if !strings.Contains(out, "ac19:") || !strings.Contains(out, "ADMITTED at L1") {
		t.Fatalf("output does not demonstrate the AC-19 wiring: %s", out)
	}
	b, err := os.ReadFile(filepath.Join(dir, "selftest.jsonl"))
	if err != nil {
		t.Fatalf("journal not written: %v", err)
	}
	if !strings.Contains(string(b), `"type":"fault_landed"`) ||
		!strings.Contains(string(b), `"type":"fault_reverted"`) ||
		!strings.Contains(string(b), `"type":"verdict"`) {
		t.Fatalf("journal lacks the lifecycle records: %.400s", b)
	}
	if strings.Contains(string(b), `"verdict":"recovered"`) == false {
		t.Fatalf("a green selftest must grade recovered: %.400s", b)
	}
}
