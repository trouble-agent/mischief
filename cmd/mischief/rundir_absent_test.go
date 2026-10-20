package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// MISCHIEF-DF-60: an ABSENT run dir must not read as a verified-clean
// sweep. reverter.Open is a CREATING open (os.MkdirAll + a minted
// boot.json on first open), so a typo'd or rotated --dir used to behave
// exactly like a clean kill switch: `revert --all` swept zero holds to a
// green "kill switch: 0 holds reverted" (rc 0) and `status` invented a
// boot_id over holds: 0 / proofs: 0 (rc 0). These tests pin the graded
// contract: both verbs name the absent state and refuse (exit 1), nothing
// is created for a path that is not there, and the PRESENT cases — empty,
// and holding live holds — keep today's exact text and exit codes.

// runCaptureBoth runs one CLI invocation with both stdout and stderr
// captured (the single-stream captureStdout/captureStderr helpers nest:
// each swaps its own os.File).
func runCaptureBoth(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stderr string
	code, stdout := captureStdout(t, func() int {
		c, errOut := captureStderr(t, func() int { return run(args) })
		stderr = errOut
		return c
	})
	return code, stdout, stderr
}

// assertStillAbsent fails the test if anything was created at path — the
// refusal half of the contract: a read/containment verb must not mint a
// journal for a run dir that does not exist.
func assertStillAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the refusal was not read-only: something now exists at %s (stat err=%v)", path, err)
	}
}

// TestRevertRefusesAbsentRunDir: `revert --dir <absent>` — the kill switch
// path (`--all`) and the single-hold path alike — exits 1, names the
// directory state on stderr, prints NO `kill switch:` success line on
// stdout, and creates nothing.
func TestRevertRefusesAbsentRunDir(t *testing.T) {
	t.Run("all", func(t *testing.T) {
		absent := filepath.Join(t.TempDir(), "typo-run")
		code, out, errOut := runCaptureBoth(t, "revert", "--all", "--dir", absent)
		if code != 1 {
			t.Fatalf("revert --all on an absent run dir: exit %d, want 1\nstdout=%s\nstderr=%s", code, out, errOut)
		}
		if strings.Contains(out, "kill switch:") {
			t.Fatalf("absent run dir still printed a kill-switch success line:\n%s", out)
		}
		want := "revert: run dir " + absent + " absent (nothing to revert)"
		if !strings.Contains(errOut, want) {
			t.Fatalf("stderr does not name the absent directory state:\nwant %q\ngot %q", want, errOut)
		}
		assertStillAbsent(t, absent)
	})
	t.Run("hold", func(t *testing.T) {
		absent := filepath.Join(t.TempDir(), "rotated-run")
		code, out, errOut := runCaptureBoth(t, "revert", "--hold", "some-id", "--dir", absent)
		if code != 1 {
			t.Fatalf("revert --hold on an absent run dir: exit %d, want 1\nstdout=%s\nstderr=%s", code, out, errOut)
		}
		if !strings.Contains(errOut, "absent") {
			t.Fatalf("stderr does not name the absent directory state: %q", errOut)
		}
		assertStillAbsent(t, absent)
	})
}

// TestStatusNamesAbsentRunDir: `status --dir <absent>` prints the absent
// state (`run_dir:` + `state: absent`) instead of counts — no `boot_id:`,
// no `holds:`, no `proofs:` — and exits 1 without creating anything.
func TestStatusNamesAbsentRunDir(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "rotated-run")
	code, out, errOut := runCaptureBoth(t, "status", "--dir", absent)
	if code != 1 {
		t.Fatalf("status on an absent run dir: exit %d, want 1\nstdout=%s\nstderr=%s", code, out, errOut)
	}
	if !strings.Contains(out, "run_dir: "+absent) {
		t.Fatalf("absent status does not name the run dir:\n%s", out)
	}
	if !strings.Contains(out, "state: absent") {
		t.Fatalf("absent status does not carry the absent state line:\n%s", out)
	}
	for _, banned := range []string{"boot_id:", "holds:", "proofs:"} {
		if strings.Contains(out, banned) {
			t.Fatalf("absent status printed the %q line — counts invented over nothing:\n%s", banned, out)
		}
	}
	assertStillAbsent(t, absent)
}

// TestPresentEmptyRunDirKeepsCleanSweep is the control arm for the absent
// refusal: a PRESENT-but-empty run dir keeps today's exact contract —
// `revert --all` prints the green zero-hold summary with rc 0, `status`
// prints its boot_id over holds: 0 / proofs: 0 with rc 0.
func TestPresentEmptyRunDirKeepsCleanSweep(t *testing.T) {
	dir := t.TempDir() // exists, empty
	code, out, errOut := runCaptureBoth(t, "revert", "--all", "--dir", dir)
	if code != 0 {
		t.Fatalf("revert --all on a present-empty run dir: exit %d, want 0\nstdout=%s\nstderr=%s", code, out, errOut)
	}
	if !strings.Contains(out, "kill switch: 0 holds reverted, wall=0s (AC-6 budget 2s)") {
		t.Fatalf("present-empty sweep text changed:\n%s", out)
	}
	code, out, errOut = runCaptureBoth(t, "status", "--dir", dir)
	if code != 0 {
		t.Fatalf("status on a present-empty run dir: exit %d, want 0\nstdout=%s\nstderr=%s", code, out, errOut)
	}
	if !strings.Contains(out, "boot_id: ") || !strings.Contains(out, "holds: 0") || !strings.Contains(out, "proofs: 0") {
		t.Fatalf("present-empty status text changed:\n%s", out)
	}
}

// TestPresentRunWithHoldsUnchanged is the second control arm: a present
// run dir holding an ARMED hold keeps today's exact behaviour on both
// verbs — status lists the hold with its boot_id (rc 0), and the kill
// switch reverts it to the green one-hold summary (rc 0).
func TestPresentRunWithHoldsUnchanged(t *testing.T) {
	writeSanctionMarker(t)
	dir := t.TempDir()
	hold, _, _ := scratchCLIHold(t, "P-CLI-DF60")
	armCLIHolds(t, dir, hold)

	code, out, errOut := runCaptureBoth(t, "status", "--dir", dir)
	if code != 0 {
		t.Fatalf("status on a run dir with holds: exit %d, want 0\nstdout=%s\nstderr=%s", code, out, errOut)
	}
	if !strings.Contains(out, "boot_id: ") || !strings.Contains(out, "holds: 1") {
		t.Fatalf("status over an armed hold changed:\n%s", out)
	}

	code, out, errOut = runCaptureBoth(t, "revert", "--all", "--dir", dir)
	if code != 0 {
		t.Fatalf("revert --all over an armed hold: exit %d, want 0\nstdout=%s\nstderr=%s", code, out, errOut)
	}
	if !strings.Contains(out, "kill switch: 1 holds reverted") || strings.Contains(out, "revert_failed") {
		t.Fatalf("kill-switch text over an armed hold changed:\n%s", out)
	}
}
