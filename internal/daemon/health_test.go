package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/trouble-agent/mischief/internal/reverter"
)

// healthDoc reads the published health.json (the file, not a test-local
// render) into a FRESH value: omitempty fields must never carry over.
func healthDoc(t *testing.T, dir string) Health {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, HealthFileName))
	if err != nil {
		t.Fatalf("read health.json: %v", err)
	}
	var h Health
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatalf("health parse: %v", err)
	}
	return h
}

// TestDaemonBootIDUniquePerNew (MISCHIEF-DF-32 AC1+AC2): every daemon
// process start mints its OWN daemon_boot — daemons over the SAME run
// dir (same journal, same host boot, same reverter fold id) get
// different values. The OLD behavior put the fold's host-boot-scoped
// BootID there, byte-identical across generations, so a stale health
// document was indistinguishable from the live daemon's.
func TestDaemonBootIDUniquePerNew(t *testing.T) {
	dir := localTempDir(t)
	live, backup := scratchPair(t, dir)

	// seed one hold so the fold carries state (the id must be unique
	// regardless of journal content, but a live-looking run dir is the
	// realistic shape)
	store, err := reverter.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := reverter.New(store).Arm(testHold(live, backup, time.Minute)); err != nil {
		t.Fatalf("arm: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	foldBoot := "" // the host-boot-scoped id every daemon used to publish
	daemonBoots := map[string]bool{}
	for i := 0; i < 3; i++ {
		d, err := New(dir)
		if err != nil {
			t.Fatalf("daemon %d: %v", i, err)
		}
		if i == 0 {
			foldBoot = d.lf.Status().BootID
			if foldBoot == "" {
				t.Fatal("fold boot id empty — test cannot compare scopes")
			}
		}
		h := d.render()
		if h.DaemonBoot == "" {
			t.Fatalf("daemon %d: daemon_boot empty", i)
		}
		if len(h.DaemonBoot) != 16 {
			t.Fatalf("daemon %d: daemon_boot %q is not hex16", i, h.DaemonBoot)
		}
		if daemonBoots[h.DaemonBoot] {
			t.Fatalf("daemon %d: daemon_boot %q repeats across process starts (AC1/AC2 violated)", i, h.DaemonBoot)
		}
		daemonBoots[h.DaemonBoot] = true
		if h.DaemonBoot == foldBoot {
			t.Fatalf("daemon %d: daemon_boot %q equals the host-boot-scoped fold id (stale-vs-live ambiguity is the bug)", i, h.DaemonBoot)
		}
		if h.PID != os.Getpid() {
			t.Fatalf("daemon %d: health pid %d, want %d", i, h.PID, os.Getpid())
		}
		d.store.Close()
	}
}

// TestDaemonBootIDRestartsGenerateNewValueOnDisk (MISCHIEF-DF-32 AC2+AC3):
// the SAME daemon's beats keep ONE stable daemon_boot across heartbeats
// (a wobbling id would be as useless as a shared one), while a RESTART
// publishes a DIFFERENT value into health.json — a consumer keying on
// daemon_boot can now tell the old document from the live daemon's.
func TestDaemonBootIDRestartsGenerateNewValueOnDisk(t *testing.T) {
	dir := localTempDir(t)

	readBoot := func() string { return healthDoc(t, dir).DaemonBoot }

	// daemon 1: first beat carries boot id A; the id stays stable across
	// subsequent beats (stability within a process is part of the
	// contract — an id that changed per beat could not key a consumer).
	d1, err := New(dir)
	if err != nil {
		t.Fatalf("daemon1: %v", err)
	}
	ctx1, cancel1 := context.WithCancel(context.Background())
	done1 := make(chan error, 1)
	go func() { done1 <- d1.Run(ctx1) }()
	if !waitForHealth(t, dir, func(h Health) bool { return h.DaemonBoot != "" && h.OK }) {
		t.Fatal("daemon1 never published a healthy first beat")
	}
	bootA := readBoot()
	if bootA == "" {
		t.Fatal("daemon1's health.json carries an empty daemon_boot")
	}
	// stability leg: force an extra beat past the ticker and compare
	if err := d1.writeHealth(); err != nil {
		t.Fatalf("daemon1 second beat: %v", err)
	}
	if got := readBoot(); got != bootA {
		t.Fatalf("daemon_boot wobbled within one process: %q then %q", bootA, got)
	}
	cancel1()
	if err := <-done1; err != nil && err != context.Canceled {
		t.Fatalf("daemon1 run: %v", err)
	}

	// daemon 2 (a restart over the same run dir): new value, on disk
	d2, err := New(dir)
	if err != nil {
		t.Fatalf("daemon2: %v", err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	done2 := make(chan error, 1)
	go func() { done2 <- d2.Run(ctx2) }()
	defer func() {
		cancel2()
		if err := <-done2; err != nil && err != context.Canceled {
			t.Fatalf("daemon2 run: %v", err)
		}
	}()
	if !waitForHealth(t, dir, func(h Health) bool {
		// daemon1's final beat may still be standing (same-second
		// started_at is NOT a discriminator at this timescale) — wait
		// for daemon2's OWN boot id on disk
		return h.DaemonBoot == d2.daemonBoot
	}) {
		t.Fatal("daemon2 never published a beat carrying its own boot id")
	}
	bootB := readBoot()
	if bootB != d2.daemonBoot {
		t.Fatalf("health.json daemon_boot %q, want daemon2's own %q", bootB, d2.daemonBoot)
	}
	if bootB == bootA {
		t.Fatalf("restart published the same daemon_boot %q (AC2 violated on the disk surface)", bootA)
	}
}

// TestDaemonBootIDRandomGenerator: the id derivation itself is
// collision-resistant enough for the scope — 200 draws, all distinct,
// hex16 shape.
func TestDaemonBootIDRandomGenerator(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := randomDaemonBootID()
		if len(id) != 16 {
			t.Fatalf("draw %d: id %q is not hex16", i, id)
		}
		if seen[id] {
			t.Fatalf("draw %d: duplicate id %q in 200 draws", i, id)
		}
		seen[id] = true
	}
}

// TestDaemonBootIDSeamHonored: the id is captured at New through the
// seam — render carries the DAEMON's own identity even after the
// generator changes, proving the value is bound at boot, not read from
// the generator at render time.
func TestDaemonBootIDSeamHonored(t *testing.T) {
	dir := localTempDir(t)
	const marker = "seammarker0000ff"

	prev := daemonBootIDFunc
	daemonBootIDFunc = func() string { return marker }
	d, err := New(dir)
	daemonBootIDFunc = prev // restore BEFORE render: the id must already be bound
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer d.store.Close()
	if h := d.render(); h.DaemonBoot != marker {
		t.Fatalf("daemon_boot %q, want the seam-injected %q bound at New", h.DaemonBoot, marker)
	}
}

// TestDaemonBootIDConcurrentRenders: beats racing on the same daemon all
// publish the SAME process boot id. Concurrent calls go through
// writeHealth — render itself must run under d.mu (its documented caller
// contract: it mutates refoldErr), so each goroutine locks like
// writeHealth does; -race guards the read path of the boot id.
func TestDaemonBootIDConcurrentRenders(t *testing.T) {
	dir := localTempDir(t)
	d, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer d.store.Close()
	want := d.render().DaemonBoot
	if want == "" {
		t.Fatal("empty daemon_boot")
	}
	var wg sync.WaitGroup
	errs := make(chan string, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.mu.Lock()
			h := d.render()
			d.mu.Unlock()
			if got := h.DaemonBoot; got != want {
				errs <- got
			}
		}()
	}
	wg.Wait()
	close(errs)
	for got := range errs {
		t.Fatalf("racing render published %q, want %q", got, want)
	}
}
