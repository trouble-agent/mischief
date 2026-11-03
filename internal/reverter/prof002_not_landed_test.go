package reverter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// PROF-002: a hold that was armed but NEVER landed must not grade `reverted`.
//
// Before the fix, revertNow graded only the UNDO (the inverse ran and the
// measured check passed) and never asked whether the fault had been applied at
// all. An arm-then-expire cycle with no injection in between therefore produced
// a green `reverted` proof — the false green an agent consuming mischief would
// read as "my code survived the fault". These tests pin the honest behaviour.
func TestRevertUnlandedHoldGradesNotLanded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.txt")
	backup := filepath.Join(dir, "state.bak")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	lc := New(store)

	hold := Hold{
		FaultID: "S-004",
		Target:  "process:self",
		Inverse: Decl{Kind: "restore-file", Params: map[string]string{
			"path": path, "backup": backup,
		}},
		Check: Decl{Kind: "file-matches-backup", Params: map[string]string{
			"path": path, "backup": backup,
		}},
		TTL: 50 * time.Millisecond,
	}
	id, err := lc.Arm(hold)
	if err != nil {
		t.Fatalf("arm: %v", err)
	}

	// Deliberately do NOT call Land: nothing was injected.

	proof, err := lc.Revert(context.Background(), id, "ttl_expiry", RoleAPI)
	if err != nil {
		t.Fatalf("revert: %v", err)
	}

	if proof.Outcome == OutcomeReverted {
		t.Fatalf("FALSE GREEN: an unlanded hold graded %q — an agent would read this as "+
			"proof the fault was survived, but nothing was ever injected", proof.Outcome)
	}
	if proof.Outcome != OutcomeNotLanded {
		t.Fatalf("outcome = %q, want %q", proof.Outcome, OutcomeNotLanded)
	}
}

// The inverse must STILL run on an unlanded hold. The journal cannot prove a
// fault was never applied (an operator may have faulted the target out of band,
// and nothing in the binary records a land today), so skipping cleanup would
// risk leaving a live fault. A redundant restore is recoverable; a fault left
// live is not. Only the GRADE changes.
func TestRevertUnlandedHoldStillCleansUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.txt")
	backup := filepath.Join(dir, "state.bak")
	if err := os.WriteFile(backup, []byte("arm-time"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The fault was applied OUT OF BAND: the file is corrupted, but the journal
	// has no land record for it.
	if err := os.WriteFile(path, []byte("corrupted-by-fault"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	lc := New(store)

	id, err := lc.Arm(Hold{
		FaultID: "S-004",
		Target:  "process:self",
		Inverse: Decl{Kind: "restore-file", Params: map[string]string{
			"path": path, "backup": backup,
		}},
		Check: Decl{Kind: "file-matches-backup", Params: map[string]string{
			"path": path, "backup": backup,
		}},
		TTL: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("arm: %v", err)
	}

	proof, err := lc.Revert(context.Background(), id, "ttl_expiry", RoleAPI)
	if err != nil {
		t.Fatalf("revert: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "arm-time" {
		t.Fatalf("cleanup did not run on an unlanded hold: file is %q, want the arm-time bytes "+
			"restored — refusing to clean up would leave an out-of-band fault live", string(got))
	}
	if proof.Outcome != OutcomeNotLanded {
		t.Fatalf("outcome = %q, want %q (cleanup ran, but nothing was injected)", proof.Outcome, OutcomeNotLanded)
	}
	if !strings.Contains(proof.Detail, "restored") {
		t.Fatalf("detail %q does not say the target was restored", proof.Detail)
	}
}

// The flip side: a hold that WAS landed still grades reverted when the measured
// check passes. Without this the fix could pass by never grading anything.
func TestRevertLandedHoldStillGradesReverted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.txt")
	backup := filepath.Join(dir, "state.bak")
	if err := os.WriteFile(backup, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The fault "landed": the file now differs from the backup.
	if err := os.WriteFile(path, []byte("corrupted-by-fault"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	lc := New(store)

	id, err := lc.Arm(Hold{
		FaultID: "S-004",
		Target:  "process:self",
		Inverse: Decl{Kind: "restore-file", Params: map[string]string{
			"path": path, "backup": backup,
		}},
		Check: Decl{Kind: "file-matches-backup", Params: map[string]string{
			"path": path, "backup": backup,
		}},
		TTL: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	if err := lc.Land(id); err != nil {
		t.Fatalf("land: %v", err)
	}

	proof, err := lc.Revert(context.Background(), id, "ttl_expiry", RoleAPI)
	if err != nil {
		t.Fatalf("revert: %v", err)
	}
	if proof.Outcome != OutcomeReverted {
		t.Fatalf("outcome = %q, want %q (a landed hold that measures clean is a real revert)",
			proof.Outcome, OutcomeReverted)
	}
}
