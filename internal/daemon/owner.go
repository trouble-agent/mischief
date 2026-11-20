package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ownerFileName is the run dir's daemon ownership lock. It is a LOCK, not a
// record: the file's existence means nothing (a killed daemon leaves it
// behind, and the next serve adopts it), the kernel's flock on it is the
// truth — the lock dies with the process holding it, so there is no
// stale-lock cleanup to get wrong.
const ownerFileName = "mischiefd.lock"

// ownerPIDRetries bounds the wait for a just-acquired holder to publish its
// pid. The holder writes the pid inside the same AcquireOwner call that takes
// the lock, so the only observable window is a few microseconds wide; the
// retries exist so a refusal that lands inside that window still NAMES the
// owner instead of degrading to "pid unknown".
const (
	ownerPIDRetries = 5
	ownerPIDDelay   = 10 * time.Millisecond
)

// ErrRunDirOwned is the refusal a second `mischief serve --dir D` gets while
// another daemon owns D. MISCHIEF-DF-71: before this guard, a second serve
// silently shared the run dir — it re-folded the same journal, beat the same
// health.json (so the `pid` field named the newest daemon while the older one
// kept ticking, and an operator watching health.json could not tell two
// daemons from one), swept the same holds, and could execute a hold's
// recorded inverse a second time. The run dir is single-writer state, so the
// guard is an exclusive claim over it, refused by name rather than shared.
var ErrRunDirOwned = errors.New("daemon: run dir is already owned by a live daemon")

// Owner is the exclusive claim one running daemon holds over its run dir for
// its whole lifetime. It is the run dir's ownership authority: while it is
// held, every other serve on that dir is refused, and health.json has exactly
// one writer (so its `pid` field can never name a process other than the one
// that wrote it).
type Owner struct {
	path string
	f    *os.File
	pid  int
}

// AcquireOwner takes the run dir's ownership lock, refusing by name when a
// live daemon already holds it. The refusal is the product's named-refusal
// contract: the caller exits nonzero with this error's text on stderr and no
// daemon loop is started.
//
// It creates the run dir (a serve on a fresh --dir is legitimate; a refusal
// can only happen on a dir that already exists) and does nothing else — the
// refusal happens BEFORE the store is opened, so a refused serve cannot
// write journal records (reverter.Open reconciles previous-boot holds and can
// append hold_stuck lines) or health.json.
func AcquireOwner(dir string) (*Owner, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("daemon: run dir: %w", err)
	}
	path := filepath.Join(dir, ownerFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("daemon: owner lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ownerRefusal(dir, ownerPIDOf(path))
		}
		return nil, fmt.Errorf("daemon: owner lock %s: %w", path, err)
	}
	// We own the dir: publish OUR pid so the next serve's refusal can name
	// the process instead of describing a lock. This write is the only thing
	// an acquiring daemon does to the run dir before its store opens.
	if err := writeOwnerPID(f, os.Getpid()); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, err
	}
	return &Owner{path: path, f: f, pid: os.Getpid()}, nil
}

// ownerRefusal builds the named refusal. The pid is named when it is known
// and the reason says so when it is not — the refusal NEVER degrades into a
// silent share, and it never guesses at a pid.
func ownerRefusal(dir string, ownerPID int) error {
	if ownerPID > 0 {
		return fmt.Errorf("%w: %s is held by live pid %d (one daemon per run dir — stop it or pass a different --dir)", ErrRunDirOwned, dir, ownerPID)
	}
	return fmt.Errorf("%w: %s is held by a live process that has not published its pid yet (one daemon per run dir — stop it or pass a different --dir)", ErrRunDirOwned, dir)
}

// writeOwnerPID replaces the lock file's contents with pid. The lock is held
// here, so this cannot race another daemon's publish; truncating first keeps
// a longer previous pid from leaving trailing bytes.
func writeOwnerPID(f *os.File, pid int) error {
	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("daemon: owner pid: %w", err)
	}
	if _, err := f.WriteAt([]byte(strconv.Itoa(pid)+"\n"), 0); err != nil {
		return fmt.Errorf("daemon: owner pid: %w", err)
	}
	return nil
}

// ownerPIDOf reports the pid published by the process that holds the lock, or
// 0 when no LIVE process can be named. Advisory only: the LOCK is the
// authority, this is the name of the holder.
//
// The liveness filter is what keeps the refusal honest in the one window where
// the bytes can lag the lock: a holder publishes its pid immediately AFTER it
// takes the lock, so a refusal that arrives in that instant still sees the
// PREVIOUS owner's bytes. Naming a dead pid (or naming nobody) would be a lie
// about who owns the dir, so a pid that is not alive is not returned; the
// short retry lets the real holder finish publishing. A recycled pid can still
// be named wrongly — the pid is a name, the lock is the claim.
func ownerPIDOf(path string) int {
	for attempt := 0; attempt < ownerPIDRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(ownerPIDDelay)
		}
		pid := readPidFile(path)
		if pid > 0 && processAlive(pid) {
			return pid
		}
	}
	return 0
}

// readPidFile reads a pid written by writeOwnerPID, 0 when unreadable.
func readPidFile(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// processAlive reports whether pid names a live process we may signal
// (EPERM means it exists but is not ours).
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// PID is the pid the owner published (this process).
func (o *Owner) PID() int {
	if o == nil {
		return 0
	}
	return o.pid
}

// Release drops the ownership claim (unlock, then close). It is idempotent and
// safe on a nil Owner, so `defer own.Release()` is always correct. The pid
// bytes stay in the file on purpose: the lock is what marks the dir free, and
// a leftover pid is a useful "who owned it last" breadcrumb, never a claim.
func (o *Owner) Release() error {
	if o == nil || o.f == nil {
		return nil
	}
	f := o.f
	o.f = nil
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return f.Close()
}
