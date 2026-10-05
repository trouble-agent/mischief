package backend_sim

import (
	"fmt"
	"strings"
)

// refusal.go — the AC-17 wiring: any primitive targeting real cloud
// providers refuses to run unless --allow-real AND a resource tag are
// present; on refusal the run LANDS IN THE SIMULATOR instead, and the
// simulator's request log must prove it received the request.
//
// The contract shape (docs/prd/mischief-v0.1.md AC-17): "Given the cloud
// plane, when a real-provider fault is requested without --allow-real and a
// resource tag, then it refuses and runs against the simulator instead —
// proof: command output naming the refusal, plus the simulator's own
// request log showing the fault landed there."
//
// The rails own the L5 vocabulary (ReasonL5Gate); this package owns the
// REDIRECT: the decision (admit-real vs refuse-to-sim) and the sim landing
// that follows a refusal, recorded in the request log with
// detail="ac17-refusal-redirect".

// RealGate is the real-provider admission input (the v0.2 opt-in fields,
// present and CHECKED in v0.1 even though the adapter itself is a stub).
type RealGate struct {
	// AllowReal is the explicit --allow-real opt-in.
	AllowReal bool
	// ResourceTag is the allowlisted provider/resource tag (e.g.
	// "aws: sandbox-lab"); empty refuses.
	ResourceTag string
	// SpendCapUSD is the spend cap; <= 0 refuses.
	SpendCapUSD float64
	// Provider names the cloud the verb targets ("aws", "gcp", ...).
	Provider string
}

// Decision is the closed vocabulary of RealAdapter decision outcomes.
type Decision string

const (
	// DecideReal: the gate admits a real-provider call (all three of
	// AllowReal + ResourceTag + SpendCap>0 present). v0.1 still refuses at
	// the adapter (stub) — the decision names what WOULD run.
	DecideReal Decision = "real"
	// DecideSim: the gate refuses; the fault must land in the simulator.
	DecideSim Decision = "simulator"
	// DecideRefused: the request names a permanent destroy (destroy_class
	// =permanent) — refused outright everywhere in v0.1, simulator
	// included (the companion-faults doctrine).
	DecideRefused Decision = "refused"
)

// Decide applies the AC-17 gate: permanent destroy refuses outright;
// otherwise the full opt-in triad (allow-real AND resource tag AND spend
// cap) admits "real" and ANY missing piece redirects to the simulator. The
// refusal names EVERY missing piece (the checkL5Gate shape).
func (g RealGate) Decide(destroyClass string) (Decision, string) {
	if strings.EqualFold(destroyClass, "permanent") {
		return DecideRefused,
			"destroy_class=permanent refuses outright in v0.1 (the companion-faults doctrine) — simulator included"
	}
	var missing []string
	if !g.AllowReal {
		missing = append(missing, "the explicit --allow-real opt-in")
	}
	if g.ResourceTag == "" {
		missing = append(missing, "an allowlisted resource tag")
	}
	if g.SpendCapUSD <= 0 {
		missing = append(missing, "a spend cap above zero")
	}
	if len(missing) > 0 {
		return DecideSim, "real-provider fault requires " + strings.Join(missing, " and ") +
			" — landing in the simulator instead (AC-17)"
	}
	return DecideReal, ""
}

// RefusalText renders the operator-facing refusal line (the "command output
// naming the refusal" half of AC-17's proof).
func RefusalText(primitive string, d Decision, detail string) string {
	return fmt.Sprintf("backend_sim: %s: %s: %s", primitive, d, detail)
}

// LandInSimOnRefusal is the AC-17 redirect: arm shape on sim (an error
// propagates — a sim that cannot serve is a failure, never a silent
// no-op), issue one real request shape against it, then assert the
// request log PROVES the sim received it (faulted=true, the armed shape).
// The returned proof text names the log path — the evidence the AC names.
func LandInSimOnRefusal(sim *Simulator, primitive string, sh Shape, path string) (proof string, err error) {
	if sim == nil {
		return "", fmt.Errorf("backend_sim: %s: refusal redirect requires a simulator (nil sim is a wiring bug)", primitive)
	}
	if err := sim.Arm(sh); err != nil {
		return "", fmt.Errorf("backend_sim: %s: arm: %w", primitive, err)
	}
	st, _, err := sim.selfCall(path)
	if err != nil {
		return "", fmt.Errorf("backend_sim: %s: sim request: %w", primitive, err)
	}
	entries, err := ReadRequestLog(sim.LogPath())
	if err != nil {
		return "", fmt.Errorf("backend_sim: %s: request log read: %w", primitive, err)
	}
	for _, e := range entries {
		if e.Path == path && e.Faulted && e.Shape == sh.Kind && e.Status == st {
			return fmt.Sprintf("ac17-redirect: %s refused the real plane and landed in the simulator: %s -> %d (%s) — request log %s proves it",
				primitive, path, st, e.Detail, sim.LogPath()), nil
		}
	}
	return "", fmt.Errorf("backend_sim: %s: sim served %s -> %d but the request log at %s carries no faulted %s entry — the landed-proof did not fire (AC-3)",
		primitive, path, st, sim.LogPath(), sh.Kind)
}

// stubBody is what every v0.2 stub returns (a refusal, never a 200).
const stubBody = "real-provider adapter is a documented v0.2 stub: fields are checked, nothing contacts a real cloud"

// RealAdapter is the v0.2 seam: the real-provider verbs behind ONE
// interface. The v0.1 implementation is RealStub — every verb returns
// ErrStubNotImplemented with the gate that WOULD have run, so a caller
// cannot mistake a stub for a real call.
type RealAdapter interface {
	// Apply issues one fault against the real provider named by the gate.
	Apply(primitive string, sh Shape, gate RealGate) error
	// SpentUSD reports the adapter's cumulative spend accounting (0.0 in
	// v0.1: nothing ever ran).
	SpentUSD() float64
}

// ErrStubNotImplemented is THE v0.1 real-adapter outcome. Errors.Is works.
type StubError struct {
	Primitive string
	Provider  string
	Gate      RealGate
	// Decision is the gate decision the stub evaluated (real | sim |
	// refused) — a stub that was never going to run says so.
	Decision Decision
	Detail   string
}

func (e *StubError) Error() string {
	return fmt.Sprintf("backend_sim: %s: v0.2 stub — not implemented (%s: %s) [provider=%q allow_real=%t tag=%q cap=%.2f]: %s",
		e.Primitive, e.Decision, e.Detail, e.Provider, e.Gate.AllowReal, e.Gate.ResourceTag, e.Gate.SpendCapUSD, stubBody)
}

// Unwrap makes every stub refusal match ErrStub (errors.Is works).
func (e *StubError) Unwrap() error { return ErrStub }

// Allowlist is the v0.2 opt-in allowlist (provider/resource tags). The stub
// consults it to REPORT what a real call would have needed; the check
// itself lives in RealGate.Decide.
type Allowlist struct {
	Tags []string
}

// Allows reports whether the gate's resource tag is allowlisted.
func (a Allowlist) Allows(tag string) bool {
	if a.Tags == nil {
		return false
	}
	for _, t := range a.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// RealStub is the v0.1 RealAdapter: opt-in, allowlisted, spend-capped
// fields all present and evaluated; Apply ALWAYS refuses with ErrStub —
// documented, tested, never a silent fall-through.
type RealStub struct {
	// Allowlist is consulted for the report text only.
	Allowlist Allowlist
	// MaxSpendUSD is the spend cap the operator declared (checked, unused:
	// nothing spends).
	MaxSpendUSD float64
	spent       float64
}

// Apply evaluates the gate, then refuses (v0.2 stub). A gate that would
// admit real work is reported as DecideReal — the refusal says the ADAPTER
// is the missing piece, not the gate.
func (s *RealStub) Apply(primitive string, sh Shape, gate RealGate) error {
	d, detail := gate.Decide("")
	if d == DecideRefused {
		return &StubError{Primitive: primitive, Provider: gate.Provider, Gate: gate,
			Decision: d, Detail: detail}
	}
	if d == DecideReal {
		// the triad passed; the STUB is the refusal (the honest wording)
		return &StubError{Primitive: primitive, Provider: gate.Provider, Gate: gate,
			Decision: d,
			Detail: fmt.Sprintf("gate admits real work (tag %q allowlisted=%t, cap %.2f) but the adapter is a v0.2 stub — nothing contacts %s",
				gate.ResourceTag, s.Allowlist.Allows(gate.ResourceTag), s.MaxSpendUSD, gate.Provider)}
	}
	return &StubError{Primitive: primitive, Provider: gate.Provider, Gate: gate,
		Decision: d, Detail: detail}
}

// SpentUSD is 0.0 forever in v0.1 (the spend cap is checked, never spent).
func (s *RealStub) SpentUSD() float64 { return s.spent }

// ErrStub is the sentinel callers match with errors.Is (StubError wraps it).
var ErrStub = fmt.Errorf("real-provider adapter not implemented (v0.2)")
