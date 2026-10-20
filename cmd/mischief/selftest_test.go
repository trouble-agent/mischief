package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSanctionMarker provisions a reason-bearing marker file and points
// MISCHIEF_SANCTION_FILE at it (the SPEC-13 env half — the plane's agent
// shape) for tests that must reach code BEHIND the MSF-032 sanction gate.
// The gate is fail-closed on the real host posture, which the test process
// shares; the marker is the sanctioned posture, nothing more.
func writeSanctionMarker(t *testing.T) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "mischief-sanction")
	if err := os.WriteFile(marker, []byte("reason: cmd-verb test — sanctioned posture for this test only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MISCHIEF_SANCTION_FILE", marker)
}

// (writeHealthyLoad for the AC-8 gate lives in battery_test.go and pins
// the selftest package's seam too — one helper, both verb test files.)

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
	writeSanctionMarker(t) // the MSF-032 gate runs before catalog validation
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
	writeSanctionMarker(t) // the MSF-032 gate runs before the land+revert loop
	writeHealthyLoad(t)    // MISCHIEF-DF-28: the AC-8 gate reads the injected posture, not the box's /proc
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

// versionSkewDescriptor declares a primitive id no build in this tree
// covers — the artifact a catalog NEWER than the binary carries (the
// documented DF-38 pairing: `git archive v0.1.0 catalog` has 10 fault
// files, a HEAD catalog has 11). The descriptor only has to load: the run
// never reaches an actuator, because the corpus check refuses the id.
const versionSkewDescriptor = `id: F-099
name: version-skew-probe
what: a primitive the catalog declares and this build's corpus does not cover
breaks: nothing — it exists to shape the catalog/binary vintage mismatch
params: {path: str}
landed_proof: {kind: size-zero, check: "stat(path).size == 0"}
inverse: {action: restore-from-copy, verify: "the path's bytes match the pre-fault copy"}
capability: none
tier: L0
backend: fs
maturity: planned
`

// TestSelftestUnknownCorpusHintsVersionSkew (MISCHIEF-DF-38): a catalog id
// this build's compiled-in corpus does not cover still FAILs (fail-closed:
// an unprovable primitive never passes) — and the run must now say why.
// The operator-facing surface is the hint: version skew, the two vintages
// to compare (the binary's own stamp via `mischief version`, the catalog's
// content-derived version and the directory it was read from), and the
// remedy. Without it the documented scenario (release binary + HEAD
// catalog) reads as a broken product.
func TestSelftestUnknownCorpusHintsVersionSkew(t *testing.T) {
	writeSanctionMarker(t) // the MSF-032 gate runs before catalog validation
	catDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(catDir, "f-099.yaml"), []byte(versionSkewDescriptor), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	args := []string{"selftest", "--primitive", "F-099", "--catalog-dir", catDir, "--dir", dir}

	code, out := captureStdout(t, func() int { return run(args) })
	if code != 1 {
		t.Fatalf("unknown-corpus primitive exit %d, want 1 (never a pass)\n%s", code, out)
	}
	if !strings.Contains(out, "not in the selftest corpus") {
		t.Fatalf("fail line no longer names the corpus: %s", out)
	}
	if strings.Contains(out, "version skew") {
		t.Fatalf("the hint must not pollute the stdout results surface:\n%s", out)
	}

	code, stderr := captureStderr(t, func() int { return run(args) })
	if code != 1 {
		t.Fatalf("unknown-corpus primitive exit %d on the stderr capture, want 1", code)
	}
	// The hint must name the skew, BOTH vintages, the offending id, and the
	// way out — a hint that only says "version skew" would not tell the
	// operator which versions to compare (the DF-38 ask).
	for _, want := range []string{"version skew", "mischief version", "content-v1", catDir, "F-099", "--catalog-dir"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("version-skew hint does not name %q:\n%s", want, stderr)
		}
	}
}

// TestSelftestNoVersionSkewHintWithoutSkew: the hint is scoped to the
// corpus mismatch. A typo id refuses at the catalog rail with its own
// message (exit 2, "not in the catalog") and a green primitive's run stays
// hint-free — a hint that fires on every failure trains the operator to
// ignore it.
func TestSelftestNoVersionSkewHintWithoutSkew(t *testing.T) {
	writeSanctionMarker(t)
	dir := t.TempDir()
	code, stderr := captureStderr(t, func() int {
		return run([]string{"selftest", "--primitive", "ZZZ-999", "--dir", dir})
	})
	if code != 2 {
		t.Fatalf("typo primitive exit %d, want 2", code)
	}
	if strings.Contains(stderr, "version skew") {
		t.Fatalf("a typo refusal must not claim version skew:\n%s", stderr)
	}

	writeHealthyLoad(t) // MISCHIEF-DF-28: the AC-8 gate reads the injected posture
	code, stderr = captureStderr(t, func() int {
		return run([]string{"selftest", "--primitive", "P-001", "--dir", dir})
	})
	if code != 0 {
		t.Fatalf("green P-001 exit %d, want 0\n%s", code, stderr)
	}
	if strings.Contains(stderr, "version skew") {
		t.Fatalf("a green run must not carry the hint:\n%s", stderr)
	}
}
