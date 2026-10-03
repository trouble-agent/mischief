package rails

import (
	"fmt"
	"strings"

	"github.com/trouble-agent/mischief/internal/catalog"
)

// Scope is the resolved scope a run asks to execute at: the ladder level
// (L0..L5) plus how it was declared.
type Scope struct {
	// Level is the ladder level, 0..5. L0 is the scratch harness mischief
	// owns (everything is always allowed there — where selftest lives); L5
	// is the real cloud provider plane.
	Level int
	// Scratch is true when the target is mischief-owned scratch (the L0
	// harness); a declared scratch scope refuses non-scratch blast (PRD
	// §6.2: scratch: true ⇒ refuse non-scratch blast).
	Scratch bool
	// AllowReal is the explicit L5 opt-in ("--allow-real"; PRD §7 ladder:
	// L5 = opt-in --allow-real + allowlist + spend cap).
	AllowReal bool
	// Allowlisted names the provider/resource tag cleared for real-provider
	// work; empty unless AllowReal.
	Allowlisted string
	// SpendCapUSD is the real-provider spend cap; 0 unless AllowReal.
	SpendCapUSD float64
}

// tierMin returns the primitive's minimum ladder level (catalog Tier.Min;
// the ladder itself is owned by the catalog schema, not redefined here).
func tierMin(d *catalog.Descriptor) int {
	if d == nil || d.Tier == nil {
		return -1
	}
	return d.Tier.Min
}

// CheckScope enforces the ladder for one primitive at one scope:
//
//   - the scope's level must be at or above the primitive's declared
//     minimum (ReasonScope);
//   - AC-19: a primitive whose selftest is not green on this host refuses
//     outside an L0 scratch scope (ReasonSelftest) — the refusal names the
//     selftest state;
//   - L5 needs the full real-provider gate: explicit opt-in AND an
//     allowlist entry AND a spend cap above zero (ReasonL5Gate).
//
// d may be nil-checked by callers, but a nil descriptor is refused: an
// unnamed primitive has no declared tier to run under.
func CheckScope(d *catalog.Descriptor, s Scope) error {
	if d == nil {
		return newRefusal(ReasonScope, "", "primitive unnamed: no declared tier to check the scope against")
	}
	if s.Level < 0 || s.Level > 5 {
		return newRefusal(ReasonScope, d.ID, fmt.Sprintf("scope level %d outside the L0..L5 ladder", s.Level))
	}
	if min := tierMin(d); s.Level < min {
		return newRefusal(ReasonScope, d.ID, fmt.Sprintf("scope L%d is below the primitive's declared minimum tier L%d (%s)", s.Level, min, d.Tier.Raw))
	}
	// AC-19: the catalog's own refusal predicate, applied at the ladder —
	// non-green selftest refuses outside an L0 scratch scope, and the
	// refusal names the state.
	if s.Level > 0 && d.Selftest.RefusesOutsideL0() {
		return newRefusal(ReasonSelftest, d.ID, fmt.Sprintf("selftest is %q on this host (green required outside L0 scratch)", string(d.Selftest)))
	}
	// L0 scratch always allowed (PRD §7: "everything, always allowed —
	// where selftest lives").
	if s.Level == 5 {
		return checkL5Gate(d, s)
	}
	return nil
}

// checkL5Gate enforces the real-provider plane: opt-in + allowlist + spend
// cap, all three. The refusal names which of the three is missing (AC-17
// shape: the output names the refusal).
func checkL5Gate(d *catalog.Descriptor, s Scope) error {
	var missing []string
	if !s.AllowReal {
		missing = append(missing, "the explicit --allow-real opt-in")
	}
	if s.Allowlisted == "" {
		missing = append(missing, "an allowlisted provider/resource tag")
	}
	if s.SpendCapUSD <= 0 {
		missing = append(missing, "a spend cap above zero")
	}
	if len(missing) > 0 {
		return newRefusal(ReasonL5Gate, d.ID, "real-provider fault (L5) requires "+strings.Join(missing, " and "))
	}
	return nil
}

// ch:trace row=MSF-004 spec=docs/SPEC-PLAN.md (SPEC-03) evidence=internal/rails/scope.go witness=none:no-live-host-run-in-worktree
