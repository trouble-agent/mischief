package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/trouble-agent/mischief/internal/reverter"
)

// localTempDir mirrors internal/reverter's test scratch root: LOCAL /tmp,
// not t.TempDir() (the reverter suite measured the bulk mount's ~1.1s
// fsync; the journal's durability contracts measure the disk, not the
// mount). Cleanup registered.
func localTempDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "mischief-daemon-test-")
	if err != nil {
		t.Fatalf("local temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

// scratchPair writes a backup/live file pair over which the roundtrip runs.
func scratchPair(t *testing.T, dir string) (live, backup string) {
	t.Helper()
	live = filepath.Join(dir, "live")
	backup = filepath.Join(dir, "live.backup")
	if err := os.WriteFile(backup, []byte("GOOD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("GOOD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return live, backup
}

// testHold builds the roundtrip hold: restore-file inverse, measured
// file-matches-backup check, short TTL.
func testHold(live, backup string, ttl time.Duration) reverter.Hold {
	return reverter.Hold{
		FaultID: "P-DAEMON-RT",
		Target:  "file:" + live,
		Inverse: reverter.Decl{Kind: "restore-file", Params: map[string]string{"path": live, "backup": backup}},
		Check:   reverter.Decl{Kind: "file-matches-backup", Params: map[string]string{"path": live, "backup": backup}},
		TTL:     ttl,
	}
}

// TestDaemonJournalRevertRoundtrip: the full M1 loop over the run dir —
// arm (write-ahead), land, daemon sees the hold, TTL expires, daemon
// reverts MEASURED from journal bytes alone, health.json reports
// reverted, and a FRESH daemon (new process fold) reads the same terminal
// state. Nothing survives in memory only.
func TestDaemonJournalRevertRoundtrip(t *testing.T) {
	dir := localTempDir(t)
	live, backup := scratchPair(t, dir)

	// arm + land (the CLI side; the fault corrupts the live file)
	store, err := reverter.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	id, err := reverter.New(store).Arm(testHold(live, backup, 80*time.Millisecond))
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	if err := os.WriteFile(live, []byte("FAULTED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := reverter.New(store).Land(id); err != nil {
		t.Fatalf("land: %v", err)
	}
	store.Close()

	// daemon 1: boots, folds the journal, reverts at TTL expiry
	d1, err := New(dir)
	if err != nil {
		t.Fatalf("daemon new: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d1.Run(ctx) }()
	defer cancel()

	// wait for the measured revert (poll the file — the daemon owns the
	// timing; the test asserts the OUTCOME)
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, _ := os.ReadFile(live)
		if string(b) == "GOOD\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon did not revert at TTL: live=%q", b)
		}
		time.Sleep(HeartbeatInterval / 2)
	}
	cancel()
	if err := <-done; err != nil && err != context.Canceled {
		t.Fatalf("daemon run: %v", err)
	}

	// health.json reports the revert (the file, not memory)
	b, err := os.ReadFile(filepath.Join(dir, HealthFileName))
	if err != nil {
		t.Fatalf("health.json: %v", err)
	}
	var h Health
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatalf("health parse: %v", err)
	}
	if !h.OK || h.Reverted != 1 || h.RevertFailed != 0 || h.Stuck != 0 {
		t.Fatalf("health after revert: %+v", h)
	}
	if len(h.Holds) != 1 || h.Holds[0].Status != string(reverter.StatusReverted) {
		t.Fatalf("health holds: %+v", h.Holds)
	}

	// daemon 2: a FRESH process folds the same journal to the same state
	d2, err := New(dir)
	if err != nil {
		t.Fatalf("daemon2: %v", err)
	}
	defer d2.store.Close()
	h2 := d2.render()
	if h2.Reverted != 1 || len(h2.Holds) != 1 || h2.Holds[0].Status != string(reverter.StatusReverted) {
		t.Fatalf("fresh fold lost terminal state: %+v", h2)
	}
}

// TestDaemonHealthAtomicPublish: health.json never appears torn — the
// publish is temp+rename, so the document on disk always parses.
func TestDaemonHealthAtomicPublish(t *testing.T) {
	dir := localTempDir(t)
	live, backup := scratchPair(t, dir)
	d, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.store.Close()
	// arm a hold so the health doc carries content, then publish 50 times
	// while a reader parses every document it can open
	store := d.lf
	id, err := store.Arm(testHold(live, backup, time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	_ = id
	stop := make(chan struct{})
	readerErr := make(chan error, 1)
	go func() {
		defer close(readerErr)
		for {
			select {
			case <-stop:
				return
			default:
			}
			b, err := os.ReadFile(filepath.Join(dir, HealthFileName))
			if err != nil {
				continue // may not exist yet — not a torn document
			}
			var h Health
			if err := json.Unmarshal(b, &h); err != nil {
				readerErr <- err
				return
			}
		}
	}()
	for i := 0; i < 50; i++ {
		if err := d.writeHealth(); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	close(stop)
	if err := <-readerErr; err != nil {
		t.Fatalf("reader saw a torn document (atomicity violated): %v", err)
	}
}

// TestDaemonRevertFailedFlipsOK: a hold whose measured revert FAILS is
// the one state that flips ok=false (a live un-reverted fault is not ok)
// and counts in revert_failed.
func TestDaemonRevertFailedFlipsOK(t *testing.T) {
	dir := localTempDir(t)
	live, backup := scratchPair(t, dir)
	d, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.store.Close()
	// arm FIRST (DF-23: the arm-side probe requires a readable backup —
	// an undo that cannot execute is refused at arm), then sabotage the
	// undo by deleting the backup so the measured revert fails.
	id, err := d.lf.Arm(testHold(live, backup, time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	// land + revert directly through the lifecycle (the health fold reads
	// whatever the journal carries)
	if err := d.lf.Land(id); err != nil {
		t.Fatal(err)
	}
	pr, err := d.lf.Revert(context.Background(), id, "direct", reverter.RoleAPI)
	if err != nil {
		t.Fatalf("revert call: %v", err)
	}
	if pr.Outcome != reverter.OutcomeRevertFailed {
		t.Fatalf("sabotaged inverse graded %s, want revert_failed", pr.Outcome)
	}
	h := d.render()
	if h.OK {
		t.Fatal("ok stayed true with a revert_failed hold")
	}
	if h.RevertFailed != 1 {
		t.Fatalf("revert_failed count: %d, want 1", h.RevertFailed)
	}
}

// TestDaemonRefoldArmsAfterBootTTL (MISCHIEF-DF-30): a hold ARMED by another
// process after the daemon booted is durable in the journal but absent from
// the boot fold — the pre-fix daemon iterated that snapshot forever, so the
// fault stayed landed past TTL and health.json froze at boot state. The
// re-folding beats must: (AC1) revert the post-boot hold at TTL expiry on
// the SAME daemon (no restart), measured, with a proof in the journal; and
// (AC2) report that hold in health.json — it goes into the document armed
// and comes out reverted, while the boot-time hold stays truly untouched on
// both surfaces.
func TestDaemonRefoldArmsAfterBootTTL(t *testing.T) {
	dir := localTempDir(t)
	live, backup := scratchPair(t, dir)

	// boot-time hold: armed BEFORE the daemon starts, long TTL, never
	// landed — it must stay armed and untouched the whole test (the
	// control leg: refolding must not smear boot state).
	store1, err := reverter.Open(dir)
	if err != nil {
		t.Fatalf("open boot store: %v", err)
	}
	id1, err := reverter.New(store1).Arm(testHold(live, backup, time.Hour))
	if err != nil {
		t.Fatalf("arm boot hold: %v", err)
	}
	if err := store1.Close(); err != nil {
		t.Fatalf("close boot store: %v", err)
	}

	// daemon: boots, folds, beats.
	d1, err := New(dir)
	if err != nil {
		t.Fatalf("daemon new: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d1.Run(ctx) }()
	defer cancel()

	// POST-BOOT arm through a SECOND store (the CLI-process shape: its own
	// journal handle, its land, the fault corrupted after the arm): the
	// daemon must pick this up from the journal bytes, not congenially
	// never see it.
	store2, err := reverter.Open(dir)
	if err != nil {
		cancel()
		<-done
		t.Fatalf("open post-boot store: %v", err)
	}
	id2, err := reverter.New(store2).Arm(testHold(live, backup, HeartbeatInterval*2))
	if err != nil {
		cancel()
		<-done
		t.Fatalf("arm post-boot hold: %v", err)
	}
	if err := os.WriteFile(live, []byte("FAULTED\n"), 0o644); err != nil {
		cancel()
		<-done
		t.Fatal(err)
	}
	if err := reverter.New(store2).Land(id2); err != nil {
		cancel()
		<-done
		t.Fatalf("land post-boot hold: %v", err)
	}
	store2.Close()

	// AC1: the SAME, never-restarted daemon reverts it at TTL — poll the
	// measured file (the daemon owns the timing; the test asserts outcome)
	deadline := time.Now().Add(15 * time.Second)
	for {
		b, _ := os.ReadFile(live)
		if string(b) == "GOOD\n" {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatalf("post-boot hold never reverted in-daemon: live=%q", b)
		}
		time.Sleep(HeartbeatInterval / 2)
	}
	cancel()
	if err := <-done; err != nil && err != context.Canceled {
		t.Fatalf("daemon run: %v", err)
	}

	// the proof is in the journal with the DAEMON's pid (the poem lives on
	// disk, not in a nice test-local fold)
	store3, err := reverter.Open(dir)
	if err != nil {
		t.Fatalf("open proof store: %v", err)
	}
	defer store3.Close()
	proofs := 0
	for _, pr := range reverter.New(store3).Status().History {
		if pr.HoldID == id2 && pr.Outcome == reverter.OutcomeReverted && pr.Reason == "ttl_expiry" {
			proofs++
		}
	}
	if proofs != 1 {
		t.Fatalf("journal rotations for %s reverts: %d, want 1", id2, proofs)
	}

	// AC2: health.json shows the post-boot hold (reverted, mine now) AND
	// the boot-time hold still armed — the document reports the journal,
	// not the boot snapshot.
	bh, err := os.ReadFile(filepath.Join(dir, HealthFileName))
	if err != nil {
		t.Fatalf("health.json: %v", err)
	}
	var h Health
	if err := json.Unmarshal(bh, &h); err != nil {
		t.Fatalf("health parse: %v", err)
	}
	byHold := map[string]HealthHold{}
	for _, row := range h.Holds {
		byHold[row.HoldID] = row
	}
	if row, ok := byHold[id2]; !ok {
		t.Fatalf("health lost the post-boot hold: %+v", h.Holds)
	} else if row.Status != string(reverter.StatusReverted) {
		t.Fatalf("post-boot hold health status %q, want reverted", row.Status)
	}
	if row, ok := byHold[id1]; !ok || row.Status != string(reverter.StatusArmed) {
		t.Fatalf("boot-time hold smeared by refold: row=%+v ok=%v", row, ok)
	}
	if !h.OK || h.Reverted != 1 || h.RevertFailed != 0 || h.Stuck != 0 {
		t.Fatalf("health totals after post-boot revert: %+v", h)
	}
}
