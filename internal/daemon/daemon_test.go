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

// TestDaemon_SurvivesRefoldError verifies MISCHIEF-DF-39: when the journal
// has a torn line, Refold() returns an error but the daemon does NOT exit —
// it continues beating, renders health.json with ok:false + refold_error,
// and recovers to ok:true once the journal is repaired.
func TestDaemon_SurvivesRefoldError(t *testing.T) {
	dir := t.TempDir()
	
	// Create an empty journal file — Refold() expects it to exist
	journalPath := filepath.Join(dir, "journal.jsonl")
	if err := os.WriteFile(journalPath, []byte{}, 0644); err != nil {
		t.Fatalf("create journal: %v", err)
	}
	
	d, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Start the daemon in a goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()

	// Wait for first healthy beat — poll until health.json appears and is ok:true
	var health Health
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		if b, err := os.ReadFile(filepath.Join(dir, "health.json")); err == nil {
			if err := json.Unmarshal(b, &health); err == nil && health.OK {
				break
			}
		}
	}
	if !health.OK {
		t.Fatalf("expected ok:true on first beat, got ok:false (refold_error=%s)", health.RefoldErr)
	}

	// Corrupt the journal with a torn line (incomplete JSON)
	f, err := os.OpenFile(journalPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	_, err = f.WriteString(`{"seq":99,"payload":"torn-line-no-closing-brace`)
	if err != nil {
		t.Fatalf("write torn line: %v", err)
	}
	f.Close()

	// Wait for the next beat — daemon should survive and render degraded
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		health = readHealth(t, dir)
		if !health.OK && health.RefoldErr != "" {
			break
		}
	}
	if health.OK {
		t.Fatalf("expected ok:false after torn line, got ok:true")
	}
	if health.RefoldErr == "" {
		t.Fatalf("expected refold_error field, got empty")
	}
	if !strings.Contains(health.RefoldErr, "refold") {
		t.Fatalf("refold_error should mention refold, got: %s", health.RefoldErr)
	}

	// Verify daemon is still alive (did not exit)
	select {
	case err := <-errCh:
		t.Fatalf("daemon exited prematurely: %v", err)
	default:
		// good — still running
	}

	// Repair the journal by truncating the torn line
	content, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	// Find the last complete line (before the torn one)
	lastNewline := strings.LastIndex(string(content), "\n")
	if lastNewline > 0 {
		err = os.WriteFile(journalPath, content[:lastNewline+1], 0644)
		if err != nil {
			t.Fatalf("truncate journal: %v", err)
		}
	}

	// Wait for recovery — next successful Refold should clear refoldErr
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		health = readHealth(t, dir)
		if health.OK && health.RefoldErr == "" {
			break
		}
	}
	if !health.OK {
		t.Fatalf("expected ok:true after repair, got ok:false (refold_error=%s)", health.RefoldErr)
	}
	if health.RefoldErr != "" {
		t.Fatalf("expected refold_error cleared after repair, got: %s", health.RefoldErr)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Fatalf("unexpected exit error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not exit after cancel")
	}
}
		t.Fatalf("expected ok:true after repair, got ok:false (refold_error=%s)", health.RefoldErr)
	}
	if health.RefoldErr != "" {
		t.Fatalf("expected refold_error cleared after repair, got: %s", health.RefoldErr)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Fatalf("unexpected exit error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not exit after cancel")
	}
}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not exit after cancel")
	}
}
// TestDaemon_SurvivesRefoldError verifies MISCHIEF-DF-39: when the journal
// has a torn line, Refold() returns an error but the daemon does NOT exit —
// it continues beating, renders health.json with ok:false + refold_error,
// and recovers to ok:true once the journal is repaired.
func TestDaemon_SurvivesRefoldError(t *testing.T) {
	dir := t.TempDir()
	d, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Start the daemon in a goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()

	// Wait for first healthy beat
	time.Sleep(150 * time.Millisecond)
	health := readHealth(t, dir)
	if !health.OK {
		t.Fatalf("expected ok:true on first beat, got ok:false")
	}

	// Corrupt the journal with a torn line (incomplete JSON)
	journalPath := filepath.Join(dir, "journal.jsonl")
	f, err := os.OpenFile(journalPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	_, err = f.WriteString(`{"seq":99,"payload":"torn-line-no-closing-brace`)
	if err != nil {
		t.Fatalf("write torn line: %v", err)
	}
	f.Close()

	// Wait for the next beat — daemon should survive and render degraded
	time.Sleep(150 * time.Millisecond)
	health = readHealth(t, dir)
	if health.OK {
		t.Fatalf("expected ok:false after torn line, got ok:true")
	}
	if health.RefoldErr == "" {
		t.Fatalf("expected refold_error field, got empty")
	}
	if !strings.Contains(health.RefoldErr, "refold") {
		t.Fatalf("refold_error should mention refold, got: %s", health.RefoldErr)
	}

	// Verify daemon is still alive (did not exit)
	select {
	case err := <-errCh:
		t.Fatalf("daemon exited prematurely: %v", err)
	default:
		// good — still running
	}

	// Repair the journal by truncating the torn line
	lines, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	// Find the last complete line (before the torn one)
	content := string(lines)
	lastNewline := strings.LastIndex(content[:len(content)-1], "\n")
	if lastNewline > 0 {
		err = os.WriteFile(journalPath, []byte(content[:lastNewline+1]), 0644)
		if err != nil {
			t.Fatalf("truncate journal: %v", err)
		}
	}

	// Wait for recovery — next successful Refold should clear refoldErr
	time.Sleep(150 * time.Millisecond)
	health = readHealth(t, dir)
	if !health.OK {
		t.Fatalf("expected ok:true after repair, got ok:false (refold_error=%s)", health.RefoldErr)
	}
	if health.RefoldErr != "" {
		t.Fatalf("expected refold_error cleared after repair, got: %s", health.RefoldErr)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Fatalf("unexpected exit error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not exit after cancel")
	}
}

func readHealth(t *testing.T, dir string) Health {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "health.json"))
	if err != nil {
		t.Fatalf("read health.json: %v", err)
	}
	var h Health
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatalf("unmarshal health: %v", err)
	}
	return h
}
