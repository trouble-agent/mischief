package backend_signal

import "fmt"

// Status is the landing state of an Outcome. The vocabulary is deliberately
// not the verdict vocabulary: SPEC-05 grades verdicts; a backend reports
// whether an independent landed-proof fired.
type Status string

const (
	// StatusLanded: an independent landed-proof fired.
	StatusLanded Status = "landed"
	// StatusNoOp: the fault was armed but no landed-proof fired.
	StatusNoOp Status = "no_op"
	// StatusInconclusive: only injector-owned evidence was available.
	StatusInconclusive Status = "inconclusive"
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

// NewLanded records a landed application with a measured proof. Callers must
// supply an independent observation; actuator-owned receipts are not proof.
func NewLanded(primitive, backend, proof string, rc *Receipt) Outcome {
	return Outcome{
		Primitive: primitive, Backend: backend, Status: StatusLanded,
		Receipt: rc, LandedProof: proof,
	}
}

// NewInconclusive records evidence that cannot establish landing because the
// primitive has no independent witness.
func NewInconclusive(primitive, backend, reason string, rc *Receipt) Outcome {
	if reason == "" {
		reason = "missing independent witness"
	}
	return Outcome{Primitive: primitive, Backend: backend, Status: StatusInconclusive, Receipt: rc, NoOpReason: reason}
}

// Inconclusive reports that the landing could not be independently witnessed.
func (o Outcome) Inconclusive() bool { return o.Status == StatusInconclusive }

// NewLandedWithWitness records a pass only when an external witness is named.
func NewLandedWithWitness(primitive, backend, proof string, rc *Receipt, witness string) Outcome {
	if witness == "" {
		return NewInconclusive(primitive, backend, "missing independent witness", rc)
	}
	return NewLanded(primitive, backend, proof+" [witness: "+witness+"]", rc)
}

// NewInconclusiveFromInjectorEvidence refuses evidence produced by the injector.
func NewInconclusiveFromInjectorEvidence(primitive, backend, proof string, rc *Receipt) Outcome {
	return NewInconclusive(primitive, backend, "missing independent witness: injector-owned proof is only a hint ("+proof+")", rc)
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
	case StatusInconclusive:
		return fmt.Sprintf("%s/%s: inconclusive (%s)", o.Backend, o.Primitive, o.NoOpReason)
	default:
		detail := o.NoOpReason
		if o.Err != nil {
			detail = fmt.Sprintf("%s (error: %v)", detail, o.Err)
		}
		return fmt.Sprintf("%s/%s: no_op (%s)", o.Backend, o.Primitive, detail)
	}
}
