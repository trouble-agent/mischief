package reverter

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── QA-MISCHIEF-002: the systemd-run spawn path must LIVENESS-VERIFY the
// owner unit; rc=0 from systemd-run with an immediately-dead owner is the
// silent-success shape this regression forbids. ─────────────────────────────

// TestUnitVerdictTable pins the ownerUnitAlive truth table, including the
// exact shapes measured on the live host: a `--collect`-reaped fast-dead
// unit reads not-found/inactive/dead with ExecMainPID=0 (systemd GC resets
// ExecMainStatus on collected units), a still-starting unit reads
// activating/start — and NONE of those grades alive.
func TestUnitVerdictTable(t *testing.T) {
	cases := []struct {
		name string
		p    unitProbe
		want bool
	}{
		{"healthy running unit", unitProbe{LoadState: "loaded", ActiveState: "active", SubState: "running", ExecMainStatus: "0", ExecMainPID: 1234}, true},
		{"collected dead unit (not-found/inactive/dead, pid 0)", unitProbe{LoadState: "not-found", ActiveState: "inactive", SubState: "dead", ExecMainStatus: "0", ExecMainPID: 0}, false},
		{"failed unit 203", unitProbe{LoadState: "loaded", ActiveState: "failed", SubState: "failed", ExecMainStatus: "203", ExecMainPID: 0}, false},
		{"failed unit status 1", unitProbe{LoadState: "loaded", ActiveState: "failed", SubState: "failed", ExecMainStatus: "1", ExecMainPID: 0}, false},
		{"unit not started yet (activating/start)", unitProbe{LoadState: "loaded", ActiveState: "activating", SubState: "start", ExecMainStatus: "0", ExecMainPID: 0}, false},
		{"active but sub-state not running", unitProbe{LoadState: "loaded", ActiveState: "active", SubState: "exited", ExecMainStatus: "0", ExecMainPID: 0}, false},
		{"active/running but pid missing", unitProbe{LoadState: "loaded", ActiveState: "active", SubState: "running"}, false},
		{"fully empty answer", unitProbe{}, false},
		{"not-found load state with otherwise-live shape", unitProbe{LoadState: "not-found", ActiveState: "active", SubState: "running", ExecMainStatus: "0", ExecMainPID: 42}, false},
	}
	for _, tc := range cases {
		if got := ownerUnitAlive(tc.p); got != tc.want {
			t.Errorf("%s: alive=%v, want %v (%+v)", tc.name, got, tc.want, tc.p)
		}
	}
}

// TestParseUnitProbeMalformedShow: a truncated or garbage show answer parses
// to the zero probe — which the verdict grades NOT alive (unknown is never
// success).
func TestParseUnitProbeMalformedShow(t *testing.T) {
	for _, out := range []string{"", "garbage line\n", "ActiveState only\nActiveState=active\n"} {
		if p := parseUnitProbe(out); ownerUnitAlive(p) {
			t.Errorf("probe %q parsed to an ALIVE verdict (%+v)", out, p)
		}
	}
	// the healthy shape still parses fully
	p := parseUnitProbe("LoadState=loaded\nActiveState=active\nSubState=running\nExecMainStatus=0\nExecMainPID=99\n")
	if !ownerUnitAlive(p) || p.ExecMainPID != 99 || p.LoadState != "loaded" {
		t.Fatalf("healthy show parsed wrong: %+v", p)
	}
}

// spawnProbeBinary writes the synthetic systemd-run backend: argv[1]
// "systemd-run" → rc 0, stdout "Running as unit: <unit>.service" (the real
// CLI accepts the START_JOB); argv[1] "show" → the script serves the state
// headers from the FAKE_* env the tests set at spawn time.
func spawnProbeBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-systemd-run.sh")
	script := `#!/bin/sh
# synthetic systemd-run/systemctl pair (QA-MISCHIEF-002 probe)
if [ "$1" = "systemd-run" ]; then
  printf 'Running as unit: mischief-owner-XXXX.service\n'
  exit 0
fi
if [ "$1" = "show" ]; then
  printf 'LoadState=%s\n' "$FAKE_LOAD_STATE"
  printf 'ActiveState=%s\n' "$FAKE_ACTIVE_STATE"
  printf 'SubState=%s\n' "$FAKE_SUB_STATE"
  printf 'ExecMainStatus=%s\n' "$FAKE_EXEC_STATUS"
  printf 'ExecMainPID=%s\n' "$FAKE_EXEC_PID"
  exit 0
fi
echo "fake-systemd-run: unknown argv: $*" >&2
exit 64
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write probe: %v", err)
	}
	return bin
}

// runDetachViaFake drives ONE Detach over the fake backend, with the show
// answer's five state headers in the child env.
func runDetachViaFake(t *testing.T, bin, load, active, sub, status, pid string) (ownerResult, error) {
	t.Helper()
	_, lf := newTestStore(t)
	t.Setenv(detachModeOverrideEnv, "systemd-run") // pin the shape (no shape-switch fallback)
	t.Setenv(spawnProbeEnv, bin)
	t.Setenv("FAKE_LOAD_STATE", load)
	t.Setenv("FAKE_ACTIVE_STATE", active)
	t.Setenv("FAKE_SUB_STATE", sub)
	t.Setenv("FAKE_EXEC_STATUS", status)
	t.Setenv("FAKE_EXEC_PID", pid)
	hold, _, _ := scratchHold(t, "P-QAM2")
	hold.TTL = 30 * time.Second
	id, err := lf.Arm(hold)
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	return lf.Detach(id, hold.TTL)
}

// TestSystemdRunDeadOwnerIsFailure is THE regression: systemd-run returns
// rc=0 (the START_JOB is accepted — verified live), but the unit's main
// process died inside its exec formation (the 203/EXEC class; the
// collected unit reads not-found/inactive/dead, ExecMainPID=0). Detach must
// grade the spawn a FAILURE naming the exec-formation class — not an
// ownerResult success.
func TestSystemdRunDeadOwnerIsFailure(t *testing.T) {
	bin := spawnProbeBinary(t)
	_, err := runDetachViaFake(t, bin, "not-found", "inactive", "dead", "0", "0")
	if err == nil {
		t.Fatal("rc=0 systemd-run with an immediately-dead owner graded SUCCESS — the silent-success bug is back")
	}
	if !errorIs(err, errOwnerExecClass) {
		t.Fatalf("err=%v, want the errOwnerExecClass class", err)
	}
	if !errorIs(err, ErrNoOwner) {
		t.Fatalf("err=%v, want ErrNoOwner wrap", err)
	}
	if !strings.Contains(err.Error(), "exec-formation") {
		t.Fatalf("err=%v, want the message to name the 203/EXEC class", err)
	}
	if !strings.Contains(err.Error(), "exec_status=0") {
		t.Fatalf("err=%v, want the collected unit's exec_status spelled out", err)
	}
}

// TestSystemdRunShowFailureIsNotSuccess: the probe itself fails (no user
// bus, vanished runtime dir) — the verdict is UNKNOWN, and unknown is the
// failure path, never success.
func TestSystemdRunShowFailureIsNotSuccess(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-failing-show.sh")
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
	_, err := runDetachViaFake(t, bin, "", "", "", "", "")
	if err == nil {
		t.Fatal("unobservable unit graded SUCCESS — unknown must fail closed")
	}
	if errorIs(err, errOwnerExecClass) {
		t.Fatalf("err=%v: an unobservable unit is a probe failure, not the exec-formation class", err)
	}
	if !errorIs(err, ErrNoOwner) {
		t.Fatalf("err=%v, want ErrNoOwner", err)
	}
	if !strings.Contains(err.Error(), "probe unavailable") {
		t.Fatalf("err=%v, want the probe-unavailable detail", err)
	}
}

// TestSystemdRunLiveOwnerStillSucceeds: the healthy shape (active/running,
// live pid) still grades success — the liveness gate must not break the
// proven path.
func TestSystemdRunLiveOwnerStillSucceeds(t *testing.T) {
	bin := spawnProbeBinary(t)
	res, err := runDetachViaFake(t, bin, "loaded", "active", "running", "0", "4242")
	if err != nil {
		t.Fatalf("healthy unit graded failure: %v", err)
	}
	if res.Mode != "systemd-run" || res.Unit == "" {
		t.Fatalf("result %+v, want the systemd-run shape with a unit handle", res)
	}
}

// TestDetachForcedSystemdRunFailureDoesNotShapeSwitch: with the shape pinned
// (operator/debug surface), a liveness-verified dead owner returns the
// exec-formation error as-is — the override exists so the choice is
// explicit, so failure must not silently re-spawn via setsid. (On the
// natural, unpinned path Detach's CALLER sees ErrNoOwner and may fall back
// to setsid; that policy is the CLI's, not the spawn's.)
func TestDetachForcedSystemdRunFailureDoesNotShapeSwitch(t *testing.T) {
	bin := spawnProbeBinary(t)
	st, lf := newTestStore(t)
	t.Setenv(detachModeOverrideEnv, "systemd-run")
	t.Setenv(spawnProbeEnv, bin)
	t.Setenv("FAKE_LOAD_STATE", "not-found")
	t.Setenv("FAKE_ACTIVE_STATE", "inactive")
	t.Setenv("FAKE_SUB_STATE", "dead")
	t.Setenv("FAKE_EXEC_STATUS", "203")
	t.Setenv("FAKE_EXEC_PID", "0")
	hold, _, _ := scratchHold(t, "P-QAM2-PIN")
	hold.TTL = 30 * time.Second
	id, err := lf.Arm(hold)
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	_, derr := lf.Detach(id, hold.TTL)
	if !errorIs(derr, errOwnerExecClass) {
		t.Fatalf("err=%v, want the exec-formation class surfaced as-is under the pin", derr)
	}
	// no hold_expired record may exist for a failed detach
	for _, l := range journalLines(t, st.Dir) {
		if recordTypeOf(t, l) == string(RecordHoldExpired) {
			t.Fatalf("failed detach recorded an owner: %s", l)
		}
	}
}

// TestProbeBinaryShowExecFails: a probe binary whose systemd-run rc=0 but
// whose `show` exec FAILS (mimicking a namespace where even the state
// probe cannot exec) must fail the spawn loudly — the closest a healthy
// host gets to the pam_namespace geometry without a container.
func TestProbeBinaryShowExecFails(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-no-show-exec.sh")
	script := `#!/bin/sh
if [ "$1" = "systemd-run" ]; then
  printf 'Running as unit: mischief-owner-ZZZZ.service\n'
  exit 0
fi
exit 126
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write probe: %v", err)
	}
	_, err := runDetachViaFake(t, bin, "", "", "", "", "")
	if err == nil {
		t.Fatal("spawn graded SUCCESS while the unit could not be observed")
	}
	if !errorIs(err, ErrNoOwner) {
		t.Fatalf("err=%v, want ErrNoOwner", err)
	}
}

// TestRealSystemdRunLiveUnitEndToEnd (honest leg, this box): the REAL
// systemd-run spawns a real sleeping owner (the probe pin is NOT set), the
// post-spawn liveness gate passes, and the unit is independently observed
// active/running. This proves the gate does not break the proven shape on a
// healthy host; the pam_namespace leg is re-proven by the foreman on a
// bunker agent.
func TestRealSystemdRunLiveUnitEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real systemd user unit; skipped in -short")
	}
	if os.Getenv(spawnProbeEnv) != "" {
		t.Skip("probe env pinned; not the real-backend leg")
	}
	if _, err := os.Stat(os.Getenv("XDG_RUNTIME_DIR") + "/systemd"); err != nil {
		t.Skipf("no user manager runtime dir: %v", err)
	}
	_, lf := newTestStore(t)
	t.Setenv(detachModeOverrideEnv, "systemd-run")
	hold, _, _ := scratchHold(t, "P-QAM2-REAL")
	hold.TTL = 45 * time.Second // killed by the test before it fires
	id, err := lf.Arm(hold)
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	res, err := lf.Detach(id, hold.TTL)
	if err != nil {
		t.Fatalf("real systemd-run detach failed (liveness gate rejected a real unit): %v", err)
	}
	if res.Mode != "systemd-run" || res.Unit == "" {
		t.Fatalf("result %+v, want the systemd-run shape", res)
	}
	// the unit really is running under the user manager (an independent
	// read, not the verdict under test)
	out, err := exec.Command("systemctl", "--user", "show", "-p", "ActiveState", "-p", "SubState", "--", res.Unit).CombinedOutput()
	if err != nil {
		t.Fatalf("show %s: %v: %s", res.Unit, err, strings.TrimSpace(string(out)))
	}
	if !strings.Contains(string(out), "ActiveState=active") || !strings.Contains(string(out), "SubState=running") {
		t.Fatalf("unit %s not active/running after a graded-success spawn: %s", res.Unit, strings.TrimSpace(string(out)))
	}
	// cleanup: end the hold and the unit (a revert within TTL is the
	// normal path; the killed owner then exits at wake with a reported
	// no-live-hold error).
	if _, err := lf.Revert(testContext(), id, "direct", RoleAPI); err != nil {
		t.Fatalf("revert: %v", err)
	}
	stop := exec.Command("systemctl", "--user", "stop", "--", res.Unit)
	_ = stop.Run()
}
