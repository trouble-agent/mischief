package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trouble-agent/mischief/internal/reverter"
)

// scratchCLIHold builds one armable hold over a real scratch file pair
// (backup with pristine bytes, live file corrupted) — the CLI-level twin of
// the reverter package's scratchHold: no network, no root, the measured
// check is a real cmp of real bytes.
func scratchCLIHold(t *testing.T, faultID string) (reverter.Hold, string, string) {
	t.Helper()
	dir := t.TempDir()
	backup := filepath.Join(dir, faultID+".backup")
	file := filepath.Join(dir, faultID+".live")
	if err := os.WriteFile(backup, []byte("GOOD-CONTENT\n"), 0o644); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	if err := os.WriteFile(file, []byte("FAULTED-CONTENT\n"), 0o644); err != nil {
		t.Fatalf("write live: %v", err)
	}
	return reverter.Hold{
		FaultID: faultID,
		Target:  "file:" + file,
		Inverse: reverter.Decl{Kind: "restore-file", Params: map[string]string{"path": file, "backup": backup}},
		Check:   reverter.Decl{Kind: "file-matches-backup", Params: map[string]string{"path": file, "backup": backup}},
		TTL:     30 * time.Second,
	}, file, backup
}

// armCLIHolds arms the given holds into a scratch run dir and closes the
// store, so the revert verb's own Open folds the journal from disk the way
// a real invocation does.
func armCLIHolds(t *testing.T, dir string, holds ...reverter.Hold) []string {
	t.Helper()
	store, err := reverter.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	lf := reverter.New(store)
	var ids []string
	for _, h := range holds {
		id, err := lf.Arm(h)
		if err != nil {
			t.Fatalf("arm %s: %v", h.FaultID, err)
		}
		ids = append(ids, id)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	return ids
}

// TestRevertAllExitsNonZeroOnFailedProof (MISCHIEF-DF-51): the kill switch
// continues the sweep past a hold whose measured revert fails (every fault
// still gets its proof) — but the verb must grade the sweep by its worst
// proof: exit non-zero, count the success and the failure separately, and
// say on stderr that a fault may still be live. The old contract printed
// "N holds reverted" from len(proofs) and returned 0 on a live fault.
func TestRevertAllExitsNonZeroOnFailedProof(t *testing.T) {
	dir := t.TempDir()
	good, _, _ := scratchCLIHold(t, "P-CLI-GOOD")
	bad, badFile, badBackup := scratchCLIHold(t, "P-CLI-BAD")
	armCLIHolds(t, dir, good, bad)
	// Sabotage the bad hold's undo AFTER arm (the arm probe requires the
	// backup readable): its revert measurably fails — check says so.
	if err := os.Remove(badBackup); err != nil {
		t.Fatalf("sabotage: %v", err)
	}
	code, out := captureStdout(t, func() int { return run([]string{"revert", "--all", "--dir", dir}) })
	if code != 1 {
		t.Fatalf("revert --all with a revert_failed proof: exit %d, want 1\nstdout=%s", code, out)
	}
	if !strings.Contains(out, "1 holds reverted, 1 revert_failed") {
		t.Fatalf("summary does not count success and failure separately: %q", out)
	}
	if !strings.Contains(out, "outcome=revert_failed") {
		t.Fatalf("failed proof not printed with its outcome: %q", out)
	}
	if strings.Contains(out, "kill switch: 2 holds reverted") {
		t.Fatalf("summary still reports the failed hold as reverted: %q", out)
	}
	// the sweep continued: the good hold is measurably reverted...
	b, err := os.ReadFile(strings.TrimPrefix(good.Target, "file:"))
	if err != nil || string(b) != "GOOD-CONTENT\n" {
		t.Fatalf("good hold not reverted: %q %v", b, err)
	}
	// ...and the failed one told the truth: its fault is still live.
	b, err = os.ReadFile(badFile)
	if err != nil || string(b) != "FAULTED-CONTENT\n" {
		t.Fatalf("revert_failed hold's fault vanished: %q %v", b, err)
	}
}

// TestRevertAllAllGreenStillZero (MISCHIEF-DF-51): the AC-6 happy path is
// unchanged — every proof measured reverted, exit 0, success summary.
func TestRevertAllAllGreenStillZero(t *testing.T) {
	dir := t.TempDir()
	good, _, _ := scratchCLIHold(t, "P-CLI-GREEN")
	armCLIHolds(t, dir, good)
	code, out := captureStdout(t, func() int { return run([]string{"revert", "--all", "--dir", dir}) })
	if code != 0 {
		t.Fatalf("all-green revert --all: exit %d, want 0\nstdout=%s", code, out)
	}
	if !strings.Contains(out, "kill switch: 1 holds reverted") || strings.Contains(out, "revert_failed") {
		t.Fatalf("all-green summary wrong: %q", out)
	}
}

// TestCountRevertFailuresClosedVocabulary (MISCHIEF-DF-51): the green
// counter only trusts a declared, measured "reverted". An unknown or empty
// outcome value — a malformed or foreign proof — counts as a failure, never
// green by accident.
func TestCountRevertFailuresClosedVocabulary(t *testing.T) {
	cases := []struct {
		name    string
		outcome reverter.Outcome
		failed  bool
	}{
		{"measured reverted is green", reverter.OutcomeReverted, false},
		{"revert_failed counts", reverter.OutcomeRevertFailed, true},
		{"unknown value counts", reverter.Outcome("weird"), true},
		{"empty value counts", reverter.Outcome(""), true},
	}
	for _, tc := range cases {
		n := countRevertFailures([]reverter.Proof{{Outcome: tc.outcome}})
		if tc.failed && n != 1 {
			t.Fatalf("%s: %d failures, want 1", tc.name, n)
		}
		if !tc.failed && n != 0 {
			t.Fatalf("%s: %d failures, want 0", tc.name, n)
		}
	}
}
