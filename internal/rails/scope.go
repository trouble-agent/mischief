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

// The sanctioned environment, per ladder level (SPEC-13 / MSF-020): a
// tier's primitives are provable only where their sanctioned environment
// exists, and a primitive that cannot be proven on a sanctioned host does
// not ship (PRD §7). The table is doctrine — CheckScope enforces the tier
// minimum and the selftest state; WHERE a sanctioned host of each shape
// comes from is the isolation contract's business:
//
//	L0  scratch           — unconditionally sanctioned: the scratch harness
//	                        is mischief-owned by construction (selftest
//	                        lives here); no marker logic applies to it.
//	L1  rootless bunker   — the ephemeral sanctioned host (a bunker agent:
//	                        uid != 0, no passwordless sudo); carries the
//	                        sanction marker. v0.1 workhorse: signals,
//	                        prlimit, LD_PRELOAD shim, seccomp, userspace
//	                        proxy, scratch fs.
//	L2/L3 host & node     — require a sanctioned NON-fleet box or container
//	                        with NET_ADMIN (netns, dm/loop, freezer,
//	                        fsfreeze, docker API): the fleet main host is
//	                        structurally out — it is the protected target,
//	                        not a lab.
//	L4  provider simulator— the simulator process runs on the sanctioned
//	                        host (L1-shaped) and stands in for the cloud
//	                        plane; no real provider contact.
//	L5  real cloud        — opt-in ONLY (--allow-real + allowlist + spend
//	                        cap, checked above), on an operator-sanctioned
//	                        environment; v0.1 refuses permanent destroy
//	                        verbs regardless.
//
// L2/L3-only consequence: the unprivileged bunker (measured: `unshare -rn`
// fails at the uid_map write) cannot exercise NET_ADMIN or docker-API
// primitives, so those are PROVISIONALLY L0/L1-only-in-catalogue (their
// live proof ran once, by hand, on the box that had the capability) until a
// fleeting sanctioned host with NET_ADMIN exists in the fleet.
//
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
