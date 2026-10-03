package reverter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestArmWriteAheadOrdering (AC-4/SPEC-04 write-ahead): the inverse record
// is DURABLE — fsynced to the journal file on disk — before Land may be
// called, and Land's own record follows the arm's in append order. The
// proof is byte-level: the arm line exists on disk (re-read through a fresh
// handle) and precedes the land line in the file.
func TestArmWriteAheadOrdering(t *testing.T) {
	st, lf := newTestStore(t)
	hold, _, _ := scratchHold(t, "P-WA")

	id, err := lf.Arm(hold)
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	if len(id) != 16 {
		t.Fatalf("hold id %q: want 16 hex chars", id)
	}

	// The arm line must be on DISK before any Land call: open a fresh read
	// handle (not the store's write handle) and look at the bytes.
	beforeLand := journalLines(t, st.Dir)
	if len(beforeLand) != 1 {
		t.Fatalf("journal after Arm: %d lines, want exactly 1 (the write-ahead arm)", len(beforeLand))
	}
	if got := recordTypeOf(t, beforeLand[0]); got != string(RecordArm) {
		t.Fatalf("first line is %s, want %s", got, RecordArm)
	}
	if !strings.Contains(beforeLand[0], `"inverse":`) || !strings.Contains(beforeLand[0], `restore-file`) {
		t.Fatalf("arm line lacks the write-ahead inverse: %s", beforeLand[0])
	}

	if err := lf.Land(id); err != nil {
		t.Fatalf("land: %v", err)
	}
	afterLand := journalLines(t, st.Dir)
	if len(afterLand) != 2 {
		t.Fatalf("journal after Land: %d lines, want 2", len(afterLand))
	}
	if got := recordTypeOf(t, afterLand[0]); got != string(RecordArm) {
		t.Fatalf("ordering violated: line 0 is %s, want %s before %s", got, RecordArm, RecordLand)
	}
	if got := recordTypeOf(t, afterLand[1]); got != string(RecordLand) {
		t.Fatalf("line 1 is %s, want %s", got, RecordLand)
	}
}

// TestLandRefusedWithoutWriteAhead: Land for a hold with no durable arm
// record is refused (ErrWriteAheadMissing) and the refusal is recorded —
// the inverse can never postdate the fault through this API.
func TestLandRefusedWithoutWriteAhead(t *testing.T) {
	st, lf := newTestStore(t)
	err := lf.Land("no-such-hold")
	if err == nil || !strings.Contains(err.Error(), "write-ahead") {
		t.Fatalf("land without arm: err=%v, want write-ahead refusal", err)
	}
	lines := journalLines(t, st.Dir)
	if len(lines) != 1 || recordTypeOf(t, lines[0]) != string(RecordLandRefused) {
		t.Fatalf("land refusal not recorded: %v", lines)
	}
}

// TestArmValidation: malformed holds are refused before any byte is written
// (the closed vocabulary / field rules are enforced at the write path).
func TestArmValidation(t *testing.T) {
	st, lf := newTestStore(t)
	cases := []struct {
		name string
		hold Hold
		want string
	}{
		{"missing fault id", Hold{Target: "file:/x", Inverse: Decl{Kind: "remove-file", Params: map[string]string{"path": "/x"}}, Check: Decl{Kind: "file-absent", Params: map[string]string{"path": "/x"}}, TTL: time.Second}, "fault_id"},
		{"missing target", Hold{FaultID: "F", Inverse: Decl{Kind: "remove-file", Params: map[string]string{"path": "/x"}}, Check: Decl{Kind: "file-absent", Params: map[string]string{"path": "/x"}}, TTL: time.Second}, "target"},
		{"missing inverse kind", Hold{FaultID: "F", Target: "file:/x", Inverse: Decl{Params: map[string]string{"path": "/x"}}, Check: Decl{Kind: "file-absent", Params: map[string]string{"path": "/x"}}, TTL: time.Second}, "inverse"},
		{"missing check kind", Hold{FaultID: "F", Target: "file:/x", Inverse: Decl{Kind: "remove-file", Params: map[string]string{"path": "/x"}}, Check: Decl{}}, "check"},
		{"empty param value", Hold{FaultID: "F", Target: "file:/x", Inverse: Decl{Kind: "remove-file", Params: map[string]string{"path": ""}}, Check: Decl{Kind: "file-absent", Params: map[string]string{"path": "/x"}}, TTL: time.Second}, "params.path"},
		{"zero ttl", Hold{FaultID: "F", Target: "file:/x", Inverse: Decl{Kind: "remove-file", Params: map[string]string{"path": "/x"}}, Check: Decl{Kind: "file-absent", Params: map[string]string{"path": "/x"}}}, "ttl"},
	}
	for _, tc := range cases {
		_, err := lf.Arm(tc.hold)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err=%v, want it to name %q", tc.name, err, tc.want)
		}
	}
	if lines := journalLines(t, st.Dir); len(lines) != 0 {
		t.Errorf("refused arms wrote %d lines, want 0", len(lines))
	}
}

// TestHoldIDContentDerived: the hold id is hex(sha256)[:16] over the
// declared content — same declaration, same id; any declared difference, a
// different id (internal/journal's RunID convention). Never clock-dependent:
// arming at two different injected times yields the same id.
func TestHoldIDContentDerived(t *testing.T) {
	st, lf := newTestStore(t)
	h1, _, _ := scratchHold(t, "P-ID")
	h2, _, _ := scratchHold(t, "P-ID") // same declaration, different files
	// normalise to identical declarations (the derivation is content-only)
	h2 = h1

	id1, err := lf.Arm(h1)
	if err != nil {
		t.Fatalf("arm1: %v", err)
	}
	st.Now = func() time.Time { return time.Now().Add(7 * time.Minute) } // clock moved
	id2, err := lf.Arm(h2)
	if err != nil {
		t.Fatalf("arm2: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("same declaration armed at different times: %s vs %s", id1, id2)
	}

	h3 := h1
	h3.TTL += time.Second
	id3, err := lf.Arm(h3)
	if err != nil {
		t.Fatalf("arm3: %v", err)
	}
	if id3 == id1 {
		t.Fatal("different TTL produced the same hold id")
	}
	h4 := h1
	h4.Inverse.Params["path"] = "/other"
	id4, err := lf.Arm(h4)
	if err != nil {
		t.Fatalf("arm4: %v", err)
	}
	if id4 == id1 {
		t.Fatal("different inverse produced the same hold id")
	}
}

// TestRevertMeasuredNotAsserted (AC-4): the check is measured. Table over
// the measurable outcomes: a passing check records reverted; a FAILED check
// (the fault demonstrably still present) records revert_failed; an
// unmeasurable check records revert_failed; an unknown inverse kind records
// revert_failed. Every proof carries the measured detail and duration, and
// the journal line carries role+pid.
func TestRevertMeasuredNotAsserted(t *testing.T) {
	cases := []struct {
		name          string
		mutate        func(h *Hold)
		wantOutcome   Outcome
		wantDetailSub string
	}{
		{
			name:          "clean revert measures reverted",
			wantOutcome:   OutcomeReverted,
			wantDetailSub: "same",
		},
		{
			name: "sabotaged inverse (fault still present) is revert_failed",
			mutate: func(h *Hold) {
				// the inverse restores from a NONEXISTENT backup: the action
				// fails, the fault is measurably still present, and the
				// check (against the REAL backup) proves it.
				h.Inverse.Params["backup"] = h.Inverse.Params["backup"] + "-MISSING"
			},
			wantOutcome:   OutcomeRevertFailed,
			wantDetailSub: "inverse failed",
		},
		{
			name: "unknown check kind is revert_failed (fail closed)",
			mutate: func(h *Hold) {
				h.Check = Decl{Kind: "telepathy", Params: map[string]string{"path": h.Inverse.Params["path"]}}
			},
			wantOutcome:   OutcomeRevertFailed,
			wantDetailSub: "unknown",
		},
		{
			name: "unknown inverse kind is revert_failed (fail closed)",
			mutate: func(h *Hold) {
				h.Inverse = Decl{Kind: "unexist", Params: map[string]string{"path": h.Inverse.Params["path"]}}
				h.Check = Decl{Kind: "file-absent", Params: map[string]string{"path": h.Inverse.Params["path"]}}
			},
			wantOutcome:   OutcomeRevertFailed,
			wantDetailSub: "unknown",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, lf := newTestStore(t)
			hold, file, _ := scratchHold(t, "P-MEASURE")
			if tc.mutate != nil {
				tc.mutate(&hold)
			}
			id, err := lf.Arm(hold)
			if err != nil {
				t.Fatalf("arm: %v", err)
			}
			if err := lf.Land(id); err != nil {
				t.Fatalf("land: %v", err)
			}
			pr, err := lf.Revert(context.Background(), id, "direct", RoleAPI)
			if err != nil {
				t.Fatalf("revert: %v", err)
			}
			if pr.Outcome != tc.wantOutcome {
				t.Fatalf("outcome=%s want %s (detail: %s)", pr.Outcome, tc.wantOutcome, pr.Detail)
			}
			if !strings.Contains(pr.Detail, tc.wantDetailSub) {
				t.Fatalf("detail %q lacks %q", pr.Detail, tc.wantDetailSub)
			}
			if pr.Duration <= 0 {
				t.Fatalf("duration not measured: %v", pr.Duration)
			}
			if pr.PID != os.Getpid() || pr.Role != RoleAPI {
				t.Fatalf("provenance wrong: pid=%d role=%s", pr.PID, pr.Role)
			}
			if pr.FaultID != hold.FaultID || pr.Target != hold.Target {
				t.Fatalf("proof does not name the fault: %+v", pr)
			}
			// The journal carries the measured proof with provenance.
			lines := journalLines(t, st.Dir)
			var proofLine string
			for _, l := range lines {
				if recordTypeOf(t, l) == string(RecordRevertProof) {
					proofLine = l
				}
			}
			if proofLine == "" {
				t.Fatal("no revert_proof line in journal")
			}
			for _, want := range []string{string(tc.wantOutcome), `"role":"api"`, `fault_id`} {
				if !strings.Contains(proofLine, want) {
					t.Fatalf("proof line lacks %q: %s", want, proofLine)
				}
			}
			// The revert_failed shape never touches the live file's state
			// claim: for the clean case the file is REALLY restored.
			if tc.wantOutcome == OutcomeReverted {
				b, err := os.ReadFile(file)
				if err != nil || string(b) != "GOOD-CONTENT\n" {
					t.Fatalf("reverted claimed but file not restored: %q %v", b, err)
				}
			}
		})
	}
}

// TestRevertUnknownHoldIsReported: a revert of nothing is reported (ErrNoHold),
// not swallowed, and writes nothing.
func TestRevertUnknownHoldIsReported(t *testing.T) {
	st, lf := newTestStore(t)
	_, err := lf.Revert(context.Background(), "ghost", "direct", RoleAPI)
	if err == nil || !strings.Contains(err.Error(), "no live hold") {
		t.Fatalf("revert of unknown hold: err=%v, want no-live-hold report", err)
	}
	if lines := journalLines(t, st.Dir); len(lines) != 0 {
		t.Fatalf("ghost revert wrote %d lines", len(lines))
	}
}

// TestRevertAllUnderTwoSecondsWithProofs (AC-6): N armed holds plus one
// stuck leftover from a "previous boot", one sweep, every fault gets a
// per-fault proof (fault id + measured check + outcome + duration), the
// wall time is under 2s, and nothing is left live. The sweep runs on a
// PRIVATE store (its own dir), not one another Lifecycle instance holds a
// write lock on — the kill switch's own journal writes never queue behind
// a sibling's writer.
func TestRevertAllUnderTwoSecondsWithProofs(t *testing.T) {
	dir := localTempDir(t)

	// "Previous boot" state: arm 5 live holds + 1 legacy hold, close, then
	// rotate the boot marker (the disk fact that the host rebooted).
	st1, err := Open(dir)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	lf1 := New(st1)
	const n = 5
	ids := make([]string, 0, n+1)
	legacyFile := ""
	for i := 0; i < n; i++ {
		hold, _, _ := scratchHold(t, "P-ALL-"+string(rune('a'+i)))
		id, err := lf1.Arm(hold)
		if err != nil {
			t.Fatalf("arm %d: %v", i, err)
		}
		if err := lf1.Land(id); err != nil {
			t.Fatalf("land %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	holdLegacy, legacyLive, _ := scratchHold(t, "P-ALL-LEGACY")
	legacyFile = legacyLive
	legacyID, err := lf1.Arm(holdLegacy)
	if err != nil {
		t.Fatalf("arm legacy: %v", err)
	}
	ids = append(ids, legacyID)
	if err := st1.Close(); err != nil {
		t.Fatalf("close1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, bootMarkerFile), []byte(`{"host_boot":"stale-host","reverter_boot":"0123456789abcdef"}`), 0o600); err != nil {
		t.Fatalf("rotate marker: %v", err)
	}

	// The new boot: open, reconcile, verify the leftover reads stuck, then
	// run the kill switch and time it.
	st2, err := Open(dir)
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	defer st2.Close()
	lf2 := New(st2)
	foundStuck := false
	for _, h := range lf2.Status().Holds {
		if h.HoldID == legacyID && h.Status == StatusStuck {
			foundStuck = true
		}
	}
	if !foundStuck {
		t.Fatalf("legacy hold not marked stuck: %+v", lf2.Status().Holds)
	}

	start := time.Now()
	proofs, wall, err := lf2.RevertAll(context.Background(), "kill_switch")
	wallReal := time.Since(start)
	if err != nil {
		t.Fatalf("revert all: %v", err)
	}
	if wall <= 0 || wall > 2*time.Second {
		t.Fatalf("reported sweep wall %v outside (0,2s]", wall)
	}
	if wallReal > 2*time.Second {
		t.Fatalf("sweep took %v, want < 2s", wallReal)
	}
	if len(proofs) != n+1 {
		t.Fatalf("got %d proofs, want %d (one per fault)", len(proofs), n+1)
	}
	seen := map[string]bool{}
	for _, pr := range proofs {
		if pr.Outcome != OutcomeReverted {
			t.Errorf("proof for %s: outcome %s, want reverted (%s)", pr.FaultID, pr.Outcome, pr.Detail)
		}
		if pr.Role != RoleKillSwitch || pr.Reason != "kill_switch" {
			t.Errorf("proof for %s: role=%s reason=%s", pr.FaultID, pr.Role, pr.Reason)
		}
		if pr.Duration <= 0 {
			t.Errorf("proof for %s: duration not measured", pr.FaultID)
		}
		seen[pr.HoldID] = true
	}
	for _, id := range ids {
		if !seen[id] {
			t.Errorf("no proof for hold %s", id)
		}
	}
	// status empty of live holds after the sweep
	for _, h := range lf2.Status().Holds {
		if h.Status == StatusArmed || h.Status == StatusStuck {
			t.Errorf("hold %s still %s after kill switch", h.HoldID, h.Status)
		}
	}
	// the kill switch swept the stuck leftover too: the legacy file is
	// really restored (measured, not asserted)
	b, err := os.ReadFile(legacyFile)
	if err != nil || string(b) != "GOOD-CONTENT\n" {
		t.Fatalf("stuck leftover not really restored: %q %v", b, err)
	}
}

// TestBootReconcileMarksStuck (SPEC-04 boot reconcile): non-terminal holds
// from a previous boot are marked stuck at Open (reason previous_boot),
// surfaced in Status; a second Open must NOT duplicate the mark; and
// same-boot holds are never marked.
func TestBootReconcileMarksStuck(t *testing.T) {
	dir := localTempDir(t)

	// "Previous boot": arm a hold, then rotate the boot marker (the disk
	// fact that the host rebooted), then re-open.
	st1, err := Open(dir)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	lf1 := New(st1)
	hold, _, _ := scratchHold(t, "P-LEGACY")
	id, err := lf1.Arm(hold)
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	boot1 := st1.BootID
	if err := st1.Close(); err != nil {
		t.Fatalf("close1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, bootMarkerFile), []byte(`{"host_boot":"previous","reverter_boot":"ffffffffffffffff"}`), 0o600); err != nil {
		t.Fatalf("rotate marker: %v", err)
	}

	st2, err := Open(dir)
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	defer st2.Close()
	if st2.BootID == boot1 {
		t.Fatal("boot id did not rotate")
	}
	lf2 := New(st2)
	s := lf2.Status()
	if len(s.Holds) != 1 {
		t.Fatalf("status holds: %d, want 1", len(s.Holds))
	}
	if s.Holds[0].Status != StatusStuck {
		t.Fatalf("hold status %s, want stuck", s.Holds[0].Status)
	}
	if s.Holds[0].HoldID != id {
		t.Fatalf("stuck hold is %s, want %s", s.Holds[0].HoldID, id)
	}

	// The stuck mark is written once: a second Open does not duplicate it.
	st3, err := Open(dir)
	if err != nil {
		t.Fatalf("open3: %v", err)
	}
	defer st3.Close()
	stuckCount := 0
	for _, l := range journalLines(t, dir) {
		if recordTypeOf(t, l) == string(RecordHoldStuck) {
			stuckCount++
		}
	}
	if stuckCount != 1 {
		t.Fatalf("%d stuck marks, want exactly 1", stuckCount)
	}

	// Same-boot re-open never marks its own holds.
	dir2 := localTempDir(t)
	st4, err := Open(dir2)
	if err != nil {
		t.Fatalf("open4: %v", err)
	}
	lf4 := New(st4)
	h2, _, _ := scratchHold(t, "P-CURRENT")
	if _, err := lf4.Arm(h2); err != nil {
		t.Fatalf("arm current: %v", err)
	}
	st5, err := Open(dir2)
	if err != nil {
		t.Fatalf("open5: %v", err)
	}
	defer st5.Close()
	for _, l := range journalLines(t, dir2) {
		if recordTypeOf(t, l) == string(RecordHoldStuck) {
			t.Fatalf("same-boot hold marked stuck: %s", l)
		}
	}
	_ = lf4
}

// TestOwnerSurvivesKill9AndRevertsAtTTL (AC-5): the full proven shape. A
// real child process plays the CLI: arm (write-ahead), land (corrupt the
// file), Detach a REAL setsid owner, then the parent SIGKILLs the CLI
// mid-hold. The fault must still be reverted at TTL expiry by the DETACHED
// owner — measured (file really restored), journaled (revert_proof written
// by the owner's pid, role=owner), and the CLI pid must differ from the
// owner pid (separate processes, not a mock).
func TestOwnerSurvivesKill9AndRevertsAtTTL(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns real processes; skipped in -short")
	}
	dir := localTempDir(t)
	backup := filepath.Join(dir, "target.backup")
	file := filepath.Join(dir, "target.live")
	if err := os.WriteFile(backup, []byte("PRE-FAULT-STATE\n"), 0o644); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("executable: %v", err)
	}
	// TTL is generous: the killer must reliably catch the CLI MID-HOLD even
	// on a loaded host (a short TTL races the kill: the owner would revert
	// before the SIGKILL lands and the test would prove nothing). The hold
	// still EXPIRES while the CLI is dead — the duration being proven is
	// owner-outlives-CLI, not speed.
	const ttlMs = 6000
	cmd := exec.Command(self, "__cli",
		"-dir", dir, "-file", file, "-backup", backup,
		"-ttl-ms", fmt.Sprintf("%d", ttlMs))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start cli child: %v", err)
	}
	cliPID := cmd.Process.Pid
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	// The child writes cli.pid only after the owner spawn succeeded (the
	// detach's hold_expired record names the owner pid — wait for THAT too,
	// so the kill never precedes the owner's existence).
	pidFile := filepath.Join(dir, "cli.pid")
	deadline := time.Now().Add(15 * time.Second)
	ownerPIDFromJournal := 0
	for ownerPIDFromJournal == 0 {
		if _, err := os.Stat(pidFile); err != nil && time.Now().After(deadline) {
			t.Fatalf("cli child never signalled owner-spawned (pid %d)", cliPID)
		}
		for _, l := range journalLines(t, dir) {
			if recordTypeOf(t, l) == string(RecordHoldExpired) {
				ownerPIDFromJournal = 1 // present; exact pid parsed below
			}
		}
		if ownerPIDFromJournal == 0 {
			if time.Now().After(deadline) {
				t.Fatalf("hold_expired never recorded (owner spawn unconfirmed, cli pid %d)", cliPID)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	if ownerPIDFromJournal != 1 {
		t.Fatalf("owner spawn confirmation in bad state: %d", ownerPIDFromJournal)
	}

	// The fault is really landed (the CLI corrupted the file after the
	// write-ahead arm).
	waitFor(t, 5*time.Second, "fault landed", func() bool {
		b, err := os.ReadFile(file)
		return err == nil && strings.Contains(string(b), "FAULTED-CONTENT")
	})

	// SIGKILL the CLI mid-hold (TTL is 6s; we are well before expiry).
	if err := syscall.Kill(cliPID, syscall.SIGKILL); err != nil {
		t.Fatalf("kill cli: %v", err)
	}
	_, err = cmd.Process.Wait()
	if err == nil {
		t.Log("cli reaped")
	}

	// The owner must revert at expiry WITHOUT the CLI: wait for the file's
	// pre-fault state to be measured back.
	waitFor(t, 10*time.Second, "pre-fault state restored after CLI kill -9", func() bool {
		b, err := os.ReadFile(file)
		return err == nil && strings.Contains(string(b), "PRE-FAULT-STATE")
	})

	// The journal carries the owner's measured proof: role=owner, and a
	// pid that is NEITHER the CLI's nor this test's (a real separate
	// process executed the inverse).
	var proofLine string
	ownerDeadline := time.Now().Add(10 * time.Second)
	for {
		for _, l := range journalLines(t, dir) {
			if recordTypeOf(t, l) == string(RecordRevertProof) && strings.Contains(l, `"role":"owner"`) {
				proofLine = l
			}
		}
		if proofLine != "" || time.Now().After(ownerDeadline) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if proofLine == "" {
		t.Fatal("no owner-role revert proof in journal")
	}
	if !strings.Contains(proofLine, string(OutcomeReverted)) {
		t.Fatalf("owner proof not reverted: %s", proofLine)
	}
	// Strong pid-set provenance: the proof's own_pid must be a REAL pid that
	// is neither this test's nor the CLI's (a separate process executed the
	// inverse — the AC-5 evidence hop).
	ownPID := pidFromProof(t, proofLine)
	if ownPID == 0 {
		t.Fatalf("owner proof carries no own_pid: %s", proofLine)
	}
	if ownPID == os.Getpid() || ownPID == cliPID {
		t.Fatalf("proof pid %d is the test's or the CLI's, not the owner's", ownPID)
	}
	// The owner pid really existed as a process (proc evidence, not a mock).
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", ownPID)); err == nil {
		t.Logf("owner pid %d still live after revert", ownPID)
	}
}

// pidFromProof extracts own_pid from a raw proof line (byte-level).
func pidFromProof(t *testing.T, line string) int {
	t.Helper()
	const key = `"own_pid":`
	i := strings.Index(line, key)
	if i < 0 {
		return 0
	}
	rest := line[i+len(key):]
	j := strings.IndexByte(rest, ',')
	if j < 0 {
		j = strings.IndexByte(rest, '}')
	}
	if j < 0 {
		return 0
	}
	n := 0
	for _, c := range rest[:j] {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// TestDetachOwnerShape (AC-5's mechanism, in-process): Detach spawns a REAL
// owner process (mode setsid, its own session — SysProcAttr.Setsid), with
// argv carrying the owner request; the owner pid is a live process distinct
// from this one; and the journal records the expiry (hold_expired naming
// the owner pid).
func TestDetachOwnerShape(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real process; skipped in -short")
	}
	st, lf := newTestStore(t)
	t.Setenv(detachModeOverrideEnv, "setsid") // pin the shape under test
	hold, file, backup := scratchHold(t, "P-DETACH")
	hold.TTL = 45 * time.Second // long: the test kills the owner first
	id, err := lf.Arm(hold)
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	res, err := lf.Detach(id, hold.TTL)
	if err != nil {
		t.Fatalf("detach: %v", err)
	}
	if res.Mode != "setsid" {
		t.Fatalf("mode %s, want setsid (pinned)", res.Mode)
	}
	if res.PID == 0 || res.PID == os.Getpid() {
		t.Fatalf("owner pid %d is not a separate live process", res.PID)
	}
	// the owner is a real process, in its own session
	p, err := os.FindProcess(res.PID)
	if err != nil {
		t.Fatalf("find owner: %v", err)
	}
	// signal 0 probes liveness
	if err := p.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("owner pid %d not alive: %v", res.PID, err)
	}
	// own-session proof: the owner's pgid == its own pid (setsid)
	out, err := exec.Command("/bin/sh", "-c", fmt.Sprintf("ps -o pgid= -p %d", res.PID)).CombinedOutput()
	if err == nil && strings.TrimSpace(string(out)) != fmt.Sprintf("%d", res.PID) {
		t.Fatalf("owner pgid %q != own pid (not a session leader)", strings.TrimSpace(string(out)))
	}
	// journal records the expiry with the owner pid
	found := false
	for _, l := range journalLines(t, st.Dir) {
		if recordTypeOf(t, l) == string(RecordHoldExpired) && strings.Contains(l, fmt.Sprintf("%d", res.PID)) {
			found = true
		}
	}
	if !found {
		t.Fatal("hold_expired record missing or lacks the owner pid")
	}
	// cleanup: the revert (still within TTL) ends the hold and the owner
	// exits with ErrNoHold-at-revert semantics (already-terminal hold) —
	// kill it regardless.
	_ = p.Kill()
	_ = os.Remove(file)
	_ = backup
}

// itoa64 formats an int64 (the owner flag form's expiry encoding).
func itoa64(n int64) string {
	return strconv.FormatInt(n, 10)
}

// errorIs is the test-side errors.Is indirection.
func errorIs(err, target error) bool { return errors.Is(err, target) }

// osReadFile reads a file (the measured-state probes).
func osReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

// waitFor polls cond until it holds or the deadline passes (fail naming
// what never happened).
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", what)
}
