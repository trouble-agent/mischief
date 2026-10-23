package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/reverter"
)

// TestArmKindRefusalLeavesRunDirEmpty (MISCHIEF-DF-72): the discriminator
// pair on HEAD was —
//
//	(A) sanction-gate refusal (no sanction, fresh dir): rc=2 and NO run dir
//	    at all (the gate runs before reverter.Open);
//	(B) kind refusal on a sanctioned host: rc=1 with a freshly MINTED run
//	    dir (boot.json + empty reverter.jsonl) — reverter.Open created the
//	    dir and installed the boot marker before Lifecycle.Arm's
//	    Hold.Validate refused the unknown kind, contradicting the
//	    "nothing was written" the refusal classes share.
//
// The fix validates the declaration BEFORE the store bootstraps
// (reverter.ValidateForArm — the same Hold.Validate + MISCHIEF-DF-23
// inverse-undo probes Arm itself runs, applied one phase earlier). Both
// legs reproduce the old divergence (verified against HEAD) and both must
// now agree: a refused arm on a fresh dir leaves the parent with NO run
// dir — nothing was written, and the operator cannot confuse the two
// refusal classes by directory shape.
func TestArmKindRefusalLeavesRunDirEmpty(t *testing.T) {
	writeSanctionMarker(t) // sanctioned posture: only the decl validation can refuse here
	parent := t.TempDir()
	runDir := filepath.Join(parent, "runs")
	backup := filepath.Join(t.TempDir(), "backup")
	if err := os.WriteFile(backup, []byte("pristine\n"), 0o644); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	t.Run("unknown inverse kind: exit 2, no run dir minted", func(t *testing.T) {
		args := []string{
			"arm",
			"--fault", "S-001",
			"--target", "scratch:/tmp/mischief-arm-test",
			"--ttl", "10s",
			"--inverse-kind", "bogus-kind",
			"--inverse-params", "path=" + runDir + "/fault-file",
			"--check-kind", "file-matches-backup",
			"--check-params", "path=" + runDir + "/fault-file,backup=" + backup,
			"--dir", runDir,
		}
		mustLeaveParentEmpty(t, parent, runDir, args)
	})

	t.Run("cross-slot kind: check kind in the inverse slot, exit 2, no run dir minted", func(t *testing.T) {
		args := []string{
			"arm",
			"--fault", "S-001",
			"--target", "scratch:/tmp/mischief-arm-test",
			"--ttl", "10s",
			"--inverse-kind", "file-absent", // a CHECK kind, offered as the inverse
			"--inverse-params", "path=" + runDir + "/fault-file",
			"--check-kind", "file-absent",
			"--check-params", "path=" + runDir + "/fault-file",
			"--dir", runDir,
		}
		mustLeaveParentEmpty(t, parent, runDir, args)
	})

	t.Run("unreadable restore backup (DF-23 probe): exit 2, no run dir minted", func(t *testing.T) {
		args := []string{
			"arm",
			"--fault", "S-001",
			"--target", "scratch:/tmp/mischief-arm-test",
			"--ttl", "10s",
			"--inverse-kind", "restore-file",
			"--inverse-params", "path=" + runDir + "/fault-file,backup=" + backup + "-MISSING",
			"--check-kind", "file-matches-backup",
			"--check-params", "path=" + runDir + "/fault-file,backup=" + backup,
			"--dir", runDir,
		}
		mustLeaveParentEmpty(t, parent, runDir, args)
	})

	t.Run("positive control: the admitted arm mints the run dir", func(t *testing.T) {
		code, out := captureStdout(t, func() int { return run(armTestArgs(t, runDir)) })
		if code != 0 || !strings.Contains(out, "armed ") {
			t.Fatalf("positive control arm: exit %d out=%q, want 0 + armed", code, out)
		}
		if _, err := os.Stat(filepath.Join(runDir, "boot.json")); err != nil {
			t.Fatalf("admitted arm did not mint boot.json under %s: %v", runDir, err)
		}
	})
}

// mustLeaveParentEmpty runs the arm and asserts the refused arm minted
// NOTHING under parent: the run dir (parent/runs) must not exist — no
// boot.json, no reverter.jsonl, no empty dir.
func mustLeaveParentEmpty(t *testing.T, parent, runDir string, args []string) {
	t.Helper()
	code, stderr := captureStderr(t, func() int { return run(args) })
	if code != 2 {
		t.Fatalf("refused arm: exit %d stderr=%q, want 2", code, stderr)
	}
	if ents, err := os.ReadDir(parent); err != nil || len(ents) != 0 {
		t.Fatalf("refused arm minted the run dir or wrote into %s: ents=%v err=%v", parent, ents, err)
	}
}

// newDF72TestStore opens a reverter store over a fresh temp dir for the
// both-surfaces parity tests (the cmd package has no shared store helper).
func newDF72TestStore(t *testing.T) *reverter.Store {
	t.Helper()
	st, err := reverter.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// TestValidateForArmMatchesLifecycleArm (MISCHIEF-DF-72): the pre-open
// validator is the ONE source of the refusal class — it must agree with
// Lifecycle.Arm on every declaration. If ValidateForArm admits a hold Arm
// refuses, the store has already been minted by then; if it refuses a hold
// Arm admits, the CLI blocks an armable hold. Every leg here runs BOTH
// surfaces and requires the same verdict.
func TestValidateForArmMatchesLifecycleArm(t *testing.T) {
	good := func() reverter.Hold {
		return reverter.Hold{
			FaultID: "S-001",
			Target:  "scratch:/tmp/x",
			TTL:     10_000_000_000,
			Inverse: reverter.Decl{Kind: "remove-file", Params: map[string]string{"path": "/tmp/df72-a"}},
			Check:   reverter.Decl{Kind: "file-absent", Params: map[string]string{"path": "/tmp/df72-a"}},
		}
	}

	t.Run("admitted hold: both surfaces admit", func(t *testing.T) {
		runSt := newDF72TestStore(t)
		lf := reverter.New(runSt)
		h := good()
		if err := reverter.ValidateForArm(h); err != nil {
			t.Fatalf("ValidateForArm refused an armable hold: %v", err)
		}
		if _, err := lf.Arm(h); err != nil {
			t.Fatalf("Lifecycle.Arm refused the same hold: %v", err)
		}
	})

	t.Run("unknown inverse kind: both refuse", func(t *testing.T) {
		runSt := newDF72TestStore(t)
		lf := reverter.New(runSt)
		h := good()
		h.Inverse.Kind = "bogus-kind"
		if err := reverter.ValidateForArm(h); err == nil {
			t.Fatal("ValidateForArm admitted an unknown inverse kind")
		}
		if _, err := lf.Arm(h); err == nil {
			t.Fatal("Lifecycle.Arm admitted an unknown inverse kind")
		}
	})

	t.Run("cross-slot kind: both refuse", func(t *testing.T) {
		runSt := newDF72TestStore(t)
		lf := reverter.New(runSt)
		h := good()
		h.Inverse.Kind = "file-absent" // check kind in the inverse slot
		if err := reverter.ValidateForArm(h); err == nil {
			t.Fatal("ValidateForArm admitted a cross-slot inverse kind")
		}
		if _, err := lf.Arm(h); err == nil {
			t.Fatal("Lifecycle.Arm admitted a cross-slot inverse kind")
		}
	})

	t.Run("unreadable restore backup: both refuse (DF-23)", func(t *testing.T) {
		runSt := newDF72TestStore(t)
		lf := reverter.New(runSt)
		h := good()
		h.Inverse = reverter.Decl{Kind: "restore-file", Params: map[string]string{
			"path": "/tmp/df72-b", "backup": "/tmp/df72-b-DOES-NOT-EXIST",
		}}
		if err := reverter.ValidateForArm(h); err == nil {
			t.Fatal("ValidateForArm admitted an unreadable restore backup")
		}
		if _, err := lf.Arm(h); err == nil {
			t.Fatal("Lifecycle.Arm admitted an unreadable restore backup")
		}
	})
}
