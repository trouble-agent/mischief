package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trouble-agent/mischief/internal/daemon"
)

// armTestArgs is the happy-path arm invocation against a scratch run dir:
// inverse restores a backup over a scratch path, check measures the restore.
// The backup is written FIRST (DF-23: arm probes its readability — an arm
// whose undo cannot execute is refused) into its OWN scratch dir (NOT the
// run dir: the refusal tests assert the run dir stays empty).
func armTestArgs(t *testing.T, dir string) []string {
	t.Helper()
	scratch := t.TempDir()
	backup := filepath.Join(scratch, "backup")
	if err := os.WriteFile(backup, []byte("pristine\n"), 0o644); err != nil {
		t.Fatalf("armTestArgs: write backup: %v", err)
	}
	return []string{
		"arm",
		"--fault", "S-001",
		"--target", "scratch:/tmp/mischief-arm-test",
		"--ttl", "10s",
		"--inverse-kind", "restore-file",
		"--inverse-params", "path=" + dir + "/fault-file,backup=" + backup,
		"--check-kind", "file-matches-backup",
		"--check-params", "path=" + dir + "/fault-file,backup=" + backup,
		"--dir", dir,
	}
}

// TestArmWritesHoldAndStatusShowsIt (DF-05 acceptance, admitted path): on a
// sanctioned (marker) host, `mischief arm` exits 0, writes the write-ahead
// arm record into the run dir, and `mischief status --dir` shows the armed
// hold with the fault id and a TTL remaining. Nothing else is written —
// arming does not land.
func TestArmWritesHoldAndStatusShowsIt(t *testing.T) {
	writeSanctionMarker(t)
	dir := t.TempDir()
	code, out := captureStdout(t, func() int { return run(armTestArgs(t, dir)) })
	if code != 0 {
		t.Fatalf("arm exit: %d, want 0", code)
	}
	if !strings.Contains(out, "armed ") || !strings.Contains(out, "fault=S-001") {
		t.Fatalf("arm output does not name the hold/fault: %q", out)
	}
	// status folds the same journal and shows the hold as armed.
	code, out = captureStdout(t, func() int { return run([]string{"status", "--dir", dir}) })
	if code != 0 {
		t.Fatalf("status exit: %d, want 0", code)
	}
	if !strings.Contains(out, "fault=S-001") || !strings.Contains(out, "armed") {
		t.Fatalf("status does not show the armed hold: %q", out)
	}
	// the journal dir carries exactly the run-dir artifacts of an arm
	// (journal + fold state) — no health.json, no proofs.
	if _, err := os.Stat(filepath.Join(dir, "health.json")); !os.IsNotExist(err) {
		t.Fatalf("arm wrote health.json into the run dir")
	}
}

// TestArmRefusesUnsanctionedHost (DF-05, SPEC-13 gate coverage): arm is the
// fault-landing path's write-ahead half, so the sanction gate covers it.
// On an unsanctioned host the verb refuses fail-closed (exit 2), names the
// missing marker, and writes NOTHING into the run dir.
func TestArmRefusesUnsanctionedHost(t *testing.T) {
	// CI runners (and worktree runs on a sanctioned lane) can carry
	// MISCHIEF_SANCTION=1 in the ambient env; the env half alone admits
	// the arm and this refusal test dies a phantom red (MISCHIEF-CI-2).
	// Neutralize both halves: only the exact value "1" sanctions, so an
	// empty MISCHIEF_SANCTION is neutral, and the marker override pins the
	// file half to an absent path regardless of any ambient
	// MISCHIEF_SANCTION_FILE. t.Setenv restores the original ambient
	// values after the test. (Same idiom as internal/sanction's
	// TestThisHostRefuses.)
	t.Setenv("MISCHIEF_SANCTION", "")
	t.Setenv("MISCHIEF_SANCTION_FILE", "/nonexistent/sanction-marker")
	dir := t.TempDir()
	code, stderr := captureStderr(t, func() int { return run(armTestArgs(t, dir)) })
	if code != 2 {
		t.Fatalf("arm on unsanctioned host: exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "not sanctioned") {
		t.Fatalf("refusal does not name the sanction check: %s", stderr)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("refused arm wrote into the run dir: %v", ents)
	}
}

// TestArmUsageValidation: missing required flags are usage errors (exit 2)
// naming every missing piece; a malformed k=v pair is refused at the flag.
func TestArmUsageValidation(t *testing.T) {
	writeSanctionMarker(t) // usage validation happens before the gate, but keep the posture honest
	dir := t.TempDir()
	// --fault and --target missing together: the refusal names BOTH.
	args := []string{"arm", "--ttl", "10s", "--inverse-kind", "remove-file",
		"--inverse-params", "path=" + dir + "/x", "--check-kind", "file-absent",
		"--check-params", "path=" + dir + "/x", "--dir", dir}
	code, stderr := captureStderr(t, func() int { return run(args) })
	if code != 2 {
		t.Fatalf("arm with missing flags: exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "--fault") || !strings.Contains(stderr, "--target") {
		t.Fatalf("usage refusal does not name the missing flags: %s", stderr)
	}
	// malformed inverse params pair (arg after the --inverse-params flag).
	mutateParam := func(args []string, flagName, bad string) {
		for i, a := range args {
			if a == flagName {
				args[i+1] = bad
				return
			}
		}
	}
	args = armTestArgs(t, dir)
	mutateParam(args, "--inverse-params", "path-only-no-equals")
	code, stderr = captureStderr(t, func() int { return run(args) })
	if code != 2 || !strings.Contains(stderr, "malformed pair") {
		t.Fatalf("malformed params: exit %d stderr=%q, want 2 + malformed pair", code, stderr)
	}
	// empty-VALUE param pair (an empty path is the classic no-op restore).
	args = armTestArgs(t, dir)
	mutateParam(args, "--inverse-params", "path=,backup=/tmp/b")
	code, stderr = captureStderr(t, func() int { return run(args) })
	if code != 2 || !strings.Contains(stderr, "malformed pair") {
		t.Fatalf("empty-value param: exit %d stderr=%q, want 2 + malformed pair", code, stderr)
	}
	// the run dir stayed empty through all three refusals.
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("refused arms wrote into the run dir: %v", ents)
	}
}

// TestArmToServeRevertAtTTL (DF-05 acceptance: serve reverts at TTL): arm a
// hold with a sub-second TTL against a scratch file, then run the reverter
// daemon's real Run loop (the serve verb's body) and let its heartbeat sweep
// the expiry. The hold must end measured-reverted and status must show the
// revert proof with role=owner.
func TestArmToServeRevertAtTTL(t *testing.T) {
	writeSanctionMarker(t)
	dir := t.TempDir()
	backup := filepath.Join(dir, "backup")
	faultFile := filepath.Join(dir, "fault-file")
	if err := os.WriteFile(backup, []byte("pristine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(faultFile, []byte("faulted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	args := armTestArgs(t, dir)
	// swap --ttl 10s → 1ms so the daemon's first heartbeat sweep finds it expired.
	for i, a := range args {
		if a == "10s" {
			args[i] = "1ms"
		}
	}
	code, out := captureStdout(t, func() int { return run(args) })
	if code != 0 {
		t.Fatalf("arm exit: %d, want 0", code)
	}
	// Run the serve verb's daemon loop for real: cancel after one heartbeat
	// (HeartbeatInterval = 5s) — the sweep reverts the expired hold.
	d, err := daemon.New(dir)
	if err != nil {
		t.Fatalf("daemon new: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	time.Sleep(daemon.HeartbeatInterval + 500*time.Millisecond)
	cancel()
	if err := <-done; err != nil && err != context.Canceled {
		t.Fatalf("daemon run: %v", err)
	}
	// status folds the proof: measured reverted, owner role, fault id.
	code, out = captureStdout(t, func() int { return run([]string{"status", "--dir", dir}) })
	if code != 0 {
		t.Fatalf("status exit: %d, want 0", code)
	}
	if !strings.Contains(out, "reverted") || !strings.Contains(out, "role=owner") {
		t.Fatalf("status lacks the measured owner revert proof: %q", out)
	}
	// the measured check's inverse really ran: the file is pristine again.
	b, err := os.ReadFile(faultFile)
	if err != nil || string(b) != "pristine\n" {
		t.Fatalf("inverse did not restore the file: %q err=%v", b, err)
	}
}

// TestArmOutputNamesOnlySupportedRevertRoute (MISCHIEF-DF-31): arm's
// write-ahead line is the user-facing contract for WHERE the hold reverts.
// It must name only a supported route — the serve verb's TTL sweep. The
// reverter's Lifecycle.Detach has no non-test caller and no CLI verb, so
// advertising "(or Detach)" pointed users at an unreachable path; this pins
// the message to the shipped surface (regression: fails on the old text,
// which contained "or Detach").
func TestArmOutputNamesOnlySupportedRevertRoute(t *testing.T) {
	writeSanctionMarker(t)
	dir := t.TempDir()
	code, out := captureStdout(t, func() int { return run(armTestArgs(t, dir)) })
	if code != 0 {
		t.Fatalf("arm exit: %d, want 0", code)
	}
	if !strings.Contains(out, "serve reverts at TTL") {
		t.Fatalf("arm output does not name the supported serve/TTL route: %q", out)
	}
	if strings.Contains(out, "Detach") {
		t.Fatalf("arm output advertises the unreachable Detach path: %q", out)
	}
}
