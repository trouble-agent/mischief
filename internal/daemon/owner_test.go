package daemon

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ownerTestChildEnv makes this TEST BINARY behave as a real daemon process
// instead of a test run: TestMain intercepts it before m.Run. A test binary
// is the only binary a package test can exec reliably, and the ownership guard
// is about PROCESSES — the pid in the refusal and in health.json must be a
// different process's, which cannot be proven in-process (both daemons there
// share os.Getpid()).
const ownerTestChildEnv = "MISCHIEFD_TEST_OWNER_CHILD_DIR"

func TestMain(m *testing.M) {
	if dir := os.Getenv(ownerTestChildEnv); dir != "" {
		runOwnerTestChild(dir)
		return
	}
	os.Exit(m.Run())
}

// runOwnerTestChild serves dir until it is killed, exactly like `serve`
// (ownership claim before the store, no metrics listener): health.json is the
// readiness surface the parent polls, and the exit path is the parent's kill.
func runOwnerTestChild(dir string) {
	d, err := New(dir)
	if err != nil {
		os.Exit(2)
	}
	if err := d.Run(context.Background()); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// TestSecondServeOnOwnedRunDirIsRefused is MISCHIEF-DF-71 AC1: a second
// `serve --dir D` while another daemon owns D is REFUSED with the product's
// named-refusal contract — nonzero (an error the CLI turns into stderr + rc 1),
// the owning pid named in the reason, no daemon loop started, and no write to
// the run dir (health.json keeps naming the daemon that wrote it).
//
// It runs the first daemon in a SEPARATE PROCESS so the pid clause is real:
// the refusal must name the child's pid, which differs from this process's.
func TestSecondServeOnOwnedRunDirIsRefused(t *testing.T) {
	dir := localTempDir(t)

	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), ownerTestChildEnv+"="+dir)
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child daemon: %v", err)
	}
	childPID := cmd.Process.Pid
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	if !waitForHealth(t, dir, func(h Health) bool { return h.PID == childPID && h.OK }) {
		t.Fatalf("child daemon never published health.json with pid %d (stderr: %q)", childPID, stderr.String())
	}
	// PREMISE: the pid under test is a DIFFERENT process's, so "the refusal
	// names the owner" cannot be satisfied by this process naming itself.
	if childPID == os.Getpid() {
		t.Fatalf("premise: child pid == test pid (%d), the cross-process claim would be vacuous", childPID)
	}
	boot := healthDoc(t, dir).DaemonBoot

	own, err := AcquireOwner(dir)
	if err == nil {
		_ = own.Release()
		t.Fatalf("a second daemon acquired run dir %s already owned by pid %d", dir, childPID)
	}
	if !errors.Is(err, ErrRunDirOwned) {
		t.Fatalf("refusal = %v, want ErrRunDirOwned", err)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(childPID)) {
		t.Fatalf("refusal %q does not name the owning pid %d", err.Error(), childPID)
	}
	// "nothing shared": the refused serve did not become a second health
	// writer, and health.json still names the daemon that wrote it.
	h := healthDoc(t, dir)
	if h.PID != childPID {
		t.Fatalf("health.pid = %d after the refusal, want the owning daemon's pid %d", h.PID, childPID)
	}
	if h.DaemonBoot != boot {
		t.Fatalf("health.daemon_boot changed to %q after the refusal, want the owner's %q", h.DaemonBoot, boot)
	}

	// the claim dies with its holder: kill the owner, then a fresh daemon may
	// take the dir (a stale lock file must NOT wedge the run dir forever).
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill child daemon: %v", err)
	}
	_, _ = cmd.Process.Wait()

	deadline := time.Now().Add(3 * time.Second)
	for {
		own, err = AcquireOwner(dir)
		if err == nil {
			break
		}
		if !errors.Is(err, ErrRunDirOwned) {
			t.Fatalf("AcquireOwner after the owner died: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("run dir %s stayed owned 3s after the owner was killed: %v", dir, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got, want := own.PID(), os.Getpid(); got != want {
		t.Fatalf("re-acquired claim publishes pid %d, want %d", got, want)
	}
	if err := own.Release(); err != nil {
		t.Fatalf("release re-acquired claim: %v", err)
	}
}

// TestRefusalNeverNamesADeadPid pins the refusal's honesty: the pid bytes are
// a breadcrumb written by the holder just after it takes the lock, so they can
// describe the PREVIOUS (dead) owner for an instant. A refusal must not report
// a dead process as the live owner — it says the holder has not published yet —
// and stale bytes must not wedge the dir once the holder is gone.
func TestRefusalNeverNamesADeadPid(t *testing.T) {
	dir := localTempDir(t)
	own, err := AcquireOwner(dir)
	if err != nil {
		t.Fatalf("AcquireOwner: %v", err)
	}

	// a pid that provably names no process: one past the kernel's pid ceiling
	raw, err := os.ReadFile("/proc/sys/kernel/pid_max")
	if err != nil {
		_ = own.Release()
		t.Skipf("no /proc/sys/kernel/pid_max on this host: %v", err)
	}
	max, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || max <= 0 {
		_ = own.Release()
		t.Skipf("unreadable pid_max %q", raw)
	}
	deadPID := max + 1
	if processAlive(deadPID) {
		_ = own.Release()
		t.Skipf("pid %d is alive on this host, cannot simulate a stale pid", deadPID)
	}
	// simulate the window: the lock is held, the bytes name a dead owner
	if err := os.WriteFile(filepath.Join(dir, ownerFileName), []byte(strconv.Itoa(deadPID)+"\n"), 0o600); err != nil {
		_ = own.Release()
		t.Fatalf("write stale pid: %v", err)
	}

	second, err := AcquireOwner(dir)
	if err == nil {
		_ = second.Release()
		_ = own.Release()
		t.Fatalf("second AcquireOwner succeeded while %d held the lock", os.Getpid())
	}
	if !errors.Is(err, ErrRunDirOwned) {
		_ = own.Release()
		t.Fatalf("refusal = %v, want ErrRunDirOwned", err)
	}
	if strings.Contains(err.Error(), strconv.Itoa(deadPID)) {
		_ = own.Release()
		t.Fatalf("refusal %q named dead pid %d as the live owner", err.Error(), deadPID)
	}
	if !strings.Contains(err.Error(), "not published") {
		_ = own.Release()
		t.Fatalf("refusal %q should say the holder has not published its pid", err.Error())
	}

	// and the stale bytes never wedge the dir: the lock is the authority
	if err := own.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	third, err := AcquireOwner(dir)
	if err != nil {
		t.Fatalf("AcquireOwner over stale bytes after the holder left: %v", err)
	}
	if got := third.PID(); got != os.Getpid() {
		t.Fatalf("re-acquired claim publishes %d, want %d (the stale bytes must be replaced)", got, os.Getpid())
	}
	if err := third.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

// TestRunRefusesSecondDaemonOnSameRunDirAndReleasesOnExit is the in-process
// half of AC1: the guard lives in the daemon's Run (so an embedder cannot
// bypass it), the refusal is immediate rather than a hang on the lock, the
// refused daemon leaves the owner's health document untouched, and the claim is
// released when the owning daemon exits so the next serve can start.
func TestRunRefusesSecondDaemonOnSameRunDirAndReleasesOnExit(t *testing.T) {
	dir := localTempDir(t)
	d1, err := New(dir)
	if err != nil {
		t.Fatalf("New owner: %v", err)
	}
	ctx1, cancel1 := context.WithCancel(context.Background())
	ownerDone := make(chan error, 1)
	go func() { ownerDone <- d1.Run(ctx1) }()
	if !waitForHealth(t, dir, func(h Health) bool { return h.PID == os.Getpid() && h.OK }) {
		cancel1()
		t.Fatalf("owner daemon never published health.json")
	}
	boot1 := healthDoc(t, dir).DaemonBoot

	d2, err := New(dir)
	if err != nil {
		t.Fatalf("New second: %v", err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	start := time.Now()
	err = d2.Run(ctx2)
	elapsed := time.Since(start)
	if !errors.Is(err, ErrRunDirOwned) {
		t.Fatalf("second Run err = %v, want ErrRunDirOwned", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("second Run took %s to refuse — the claim must be try-lock, not a blocking queue", elapsed)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(os.Getpid())) {
		t.Fatalf("refusal %q does not name the owning pid %d", err.Error(), os.Getpid())
	}
	// the refused daemon wrote nothing: the document is still the owner's
	if h := healthDoc(t, dir); h.DaemonBoot != boot1 || h.PID != os.Getpid() {
		t.Fatalf("health after refusal = boot %q pid %d, want owner's boot %q pid %d", h.DaemonBoot, h.PID, boot1, os.Getpid())
	}

	// the owner stops: the lock is released with it
	cancel1()
	select {
	case err := <-ownerDone:
		if err != nil && err != context.Canceled {
			t.Fatalf("owner exit error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("owner daemon did not exit after cancel")
	}

	d3, err := New(dir)
	if err != nil {
		t.Fatalf("New successor: %v", err)
	}
	ctx3, cancel3 := context.WithCancel(context.Background())
	defer cancel3()
	succDone := make(chan error, 1)
	go func() { succDone <- d3.Run(ctx3) }()
	if !waitForHealth(t, dir, func(h Health) bool { return h.DaemonBoot != boot1 && h.OK }) {
		t.Fatalf("successor daemon could not start after the owner exited (lock not released)")
	}
	cancel3()
	select {
	case err := <-succDone:
		if err != nil && err != context.Canceled {
			t.Fatalf("successor exit error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("successor daemon did not exit after cancel")
	}
}
