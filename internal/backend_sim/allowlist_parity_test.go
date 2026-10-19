package backend_sim_test

import (
	"testing"

	"github.com/trouble-agent/mischief/internal/backend_sim"
	"github.com/trouble-agent/mischief/internal/catalog"
	"github.com/trouble-agent/mischief/internal/cloudallow"
	"github.com/trouble-agent/mischief/internal/rails"
)

// allowlist_parity_test.go — MISCHIEF-DF-47's "one authoritative source"
// contract: the simulator's AC-17 gate (backend_sim.RealGate.Decide) and
// the rails L5 gate (rails.CheckScope at L5) must AGREE on membership for
// every value in the probe set — a tag the sim refuses, the ladder must
// refuse, and vice versa. Both consult internal/cloudallow; this test
// fails if either gate ever re-grows a private membership rule.

// l5Probe exercises the rails L5 gate directly (the primitives' tier
// minimums and selftests are green in the fixture, so only the L5 triad
// is under test).
func l5Probe(t *testing.T, tag string) error {
	t.Helper()
	d := &catalog.Descriptor{
		ID:       "P-PARITY",
		Tier:     &catalog.Tier{Raw: "L0 (parity fixture)", Min: 0},
		Selftest: catalog.SelftestGreen,
	}
	return rails.CheckScope(d, rails.Scope{Level: 5, AllowReal: true, Allowlisted: tag, SpendCapUSD: 5})
}

// TestSimulatorAndRailsGatesAgreeOnAllowlist: the parity sweep.
func TestSimulatorAndRailsGatesAgreeOnAllowlist(t *testing.T) {
	probe := []string{
		"aws:sandbox-lab", // the one allowlisted tag — both ADMIT
		"aws:prod-db",
		"prod-fleet-main-host",
		"*",
		"",
		"   ",
		"\t",
		"aws:sandbox", // the old fixture tag: unlisted, must refuse
		"sim:hel1",    // the old rails fixture tag: unlisted, must refuse
		" aws:sandbox-lab",
		"AWS:SANDBOX-LAB",
		"aws:sandbox-lab*",
	}
	for _, tag := range probe {
		wantAllow := cloudallow.Allows(tag)

		simD, simDetail := backend_sim.RealGate{
			AllowReal: true, ResourceTag: tag, SpendCapUSD: 5, Provider: "aws",
		}.Decide("reversible")
		simAllow := simD == backend_sim.DecideReal

		railsErr := l5Probe(t, tag)
		railsAllow := railsErr == nil

		if simAllow != wantAllow || railsAllow != wantAllow {
			t.Fatalf("gate disagreement on tag %q: cloudallow=%t sim=%s (%q) rails=%t (err=%v)",
				tag, wantAllow, simD, simDetail, railsAllow, railsErr)
		}
	}
}
