package backend_signal

import "fmt"

// The seccomp-notify supervisor backend (SPEC-06, backend "sec"):
// S-013 (syscall-subset kill via SECCOMP_RET_ERRNO) and the S-005 fsync
// class ride this backend. The design:
//
//   - The supervisor process loads a BPF filter over the target's syscall
//     set with SECCOMP_RET_USER_NOTIF as the default action and listens on
//     the notifier fd (this host PROVES the mechanism: seccomp-probe ran
//     `notif=80 resp=24`, probe/RESULTS.md — the kernel support is present
//     and rootless).
//   - Per rule, the supervisor answers a notification with
//     SECCOMP_USER_NOTIF_FLAG_CONTINUE (pass-through) or
//     SECCOMP_RET_ERRNO (the injected fault), and counts every injection
//     into a counter file — the same landed-proof contract as the shim.
//   - The filter is dropped with the process; there is no persistent host
//     state (the spec's "supervisor detached, filter dropped with the
//     process").
//
// What v0.1 SHIPS is the refusal, not the supervisor: the notifier loop
// needs a helper binary (the C supervisor compiled from a seccomp-probe
// derivative) and that helper does not exist yet. Per AC-11 the verb stays
// available and the refusal NAMES the missing piece; per AC-3 we do not
// ship a supervisor stub that arms nothing and reports green — a
// supervisor that never supervises is precisely the green-but-does-nothing
// backend this package exists to prevent.
//
// NOT-LIST: this stub does not touch seccomp(2) beyond availability
// probing (SECCOMP_GET_NOTIF_SIZES); it arms nothing, attaches nothing and
// never returns a landed outcome.

// NotifSizes is the kernel's seccomp user-notify buffer sizes (the
// SECCOMP_GET_NOTIF_SIZES result). Zeroes mean "not measured".
type NotifSizes struct {
	Notif, NotifResp, SavedSig uint16
}

// sysNotifSizes issues seccomp(SECCOMP_GET_NOTIF_SIZES) on this host. The
// syscall is rootless; the probe measured notif=80 resp=24 here.
func sysNotifSizes() (NotifSizes, error) {
	_, scNum, err := sysNumbers()
	if err != nil {
		return NotifSizes{}, err
	}
	var sz [3]uint16
	_, _, en := syscall3(scNum, 3 /* SECCOMP_GET_NOTIF_SIZES */, 0, uintptr(ptrOf(&sz)))
	if en != 0 {
		return NotifSizes{}, en
	}
	return NotifSizes{Notif: sz[0], NotifResp: sz[1], SavedSig: sz[2]}, nil
}

// seccompSupervisor is the armed shape of a seccomp fault. v0.1 can
// construct it only when every dependency exists; Supervisor checks the
// chain and refuses naming the FIRST absent piece (deterministic refusals
// are diffable refusals).
type seccompSupervisor struct {
	Primitive string
	Syscalls  []string
	Errno     string
	proofPath string
}

// Supervisor designs a seccomp-notify fault for the named syscalls and
// reports availability. It ALWAYS returns an UnavailableError today: the
// helper binary the notifier loop needs is not shipped. The kernel-side
// availability (GET_NOTIF_SIZES) is measured first so the refusal can name
// the exact layer that is absent — kernel support present, helper missing
// (AC-11: name what is absent).
func Supervisor(primitive string, syscalls []string, errno string, dir string) (*seccompSupervisor, error) {
	if len(syscalls) == 0 {
		return nil, NewUnavailable("seccomp supervisor: no syscall subset declared for %s", primitive)
	}
	if !ValidErrno(errno) {
		return nil, NewUnavailable("seccomp supervisor: errno %q is not in the closed vocabulary for %s", errno, primitive)
	}
	if _, err := sysNotifSizes(); err != nil {
		// the kernel layer itself is unavailable on this host
		return nil, NewUnavailable(
			"seccomp user-notify for %s: seccomp(SECCOMP_GET_NOTIF_SIZES) failed: %v (kernel support absent)",
			primitive, err)
	}
	return nil, NewUnavailable(
		"seccomp user-notify supervisor for %s: kernel support present (GET_NOTIF_SIZES ok) but the notifier helper binary is not shipped in v0.1 (needs supervisor helper compiled from probe/seccomp-probe.c); refusing instead of arming a supervisor that would never supervise",
		primitive)
}

// unavailable is the error AC-11 grades capability_unavailable from: the
// verb stays available, the refusal names the missing capability, and the
// run fails loudly instead of skipping silently.
type UnavailableError struct{ Detail string }

func (e *UnavailableError) Error() string { return "capability_unavailable: " + e.Detail }

// NewUnavailable builds a capability_unavailable refusal.
func NewUnavailable(format string, args ...any) error {
	return &UnavailableError{Detail: fmt.Sprintf(format, args...)}
}

// IsUnavailable reports whether err is a capability_unavailable refusal
// (AC-11's machine-checkable shape).
func IsUnavailable(err error) bool {
	ue, ok := err.(*UnavailableError)
	return ok && ue.Detail != ""
}
