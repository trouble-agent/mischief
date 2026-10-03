// The SPEC-07 landed-proof record and the primitive contract it rides on.
//
// AC-3 (anti-gaming): a primitive whose landed-proof never fires grades
// no_op and the run FAILS — Proof.ReadbackOk is the field a caller checks;
// a false/zero Proof is the no_op shape. AC-13: the proof never rests on
// the fault's own exit code — every Evidence entry is a host-side measured
// read (systemd properties, sysfs files, cgroupfs files, mountinfo).
//
// The evidence contract ("published in the catalog sense", AC-3/AC-19):
// every landed-proof names its mechanism (Kind), the concrete probe that
// fired (Property), and the measured read-back lines (Evidence). The
// catalog descriptors carry the same contract as prose; this type is its
// executable shape.
package backend_resource

import (
	"fmt"
	"strings"
)

// Proof is the landed-proof record of one landing. Zero-value Proof is the
// AC-3 no_op shape: Landed=false means nothing was proven landed.
type Proof struct {
	// Primitive is the primitive name that produced the proof.
	Primitive string
	// Landed reports whether the landed-proof's read-back FIRED (AC-3:
	// no fired proof ⇒ no_op, run fails).
	Landed bool
	// Verdict is the SPEC-07 verdict value ("landed" / "no_op").
	Verdict string
	// Evidence is the measured read-back lines (host state, one per
	// entry: systemd properties, cgroupfs/sysfs contents, mount state).
	Evidence []string
	// Property names the proof mechanism and the concrete probe
	// (catalog landed_proof.check's executable twin).
	Property string
	// ReadbackOk reports whether the read-back itself completed
	// (a landing whose verification read failed is NOT a landed proof).
	ReadbackOk bool
}

// NoOp renders the AC-3 no_op grade for this primitive: the run FAILS.
func (p Proof) NoOp() Proof {
	return Proof{Primitive: p.Primitive, Landed: false, Verdict: VerdictNoOp,
		Property: p.Property, ReadbackOk: false}
}

// String renders the proof as the journal-ready one-liner plus evidence
// lines (the landed-proof shape published in the catalog sense).
func (p Proof) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: verdict=%s landed=%t readback_ok=%t\n", p.Primitive, p.Verdict, p.Landed, p.ReadbackOk)
	if p.Property != "" {
		fmt.Fprintf(&b, "  property: %s\n", p.Property)
	}
	for _, e := range p.Evidence {
		fmt.Fprintf(&b, "  evidence: %s\n", e)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// Release is the reverter half: undo the landing and PROVE the undo (the
// returned string is the revert proof; an error means the revert did not
// verify). AC-4's shape (measured revert, not asserted) at the primitive
// layer.
type Release func() (string, error)

// SelftestReport is one primitive's selftest outcome on this host (AC-19).
type SelftestReport struct {
	// Primitive is the primitive name.
	Primitive string
	// Green reports a green selftest on this host.
	Green bool
	// Detail carries the measured summary of what was proven (or what
	// the honesty check verified).
	Detail string
	// Skipped records a clean skip (capability absent on this host) — a
	// skip is recorded, never a silent pass.
	Skipped string
	// Fail carries the failure reason when Green=false and Skipped="".
	Fail string
}

// Status renders the report's AC-19 state: green | skipped | failing.
func (r SelftestReport) Status() string {
	switch {
	case r.Green:
		return "green"
	case r.Skipped != "":
		return "skipped"
	default:
		return "failing"
	}
}

// primitive is the SPEC-07 primitive contract: name, live capability,
// landing (with landed-proof + reverter) and selftest. The L2 primitives
// take their fixture evidence through their Probe implementations (the
// host seam), which is the privileged paths' dependency-injection point.
type primitive interface {
	// Name is the stable primitive name (catalog-sense publishable).
	Name() string
	// Probe answers the live capability question with measured basis.
	Probe() (CapabilityResult, error)
	// Inject lands the fault on a scratch target and returns the
	// landed-proof plus the reverter. A capability gap returns an
	// ErrCapabilityUnavailable refusal (AC-11) and lands nothing.
	Inject() (Proof, Release, error)
	// Selftest runs the primitive's L0 selftest on this host.
	Selftest() SelftestReport
}

// errText renders an error's message ("" for nil).
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// truncateLine reduces multi-line command output to its first non-empty
// line, capped, for refusal/basis text.
func truncateLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// scratchToken returns a short unique token for transient scratch names
// (units, cgroup names): pid + monotonic-ish nanos, no randomness needed
// (uniqueness is per-host-bus, and the bus is this session's).
func scratchToken() string {
	return fmt.Sprintf("%d-%d", pidOfProcess(), nanosOfNow())
}

// appendUnitEvidence assembles the systemd-run landed-proof's evidence
// lines: the start banner line plus the property read-back.
func appendUnitEvidence(startCmd, startOut string, props []string) []string {
	ev := []string{startCmd + " -> " + startOut}
	return append(ev, props...)
}
