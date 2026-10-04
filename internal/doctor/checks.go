package doctor

import (
	"github.com/trouble-agent/mischief/internal/rails"
	"github.com/trouble-agent/mischief/internal/sanction"
)

// LoadThreshold / PSIThreshold are the doctor's load-gate arm thresholds —
// the same numbers a mischief run's gate uses, so doctor reports the gate
// the run would actually meet (or refuse at).
const (
	LoadThreshold = 12.0
	PSIThreshold  = 50.0
)

// SanctionCheck is the SPEC-13 marker gate seam: package-level so tests can
// pin the pass and fail arms without touching the host's real sanction
// state. Production = sanction.Check's zero-value Options (live hostname,
// live env, default marker path).
var SanctionCheck = func() error { return sanction.Check(sanction.Options{}) }

// ReadLoad is the live /proc read seam: package-level so tests can point it
// at a stub instead of the real box's numbers (the ambient-filesystem
// discipline: a test never depends on, or perturbs, the live host's load).
var ReadLoad = rails.ReadLoad

// CheckLoad gates one reading against the doctor's thresholds. Package-level
// for the same seam reason.
var CheckLoad = func(r rails.LoadReading) error {
	gate, err := rails.NewLoadGate(rails.LoadGateConfig{LoadThreshold: LoadThreshold, PSIThreshold: PSIThreshold})
	if err != nil {
		return err
	}
	return gate.Check(r)
}
