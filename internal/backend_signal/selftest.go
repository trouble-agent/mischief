package backend_signal

import (
	"fmt"
	"sort"
)

// SelftestState mirrors the catalog's declared selftest vocabulary
// (internal/catalog SelftestState) without importing the package: the
// AC-19 predicate is the schema's, restated here at the boundary it is
// enforced at. A descriptor's own value comes from the catalog; this
// package's Selftest* functions feed it.
type SelftestState string

const (
	// SelftestGreen: the selftest ran green on THIS host.
	SelftestGreen SelftestState = "green"
	// SelftestMissing: no selftest exists for the primitive.
	SelftestMissing SelftestState = "missing"
	// SelftestFailing: the selftest exists and is red on this host.
	SelftestFailing SelftestState = "failing"
	// SelftestUnknown: the selftest has not been run here.
	SelftestUnknown SelftestState = "unknown"
)

// RefusesOutsideL0 is the AC-19 predicate: everything except green refuses.
func (s SelftestState) RefusesOutsideL0() bool { return s != SelftestGreen }

// L0 is the scratch scope level.
const L0 = 0

// SelftestFn is one primitive's selftest: land on a scratch target, prove
// landed, revert, prove reverted. A selftest that cannot do all four on
// this host leaves the primitive un-selftested (refusing outside L0).
type SelftestFn func() error

// Inverter is the exported face of an armed fault's recorded inverse: the
// package's fault handles expose Resume() (the SIGCONT inverse of a
// SIGSTOP landing, with its own measured proof). External callers hold
// the interface, never the unexported fault type.
type Inverter interface {
	Resume() Outcome
}

// selftests is the registry of this package's selftests, keyed by
// primitive id (the catalog's fault ids).
var selftests = map[string]SelftestFn{
	"P-001": SelftestSigStop,
	"P-003": SelftestSigKill,
	"S-001": SelftestShim,
	"S-008": SelftestPrlimit,
}

// SelftestNames returns the registered selftest ids, sorted.
func SelftestNames() []string {
	out := make([]string, 0, len(selftests))
	for id := range selftests {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Selftest runs the named primitive's selftest and reports the resulting
// SelftestState (green, or failing with the reason). An unregistered
// primitive is SelftestMissing by definition.
func Selftest(primitive string) (SelftestState, error) {
	fn, ok := selftests[primitive]
	if !ok {
		return SelftestMissing, nil
	}
	if err := fn(); err != nil {
		return SelftestFailing, err
	}
	return SelftestGreen, nil
}

// Gate is the AC-19 admission gate a run consults before applying a fault
// outside an L0 scratch scope. L0 (the scratch harness where selftest
// lives) is admitted UNCONDITIONALLY — "everything, always allowed"; above
// L0 the primitive's selftest must be green on this host.
func Gate(primitive string, scopeLevel int) error {
	if scopeLevel <= L0 {
		// L0 scratch: everything is always allowed — where selftest lives.
		return nil
	}
	st, err := Selftest(primitive)
	if err != nil {
		return fmt.Errorf("selftest for %s is FAILING on this host and the scope is L%d (outside L0): %v (AC-19: an un-selftested primitive refuses outside L0)",
			primitive, scopeLevel, err)
	}
	if st.RefusesOutsideL0() {
		return fmt.Errorf("selftest for %s is %s on this host; refusing to run at scope L%d (outside L0) (AC-19: an un-selftested primitive refuses outside L0)",
			primitive, st, scopeLevel)
	}
	return nil
}
