package backend_signal

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

// Resource is the rlimit resource a prlimit fault squeezes. The C-level
// resource numbers are wired per-GOARCH (they are stable ABI constants, not
// guesses); an unsupported GOARCH grades capability_unavailable naming the
// architecture (AC-11) instead of issuing a guessed syscall number.
type Resource int

const (
	// ResNoFile is RLIMIT_NOFILE (max open file descriptors).
	ResNoFile Resource = iota
	// ResNProc is RLIMIT_NPROC (max processes for the invoking uid).
	ResNProc
)

// Rlimit is the soft/hard pair of one resource (the prlimit64 wire shape).
type Rlimit struct {
	Cur uint64
	Max uint64
}

// sysRes maps a Resource onto its C-level RLIMIT_* number for this GOARCH.
// The wiring is explicit: amd64 carries the glibc numbers (NOFILE 7, NPROC
// 6); any other architecture is unwired.
func (r Resource) sysRes() (uintptr, error) {
	switch r {
	case ResNoFile:
		return 7, nil // RLIMIT_NOFILE (amd64)
	case ResNProc:
		return 6, nil // RLIMIT_NPROC (amd64)
	}
	return 0, fmt.Errorf("prlimit: unknown resource %d", int(r))
}

// rlimit64 is the struct prlimit64(2) exchanges (the kernel ABI: two
// uint64s — NOT the Go syscall.Rlimit, whose Cur/Max are int32 on 386 and
// whose syscall.Prlimit wrapper does not exist).
type rlimit64 struct{ Cur, Max uint64 }

// archRefusals wires the architecture-gated syscall numbers. Only amd64 is
// wired (probed live on this host); every other GOARCH returns a named
// capability_unavailable refusal. runtime.GOARCH is compile-time constant,
// so the wiring is exact per binary.
var archRefusals = map[string]struct{ prlimit64, seccomp uintptr }{
	"amd64": {302, 317},
}

// goarch is this binary's GOARCH (runtime.GOARCH).
var goarch = runtime.GOARCH

func sysNumbers() (prlimit64, seccomp uintptr, err error) {
	w, ok := archRefusals[goarch]
	if !ok {
		return 0, 0, fmt.Errorf(
			"capability_unavailable: prlimit64(2)/seccomp(2) syscall numbers are not wired for GOARCH=%s (wired: amd64); refusing rather than guessing an ABI number",
			goarch)
	}
	return w.prlimit64, w.seccomp, nil
}

// prlimitRaw issues prlimit64(2) directly: new==nil reads, old==nil writes.
// This needs no cgo and no root: a process may always read another
// process's limits, and may lower another process's soft limits when it
// may signal it (same user — the rootless contract of the fault tier).
func prlimitRaw(pid int, res Resource, newv, old *rlimit64) error {
	num, err := res.sysRes()
	if err != nil {
		return err
	}
	prl, _, serr := sysNumbers()
	if serr != nil {
		return serr
	}
	_, _, en := syscall.Syscall6(prl, uintptr(pid), num,
		uintptr(unsafe.Pointer(newv)), uintptr(unsafe.Pointer(old)), 0, 0)
	if en != 0 {
		return en
	}
	return nil
}

// prlimitFault is one armed prlimit fault: the target pid, the resource,
// the squeeze value and the saved pre-fault limits (the inverse material).
type prlimitFault struct {
	Primitive string
	PID       int
	Res       Resource
	To        uint64
	Saved     Rlimit
	// receipt is the syscall outcome of the squeeze.
	receipt Receipt
}

// Squeeze prlimit values a v0.1 fault is allowed to pick. Refusing
// arbitrary values here keeps the language anchored (and stops a fat-finger
// "unlimited" being graded as a squeeze).
var allowedSqueezes = map[Resource]map[uint64]bool{
	ResNoFile: {16: true, 32: true, 64: true, 128: true},
	ResNProc:  {16: true, 32: true, 64: true, 128: true, 200: true},
}

// allowedSqueeze reports whether v is a sanctioned squeeze value for res.
func allowedSqueeze(res Resource, v uint64) bool { return allowedSqueezes[res][v] }

// ApplyPrlimit lowers ONE resource of a LIVE target pid and proves the
// squeeze landed by reading the limits back through BOTH kernel surfaces:
// prlimit64(2) itself (the write surface) and /proc/<pid>/limits (the
// independent read surface procfs renders). One surface agreeing is not
// landed-proof; two independent renderings of the same value are.
//
// It refuses pid <= 1 (structurally excluded), refuses a dead/zombie target
// before arming (a squeeze over a corpse is a receipt without a landing —
// the AC-3 no-op arm), and refuses squeezing to a value ABOVE the current
// soft limit (raising is not a fault; it would also need CAP_SYS_RESOURCE).
func ApplyPrlimit(primitive string, pid int, res Resource, to uint64) (Outcome, *prlimitFault) {
	if err := checkTargetPID(pid); err != nil {
		return NewFailed(primitive, "prlimit", err, nil), nil
	}
	if _, err := procState(pid); err != nil {
		// ENOENT: the target died between resolution and arming — a
		// no_op with a named reason, never a fake landing.
		return NewNoOp(primitive, "prlimit",
			fmt.Sprintf("target /proc/%d unreadable before arming: %v", pid, err), nil), nil
	}
	var cur rlimit64
	if err := prlimitRaw(pid, res, nil, &cur); err != nil {
		return NewFailed(primitive, "prlimit", fmt.Errorf("prlimit64 read (pid %d): %w", pid, err), nil), nil
	}
	if to >= cur.Cur {
		return NewFailed(primitive, "prlimit",
			fmt.Errorf("refusing to set %s soft limit to %d: current soft is %d (a squeeze must lower; raising needs CAP_SYS_RESOURCE and is not a fault)",
				resName(res), to, cur.Cur), nil), nil
	}
	newv := rlimit64{Cur: to, Max: cur.Max}
	if err := prlimitRaw(pid, res, &newv, nil); err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			return NewFailed(primitive, "prlimit",
				fmt.Errorf("prlimit64 set (pid %d, %s=%d): %w (rootless: the invoker must own the target)", pid, resName(res), to, err), nil), nil
		}
		return NewFailed(primitive, "prlimit", fmt.Errorf("prlimit64 set (pid %d, %s=%d): %w", pid, resName(res), to, err), nil), nil
	}
	f := &prlimitFault{
		Primitive: primitive, PID: pid, Res: res, To: to,
		Saved: Rlimit{Cur: cur.Cur, Max: cur.Max},
		receipt: Receipt{
			Kind:   "syscall",
			Detail: fmt.Sprintf("prlimit64(pid=%d, %s, %d->%d)=0", pid, resName(res), cur.Cur, to),
		},
	}
	// Landed-proof: read back through BOTH surfaces — prlimit64 (the same
	// syscall that wrote it) and /proc/<pid>/limits (procfs's independent
	// rendering of the kernel object).
	var rb rlimit64
	if err := prlimitRaw(pid, res, nil, &rb); err != nil {
		return NewNoOp(primitive, "prlimit",
			fmt.Sprintf("squeeze issued but read-back failed: %v", err), &f.receipt), nil
	}
	pl, err := readProcLimits(pid)
	if err != nil {
		return NewNoOp(primitive, "prlimit",
			fmt.Sprintf("squeeze issued but /proc/%d/limits read-back failed: %v", pid, err), &f.receipt), nil
	}
	viaProc := limitsToRlimit(pl, res)
	if rb.Cur != to || viaProc.Cur != to {
		return NewNoOp(primitive, "prlimit",
			fmt.Sprintf("squeeze issued but did not land: prlimit64 reads %d, /proc limits reads %d, wanted %d",
				rb.Cur, viaProc.Cur, to), &f.receipt), nil
	}
	proof := fmt.Sprintf("prlimit-readback: %s soft=%d (was %d) via prlimit64 AND /proc/%d/limits",
		resName(res), rb.Cur, cur.Cur, pid)
	return NewLanded(primitive, "prlimit", proof, &f.receipt), f
}

// Revert restores the saved soft limit and proves the undo the same way the
// fault proved its landing (both surfaces read the old value again).
func (f *prlimitFault) Revert() Outcome {
	old := rlimit64{Cur: f.Saved.Cur, Max: f.Saved.Max}
	if err := prlimitRaw(f.PID, f.Res, &old, nil); err != nil {
		return NewFailed(f.Primitive, "prlimit", fmt.Errorf("revert: prlimit64 restore: %w", err), &f.receipt)
	}
	var rb rlimit64
	if err := prlimitRaw(f.PID, f.Res, nil, &rb); err != nil {
		return NewFailed(f.Primitive, "prlimit", fmt.Errorf("revert: read-back: %w", err), &f.receipt)
	}
	pl, err := readProcLimits(f.PID)
	if err != nil {
		return NewFailed(f.Primitive, "prlimit", fmt.Errorf("revert: /proc limits read-back: %w", err), &f.receipt)
	}
	viaProc := limitsToRlimit(pl, f.Res)
	if rb.Cur != f.Saved.Cur || viaProc.Cur != f.Saved.Cur {
		return NewFailed(f.Primitive, "prlimit",
			fmt.Errorf("revert issued but not proven: prlimit64 reads %d, /proc limits reads %d, wanted %d",
				rb.Cur, viaProc.Cur, f.Saved.Cur), &f.receipt)
	}
	proof := fmt.Sprintf("prlimit-restore: %s soft=%d via prlimit64 AND /proc/%d/limits",
		resName(f.Res), rb.Cur, f.PID)
	return NewLanded(f.Primitive, "prlimit", proof, &f.receipt)
}

// SpawnWithPrlimit starts cmd, applies the squeeze to the fresh child, and
// returns the outcome plus a kill function for cleanup. The child is a real
// process (no mocks): the squeeze lands on a live pid the test can observe.
func SpawnWithPrlimit(primitive string, cmd *exec.Cmd, res Resource, to uint64) (Outcome, *prlimitFault, func(), error) {
	if err := cmd.Start(); err != nil {
		return NewFailed(primitive, "prlimit", fmt.Errorf("spawn: %w", err), nil), nil, nil, err
	}
	kill := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}
	out, f := ApplyPrlimit(primitive, cmd.Process.Pid, res, to)
	return out, f, kill, nil
}

// SelftestPrlimit is the primitive's selftest: land a NOFILE squeeze on a
// scratch child, prove landed, revert, prove reverted — all four measured
// (the AC-19 contract: a selftest proves the mechanism on THIS host).
func SelftestPrlimit() error {
	child := exec.Command("sleep", "10")
	out, f, kill, err := SpawnWithPrlimit("S-008", child, ResNoFile, 64)
	if err != nil {
		return fmt.Errorf("selftest prlimit: spawn: %w", err)
	}
	defer kill()
	if !out.Landed() {
		return fmt.Errorf("selftest prlimit: land did not prove: %s", out)
	}
	r := f.Revert()
	if !r.Landed() {
		return fmt.Errorf("selftest prlimit: revert did not prove: %s", r)
	}
	return nil
}

func resName(r Resource) string {
	if r == ResNoFile {
		return "RLIMIT_NOFILE"
	}
	return "RLIMIT_NPROC"
}

// checkTargetPID refuses pid <= 1: init is structurally excluded and pid 0
// means "every process in my process group" — a misparse must not become a
// broadcast. (The full protected-target policy is SPEC-03's rails; this is
// the backend's own floor.)
func checkTargetPID(pid int) error {
	if pid <= 1 {
		return fmt.Errorf("refusing pid %d: pid <= 1 is structurally excluded (init / process-group broadcast)", pid)
	}
	return nil
}

// limitLine renders one parsed limit row the way /proc/<pid>/limits does
// (used by tests to assert the parsed shape against the file's own text).
func limitLine(name string, v Rlimit, unit string) string {
	soft, hard := "unlimited", "unlimited"
	if v.Cur != Unlimited {
		soft = fmt.Sprint(v.Cur)
	}
	if v.Max != Unlimited {
		hard = fmt.Sprint(v.Max)
	}
	return strings.Join([]string{name, soft, hard, unit}, " ")
}
