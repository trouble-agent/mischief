package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── the retry flag on a terminal revert_failed hold (MISCHIEF-DF-52) ─────────
//
// Before this flag a revert_failed hold was a dead end: `revert --hold <id>`
// refused it ("already terminal: revert_failed"), `revert --all` skipped it,
// and the only recovery was re-arming a fresh hold — while the failed proof
// itself says that hold's fault may STILL be live. The retry re-executes the
// recorded inverse as one more measured AC-4 attempt and appends a new proof.
//
// Every leg below drives the real `revert` verb (run + captureStdout/
// captureStderr, the package's existing harness) over a real scratch file
// pair — a failed revert is produced the honest way: the inverse's undo is
// broken AFTER arm (the arm-time probe refuses an unreadable backup before it
// is journaled), then healed or not depending on the leg.

// journalProof is the subset of the reverter.jsonl revert_proof record this
// file reads back — the raw journal line a consumer sees, not the in-memory
// Proof.
type journalProof struct {
	Type    string            `json:"type"`
	HoldID  string            `json:"hold_id"`
	Outcome string            `json:"outcome"`
	Fields  map[string]string `json:"fields"`
}

// readJournalProofs returns every revert_proof line the run dir's journal
// holds, in append order (the order the fold also uses).
func readJournalProofs(t *testing.T, dir string) []journalProof {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "reverter.jsonl"))
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	var out []journalProof
	for _, ln := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		var rec journalProof
		if err := json.Unmarshal([]byte(ln), &rec); err != nil {
			t.Fatalf("journal line is not valid JSON (%v): %q", err, ln)
		}
		if rec.Type == "revert_proof" {
			out = append(out, rec)
		}
	}
	return out
}

// holdStatusLine returns the row `status` prints for one fault id.
func holdStatusLine(t *testing.T, out, faultID string) string {
	t.Helper()
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "fault="+faultID) {
			return ln
		}
	}
	t.Fatalf("status printed no hold row for %s:\n%s", faultID, out)
	return ""
}

// failOneHold arms a hold, breaks its undo after the arm, and reverts it
// through the verb so the fold grades the hold the terminal revert_failed
// state a retry exists for. It returns the run dir, the hold id and the
// live/backup paths, so a leg can heal the environmental cause.
func failOneHold(t *testing.T, faultID string) (dir, id, file, backup string) {
	t.Helper()
	dir = t.TempDir()
	h, live, bak := scratchCLIHold(t, faultID)
	id = armCLIHolds(t, dir, h)[0]
	if err := os.Remove(bak); err != nil {
		t.Fatalf("sabotage backup after arm: %v", err)
	}
	code, out := captureStdout(t, func() int {
		return run([]string{"revert", "--hold", id, "--dir", dir})
	})
	if code != 1 || !strings.Contains(out, "outcome=revert_failed") {
		t.Fatalf("first revert of a sabotaged hold: exit %d out=%q, want exit 1 with outcome=revert_failed", code, out)
	}
	return dir, id, live, bak
}

// TestRevertRetryReexecutesFailedRevert: the acceptance path. A revert_failed
// hold whose environmental cause has healed is retried with --retry: the verb
// exits 0, the fault is measurably undone, the journal holds BOTH proofs (the
// failed attempt AND the retry — evidence is appended, never rewritten), and
// `status` folds the retry's outcome into the hold.
func TestRevertRetryReexecutesFailedRevert(t *testing.T) {
	dir, id, live, backup := failOneHold(t, "DF52-HEALED")
	// heal the cause the failed attempt recorded: the inverse's backup is
	// readable again, so the SAME recorded inverse can now execute.
	if err := os.WriteFile(backup, []byte("GOOD-CONTENT\n"), 0o644); err != nil {
		t.Fatalf("heal backup: %v", err)
	}
	code, out := captureStdout(t, func() int {
		return run([]string{"revert", "--hold", id, "--retry", "--dir", dir})
	})
	if code != 0 {
		t.Fatalf("retry of a healable revert_failed hold: exit %d, want 0\nstdout=%s", code, out)
	}
	if !strings.Contains(out, "retried "+id) || !strings.Contains(out, "outcome=reverted") {
		t.Fatalf("retry output does not report this hold's measured revert: %q", out)
	}
	// the retry ran the recorded inverse against the real resource
	b, err := os.ReadFile(live)
	if err != nil || string(b) != "GOOD-CONTENT\n" {
		t.Fatalf("retry did not restore the live file: %q %v", b, err)
	}
	proofs := readJournalProofs(t, dir)
	if len(proofs) != 2 {
		t.Fatalf("journal holds %d proofs, want 2 (the failed attempt + the retry): %+v", len(proofs), proofs)
	}
	if proofs[0].Outcome != "revert_failed" || proofs[1].Outcome != "reverted" {
		t.Fatalf("journal outcomes = [%s %s], want [revert_failed reverted]", proofs[0].Outcome, proofs[1].Outcome)
	}
	if got := proofs[0].Fields["reason"]; got != "direct" {
		t.Fatalf("first proof reason = %q, want direct", got)
	}
	if got := proofs[1].Fields["reason"]; got != "retry" {
		t.Fatalf("retry proof reason = %q, want retry (the journal must tell the two attempts apart)", got)
	}
	// status shows the LATEST outcome, with both proofs in its history.
	// MISCHIEF-DF-54 took terminal holds (reverted / revert_failed) off the
	// DEFAULT status listing — they are recorded facts, not active state — so
	// this hold's own row is read back from the complete listing
	// (`status --all`); the default surface must not carry it at all.
	scode, sout := captureStdout(t, func() int { return run([]string{"status", "--all", "--dir", dir}) })
	if scode != 0 {
		t.Fatalf("status --all exit: %d\n%s", scode, sout)
	}
	hold := holdStatusLine(t, sout, "DF52-HEALED")
	if !strings.Contains(hold, "reverted") || strings.Contains(hold, "revert_failed") {
		t.Fatalf("status does not show the retry's outcome for the hold: %q", hold)
	}
	if !strings.Contains(sout, "proofs: 2") {
		t.Fatalf("status history does not list both proofs: %q", sout)
	}
	// The mirror-image half of the same contract (MISCHIEF-DF-54): the
	// DEFAULT surface carries no terminal row for this hold, while the proofs
	// stay in its history.
	_, deflt := captureStdout(t, func() int { return run([]string{"status", "--dir", dir}) })
	if strings.Contains(deflt, "fault=DF52-HEALED") {
		t.Fatalf("the default status surface lists a terminal hold: %q", deflt)
	}
	if !strings.Contains(deflt, "proofs: 2") {
		t.Fatalf("the default status surface lost the proof history: %q", deflt)
	}
}

// TestRevertRetryFailsAgainExitsOneAndKeepsBothProofs: a retry is a MEASURED
// attempt, not a wish — when the cause is still broken the verb exits 1, the
// fault is left live, and the second proof lands on the record next to the
// first (`status --all` keeps grading revert_failed, so nothing reads as
// green).
func TestRevertRetryFailsAgainExitsOneAndKeepsBothProofs(t *testing.T) {
	dir, id, live, _ := failOneHold(t, "DF52-STILL-BROKEN")
	code, out := captureStdout(t, func() int {
		return run([]string{"revert", "--hold", id, "--retry", "--dir", dir})
	})
	if code != 1 {
		t.Fatalf("retry that fails again: exit %d, want 1\nstdout=%s", code, out)
	}
	if !strings.Contains(out, "retried "+id) || !strings.Contains(out, "outcome=revert_failed") {
		t.Fatalf("failed retry does not grade its own attempt: %q", out)
	}
	b, err := os.ReadFile(live)
	if err != nil || string(b) != "FAULTED-CONTENT\n" {
		t.Fatalf("live file changed during a failed retry: %q %v", b, err)
	}
	proofs := readJournalProofs(t, dir)
	if len(proofs) != 2 || proofs[1].Outcome != "revert_failed" {
		t.Fatalf("journal = %+v, want 2 proofs whose retry is revert_failed", proofs)
	}
	// The complete listing (MISCHIEF-DF-54: terminal holds are facts, kept off
	// the default surface) still grades this hold revert_failed.
	_, sout := captureStdout(t, func() int { return run([]string{"status", "--all", "--dir", dir}) })
	if hold := holdStatusLine(t, sout, "DF52-STILL-BROKEN"); !strings.Contains(hold, "revert_failed") {
		t.Fatalf("status lost the hold's failed grade: %q", hold)
	}
}

// TestRevertRefusalOnFailedHoldNamesRetryFlag (acceptance 3): the refusal an
// operator hits WITHOUT the flag must name it, instead of only stating that
// the hold is terminal. The refusal still writes nothing.
func TestRevertRefusalOnFailedHoldNamesRetryFlag(t *testing.T) {
	dir, id, _, _ := failOneHold(t, "DF52-REFUSED")
	code, stderr := captureStderr(t, func() int {
		return run([]string{"revert", "--hold", id, "--dir", dir})
	})
	if code != 1 {
		t.Fatalf("plain revert of a revert_failed hold: exit %d, want 1", code)
	}
	if !strings.Contains(stderr, "--retry") {
		t.Fatalf("refusal does not name the --retry flag: %q", stderr)
	}
	if !strings.Contains(stderr, "already terminal: revert_failed") {
		t.Fatalf("refusal no longer names the terminal status it refused: %q", stderr)
	}
	if n := len(readJournalProofs(t, dir)); n != 1 {
		t.Fatalf("a refused revert appended a proof: %d proofs, want 1", n)
	}
}

// TestRevertRetryRefusesNonFailedHold: --retry admits ONE state. A live hold
// belongs to the plain path (a "retry" there would revert a live fault under a
// misleading name) and a hold already measured reverted has nothing left to
// undo (a second proof would fabricate a revert). Both refuse, exit 1, name
// the status, and write nothing.
func TestRevertRetryRefusesNonFailedHold(t *testing.T) {
	dir := t.TempDir()
	h, live, _ := scratchCLIHold(t, "DF52-LIVE")
	id := armCLIHolds(t, dir, h)[0]

	code, stderr := captureStderr(t, func() int {
		return run([]string{"revert", "--hold", id, "--retry", "--dir", dir})
	})
	if code != 1 {
		t.Fatalf("--retry on a live hold: exit %d, want 1", code)
	}
	if !strings.Contains(stderr, "revert_failed") || !strings.Contains(stderr, "armed") {
		t.Fatalf("refusal does not name the admitted status vs the hold's own: %q", stderr)
	}
	if n := len(readJournalProofs(t, dir)); n != 0 {
		t.Fatalf("refused retry on a live hold appended %d proofs, want 0", n)
	}
	b, err := os.ReadFile(live)
	if err != nil || string(b) != "FAULTED-CONTENT\n" {
		t.Fatalf("refused retry executed the inverse anyway: %q %v", b, err)
	}

	// leg 2: a hold the plain path already measured reverted
	code, out := captureStdout(t, func() int {
		return run([]string{"revert", "--hold", id, "--dir", dir})
	})
	if code != 0 {
		t.Fatalf("plain revert of the live hold: exit %d, want 0\n%s", code, out)
	}
	code, stderr = captureStderr(t, func() int {
		return run([]string{"revert", "--hold", id, "--retry", "--dir", dir})
	})
	if code != 1 {
		t.Fatalf("--retry on a reverted hold: exit %d, want 1", code)
	}
	if !strings.Contains(stderr, "reverted") {
		t.Fatalf("refusal does not name the hold's status: %q", stderr)
	}
	if n := len(readJournalProofs(t, dir)); n != 1 {
		t.Fatalf("refused retry on a reverted hold appended a second proof: %d proofs, want 1", n)
	}
}

// TestRevertRetryFlagRequirements: --retry re-executes ONE hold's inverse, so
// the flag combinations that cannot mean that are usage errors (exit 2, the
// flag named) rather than a silent fall-through to another path.
func TestRevertRetryFlagRequirements(t *testing.T) {
	dir := t.TempDir()
	code, stderr := captureStderr(t, func() int {
		return run([]string{"revert", "--all", "--retry", "--dir", dir})
	})
	if code != 2 || !strings.Contains(stderr, "--retry") || !strings.Contains(stderr, "--all") {
		t.Fatalf("--retry --all: exit %d stderr=%q, want exit 2 naming both flags", code, stderr)
	}
	code, stderr = captureStderr(t, func() int {
		return run([]string{"revert", "--retry", "--dir", dir})
	})
	if code != 2 || !strings.Contains(stderr, "--hold") {
		t.Fatalf("--retry without --hold: exit %d stderr=%q, want exit 2 naming --hold", code, stderr)
	}
	// an id that was never armed has no recorded inverse to re-execute
	code, stderr = captureStderr(t, func() int {
		return run([]string{"revert", "--hold", "deadbeefdeadbeef", "--retry", "--dir", dir})
	})
	if code != 1 || !strings.Contains(stderr, "no live hold") {
		t.Fatalf("--retry on an unknown hold: exit %d stderr=%q, want exit 1", code, stderr)
	}
}
