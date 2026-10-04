package rails

import "fmt"

// RefusalReason is the closed refusal taxonomy (SPEC-03 owns it; the PRD's
// verdict vocabulary §6.4 grades every rail refusal aborted, and the CLI
// maps it to exit 2). The taxonomy is closed on purpose: a refusal that
// cannot name which rail stopped the run is how a "green but meaningless"
// run passes.
type RefusalReason string

const (
	// ReasonNoTarget: no target was declared and there is no default (AC-2).
	ReasonNoTarget RefusalReason = "no_target"
	// ReasonProtected: the target is on the protected list and no explicit
	// operator override was given (AC-7).
	ReasonProtected RefusalReason = "protected_target"
	// ReasonExcluded: the target is structurally excluded (PID 1, kernel
	// threads, mischief's own tree/reverter) — unconditional, no override.
	ReasonExcluded RefusalReason = "structurally_excluded"
	// ReasonScope: the requested scope sits below the primitive's minimum
	// ladder level (AC-19 shape at the ladder itself).
	ReasonScope RefusalReason = "scope_below_minimum"
	// ReasonSelftest: the primitive's selftest is not green on this host and
	// the scope is above L0 (AC-19).
	ReasonSelftest RefusalReason = "selftest_not_green"
	// ReasonL5Gate: a real-provider (L5) fault without the explicit opt-in,
	// its allowlist, or its spend cap (PRD §7 ladder, AC-17 shape).
	ReasonL5Gate RefusalReason = "l5_not_allowed"
	// ReasonLoad: the host is at or above the load gate (AC-8); the refusal
	// text names the measured numbers.
	ReasonLoad RefusalReason = "load_gate"
	// ReasonSanction: the host itself is not sanctioned for mischief
	// (SPEC-13 / MSF-020) — no reason-bearing sanction marker (file and/or
	// env). The host-admission rail lands with internal/sanction; it is the
	// FIRST rail, checked before target resolution, the scope ladder or the
	// load gate.
	ReasonSanction RefusalReason = "host_not_sanctioned"
)

// ExitCode is the CLI exit code every refusal maps to: 2. The verb refuses
// and lands nothing.
const ExitCode = 2

// Refusal is the one shape every rail refusal takes, whatever produced it:
// the closed reason, the primitive and target it concerns, the human text
// (which names the specifics — measurements, missing selftest state), and
// the verdict value the run grades (always aborted from the rails).
type Refusal struct {
	// Reason is one of the closed taxonomy values.
	Reason RefusalReason
	// Primitive is the primitive id the refusal concerns ("" when the
	// refusal precedes primitive selection — target resolution).
	Primitive string
	// Detail is the human explanation; it names the numbers where the AC
	// requires them (AC-8: threshold vs measured).
	Detail string
	// Verdict is the vocabulary value the run grades (PRD §6.4: aborted —
	// "rails/TTL/blast refusal").
	Verdict string
}

// Error renders "rails: <reason>: <detail>".
func (r *Refusal) Error() string {
	return fmt.Sprintf("rails: %s: %s", r.Reason, r.Detail)
}

// Exit is the CLI exit code for any refusal (AC-2 shape: exit 2, nothing
// landed).
func (r *Refusal) Exit() int { return ExitCode }

// MapExit is the cmd-level mapping between an error and the CLI exit code:
// a rails refusal exits 2 (AC-2's "exits 2 and lands nothing"), any other
// error exits 1, nil exits 0. A future main() calls this for every verb so
// the exit contract is defined once.
func MapExit(err error) int {
	switch {
	case err == nil:
		return 0
	case IsRefusal(err):
		return ExitCode
	default:
		return 1
	}
}

// IsRefusal reports whether err is (or wraps) a rails refusal — either the
// generic rail refusal (*Refusal) or the target-resolution one
// (*ResolveError); both carry the closed taxonomy and exit 2.
func IsRefusal(err error) bool {
	for err != nil {
		switch err.(type) {
		case *Refusal, *ResolveError:
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// newRefusal builds a refusal with the fixed verdict.
func newRefusal(reason RefusalReason, primitive, detail string) *Refusal {
	return &Refusal{Reason: reason, Primitive: primitive, Detail: detail, Verdict: VerdictAborted}
}

// ch:trace row=MSF-004 spec=docs/SPEC-PLAN.md (SPEC-03) evidence=internal/rails/refuse.go witness=none:no-live-host-run-in-worktree
