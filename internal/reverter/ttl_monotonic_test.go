package reverter

import (
	"context"
	"testing"
	"time"
)

// ttl_monotonic (MISCHIEF-DF-59) — the runnable containment contract.
//
// Base measured the defect: a declared 10s TTL reverted at 9.27s (early,
// wall-clock step between arm and the sweep tick) and at 14.21s (+4.2s).
// The containment point must be the DECLARED TTL on the in-process
// monotonic clock; the wall Expiry (second grid, +1s overshoot) stays the
// durable cross-process boundary but never the waiter's clock.

// TestTTLContainmentMatchesDeclaredTTL: a declared TTL T is a measured
// lower bound (never revert before T) and lands within T+2s under 50ms
// sampling. The hold is armed, landed and then polled on the real clock;
// the revert runs the same measured path the daemon OwnerMain sweep runs.
func TestTTLContainmentMatchesDeclaredTTL(t *testing.T) {
	st, lf := newTestStore(t)
	hold, _, _ := scratchHold(t, "P-DF59-MONO")
	const ttl = 1500 * time.Millisecond
	hold.TTL = ttl
	armed := time.Now()
	id, err := lf.Arm(hold)
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	if err := lf.Land(id); err != nil {
		t.Fatalf("land: %v", err)
	}

	// The deadline is anchored at arm time, now + declared TTL — the
	// containment point, not the second grid's +1s overshoot.
	dl := st.holds[id].Deadline
	if dl.Before(armed.Add(ttl - 50*time.Millisecond)) || dl.After(armed.Add(ttl + 250*time.Millisecond)) {
		t.Fatalf("monotonic deadline %v not anchored at arm time + TTL (arm captured %v, want ~%v)",
			dl.Format(time.RFC3339Nano), armed.Format(time.RFC3339Nano), armed.Add(ttl).Format(time.RFC3339Nano))
	}

	// 50ms sampling: hold NOT due until the monotonic clock crosses the
	// deadline, then revert through the measured path.
	upper := armed.Add(ttl + 2 * time.Second)
	var revertedAt time.Time
	var pr Proof
	for {
		if ids := lf.DueHoldIDs(); len(ids) == 1 {
			pr, err = lf.Revert(context.Background(), id, "ttl_expiry", RoleOwner)
			if err != nil {
				t.Fatalf("revert at containment point: %v", err)
			}
			revertedAt = time.Now()
			break
		} else if len(ids) > 1 {
			t.Fatalf("due ids leaked across holds: %v", ids)
		}
		if time.Now().After(upper) {
			t.Fatalf("TTL %v never came due by %v — the monotonic deadline is not honored",
				ttl, upper.Format(time.RFC3339Nano))
		}
		time.Sleep(50 * time.Millisecond)
	}
	elapsed := revertedAt.Sub(armed)
	if elapsed < ttl {
		t.Fatalf("reverted at %v — EARLIER than the declared TTL %v (the early-expiry contract break)",
			elapsed, ttl)
	}
	if elapsed > ttl+2*time.Second {
		t.Fatalf("reverted at %v — beyond the T+2s containment bound (T=%v)", elapsed, ttl)
	}
	// A landed hold grades reverted at its containment point (the measured
	// check, not an assertion).
	if pr.Outcome != OutcomeReverted {
		t.Fatalf("containment revert graded %s (%s), want reverted", pr.Outcome, pr.Detail)
	}
}

// TestWallJumpCannotCauseEarlyExpiry pins the measured defect shape at a
// 10s TTL: a forward wall-clock step is a frame the OLD fold consumed
// (now.Before(wallExpiry) with a stepped wall clock fired ~0.7s early);
// the monotonic deadline must be deaf to it, while the wait itself stays
// real (a short-TTL hold in the same store still comes due on time).
func TestWallJumpCannotCauseEarlyExpiry(t *testing.T) {
	st, lf := newTestStore(t)

	// 10s shape (the measured 9.27s early revert): arm, then build the
	// jumped wall frame — no monotonic reading (Round(0) strips it), 2s
	// past the arm on the wall, which is guaranteed past the durable
	// wall Expiry (second grid, ≤1s overshoot) yet only ~2s into a 10s
	// TTL. Premise first: this frame WOULD have read expired to the old
	// wall comparison.
	hold, _, _ := scratchHold(t, "P-DF59-JUMP10")
	const ttl10 = 10 * time.Second
	hold.TTL = ttl10
	id10, err := lf.Arm(hold)
	if err != nil {
		t.Fatalf("arm 10s: %v", err)
	}
	h10 := st.holds[id10]
	wallJumped := time.Now().Round(0).Add(ttl10 + 2*time.Second)
	if !wallJumped.After(h10.Expiry) {
		t.Fatalf("premise broken: jumped wall frame %v is not past the durable wall expiry %v", wallJumped, h10.Expiry)
	}

	// Now the live half: a SHORT-TTL hold in the same store must come due
	// on the real monotonic clock — after the wall jump frame existed, the
	// wait is still honored, not stretched by it.
	hold, _, _ = scratchHold(t, "P-DF59-JUMPSHORT")
	const ttlShort = 1500 * time.Millisecond
	hold.TTL = ttlShort
	armed := time.Now()
	idShort, err := lf.Arm(hold)
	if err != nil {
		t.Fatalf("arm short: %v", err)
	}
	if err := lf.Land(idShort); err != nil {
		t.Fatalf("land short: %v", err)
	}
	// Rebuild the jump frame NOW (mid-hold): 10s past the wall — deep past
	// BOTH holds' durable wall expiry, still ~1.4s into the short TTL.
	wallJumped = time.Now().Round(0).Add(10 * time.Second)
	if !wallJumped.After(st.holds[idShort].Expiry) {
		t.Fatalf("premise broken: mid-hold jump frame %v is not past the short hold's wall expiry %v",
			wallJumped, st.holds[idShort].Expiry)
	}

	// Mid-hold, with the jumped frame staring at a "long past" wall
	// expiry: NOTHING is due on the monotonic clock.
	upper := armed.Add(ttlShort + 2 * time.Second)
	var revertedAt time.Time
	for {
		if ids := lf.DueHoldIDs(); len(ids) == 1 {
			if _, err := lf.Revert(context.Background(), idShort, "ttl_expiry", RoleOwner); err != nil {
				t.Fatalf("revert short: %v", err)
			}
			revertedAt = time.Now()
			break
		}
		if time.Now().After(upper) {
			t.Fatalf("short TTL %v never came due by %v — a wall step may only never fire early, it may not delay the wait either",
				ttlShort, upper.Format(time.RFC3339Nano))
		}
		time.Sleep(50 * time.Millisecond)
	}
	if elapsed := revertedAt.Sub(armed); elapsed < ttlShort {
		t.Fatalf("short hold reverted at %v < declared TTL %v — early expiry", elapsed, ttlShort)
	}
	// The 10s hold outlived the whole test (≤ T+2s for a 1.5s TTL): still
	// armed, NOT due — the wall jump did not fire it early.
	if got := st.holds[id10]; got.Status != StatusArmed {
		t.Fatalf("10s hold state after the short hold's containment: %s, want still armed (wall jump must not fire early)", got.Status)
	}
	if st.holds[id10].due(time.Now()) {
		t.Fatalf("10s hold reported due ~%v after arm — the wall-clock step fired the TTL early (monotonic deadline not valid)",
			time.Since(armed))
	}
}

// TestDeadlineSurvivesRefold: a re-fold is a re-read of the same bytes — the
// containment point anchored in this process must be carried forward
// untouched, not re-anchored from the wall clock (which a step between folds
// could move). A cold Open of the same journal (crash/restart recovery)
// re-anchors from the wall remaining measured once — and must NOT report a
// still-live hold as due.
func TestDeadlineSurvivesRefold(t *testing.T) {
	dir := localTempDir(t)
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	lf := New(st)
	hold, _, _ := scratchHold(t, "P-DF59-CARRY")
	hold.TTL = 10 * time.Second
	id, err := lf.Arm(hold)
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	d1 := st.holds[id].Deadline
	if d1.IsZero() {
		t.Fatal("fresh arm did not anchor a monotonic deadline")
	}
	if err := st.Refold(); err != nil {
		t.Fatalf("refold: %v", err)
	}
	d2 := st.holds[id].Deadline
	if !d2.Equal(d1) {
		t.Fatalf("re-fold moved the containment point: %v -> %v", d1.Format(time.RFC3339Nano), d2.Format(time.RFC3339Nano))
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	st2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	if got := st2.holds[id].Deadline; got.IsZero() {
		t.Fatal("cold fold did not anchor a monotonic deadline for a foreign arm")
	}
	if ids := New(st2).DueHoldIDs(); len(ids) != 0 {
		t.Fatalf("cold fold reported a live 10s hold due: %v", ids)
	}
}
