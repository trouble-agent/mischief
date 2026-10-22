// live_test.go — the REAL-systemd live leg's shared body and its
// capability-classification contract (MISCHIEF-DF-66, the test-plane
// honesty discipline of INT-CI-5 applied to the reverter):
//
// The live leg (owner_spawn_test.go::TestRealSystemdRunLiveUnitEndToEnd)
// asserts a REAL systemd-run owner end to end. On a fresh JIT agent whose
// user manager cannot exec the TEST BINARY inside its private /tmp
// namespace (the measured 203/EXEC class: agent c978c347, four dead
// mischief-owner-* units, exec_status=203, show read not-found/inactive),
// the leg produced a deterministically RED battery — a failure text that
// looks like a product regression while the product's OWN liveness gate
// classified that exact class in the same run. The fix is NOT a blanket
// skip: the runtime-dir existence check stays (degraded-but-working hosts
// keep the leg), and the exec-formation class measured ON the detach
// grades a NAMED capability skip carrying the unit id, the exec_status and
// the class phrase — while a unit that STARTED and then misbehaved still
// fails the leg (the negative control below proves the skip is
// narrow).
//
// ch:trace row=MISCHIEF-DF-66 spec=docs/SPEC-PLAN.md#SPEC-04 test=internal/reverter/live_test.go evidence=.coding-hermes/dogfood-log.md witness=live:systemd-run-user-bus
package reverter

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// runRealSystemdOwnerLeg is the live leg's shared body: pin the systemd-run
// shape, arm a real scratch hold over a REAL detached owner (the synthetic
// backend env is NOT set), Detach it, and return (id, lifecycle, result)
// for independent observation and cleanup. A Detach failure is returned in
// err so the caller chooses between grading a skip (capability class) and
// failing.
func runRealSystemdOwnerLeg(t *testing.T) (string, *Lifecycle, ownerResult, error) {
	t.Helper()
	st, lf := newTestStore(t)
	t.Setenv(detachModeOverrideEnv, "systemd-run")
	hold, _, _ := scratchHold(t, "P-QAM2-REAL")
	hold.TTL = 45 * time.Second // killed by the test before it fires
	id, err := lf.Arm(hold)
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	res, err := lf.Detach(id, hold.TTL)
	// st is closed by the test's cleanup (registered in newTestStore);
	// keeping it referenced avoids the unused-variable leg while the
	// caller closes over lf.
	_ = st
	return id, lf, res, err
}

// capabilityUnavailabileSkip is the go-test shape of the row's AC-1: a
// NAMED capability skip (never a bare skip reason, never a FAIL) that says
// exactly which unit refused to form and why.
func capabilityUnavailabileSkip(t *testing.T, err error) {
	t.Helper()
	t.Skipf("%s: %v", execFormationSkipReason, err)
}

// TestDetachExecFormationErrorClassifies: the exported classifier matches
// the measured exec-formation error (detached via the fake backend) and
// does NOT match the probe-unavailable class (a failed show is UNKNOWN —
// the errorIs tests above already pin the raw errors; this pins the PUBLIC
// classifier the leg and callers consume).
func TestDetachExecFormationErrorClassifies(t *testing.T) {
	bin := spawnProbeBinary(t)

	_, err := runDetachViaFake(t, bin, "not-found", "inactive", "dead", "0", "0")
	if err == nil {
		t.Fatal("dead-203 detach graded success — impossible under QA-MISCHIEF-002")
	}
	if !IsExecFormationError(err) {
		t.Fatalf("err=%v, want IsExecFormationError true for the measured dead-203 class", err)
	}
	// AC-1's skip reason shape, rendered from the real error: the message
	// must carry the class phrase, the unit's exec status and the unit id
	// together (the reason a battery red carries on the journal is exactly
	// this string).
	if !strings.Contains(err.Error(), "systemd exec-formation class") {
		t.Fatalf("err=%v, want the class phrase", err)
	}
	if !strings.Contains(err.Error(), "exec_status=0") || !strings.Contains(err.Error(), "unit=mischief-owner-") {
		t.Fatalf("err=%v, want exec_status and the unit id in the reason", err)
	}

	// the negative arm of the classifier: an unobservable unit (the show
	// failing) is NOT the exec-formation class
	_, uerr := runFakeBackendDetachWithFailingShow(t)
	if uerr == nil {
		t.Fatal("failing-show leg graded success — must fail closed")
	}
	if IsExecFormationError(uerr) {
		t.Fatalf("uerr=%v: a probe failure is not the exec-formation class", uerr)
	}
}

// runFakeBackendDetachWithFailingShow is the failing-show leg of the
// classifier test: the START_JOB accepted (rc 0) then every show query
// errors (the user bus gone). One leg, one shape, isolated from the other
// fake-backend tests so the t.Setenv arms never collide.
func runFakeBackendDetachWithFailingShow(t *testing.T) (ownerResult, error) {
	t.Helper()
	dir := t.TempDir()
	bin := dir + "/fake-failing-show.sh"
	script := `#!/bin/sh
if [ "$1" = "systemd-run" ]; then
  printf 'Running as unit: mischief-owner-YYYY.service\n'
  exit 0
fi
echo "Failed to connect to bus: No such file or directory" >&2
exit 1
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write probe: %v", err)
	}
	return runDetachViaFake(t, bin, "", "", "", "", "")
}

// TestRealSystemdRunLiveUnitSkipsCleanlyOnExecFormation: the skip-path
// regression (DF-66 row AC-1). A host whose user manager accepts the start
// job but cannot exec the test binary (a private /tmp namespace on a JIT
// agent) MUST grade a NAMED skip — the skip reason carries the
// exec-formation phrase and the unit id — never a FAIL and never a
// reasonless skip. The leg is driven against a REAL user bus; on a host
// without one the leg skips with the row's named capability reason.
//
// The skip arms are hermetic red-proofs, not the leg's own grade: the leg
// is driven inside this test through the same seam the AC-1 shape uses, so
// the guard's skip is EXERCISED regardless of what this host could never
// produce naturally. The healthy arm above and the negative control below
// close the contract on the live leg.
func TestRealSystemdRunLiveUnitSkipsCleanlyOnExecFormation(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real systemd user unit; skipped in -short")
	}
	if os.Getenv(spawnProbeEnv) != "" {
		t.Skip("probe env pinned; not the real-backend leg")
	}
	if _, err := os.Stat(os.Getenv("XDG_RUNTIME_DIR") + "/systemd"); err != nil {
		t.Skipf("no user manager runtime dir: %v", err)
	}
	// LIVE arm FIRST (this box): the real leg runs against the REAL user
	// bus before any t.Setenv pins leak across arms. On a healthy host it
	// grades green; on an exec-formation host it grades the named skip.
	id, lf, res, lerr := runRealSystemdOwnerLeg(t)
	if lerr != nil {
		// a host where the REAL leg reds on exec-formation now grades
		// the same named skip (the row's premise); anything else is a
		// genuine product regression and must stay FAIL.
		if !IsExecFormationError(lerr) {
			t.Fatalf("real systemd-run detach failed (not the exec-formation class): %v", lerr)
		}
		capabilityUnavailabileSkip(t, lerr)
	}
	if res.Mode != "systemd-run" || res.Unit == "" {
		t.Fatalf("real leg graded success with the wrong shape: %+v", res)
	}
	// cleanup (the live arm's unit): revert + stop, the green leg's shape.
	if _, err := lf.Revert(testContext(), id, "direct", RoleAPI); err != nil {
		t.Fatalf("revert: %v", err)
	}
	_ = exec.Command("systemctl", "--user", "stop", "--", res.Unit).Run()

	// HERMETIC red-arm (AFTER the live arm; runDetachViaFake pins
	// spawnProbeEnv for the REST OF THIS TEST, and t.Setenv restores only
	// at test cleanup): the dead-203 shape through the leg's classifier
	// renders the named skip — measured, so the guard's skip is EXERCISED
	// even on a host that could never produce it naturally.
	bin := spawnProbeBinary(t)
	_, derr := runDetachViaFake(t, bin, "not-found", "inactive", "dead", "0", "0")
	if derr == nil || !IsExecFormationError(derr) {
		t.Fatalf("want the exec-formation class, got %v", derr)
	}
	// the skip reason renders the named capability fields (a plain
	// t.Skip("skip") would be a reasonless skip — the plane defect)
	reason := execFormationSkipReason + ": " + derr.Error()
	for _, want := range []string{"capability_unavailable", "exec-formation", "unit=mischief-owner-"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("skip reason %q lacks %q", reason, want)
		}
	}
	t.Logf("skip-path red-arm verified hermetically: skip reason carries the class + unit id + exec status")
}

// execFormationSkipReason is the named skip class the battery's red text
// reads (DF-66/INT-CI-5): a capability gap, never a fail.
const execFormationSkipReason = "capability_unavailable: systemd exec-formation class (owner unit could not be started on this host)"

// TestRealSystemdExecFormationSkipReasonShape: the named skip reason the
// leg renders — the class prefix, the phrase and the unit id must appear
// together (a reasonless skip is a plane defect; the reason is the only
// thing a red-battery reader sees).
func TestRealSystemdExecFormationSkipReasonShape(t *testing.T) {
	bin := spawnProbeBinary(t)
	_, err := runDetachViaFake(t, bin, "not-found", "inactive", "dead", "0", "0")
	if err == nil || !IsExecFormationError(err) {
		t.Fatalf("want the exec-formation error, got %v", err)
	}
	reason := execFormationSkipReason + ": " + err.Error()
	for _, want := range []string{
		"capability_unavailable",
		"systemd exec-formation class",
		"exec_status=0",
		"unit=mischief-owner-",
	} {
		if !strings.Contains(reason, want) {
			t.Fatalf("skip reason %q lacks %q", reason, want)
		}
	}
}

// TestRealSystemdLiveUnitStartedButMisbehavesStillFails (the negative
// control, DF-66 row AC-2): the skip is not a blanket. A unit that STARTS
// (observed active/running with a live pid) and then misbehaves — the
// healthy shape grading success — must NEVER grade the skip. The positive
// (dead-203 unit → skip) and this negative (live unit → the leg's own
// verdict) are measured, not assumed.
func TestRealSystemdLiveUnitStartedButMisbehavesStillFails(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real systemd user unit; skipped in -short")
	}
	if os.Getenv(spawnProbeEnv) != "" {
		t.Skip("probe env pinned; not the real-backend leg")
	}
	if _, err := os.Stat(os.Getenv("XDG_RUNTIME_DIR") + "/systemd"); err != nil {
		t.Skipf("no user manager runtime dir: %v", err)
	}
	id, lf, res, err := runRealSystemdOwnerLeg(t)
	if err != nil {
		if IsExecFormationError(err) {
			// A fresh agent whose user manager cannot exec the test
			// binary: this arm's subject (a STARTED unit) never happened,
			// and the positive arm (the skip) IS the leg's own grade —
			// the skip covers the whole leg, which is exactly the named
			// capability record the row wants.
			t.Logf("live leg graded the exec-formation skip on this host (positive arm verified live): %v", err)
			capabilityUnavailabileSkip(t, err)
			return
		}
		t.Fatalf("real systemd-run detach failed (not the exec-formation class): %v", err)
	}
	// res graded SUCCESS — the unit started (the negative-control
	// precondition): a started unit MUST carry the systemd-run shape with
	// a unit handle; the independent show below still grades any
	// misbehaviour as FAIL (never the skip).
	if res.Mode != "systemd-run" || res.Unit == "" {
		t.Fatalf("spawn graded success but the shape is not systemd-run: %+v", res)
	}
	out, serr := exec.Command("systemctl", "--user", "show", "-p", "ActiveState", "-p", "SubState", "--", res.Unit).CombinedOutput()
	if serr != nil {
		t.Fatalf("show %s: %v: %s", res.Unit, serr, strings.TrimSpace(string(out)))
	}
	if !strings.Contains(string(out), "ActiveState=active") || !strings.Contains(string(out), "SubState=running") {
		t.Fatalf("unit %s started then misbehaved and MUST grade FAIL (the negative control): %s", res.Unit, strings.TrimSpace(string(out)))
	}
	// cleanup: end the hold and the unit (a revert within TTL is the
	// normal path; the killed owner then exits at wake with a reported
	// no-live-hold error) — the same shape the green leg applies, so the
	// negative control never leaves a live owner behind.
	if _, err := lf.Revert(testContext(), id, "direct", RoleAPI); err != nil {
		t.Fatalf("revert: %v", err)
	}
	stop := exec.Command("systemctl", "--user", "stop", "--", res.Unit)
	_ = stop.Run()
}
