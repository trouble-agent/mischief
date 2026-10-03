package backend_signal

import "fmt"

// Status is the two-value landing state of an Outcome. The vocabulary is
// deliberately not the verdict vocabulary: SPEC-05 grades verdicts; a
// backend only ever reports whether its landed-proof fired.
type Status string

const (
	// StatusLanded: the landed-proof fired (measured, independent of the
	// fault's own exit code — AC-13).
	StatusLanded Status = "landed"
	// StatusNoOp: the fault was armed but no landed-proof fired. Per AC-3
	// this FAILS the run (ExitCode 1); it is never a pass.
	StatusNoOp Status = "no_op"
)

// Receipt is the measured evidence that the fault mechanism was delivered:
// the syscall outcome itself ("kill(pid, SIGSTOP)=0", "prlimit64(...)=0").
// A receipt is necessary but never sufficient: a kill receipt on a zombie
// process is a receipt without a landing (the AC-3 no_op arm, tested live).
type Receipt struct {
	Kind   string
	Detail string
}

func (r Receipt) String() string { return r.Kind + ": " + r.Detail }

// Outcome is the measured result of one fault application.
type Outcome struct {
	// Primitive is the primitive id the application ran for.
	Primitive string
	// Backend names the actuator family ("sig", "shim", "prlimit", "sec").
	Backend string
	// Status is the landing state (landed / no_op).
	Status Status
	// Receipt is the delivery receipt; nil when the mechanism was never
	// issued (a refused application).
	Receipt *Receipt
	// LandedProof is the measured proof text when Status is landed
	// ("<kind>: <measurement>"), "" otherwise.
	LandedProof string
	// NoOpReason names why nothing landed; required for StatusNoOp (a
	// no_op without a stated reason is a contract violation, and NewNoOp
	// substitutes a loud default rather than allow one).
	NoOpReason string
	// Err carries a hard failure (refused, unreadable target, EPERM).
	// Err != nil implies ExitCode 1; a refused application never landed,
	// so Status stays no_op.
	Err error
}

// NewLanded records a landed application with its measured proof.
func NewLanded(primitive, backend, proof string, rc *Receipt) Outcome {
	return Outcome{
		Primitive: primitive, Backend: backend, Status: StatusLanded,
		Receipt: rc, LandedProof: proof,
	}
}

// NewNoOp records an armed-but-not-landed application. An empty reason is a
// contract violation: it is replaced with a loud default (pinned by test)
// because AC-3's no_op must always say what did not land.
func NewNoOp(primitive, backend, reason string, rc *Receipt) Outcome {
	if reason == "" {
		reason = "no_op graded without a stated reason — contract violation (AC-3 requires naming what did not land)"
	}
	return Outcome{
		Primitive: primitive, Backend: backend, Status: StatusNoOp,
		Receipt: rc, NoOpReason: reason,
	}
}

// NewFailed records a refused or hard-failed application (never landed).
func NewFailed(primitive, backend string, err error, rc *Receipt) Outcome {
	return Outcome{
		Primitive: primitive, Backend: backend, Status: StatusNoOp,
		Receipt: rc, Err: err,
		NoOpReason: "refused before landing: " + err.Error(),
	}
}

// ExitCode maps the outcome onto the process exit contract: landed = 0,
// everything else non-zero. A no_op MUST fail the run (AC-3); the exit code
// is the part the verdict engine and the CLI share.
func (o Outcome) ExitCode() int {
	if o.Status == StatusLanded && o.Err == nil {
		return 0
	}
	return 1
}

// Landed reports whether the landed-proof fired.
func (o Outcome) Landed() bool { return o.Status == StatusLanded && o.Err == nil }

func (o Outcome) String() string {
	switch o.Status {
	case StatusLanded:
		return fmt.Sprintf("%s/%s: landed (%s)", o.Backend, o.Primitive, o.LandedProof)
	default:
		detail := o.NoOpReason
		if o.Err != nil {
			detail = fmt.Sprintf("%s (error: %v)", detail, o.Err)
		}
		return fmt.Sprintf("%s/%s: no_op (%s)", o.Backend, o.Primitive, detail)
	}
}
