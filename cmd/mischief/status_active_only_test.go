package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/reverter"
)

// MISCHIEF-DF-54: `status` is the ACTIVE-hold surface (armed or stuck — the
// contract the Status type has always documented). Terminal holds
// (reverted / revert_failed) are recorded FACTS: they live in the proof
// history and the proof count, and `status --all` is the complete listing.
// Before the fix a clean `revert --all` still printed twelve terminal rows,
// each with a live-looking ttl_remaining, so AC-6's own proof step
// ("`mischief status` empty") could never pass.

// statusArmArgs builds a successful arm invocation for ONE distinct hold:
// its own scratch backup plus the faulted target the inverse will restore,
// and a check that measures the restore. Each call gets a fresh scratch dir
// (distinct target path), so two arms in one run dir yield two distinct
// content-derived hold ids and two independently revertible faults.
func statusArmArgs(t *testing.T, dir, fault string) []string {
	t.Helper()
	scratch := t.TempDir()
	backup := filepath.Join(scratch, "backup")
	target := filepath.Join(scratch, "fault-file")
	if err := os.WriteFile(backup, []byte("pristine\n"), 0o644); err != nil {
		t.Fatalf("statusArmArgs: write backup: %v", err)
	}
	if err := os.WriteFile(target, []byte("faulted\n"), 0o644); err != nil {
		t.Fatalf("statusArmArgs: write faulted target: %v", err)
	}
	return []string{
		"arm",
		"--fault", fault,
		"--target", "scratch:" + target,
		"--ttl", "10s",
		"--inverse-kind", "restore-file",
		"--inverse-params", "path=" + target + ",backup=" + backup,
		"--check-kind", "file-matches-backup",
		"--check-params", "path=" + target + ",backup=" + backup,
		"--dir", dir,
	}
}

// statusArm arms one hold through the real CLI and returns its hold id,
// read back from arm's own "armed <id> fault=..." line (never guessed).
func statusArm(t *testing.T, dir, fault string) string {
	t.Helper()
	code, out := captureStdout(t, func() int { return run(statusArmArgs(t, dir, fault)) })
	if code != 0 {
		t.Fatalf("arm %s exit: %d, want 0 (out=%q)", fault, code, out)
	}
	fields := strings.Fields(out)
	if len(fields) < 2 || fields[0] != "armed" {
		t.Fatalf("arm %s output does not name the hold id: %q", fault, out)
	}
	return fields[1]
}

// statusSections splits a status text document at its `proofs:` line: the
// hold rows are what sits between the `holds:` line and it; the proof rows
// follow. Structural (not substring-anywhere) assertions are what keep
// "the id appears somewhere in the output" from standing in for "the id is
// listed as an active hold" — every proof row also names a hold id.
func statusSections(t *testing.T, out string) (holds, proofs string) {
	t.Helper()
	i := strings.Index(out, "proofs: ")
	if i < 0 {
		t.Fatalf("status output has no proofs line: %q", out)
	}
	head, proofs := out[:i], out[i:]
	j := strings.Index(head, "holds: ")
	if j < 0 {
		t.Fatalf("status output has no holds line: %q", out)
	}
	rest := head[j:]
	nl := strings.Index(rest, "\n")
	if nl < 0 {
		t.Fatalf("status holds line is not terminated: %q", head)
	}
	return rest[nl+1:], proofs
}

// statusJSONHolds counts the hold rows of a `status --json` document,
// matching the "holds" key case-insensitively: the row COUNT is this row's
// contract, while the key SPELLING is the separate status --json casing
// work (MISCHIEF-DF-53) — this test must not fight it.
func statusJSONHolds(t *testing.T, out string) int {
	t.Helper()
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("status --json is not a JSON object: %v (%q)", err, out)
	}
	for k, v := range doc {
		if !strings.EqualFold(k, "holds") {
			continue
		}
		var rows []json.RawMessage
		if err := json.Unmarshal(v, &rows); err != nil {
			t.Fatalf("holds is not an array: %v (%s)", err, v)
		}
		return len(rows)
	}
	t.Fatalf("status --json has no holds key: %s", out)
	return -1
}

// TestStatusHidesTerminalHoldsAfterRevertAll is AC-6's proof step: after a
// clean `revert --all` the default status surface reads as ZERO active
// holds, no terminal row prints a live-looking ttl_remaining, and both
// reverts survive as facts in the proof history. `status --all` carries the
// complete listing.
func TestStatusHidesTerminalHoldsAfterRevertAll(t *testing.T) {
	writeSanctionMarker(t)
	dir := t.TempDir()
	idA := statusArm(t, dir, "S-001")
	idB := statusArm(t, dir, "S-004")

	code, out := captureStdout(t, func() int { return run([]string{"revert", "--all", "--dir", dir}) })
	if code != 0 {
		t.Fatalf("revert --all exit: %d, want 0 (out=%q)", code, out)
	}

	code, out = captureStdout(t, func() int { return run([]string{"status", "--dir", dir}) })
	if code != 0 {
		t.Fatalf("status exit: %d, want 0 (out=%q)", code, out)
	}
	if !strings.Contains(out, "holds: 0") {
		t.Fatalf("status after revert --all does not read as zero active holds: %q", out)
	}
	if strings.Contains(out, "ttl_remaining") {
		t.Fatalf("a terminal hold still printed a live-looking ttl_remaining: %q", out)
	}
	holds, proofs := statusSections(t, out)
	if strings.TrimSpace(holds) != "" {
		t.Fatalf("terminal holds are still listed as active holds:\n%s", holds)
	}
	if !strings.Contains(out, "proofs: 2") {
		t.Fatalf("the two measured reverts are not in the proof count: %q", out)
	}
	for _, id := range []string{idA, idB} {
		if !strings.Contains(proofs, id) {
			t.Errorf("terminal fact lost: hold %s has no proof row:\n%s", id, proofs)
		}
	}

	// --all is the complete listing: terminal rows come back, active-only
	// fields do not.
	code, out = captureStdout(t, func() int { return run([]string{"status", "--all", "--dir", dir}) })
	if code != 0 {
		t.Fatalf("status --all exit: %d, want 0 (out=%q)", code, out)
	}
	if !strings.Contains(out, "holds: 2") {
		t.Fatalf("status --all does not list the complete hold set: %q", out)
	}
	holds, _ = statusSections(t, out)
	for _, id := range []string{idA, idB} {
		if !strings.Contains(holds, id) {
			t.Errorf("status --all lost terminal hold %s:\n%s", id, holds)
		}
	}
	if !strings.Contains(holds, string(reverter.StatusReverted)) {
		t.Errorf("status --all does not carry the terminal status:\n%s", holds)
	}
	if strings.Contains(holds, "ttl_remaining") {
		t.Fatalf("status --all printed ttl_remaining for terminal rows:\n%s", holds)
	}
}

// TestStatusKeepsActiveHoldsAndTTL (control for the filter): a hold that
// still owes an inverse must stay visible — with its TTL remaining — in
// both listing modes, and only the LIVE row may carry a countdown. Without
// this arm, "the listing is empty" would pass for a status verb that lists
// nothing at all.
func TestStatusKeepsActiveHoldsAndTTL(t *testing.T) {
	writeSanctionMarker(t)
	dir := t.TempDir()
	live := statusArm(t, dir, "S-001")
	done := statusArm(t, dir, "S-004")

	code, out := captureStdout(t, func() int { return run([]string{"revert", "--hold", done, "--dir", dir}) })
	if code != 0 {
		t.Fatalf("revert --hold exit: %d, want 0 (out=%q)", code, out)
	}

	code, out = captureStdout(t, func() int { return run([]string{"status", "--dir", dir}) })
	if code != 0 {
		t.Fatalf("status exit: %d, want 0 (out=%q)", code, out)
	}
	if !strings.Contains(out, "holds: 1") {
		t.Fatalf("status does not list exactly the live hold: %q", out)
	}
	holds, proofs := statusSections(t, out)
	if !strings.Contains(holds, live) || !strings.Contains(holds, string(reverter.StatusArmed)) {
		t.Fatalf("the armed hold is missing from the default listing:\n%s", holds)
	}
	if !strings.Contains(holds, "ttl_remaining") {
		t.Fatalf("the armed hold lost its ttl_remaining:\n%s", holds)
	}
	if strings.Contains(holds, done) {
		t.Fatalf("the reverted hold %s is still listed as an active hold:\n%s", done, holds)
	}
	if !strings.Contains(proofs, done) {
		t.Errorf("the reverted hold's fact is missing from the proof history:\n%s", proofs)
	}

	// --all: both holds, exactly one ttl_remaining (the live row's).
	code, out = captureStdout(t, func() int { return run([]string{"status", "--all", "--dir", dir}) })
	if code != 0 {
		t.Fatalf("status --all exit: %d, want 0 (out=%q)", code, out)
	}
	if !strings.Contains(out, "holds: 2") {
		t.Fatalf("status --all does not list both holds: %q", out)
	}
	holds, _ = statusSections(t, out)
	for _, id := range []string{live, done} {
		if !strings.Contains(holds, id) {
			t.Errorf("status --all lost hold %s:\n%s", id, holds)
		}
	}
	if n := strings.Count(holds, "ttl_remaining"); n != 1 {
		t.Fatalf("%d ttl_remaining fields in --all, want exactly 1 (the armed row):\n%s", n, holds)
	}
}

// TestStatusJSONDefaultIsActiveSet: the JSON surface carries the same
// contract as the text one — a consumer reading `status --json` after a
// clean revert sees no active holds, and --json --all sees the complete set.
func TestStatusJSONDefaultIsActiveSet(t *testing.T) {
	writeSanctionMarker(t)
	dir := t.TempDir()
	id := statusArm(t, dir, "S-001")
	code, out := captureStdout(t, func() int { return run([]string{"revert", "--hold", id, "--dir", dir}) })
	if code != 0 {
		t.Fatalf("revert --hold exit: %d, want 0 (out=%q)", code, out)
	}

	code, out = captureStdout(t, func() int { return run([]string{"status", "--json", "--dir", dir}) })
	if code != 0 {
		t.Fatalf("status --json exit: %d, want 0 (out=%q)", code, out)
	}
	if n := statusJSONHolds(t, out); n != 0 {
		t.Fatalf("status --json default carries %d holds after a revert, want 0: %s", n, out)
	}

	code, out = captureStdout(t, func() int { return run([]string{"status", "--json", "--all", "--dir", dir}) })
	if code != 0 {
		t.Fatalf("status --json --all exit: %d, want 0 (out=%q)", code, out)
	}
	if n := statusJSONHolds(t, out); n != 1 {
		t.Fatalf("status --json --all carries %d holds, want 1: %s", n, out)
	}
}

// TestActiveSetEmptyAfterRevertIsTheLibraryContract pins the fix at the
// surface it reads from: after a measured revert the ACTIVE set is empty on
// both folds (the live read and the health-facing snapshot), the terminal
// fact survives in History, and the complete listings agree. The CLI filter
// is only honest while this holds.
func TestActiveSetEmptyAfterRevertIsTheLibraryContract(t *testing.T) {
	writeSanctionMarker(t)
	dir := t.TempDir()
	id := statusArm(t, dir, "S-004")
	code, out := captureStdout(t, func() int { return run([]string{"revert", "--hold", id, "--dir", dir}) })
	if code != 0 {
		t.Fatalf("revert --hold exit: %d, want 0 (out=%q)", code, out)
	}

	store, err := reverter.Open(dir)
	if err != nil {
		t.Fatalf("open run dir: %v", err)
	}
	defer store.Close()
	lf := reverter.New(store)

	st := lf.Status()
	if len(st.Holds) != 0 {
		t.Fatalf("active set after a measured revert: %+v, want empty", st.Holds)
	}
	if len(st.History) != 1 || st.History[0].Outcome != reverter.OutcomeReverted || st.History[0].HoldID != id {
		t.Fatalf("the terminal fact did not survive in History: %+v", st.History)
	}
	all := lf.StatusAll()
	if len(all.Holds) != 1 || all.Holds[0].HoldID != id || all.Holds[0].Status != reverter.StatusReverted {
		t.Fatalf("complete listing after a measured revert: %+v", all.Holds)
	}
	// The degraded read (the daemon's health leg) must answer the same way
	// in both modes, or health and status would disagree about the same fold.
	if snap := lf.StatusSnapshot(); len(snap.Holds) != 0 {
		t.Fatalf("snapshot active set: %+v, want empty", snap.Holds)
	}
	if snapAll := lf.StatusSnapshotAll(); len(snapAll.Holds) != 1 {
		t.Fatalf("snapshot complete listing: %+v, want the terminal hold", snapAll.Holds)
	}
}

// TestHoldStatusActiveVocabulary pins the one predicate the whole split
// rests on: armed/stuck are active (they still owe an inverse), reverted and
// revert_failed are terminal facts, and an unknown value is NOT active
// (fail-safe: an unrecognized status must never be advertised as a live
// hold).
func TestHoldStatusActiveVocabulary(t *testing.T) {
	cases := []struct {
		status reverter.HoldStatus
		want   bool
	}{
		{reverter.StatusArmed, true},
		{reverter.StatusStuck, true},
		{reverter.StatusReverted, false},
		{reverter.StatusRevertFailed, false},
		{reverter.HoldStatus(""), false},
		{reverter.HoldStatus("landed"), false},
	}
	for _, tc := range cases {
		if got := tc.status.Active(); got != tc.want {
			t.Errorf("%q.Active() = %v, want %v", tc.status, got, tc.want)
		}
	}
}
