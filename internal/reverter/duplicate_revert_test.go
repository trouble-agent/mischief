package reverter

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// dupCountExec wraps the production executor and counts inverse executions.
// It is the in-process observable of "the recorded inverse RAN": the
// reverse-safety contract is about EXECUTION, not only about the rows that
// survive it — a test that only counted revert_proof rows could not tell a
// double-executed inverse that lost an append race from one well-behaved
// attempt.
type dupCountExec struct {
	inner   executor
	actions int32
	checks  int32
}

func (c *dupCountExec) execAction(kind string, d Decl) (string, error) {
	atomic.AddInt32(&c.actions, 1)
	return c.inner.execAction(kind, d)
}

func (c *dupCountExec) evalCheck(kind string, d Decl) (string, bool, error) {
	atomic.AddInt32(&c.checks, 1)
	return c.inner.evalCheck(kind, d)
}

// dupProofRows counts the journal FILE's revert_proof lines naming holdID
// (the durable record, read as bytes — never through the fold the audit is
// about).
func dupProofRows(t *testing.T, dir, holdID string) int {
	t.Helper()
	n := 0
	for _, line := range journalLines(t, dir) {
		if recordTypeOf(t, line) != string(RecordRevertProof) {
			continue
		}
		if i := strings.Index(line, `"hold_id":"`); i >= 0 && strings.HasPrefix(line[i+len(`"hold_id":"`):], holdID) {
			n++
		}
	}
	return n
}

// dupStatus folds lf's current view and returns one hold's status.
func dupStatus(t *testing.T, lf *Lifecycle, holdID string) HoldStatus {
	t.Helper()
	for _, h := range lf.StatusAll().Holds {
		if h.HoldID == holdID {
			return h.Status
		}
	}
	return ""
}

// TestTwoFoldsOverOneJournalRevertOneHoldExactlyOnce is MISCHIEF-DF-71's
// duplicate-daemon hazard: two independent folds over ONE journal — what a
// second `mischief serve --dir D`, a detached TTL owner, or a concurrent
// `revert --hold` each are — must produce exactly ONE revert attempt for a
// hold: one inverse execution and ONE revert_proof row.
//
// A second execution of a recorded inverse is not merely a duplicate row:
// the inverse is not assumed idempotent (a restore can overwrite a file
// another actor just repaired, a remove can delete a file that is live
// again), so "one hold, one undo" is the reverse-safety contract.
//
// Both stores fold BEFORE either reverts — the real interleaving: each
// daemon re-folds the journal on its own tick, so both observe the same
// hold as armed and both may enter the revert path.
func TestTwoFoldsOverOneJournalRevertOneHoldExactlyOnce(t *testing.T) {
	dir := localTempDir(t)
	hold, _, _ := scratchHold(t, "P-DF71-DUP")

	// seed: one armed + landed hold (the CLI's half of the loop)
	seed, err := Open(dir)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	id, err := New(seed).Arm(hold)
	if err != nil {
		seed.Close()
		t.Fatalf("arm: %v", err)
	}
	// scratchHold leaves the live file faulted; land records the fault.
	if err := New(seed).Land(id); err != nil {
		seed.Close()
		t.Fatalf("land: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}

	// two independent folds over the same journal (two daemons' views)
	s1, err := Open(dir)
	if err != nil {
		t.Fatalf("open fold 1: %v", err)
	}
	defer s1.Close()
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("open fold 2: %v", err)
	}
	defer s2.Close()

	exec := &dupCountExec{inner: scratchExec{}}
	lf1, lf2 := New(s1), New(s2)
	lf1.exec = exec
	lf2.exec = exec

	// PREMISE: both folds see the hold as armed. Without this the test
	// proves nothing — a fold that already knew the terminal status would
	// be refused by the pre-existing snapshot check, not by the guard.
	for i, lf := range []*Lifecycle{lf1, lf2} {
		if got := dupStatus(t, lf, id); got != StatusArmed {
			t.Fatalf("premise: fold %d sees %q, want armed", i+1, got)
		}
	}

	// both attempt the revert at the same instant
	type result struct {
		proof Proof
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, lf := range []*Lifecycle{lf1, lf2} {
		wg.Add(1)
		go func(lf *Lifecycle) {
			defer wg.Done()
			<-start
			p, err := lf.Revert(context.Background(), id, "ttl_expiry", RoleOwner)
			results <- result{proof: p, err: err}
		}(lf)
	}
	close(start)
	wg.Wait()
	close(results)

	attempts, declined := 0, 0
	var declineErr error
	for r := range results {
		if r.err == nil {
			attempts++
			continue
		}
		declined++
		declineErr = r.err
	}
	if attempts != 1 || declined != 1 {
		t.Fatalf("attempts=%d declined=%d inverse_executions=%d proof_rows=%d, want exactly 1 attempt and 1 refusal (both folds entered the revert path)",
			attempts, declined, atomic.LoadInt32(&exec.actions), dupProofRows(t, dir, id))
	}
	if got := atomic.LoadInt32(&exec.actions); got != 1 {
		t.Fatalf("the recorded inverse executed %d times for one hold, want exactly 1", got)
	}
	if got := dupProofRows(t, dir, id); got != 1 {
		t.Fatalf("journal carries %d revert_proof rows for hold %s, want exactly 1", got, id)
	}
	// the loser declines with the NAMED refusal and writes nothing itself
	if !errors.Is(declineErr, ErrAlreadyAttempted) {
		t.Fatalf("loser's error = %v, want ErrAlreadyAttempted", declineErr)
	}
	// the durable fold agrees with the file: one terminal proof
	if got := dupStatus(t, lf1, id); got != StatusReverted {
		t.Fatalf("folded status %q after the race, want reverted", got)
	}
	if b, err := os.ReadFile(hold.Inverse.Params["path"]); err != nil || string(b) != "GOOD-CONTENT\n" {
		t.Fatalf("target after the race = %q (err %v), want the backup content restored once", b, err)
	}
}

// TestRefoldDoesNotDuplicateProofHistory is the same hazard on the READ
// side: a long-lived fold re-reads the journal on every tick, so a fold
// that APPENDS onto its live maps instead of rebuilding them counts each
// proof once more per refold — three daemon ticks turn one revert into four
// proofs in memory (and the health/status surfaces read that fold). The
// re-read must be idempotent: fold twice, get the same history.
func TestRefoldDoesNotDuplicateProofHistory(t *testing.T) {
	dir := localTempDir(t)
	hold, _, _ := scratchHold(t, "P-DF71-REFOLD")

	seed, err := Open(dir)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	id, err := New(seed).Arm(hold)
	if err != nil {
		seed.Close()
		t.Fatalf("arm: %v", err)
	}
	if err := New(seed).Land(id); err != nil {
		seed.Close()
		t.Fatalf("land: %v", err)
	}
	if _, err := New(seed).Revert(context.Background(), id, "direct", RoleAPI); err != nil {
		seed.Close()
		t.Fatalf("revert: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}

	st, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	for i := 0; i < 3; i++ {
		if err := st.Refold(); err != nil {
			t.Fatalf("refold %d: %v", i+1, err)
		}
	}
	got := 0
	for _, p := range New(st).StatusAll().History {
		if p.HoldID == id {
			got++
		}
	}
	if want := dupProofRows(t, dir, id); got != want {
		t.Fatalf("fold carries %d proofs for hold %s after 3 refolds, journal file carries %d (a refold must re-read, not append)", got, id, want)
	}
}
