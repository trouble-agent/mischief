// Package daemon is the reverter daemon (mischiefd, SPEC-04's surface):
// it owns the run dir's journal, folds state at boot (Open's reconcile
// marks previous-boot leftovers stuck), reverts at TTL expiry from journal
// bytes alone, and writes /health.json into the run dir so a reader never
// needs to talk to the daemon to answer "is mischief holding anything?".
//
// Health contract (PRD §6.1 mischiefd row: "owns TTLs, reconciles leftovers
// at boot, serves /health.json"): the file is written ATOMICALLY
// (temp+rename) on every state change and on a periodic heartbeat, so a
// concurrent reader never sees a half-written document. The document is
// scrubbed on write by the reverter's own scrub rules — it never carries
// credential-shaped material.
//
// NOT-LIST (M1 scope):
//
//   - The daemon owns ONE run dir (per-run session state, PRD §5: the
//     armed set is per-run; a global daemon over all runs is the wrong
//     turn the PRD names). A future supervisor may run one per run dir.
//   - It does not serve HTTP: /health.json is a FILE in the run dir (the
//     PRD's health shape on this fleet — a file a reverse proxy or a
//     curl can read), not a socket to guard.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/trouble-agent/mischief/internal/reverter"
)

// HealthFileName is the health document's name inside the run dir.
const HealthFileName = "health.json"

// HeartbeatInterval is how often the daemon refreshes health.json even
// when nothing changed (TTL remaining decays; a reader deserves fresh
// numbers without a state change).
const HeartbeatInterval = 5 * time.Second

// Health is the health document.
type Health struct {
	// OK is true when the daemon is folding state and no hold is in a
	// failed terminal state without an operator decision. Stuck holds from
	// previous boots do NOT flip OK to false (they are a reported
	// condition, not a daemon defect) — they are listed.
	OK bool `json:"ok"`
	// DaemonBoot is THIS daemon process's boot id (hex16), minted fresh
	// at every daemon start (MISCHIEF-DF-32) — a consumer keying on it
	// can tell a stale health.json from the live daemon's. It is NOT the
	// reverter fold id (that one is host-boot-scoped and shared by every
	// process of the same host boot; see internal/reverter/journal.go).
	DaemonBoot string `json:"daemon_boot"`
	// PID is the daemon process's pid.
	PID int `json:"pid"`
	// RunDir is the absolute run dir (the journal's home).
	RunDir string `json:"run_dir"`
	// StartedAt is the daemon's start time (unix seconds).
	StartedAt int64 `json:"started_at"`
	// GeneratedAt is this document's write time (unix seconds).
	GeneratedAt int64 `json:"generated_at"`
	// Holds is one entry per known hold with its status and TTL remaining.
	Holds []HealthHold `json:"holds"`
	// Stuck is the count of holds boot reconcile marked stuck (previous
	// boots' leftovers, PRD §7: surfaced in status and health).
	Stuck int `json:"stuck"`
	// Reverted is the count of holds with a measured revert proof.
	Reverted int `json:"reverted"`
	// RevertFailed is the count of holds whose measured revert FAILED
	// (the fault may still be live — the one state that flips OK false).
	RevertFailed int `json:"revert_failed"`
	// RefoldError is non-empty when the tick's journal re-fold failed
	// (corruption): the document then carries the LAST KNOWN fold and
	// ok=false — never a silent stale view (MISCHIEF-DF-30).
	RefoldError string `json:"refold_error,omitempty"`
}

// HealthHold is one hold's health row.
type HealthHold struct {
	HoldID          string  `json:"hold_id"`
	FaultID         string  `json:"fault_id"`
	Target          string  `json:"target"`
	Status          string  `json:"status"`
	TTLRemainingSec float64 `json:"ttl_remaining_sec,omitempty"`
	StuckAt         int64   `json:"stuck_at,omitempty"`
}

// Daemon is the reverter daemon over one run dir.
type Daemon struct {
	// Dir is the run dir (journal + health.json live here).
	Dir string
	// Now is the clock seam (tests inject).
	Now func() time.Time

	mu      sync.Mutex
	lf      *reverter.Lifecycle
	store   *reverter.Store
	started time.Time
	// daemonBoot is THIS PROCESS's boot id (health.json `daemon_boot`,
	// MISCHIEF-DF-32): minted once at New, unique per daemon start —
	// deliberately NOT the reverter fold's host-boot-scoped BootID, so a
	// stale health document is distinguishable from the live daemon's.
	daemonBoot string
	// refoldErr is the tick's re-fold failure (corruption), held for the
	// render to report (MISCHIEF-DF-39): the loop must NOT exit on it — a
	// daemon that dies on a torn journal line leaves health.json frozen at
	// the last good beat, ok:true, with the fault in the dark.
	refoldErr error
}

// New builds a daemon over dir. A torn journal at boot is DEGRADED, not
// fatal (MISCHIEF-DF-40): the store is opened, the fold error is HELD, and
// the daemon still starts so its first beat can render health.json with
// ok:false + refold_error. A serve that exits here leaves the operator with
// no health surface at all (a missing file is not a health document). Only
// a failure that yields no store at all is an error.
func New(dir string) (*Daemon, error) {
	store, err := reverter.Open(dir)
	if store == nil {
		return nil, err
	}
	d := &Daemon{Dir: dir, Now: time.Now, lf: reverter.New(store), store: store, started: time.Now(), daemonBoot: daemonBootID()}
	if err != nil {
		d.refoldErr = err
	}
	return d, nil
}

// Run blocks until ctx is cancelled: heartbeat health.json, revert expired
// holds from the journal state, repeat. Boot reconcile already ran inside
// reverter.Open (previous-boot leftovers are stuck before the first beat).
func (d *Daemon) Run(ctx context.Context) error {
	defer d.store.Close()
	if err := d.writeHealth(); err != nil {
		return err
	}
	tick := time.NewTicker(HeartbeatInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = d.writeHealth() // final beat: a stopping daemon still reports
			return ctx.Err()
		case <-tick.C:
			if err := d.revertExpired(); err != nil {
				// A re-fold failure is DEGRADED, not fatal (MISCHIEF-DF-39):
				// hold it for the render, skip the sweep (the fold may be
				// mid-torn-line), and keep beating so writeHealth renders
				// the stale leg with ok:false + refold_error — the operator
				// sees the corruption and the process survives it. Any
				// other error still exits: only the re-fold is survivable.
				if isRefoldErr(err) {
					d.setRefoldErr(err)
					// MISCHIEF-DF-39-R2: the beat must still PUBLISH. Skipping
					// writeHealth here leaves the last good document standing
					// (ok:true, frozen TTLs, no refold_error) over a corrupted
					// journal — a false green exactly when containment is
					// suspended. render() re-reads the journal itself, so this
					// beat renders the corruption.
					if herr := d.writeHealth(); herr != nil {
						return herr
					}
					continue
				}
				return err
			}
			if err := d.writeHealth(); err != nil {
				return err
			}
		}
	}
}

// isRefoldErr classifies a revertExpired error as the tick's re-fold
// failure (the ONLY survivable one): the wrap chain is
// `daemon: refold: reverter: …`, so the prefix test is exact. Anything
// else (a proof write failed, the check binary vanished) still kills the
// daemon — the journal evidence contract must not fail silently.
func isRefoldErr(err error) bool {
	return strings.Contains(err.Error(), "daemon: refold: reverter:")
}

// setRefoldErr records the held re-fold failure (caller = Run's loop, the
// single writer); clearRefoldErr drops it on a beat that folded clean.
// The render reads both under d.mu (MISCHIEF-DF-39).
func (d *Daemon) setRefoldErr(err error) {
	d.mu.Lock()
	d.setRefoldErrLocked(err)
	d.mu.Unlock()
}

// setRefoldErrLocked is the render's variant: render runs UNDER writeHealth's
// d.mu and sync.Mutex is not reentrant, so a rendering beat that re-locked
// would self-deadlock (MISCHIEF-DF-41) — the process hangs on the very path
// that must report the corruption, and only SIGKILL ends it.
func (d *Daemon) setRefoldErrLocked(err error) {
	d.refoldErr = err
}

// clearRefoldErr drops the held re-fold failure on a beat that folded
// clean — recovery is explicit: the very next document says ok:true again
// with the corruption gone (MISCHIEF-DF-39 AC3).
func (d *Daemon) clearRefoldErr() {
	d.refoldErr = nil
}

// clearRefoldErrMu drops the held re-fold failure under d.mu (caller must
// hold the lock — this is the variant called from within locked contexts).
// Deprecated: use clearRefoldErr directly.
func (d *Daemon) clearRefoldErrMu() {
	d.refoldErr = nil
}

// revertExpired reverts every armed hold whose TTL has passed, with the
// owner role and reason ttl_expiry (AC-5's daemon-side half: the reverter
// outlives the CLI).
func (d *Daemon) revertExpired() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	// Re-fold from the journal first (MISCHIEF-DF-30): holds armed by
	// other processes AFTER this daemon booted are durable in the journal
	// but absent from the boot fold — the boot-time snapshot must not own
	// the TTLs of holds it never saw. Corruption fails the tick loudly
	// (a store over a corrupt journal reverts nothing silently).
	if err := d.store.Refold(); err != nil {
		return fmt.Errorf("daemon: refold: %w", err)
	}
	d.refoldErr = nil // a clean fold drops any held defect: next render is ok:true
	now := d.Now()
	// The active view is the right one here: a terminal hold owes no TTL.
	for _, h := range d.lf.Status().Holds {
		if h.Status != reverter.StatusArmed || h.Expiry.IsZero() || now.Before(h.Expiry) {
			continue
		}
		if _, err := d.lf.Revert(context.Background(), h.HoldID, "ttl_expiry", reverter.RoleOwner); err != nil {
			// a hold that disappeared between snapshot and revert is
			// reported, not fatal; a proof-write failure IS fatal (the
			// journal is the evidence contract).
			if err != reverter.ErrNoHold {
				return fmt.Errorf("daemon: ttl revert %s: %w", h.HoldID, err)
			}
		}
	}
	return nil
}

// writeHealth renders the health document and installs it ATOMICALLY
// (temp + rename in the same dir, same filesystem — a reader sees the old
// document or the new one, never a torn file).
func (d *Daemon) writeHealth() error {
	d.mu.Lock()
	h := d.render()
	d.mu.Unlock()
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return fmt.Errorf("daemon: health encode: %w", err)
	}
	b = append(b, '\n')
	tmp := filepath.Join(d.Dir, HealthFileName+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("daemon: health write: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(d.Dir, HealthFileName)); err != nil {
		return fmt.Errorf("daemon: health publish: %w", err)
	}
	return nil
}

// render folds the current state into a Health document. Caller holds d.mu.
// It re-folds the journal first (MISCHIEF-DF-30): health.json must report
// holds armed after boot, not freeze at the boot snapshot. A refold error
// renders the LAST KNOWN fold — health never goes dark — with ok=false
// naming the defect (the one condition that is not a hold state).
func (d *Daemon) render() Health {
	now := d.Now()
	if err := d.store.Refold(); err != nil {
		// The journal is unreadable RIGHT NOW (the tick's held failure or
		// a fresh one): render the LAST KNOWN fold with ok:false naming
		// the defect, and hold the error on the daemon so a later
		// writeHealth (e.g. the ctx-cancel final beat) degrades too.
		//
		// MISCHIEF-DF-41: render is called WITH d.mu already held (writeHealth),
		// so it must use the non-locking setter — the locking one would
		// self-deadlock and the corruption report itself would hang.
		d.setRefoldErrLocked(err)
		h := d.staleRender(now)
		h.OK = false
		h.RefoldError = err.Error()
		return h
	}
	d.clearRefoldErr() // the document must never out-stale the fold it just read
	// MISCHIEF-DF-54: health keeps the COMPLETE listing (every known hold,
	// terminal included) — its reverted / revert_failed counters and the
	// ok=false a live revert_failed raises are derived from the terminal
	// rows, which the CLI's active-only default no longer carries.
	st := d.lf.StatusAll()
	h := Health{
		OK:          true,
		DaemonBoot:  d.daemonBoot, // the PROCESS's id (DF-32), not the fold's host-boot id
		PID:         os.Getpid(),
		RunDir:      d.Dir,
		StartedAt:   d.started.Unix(),
		GeneratedAt: now.Unix(),
	}
	for _, hold := range st.Holds {
		row := HealthHold{
			HoldID:  hold.HoldID,
			FaultID: hold.FaultID,
			Target:  hold.Target,
			Status:  string(hold.Status),
		}
		switch hold.Status {
		case reverter.StatusArmed:
			if !hold.Expiry.IsZero() {
				row.TTLRemainingSec = hold.Expiry.Sub(now).Seconds()
			}
		case reverter.StatusStuck:
			h.Stuck++
			if !hold.StuckAt.IsZero() {
				row.StuckAt = hold.StuckAt.Unix()
			}
		case reverter.StatusReverted:
			h.Reverted++
		case reverter.StatusRevertFailed:
			h.RevertFailed++
			h.OK = false // a live un-reverted fault is NOT ok
		}
		h.Holds = append(h.Holds, row)
	}
	return h
}

// staleRender renders the last-known fold WITHOUT re-reading the journal
// (the degraded leg of render: the journal is unreadable, so re-folding
// it inside Status would panic — the Store's maps are nil after a failed
// boot-less fold). It is the honest frozen view, never a green one. It
// reads the COMPLETE listing, so its reverted / revert_failed counters and
// its ok=false match the live render's for the same fold (MISCHIEF-DF-54).
func (d *Daemon) staleRender(now time.Time) Health {
	st := d.lf.StatusSnapshotAll()
	h := Health{
		OK:          true,
		DaemonBoot:  d.daemonBoot, // the PROCESS's id (DF-32), not the fold's host-boot id
		PID:         os.Getpid(),
		RunDir:      d.Dir,
		StartedAt:   d.started.Unix(),
		GeneratedAt: now.Unix(),
	}
	for _, hold := range st.Holds {
		row := HealthHold{
			HoldID:  hold.HoldID,
			FaultID: hold.FaultID,
			Target:  hold.Target,
			Status:  string(hold.Status),
		}
		switch hold.Status {
		case reverter.StatusArmed:
			if !hold.Expiry.IsZero() {
				row.TTLRemainingSec = hold.Expiry.Sub(now).Seconds()
			}
		case reverter.StatusStuck:
			h.Stuck++
			if !hold.StuckAt.IsZero() {
				row.StuckAt = hold.StuckAt.Unix()
			}
		case reverter.StatusReverted:
			h.Reverted++
		case reverter.StatusRevertFailed:
			h.RevertFailed++
			h.OK = false
		}
		h.Holds = append(h.Holds, row)
	}
	return h
}

// RevertAll is the daemon-side kill switch (an operator may point the CLI
// at the same run dir; this is for a daemon asked directly to sweep).
func (d *Daemon) RevertAll(ctx context.Context) ([]reverter.Proof, time.Duration, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lf.RevertAll(ctx, "kill_switch")
}
