package reverter

import (
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

// defaultDetach spawns the TTL owner OUTSIDE the CLI's fate (AC-5). Two
// shapes, in order of preference:
//
//  1. The proven shape: systemd-run --user --scope -p RuntimeMaxSec=<n> —
//     the scope bounds the owner's lifetime; the user manager reaps it.
//     Used when the user manager's runtime dir is present (systemctl
//     --user degraded hosts can fall through to shape 2; pin with
//     MISCHIEF_REVERTER_DETACH_MODE when the choice matters).
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

// detachSystemdRun is the proven shape: `systemd-run --user --collect
// --unit=<name> -p RuntimeMaxSec=<n> <self> __owner ...`. This is the
// TRANSIENT SERVICE form, which starts the unit and returns immediately —
// the `--scope` form runs the command INSIDE the caller's terminal session
// and blocks until it exits (measured on this host: a scope hold Detach for
// the owner's whole TTL sleep, which deadlocks the arming CLI), so the
// owner would not be detached at all. RuntimeMaxSec still bounds the
// owner's lifetime; the user manager reaps it; --collect GCs the failed
// unit after exit.
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
	cmd := exec.Command("systemd-run", args...)
	cmd.Env = append(os.Environ(), ownerEnvPairs(req)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return ownerResult{}, fmt.Errorf("%w: systemd-run: %v: %s", ErrNoOwner, err, strings.TrimSpace(string(out)))
	}
	return ownerResult{Mode: "systemd-run", PID: 0, Unit: unit}, nil
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
