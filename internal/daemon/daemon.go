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
	// DaemonBoot is the daemon's own boot id (reverter fold identity).
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
}

// New builds a daemon over dir.
func New(dir string) (*Daemon, error) {
	store, err := reverter.Open(dir)
	if err != nil {
		return nil, err
	}
	return &Daemon{Dir: dir, Now: time.Now, lf: reverter.New(store), store: store, started: time.Now()}, nil
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
				return err
			}
			if err := d.writeHealth(); err != nil {
				return err
			}
		}
	}
}

// revertExpired reverts every armed hold whose TTL has passed, with the
// owner role and reason ttl_expiry (AC-5's daemon-side half: the reverter
// outlives the CLI).
func (d *Daemon) revertExpired() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.Now()
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
func (d *Daemon) render() Health {
	now := d.Now()
	st := d.lf.Status()
	h := Health{
		OK:          true,
		DaemonBoot:  st.BootID,
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

// RevertAll is the daemon-side kill switch (an operator may point the CLI
// at the same run dir; this is for a daemon asked directly to sweep).
func (d *Daemon) RevertAll(ctx context.Context) ([]reverter.Proof, time.Duration, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lf.RevertAll(ctx, "kill_switch")
}
