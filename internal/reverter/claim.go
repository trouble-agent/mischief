package reverter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// revertClaimFileName is the run dir's cross-process revert claim. It is a
// LOCK, not a record: the file's existence means nothing (a crashed process
// leaves it behind and the next attempt adopts it), the kernel's flock on it
// is the truth — the lock dies with its holder, so there is no stale-lock
// recovery step to get wrong (MISCHIEF-DF-71).
const revertClaimFileName = "revert.lock"

// ErrAlreadyAttempted is the decline of an attempt that lost the run dir's
// revert claim to an attempt that already ran: the hold gained its
// revert_proof row between this caller's fold and the moment the claim was
// taken, i.e. another process (a second serve daemon, a detached TTL owner,
// a concurrent `revert --hold`) already executed this hold's recorded
// inverse. The inverse is not assumed idempotent, so the loser declines
// naming the existing evidence instead of running the undo a second time
// (MISCHIEF-DF-71 AC3: one hold, one undo).
var ErrAlreadyAttempted = errors.New("reverter: another process already executed a revert attempt for this hold")

// revertClaim is an exclusive cross-process claim over one run dir's revert
// path, held for the duration of ONE attempt (re-check → execute the
// recorded inverse → append the proof → release).
//
// Why a claim and not just the in-process mutex: two independent folds over
// the same journal each hold their OWN store and mutex, so both can observe
// the same hold as armed and both can execute its recorded inverse. The
// journal file's bytes are the shared state; its directory is the shared
// place to put the lock. flock is per open file description, so two handles
// in ONE process contend exactly like two processes do — the in-process
// concurrency tests exercise the same mechanism the CLI cross-process path
// uses.
type revertClaim struct {
	f *os.File
}

// claimRevert takes the run dir's revert claim, blocking until it is free.
// Blocking (not try-lock) is the right shape here: the holder's critical
// section is one native inverse plus one fsync, so a waiter's latency is
// bounded by the work it is waiting for, and refusing on contention would
// turn a natural queue into a spurious failure. The caller must release the
// claim on every path (a deferred release immediately after a successful
// claim).
func claimRevert(dir string) (*revertClaim, error) {
	path := filepath.Join(dir, revertClaimFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("reverter: revert claim: %w", err)
	}
	if err := flockExclusive(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("reverter: revert claim %s: %w", path, err)
	}
	return &revertClaim{f: f}, nil
}

// release drops the claim (unlock, then close). It is safe on a nil claim
// and idempotent, so `defer claim.release()` is always correct.
func (c *revertClaim) release() error {
	if c == nil || c.f == nil {
		return nil
	}
	f := c.f
	c.f = nil
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return f.Close()
}

// flockExclusive takes a blocking exclusive flock, retrying on EINTR: a
// signal delivered while waiting must not convert the claim into a silent
// skip, because a skipped claim is an unprotected revert.
func flockExclusive(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err == nil {
			return nil
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		return err
	}
}
