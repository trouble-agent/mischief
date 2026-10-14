package reverter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ownerHoldIDEnv / ownerDirEnv / ownerExpiryEnv carry the owner request to
// the child process as a FALLBACK (the argv flags are primary — see
// ParseOwnerArgs). The journal is the durable source of truth; the request
// only names WHICH hold and WHEN — every fact the owner acts on (inverse,
// check, fault) it re-reads from the journal bytes.
const (
	ownerDirEnv    = "MISCHIEF_REVERTER_DIR"
	ownerHoldIDEnv = "MISCHIEF_REVERTER_HOLD"
	ownerExpiryEnv = "MISCHIEF_REVERTER_EXPIRY"
)

// detachModeOverrideEnv forces a detach shape ("setsid" or "systemd-run").
// On a host whose systemd user session is degraded, the natural probe can
// mispredict and pick setsid; tests (and an operator debugging the owner
// shape) set it to pin one. Unknown values fall through to the default
// order (the env is a hint, never a new code path).
const detachModeOverrideEnv = "MISCHIEF_REVERTER_DETACH_MODE"

// spawnProbeEnv is internal to this package: it injects the synthetic
// systemd-run backend used by the regression tests. It is NOT an operator
// knob — an operator override goes through detachModeOverrideEnv. Unset (or
// pointing at a missing file) on any production host, the real backend runs.
const spawnProbeEnv = "MISCHIEF_REVERTER_SYSTEMD_RUN_FAKE"

// ownerLivenessBeat is the delay between the unit's start and the
// post-spawn liveness re-probe. Short on purpose (it bounds every Arm for
// every caller); long enough that systemd has STARTED the unit's exec when
// the verdict is read — the 203/EXEC class fails the START_JOB well inside
// one beat.
const ownerLivenessBeat = 400 * time.Millisecond

// ownerProbeTimeout bounds one `systemctl --user show` (per-property
// IndividualCall). The user manager answers in milliseconds; a hung manager
// must not wedge the arm beyond the kill-switch budget this package owns.
const ownerProbeTimeout = 3 * time.Second

// errOwnerExecClass marks the verified-dead spawn: systemd-run accepted the
// start job but the unit's main process exited immediately (status 203,
// EXEC-formation failure — e.g. the binary is unreachable inside a private
// /tmp mount namespace). Callers (and tests) match this class with
// errors.Is; the message names the status and the unit.
var errOwnerExecClass = errors.New("owner unit exited immediately (systemd exec-formation class, e.g. 203/EXEC)")

// defaultDetach spawns the TTL owner OUTSIDE the CLI's fate (AC-5). Two
// shapes, in order of preference:
//
//  1. The proven shape: systemd-run --user --collect --unit=<name>
//     -p RuntimeMaxSec=<n> — the scope bounds the owner's lifetime; the
//     user manager reaps it. Used when the user manager's runtime dir is
//     present (systemctl --user degraded hosts can fall through to shape 2;
//     pin with MISCHIEF_REVERTER_DETACH_MODE when the choice matters).
//     The spawn is LIVENESS-VERIFIED: rc=0 from systemd-run only means the
//     START_JOB was accepted, not that the owner survived its exec
//     formation (the 203/EXEC class — the start job itself returns 0 on a
//     fast dead unit, verified live). A unit measured inactive after the
//     beat FAILS the spawn, naming the exec-formation class.
//  2. A real setsid child re-exec'ing OwnerMain: double-fork semantics via
//     SysProcAttr{Setsid: true}, so the owner is in its own session, NOT a
//     process-group member the CLI's death (or even its process-group kill)
//     would touch. kill -9 of the CLI never reaches it.
//
// Both shapes exec THIS binary (os.Executable) — no external helper exists
// to forget, no cwd assumption: the owner names the journal by absolute dir.
func defaultDetach(req ownerRequest) (ownerResult, error) {
	self, err := os.Executable()
	if err != nil {
		return ownerResult{}, fmt.Errorf("%w: executable: %v", ErrNoOwner, err)
	}
	switch strings.TrimSpace(strings.ToLower(os.Getenv(detachModeOverrideEnv))) {
	case "setsid":
		return detachSetsid(self, req)
	case "systemd-run":
		return detachSystemdRun(self, req)
	}
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" && systemdUserReachable(runtimeDir) {
		return detachSystemdRun(self, req)
	}
	return detachSetsid(self, req)
}

// systemdUserReachable reports whether the user manager answers on the
// runtime dir's bus socket (the cheap existence probe; a full systemd-run
// probe would spawn a unit per check).
func systemdUserReachable(runtimeDir string) bool {
	fi, err := os.Stat(runtimeDir + "/systemd")
	if err != nil {
		return false
	}
	return fi.IsDir()
}

// systemdRunBackend is the seam the spawn uses to run the systemd-run /
// systemctl --user pair. The default runs the real binaries; the regression
// tests swap in a synthetic backend. It exists so the 203/EXEC
// silent-success shape is reproducible on a healthy host without root,
// network, or a real degraded namespace.
type systemdRunBackend interface {
	// runSystemdRun starts the transient unit and returns its combined
	// output (the CLI's rc is encoded in err).
	runSystemdRun(env []string, args ...string) (string, error)
	// runSystemctlShow queries the unit's LoadState, ActiveState, SubState,
	// ExecMainStatus and ExecMainPID (one property per line, name=value).
	runSystemctlShow(env []string, unit string) (string, error)
}

type realSystemdRunBackend struct{}

func (realSystemdRunBackend) runSystemdRun(env []string, args ...string) (string, error) {
	cmd := exec.Command("systemd-run", args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (realSystemdRunBackend) runSystemctlShow(env []string, unit string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ownerProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "show",
		"-p", "LoadState", "-p", "ActiveState", "-p", "SubState",
		"-p", "ExecMainStatus", "-p", "ExecMainPID", "--", unit)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// systemdRunBackendFor picks the backend for one spawn: the synthetic probe
// only when MISCHIEF_REVERTER_SYSTEMD_RUN_FAKE names an executable file
// (tests); otherwise the real binaries.
func systemdRunBackendFor(env []string) systemdRunBackend {
	if p := os.Getenv(spawnProbeEnv); p != "" {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return fakeSystemdRunBackend{bin: p, env: env}
		}
	}
	return realSystemdRunBackend{}
}

// fakeSystemdRunBackend drives a synthetic systemd-run: the probe binary
// receives `systemd-run <args...>` or `show <unit>` as argv, prints the
// command output on stdout, and its exit code is the command's rc. Both the
// argv shape and the env contract match the real backend so a probe written
// for the tests exercises the exact verdict logic production runs.
type fakeSystemdRunBackend struct {
	bin string
	env []string
}

func (f fakeSystemdRunBackend) run(argv ...string) (string, error) {
	cmd := exec.Command(f.bin, argv...)
	cmd.Env = f.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (f fakeSystemdRunBackend) runSystemdRun(env []string, args ...string) (string, error) {
	return f.run(append([]string{"systemd-run"}, args...)...)
}

func (f fakeSystemdRunBackend) runSystemctlShow(env []string, unit string) (string, error) {
	return f.run("show", unit)
}

// unitProbe is the parsed `systemctl --user show` verdict input.
type unitProbe struct {
	LoadState      string
	ActiveState    string
	SubState       string
	ExecMainStatus string
	ExecMainPID    int64
}

// parseUnitProbe reads name=value lines (the show form). Unknown or missing
// properties stay zero — the verdict treats a truncated answer as "no live
// evidence", never as success.
func parseUnitProbe(out string) unitProbe {
	var p unitProbe
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "LoadState":
			p.LoadState = v
		case "ActiveState":
			p.ActiveState = v
		case "SubState":
			p.SubState = v
		case "ExecMainStatus":
			p.ExecMainStatus = v
		case "ExecMainPID":
			n, err := strconv.ParseInt(v, 10, 64)
			if err == nil {
				p.ExecMainPID = n
			}
		}
	}
	return p
}

// ownerUnitAlive is the liveness verdict for the spawned owner unit. TRUE
// only when the properties name a RUNNING process (active/running with a
// live ExecMainPID); every other reading — including the not-found/inactive
// reading of a `--collect`-reaped dead unit, and a unit systemd has not
// reached running state yet — is FALSE. The failure path is one-way: a unit
// that never reached running cannot be graded alive, because "the start job
// returned 0" is exactly the evidence the 203/EXEC silent-success bug
// taught us not to trust.
func ownerUnitAlive(p unitProbe) bool {
	if p.ActiveState != "active" {
		return false
	}
	if p.SubState != "running" {
		return false
	}
	if p.ExecMainPID <= 0 {
		return false
	}
	if p.LoadState == "not-found" {
		return false
	}
	return true
}

// probeOwnerUnit waits one beat after the start, then asks the user manager
// for the unit's state. The beat is the anti-race: `show` immediately after
// the start can observe the unit before the job lands; starting the clock
// AFTER the (rc=0) start and re-reading once has, on every measured host,
// been sufficient to observe either active/running or the dead state.
func probeOwnerUnit(ctx context.Context, b systemdRunBackend, env []string, unit string) (unitProbe, error) {
	select {
	case <-ctx.Done():
		return unitProbe{}, ctx.Err()
	case <-time.After(ownerLivenessBeat):
	}
	showCtx, cancel := context.WithTimeout(ctx, ownerProbeTimeout)
	defer cancel()
	_ = showCtx // the backend owns its own timeout; ctx cancellation is checked above
	out, err := b.runSystemctlShow(env, unit)
	if err != nil {
		// The verdict needs the manager's answer. A show that cannot run
		// (no user bus, vanished runtime dir) is UNKNOWN, and unknown is
		// the failure path: fall back rather than grade a unit we could
		// not observe.
		return unitProbe{}, fmt.Errorf("systemctl --user show %s: %v: %s", unit, err, strings.TrimSpace(out))
	}
	return parseUnitProbe(out), nil
}

// detachSystemdRun is the proven shape: `systemd-run --user --collect
// --unit=<name> -p RuntimeMaxSec=<n> <self> __owner ...`. This is the
// TRANSIENT SERVICE form, which starts the unit and returns immediately —
// the `--scope` form runs the command INSIDE the caller's terminal session
// and blocks until it exits (measured on this host: a scope hold Detach for
// the owner's whole TTL sleep, which deadlocks the arming CLI), so the
// owner would not be detached at all. RuntimeMaxSec still bounds the
// owner's lifetime; the user manager reaps it; --collect GCs the failed
// unit after exit.
//
// The spawn is LIVENESS-VERIFIED (QA-MISCHIEF-002): a 0 rc from systemd-run
// is only the START_JOB's acceptance — the unit's main process can still
// die inside its exec formation (status 203/EXEC) and the user manager
// collects the corpse silently. detachSystemdRun therefore re-probes the
// unit after a beat and grades the spawn by the MEASURED unit state:
//
//   - alive (active/running, live pid) → success.
//   - dead/never-ran → errOwnerExecClass failure naming the class, the
//     unit's exec status and the show reading. When the DETACH_MODE pin
//     forced this shape, the error is returned as-is (no silent shape
//     switch); on the natural path Detach falls back to the setsid shape.
//   - the probe itself fails (user bus gone) → failure naming the probe —
//     unknown is never graded success.
//
// A best-effort explicit `systemctl --user stop` + daemon-reload reaps the
// zombie unit on the failure paths (its RuntimeMaxSec would otherwise keep
// the dead unit loadable and named after the hold).
func detachSystemdRun(self string, req ownerRequest) (ownerResult, error) {
	unit := "mischief-owner-" + req.HoldID
	args := []string{
		"--user", "--collect",
		"--unit=" + unit,
		"-p", "RuntimeMaxSec=" + strconv.FormatInt(req.RuntimeMaxSec, 10),
		self, "__owner",
		"-dir", req.Dir,
		"-hold", req.HoldID,
		"-expiry", strconv.FormatInt(req.Expiry, 10),
	}
	env := append(os.Environ(), ownerEnvPairs(req)...)
	b := systemdRunBackendFor(env)
	out, err := b.runSystemdRun(env, args...)
	if err != nil {
		return ownerResult{}, fmt.Errorf("%w: systemd-run: %v: %s", ErrNoOwner, err, strings.TrimSpace(out))
	}
	probe, perr := probeOwnerUnit(context.Background(), b, env, unit)
	if perr == nil && ownerUnitAlive(probe) {
		return ownerResult{Mode: "systemd-run", PID: 0, Unit: unit}, nil
	}
	// The unit is measured not-running (or unobservable): the spawn FAILED
	// regardless of systemd-run's rc — the verified 203/EXEC silent-success
	// shape. Grade it loudly, then reap the unit. Two distinct failure
	// shapes: the probe itself failed (UNKNOWN — the exec-formation class
	// would be a false claim) vs. an observed not-running unit.
	status := "unknown"
	var detail string
	class := errOwnerExecClass
	if perr != nil {
		detail = "probe unavailable: " + perr.Error()
		class = ErrNoOwner // unobservable: a plain owner failure, NOT the measured exec-formation class
	} else {
		detail = fmt.Sprintf("show: load=%s active=%s sub=%s exec_status=%s pid=%d",
			probe.LoadState, probe.ActiveState, probe.SubState, probe.ExecMainStatus, probe.ExecMainPID)
		status = probe.ExecMainStatus
		if status == "" {
			// The show contract always carries ExecMainStatus=; an absent
			// value means the answer was malformed (still graded not-alive).
			status = "unset"
		}
	}
	stop := exec.Command("systemctl", "--user", "stop", "--", unit)
	stop.Env = env
	_ = stop.Run() // best-effort reap of the dead/zombie unit
	reload := exec.Command("systemctl", "--user", "daemon-reload")
	reload.Env = env
	_ = reload.Run()
	return ownerResult{}, fmt.Errorf("%w: %w: unit=%s exec_status=%s (%s); systemd-run out: %s",
		ErrNoOwner, class, unit, status, detail, strings.TrimSpace(out))
}

// detachSetsid is the fallback shape: a real forked child in its own
// session. The env carries the owner request; argv carries the same facts
// (the child's flag parser is the one source of the expiry).
func detachSetsid(self string, req ownerRequest) (ownerResult, error) {
	cmd := exec.Command(self, "__owner",
		"-dir", req.Dir,
		"-hold", req.HoldID,
		"-expiry", strconv.FormatInt(req.Expiry, 10))
	cmd.Env = append(os.Environ(), ownerEnvPairs(req)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return ownerResult{}, fmt.Errorf("%w: setsid child: %v", ErrNoOwner, err)
	}
	pid := cmd.Process.Pid
	// Wait is deliberately NOT called: the owner outlives this process on
	// purpose (it is reparented to init when the CLI dies, and reaped by
	// its own session when it finishes).
	go func() { _ = cmd.Wait() }()
	return ownerResult{Mode: "setsid", PID: pid}, nil
}

// ownerEnvPairs is the env the owner child receives (in addition to the
// ambient env — the owner needs PATH/HOME for its own scratch execs, which
// minimalEnv constrains at execution time).
func ownerEnvPairs(req ownerRequest) []string {
	return []string{
		ownerDirEnv + "=" + req.Dir,
		ownerHoldIDEnv + "=" + req.HoldID,
		ownerExpiryEnv + "=" + strconv.FormatInt(req.Expiry, 10),
	}
}

// OwnerFlags is the parsed owner request (argv + env agreement checked).
type OwnerFlags struct {
	Dir    string
	HoldID string
	Expiry time.Time
}

// ParseOwnerArgs parses the `__owner` subcommand's flags (-dir -hold
// -expiry). It is a separate function so OwnerMain callers (the test
// binary's __owner branch) share the exact parser the real CLI will call.
func ParseOwnerArgs(args []string) (OwnerFlags, error) {
	var f OwnerFlags
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-dir":
			i++
			if i >= len(args) {
				return f, fmt.Errorf("owner: -dir requires a value")
			}
			f.Dir = args[i]
		case "-hold":
			i++
			if i >= len(args) {
				return f, fmt.Errorf("owner: -hold requires a value")
			}
			f.HoldID = args[i]
		case "-expiry":
			i++
			if i >= len(args) {
				return f, fmt.Errorf("owner: -expiry requires a value")
			}
			n, err := strconv.ParseInt(args[i], 10, 64)
			if err != nil {
				return f, fmt.Errorf("owner: -expiry: %w", err)
			}
			f.Expiry = time.Unix(0, n)
		default:
			return f, fmt.Errorf("owner: unknown flag %q", args[i])
		}
	}
	if f.Dir == "" {
		if v := os.Getenv(ownerDirEnv); v != "" {
			f.Dir = v
		} else {
			return f, fmt.Errorf("owner: dir missing (flag or %s)", ownerDirEnv)
		}
	}
	if f.HoldID == "" {
		if v := os.Getenv(ownerHoldIDEnv); v != "" {
			f.HoldID = v
		} else {
			return f, fmt.Errorf("owner: hold missing (flag or %s)", ownerHoldIDEnv)
		}
	}
	if f.Expiry.IsZero() {
		if v := os.Getenv(ownerExpiryEnv); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return f, fmt.Errorf("owner: expiry from %s: %w", ownerExpiryEnv, err)
			}
			f.Expiry = time.Unix(0, n)
		} else {
			return f, fmt.Errorf("owner: expiry missing (flag or %s)", ownerExpiryEnv)
		}
	}
	return f, nil
}

// RunOwner is the owner process's main: parse, open, wait, measured revert.
// The expiry env/flag is the WAKE time; the revert still reads the hold
// from the journal (a hold deleted between spawn and expiry reverts to
// ErrNoHold, which is reported, not swallowed).
func RunOwner(args []string) error {
	f, err := ParseOwnerArgs(args)
	if err != nil {
		return err
	}
	return OwnerMain(f.Dir, f.HoldID, f.Expiry)
}
