package backend_signal

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

// Signal fault backends (SPEC-06, backend "sig"): SIGSTOP (hang without a
// crash, P-001) and SIGKILL (crash without cleanup, P-003), delivered
// rootless to a same-user target pid.
//
// The landed-proof contract:
//
//   - SIGSTOP: the kill() receipt (kill(pid, SIGSTOP)=0) is necessary but
//     NOT sufficient — a zombie target accepts the kill and stays a zombie
//     (measured on this host, probe arm: kill(z, SIGSTOP)=0 with state
//     still Z). Landed means /proc/<pid>/stat read state 'T' for
//     minStateSamples consecutive samples. A signal fault that cannot
//     produce that observation is a no_op, however green the receipt.
//   - SIGKILL: the WaitStatus receipt (Signaled() && Signal()==SIGKILL)
//     IS the landing — there is no post-state to sample; the target's
//     disappearance by signal 9 is the fault.
//
// Inverse: SIGCONT for SIGSTOP (proved by a non-T state observation after
// the continue); no inverse is needed for SIGKILL (the catalog records
// "restart" as the recovery action, outside this package).

// minStateSamples is how many consecutive 'T' samples the SIGSTOP
// landed-proof requires (the catalog check: "state 'T' for 2 consecutive
// samples").
const minStateSamples = 2

// statePollInterval is the sample interval for /proc state observation.
const statePollInterval = 20 * time.Millisecond

// defaultStateWindow is how long a landed-proof observation may take
// before the fault grades no_op. Generous by design: an unloaded host
// proves 'T' in under 100ms; the window exists to bound a wedged target,
// not to race the scheduler.
const defaultStateWindow = 5 * time.Second

// SignalName is the supported signal vocabulary of the sig backend.
type SignalName string

const (
	// SigStop is SIGSTOP (unblockable, uncatchable stop).
	SigStop SignalName = "SIGSTOP"
	// SigCont is SIGCONT (the SIGSTOP inverse).
	SigCont SignalName = "SIGCONT"
	// SigKill is SIGKILL (unblockable, uncatchable termination).
	SigKill SignalName = "SIGKILL"
)

// sysSignal maps the named signal onto its syscall number. The names are
// the vocabulary; the numbers are the ABI (stable on Linux).
func sysSignal(s SignalName) (syscall.Signal, error) {
	switch s {
	case SigStop:
		return syscall.SIGSTOP, nil
	case SigCont:
		return syscall.SIGCONT, nil
	case SigKill:
		return syscall.SIGKILL, nil
	}
	return 0, fmt.Errorf("signal %q is not in the v0.1 sig vocabulary (SIGSTOP|SIGCONT|SIGKILL); tgkill/SIGSEGV-family/storms are named refusals, not silent falls-through", string(s))
}

// signalFault is one armed signal fault.
type signalFault struct {
	Primitive string
	PID       int
	Signal    SignalName
	receipt   Receipt
}

// ApplySignal delivers sig to a live target pid and proves the landing.
// Rootless: the invoker must already own the target (an EPERM kill is a
// named refusal; there is no elevation in this package).
func ApplySignal(primitive string, pid int, sig SignalName) (Outcome, *signalFault) {
	num, err := sysSignal(sig)
	if err != nil {
		return NewFailed(primitive, "sig", err, nil), nil
	}
	if err := checkTargetPID(pid); err != nil {
		return NewFailed(primitive, "sig", err, nil), nil
	}
	// A zombie target is deliverable-but-unlandable: the receipt returns
	// success and the state never leaves 'Z'. Refuse it BEFORE arming so
	// the no-op arm in tests has the same shape a live race would produce.
	st, err := procState(pid)
	if err != nil {
		return NewNoOp(primitive, "sig",
			fmt.Sprintf("target /proc/%d unreadable before arming: %v", pid, err), nil), nil
	}
	if st == 'Z' {
		return NewNoOp(primitive, "sig",
			fmt.Sprintf("target pid %d is a zombie (state Z): a delivery receipt could never prove landing", pid), nil), nil
	}
	if err := syscall.Kill(pid, num); err != nil {
		if err == syscall.ESRCH {
			return NewNoOp(primitive, "sig",
				fmt.Sprintf("kill returned ESRCH: pid %d vanished before arming", pid), nil), nil
		}
		return NewFailed(primitive, "sig", fmt.Errorf("kill(pid=%d, %s): %w", pid, sig, err), nil), nil
	}
	f := &signalFault{
		Primitive: primitive, PID: pid, Signal: sig,
		receipt: Receipt{Kind: "syscall", Detail: fmt.Sprintf("kill(pid=%d, %s)=0", pid, sig)},
	}
	switch sig {
	case SigStop:
		// Landed-proof: /proc/<pid>/stat state 'T' for 2 consecutive samples.
		if !waitFor(defaultStateWindow, statePollInterval, minStateSamples, func() bool {
			s, err := procState(pid)
			return err == nil && s == 'T'
		}) {
			s, _ := procState(pid)
			return NewNoOp(primitive, "sig",
				fmt.Sprintf("kill receipt ok but /proc/%d/stat never read 'T' within %s (last state %q) — the anti-gaming arm",
					pid, defaultStateWindow, string(rune(s))), &f.receipt), nil
		}
		proof := fmt.Sprintf("proc-state: /proc/%d/stat state T for %d consecutive samples", pid, minStateSamples)
		return NewLanded(primitive, "sig", proof, &f.receipt), f
	case SigKill:
		// Landed-proof: reap and check WaitStatus — the target must die BY
		// the signal. ApplySignal takes the reap (the caller spawned the
		// child; the Wait is bounded and required for the receipt).
		ws, waited := reapWithin(pid, defaultStateWindow)
		if !waited {
			return NewNoOp(primitive, "sig",
				fmt.Sprintf("kill receipt ok but no exit within %s to prove death-by-SIGKILL", defaultStateWindow), &f.receipt), nil
		}
		if !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
			return NewNoOp(primitive, "sig",
				fmt.Sprintf("target exited but not by SIGKILL (Signaled=%v, signal=%s) — the observed exit is not this fault's landing", ws.Signaled(), ws.Signal()), &f.receipt), nil
		}
		proof := fmt.Sprintf("wait-status: pid %d reaped, Signaled=true, signal=%s", pid, ws.Signal())
		return NewLanded(primitive, "sig", proof, &f.receipt), f
	case SigCont:
		// SIGCONT is the inverse verb, not a fault; landing it means the
		// target left 'T' (proved by Resume against its own proof shape).
		if !waitFor(defaultStateWindow, statePollInterval, 1, func() bool {
			s, err := procState(pid)
			return err == nil && s != 'T'
		}) {
			return NewNoOp(primitive, "sig",
				fmt.Sprintf("SIGCONT receipt ok but /proc/%d/stat still 'T'", pid), &f.receipt), nil
		}
		s, _ := procState(pid)
		proof := fmt.Sprintf("proc-state: /proc/%d/stat state %q after SIGCONT", pid, string(rune(s)))
		return NewLanded(primitive, "sig", proof, &f.receipt), f
	}
	return NewFailed(primitive, "sig", fmt.Errorf("unreachable: %q passed validation", sig), &f.receipt), nil
}

// Resume is the recorded inverse of a SIGSTOP fault: deliver SIGCONT and
// prove the undo (the state leaves 'T').
func (f *signalFault) Resume() Outcome {
	if f.Signal != SigStop {
		return NewFailed(f.Primitive, "sig", fmt.Errorf("resume: the inverse of %s is not defined here", f.Signal), &f.receipt)
	}
	out, _ := ApplySignal(f.Primitive, f.PID, SigCont)
	return out
}

// SpawnWithSignal starts cmd, waits for it to be observable in /proc, then
// applies the signal fault to the fresh child. Returns the kill/wait
// cleanup for the caller's defer.
func SpawnWithSignal(primitive string, cmd *exec.Cmd, sig SignalName) (Outcome, *signalFault, func(), error) {
	if err := cmd.Start(); err != nil {
		return NewFailed(primitive, "sig", fmt.Errorf("spawn: %w", err), nil), nil, nil, err
	}
	pid := cmd.Process.Pid
	kill := func() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	}
	// Wait until the child is observable as a live (non-Z) process —
	// sampling /proc immediately after Start races the exec.
	if !waitFor(defaultStateWindow, statePollInterval, 1, func() bool {
		s, err := procState(pid)
		return err == nil && s != 'Z'
	}) {
		kill()
		return NewNoOp(primitive, "sig",
			fmt.Sprintf("spawned pid %d never became observable in /proc", pid), nil), nil, kill, nil
	}
	out, f := ApplySignal(primitive, pid, sig)
	return out, f, kill, nil
}

// reapWithin waits for pid to be reaped and returns its WaitStatus. It is
// used by the SIGKILL landed-proof; ok=false when the window expires. The
// caller must own the child (it spawned it) — reaping a non-child fails
// with ECHILD and grades no_op, honestly.
func reapWithin(pid int, window time.Duration) (ws syscall.WaitStatus, ok bool) {
	deadline := time.Now().Add(window)
	for {
		var status syscall.WaitStatus
		wpid, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
		if err == nil && wpid == pid {
			return status, true
		}
		if err != nil && err != syscall.EINTR {
			return 0, false
		}
		if time.Now().After(deadline) {
			return 0, false
		}
		time.Sleep(statePollInterval)
	}
}

// SelftestSigStop is the P-001 selftest: land a SIGSTOP on a scratch child,
// prove landed, resume, prove reverted — all four measured on THIS host.
func SelftestSigStop() error {
	child := exec.Command("sleep", "10")
	out, f, kill, err := SpawnWithSignal("P-001", child, SigStop)
	if err != nil {
		return fmt.Errorf("selftest sigstop: spawn: %w", err)
	}
	defer kill()
	if !out.Landed() {
		return fmt.Errorf("selftest sigstop: land did not prove: %s", out)
	}
	r := f.Resume()
	if !r.Landed() {
		return fmt.Errorf("selftest sigstop: resume did not prove: %s", r)
	}
	return nil
}

// SelftestSigKill is the P-003 selftest: land a SIGKILL on a scratch child
// and prove death-by-signal on THIS host.
func SelftestSigKill() error {
	child := exec.Command("sleep", "10")
	out, _, kill, err := SpawnWithSignal("P-003", child, SigKill)
	if err != nil {
		return fmt.Errorf("selftest sigkill: spawn: %w", err)
	}
	kill() // the child is already dead; this reaps defensively
	if !out.Landed() {
		return fmt.Errorf("selftest sigkill: land did not prove: %s", out)
	}
	return nil
}
