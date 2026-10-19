// Tests for the scope ladder (CheckScope): tier minimums, the AC-19 selftest
// refusal, the L5 real-provider gate, and PlanFrom's arm-nothing-on-refusal
// seam. Descriptor fixtures are built in-test (the catalog schema is data);
// no host probing happens here.
//
// ch:trace row=MSF-004 spec=docs/SPEC-PLAN.md (SPEC-03) test=internal/rails/scope_test.go evidence=internal/rails/scope_test.go witness=none:no-live-host-run-in-worktree
package rails

import (
	"strconv"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/catalog"
)

// desc returns a minimal valid descriptor for the tests' purposes: the
// fields CheckScope reads (ID, Tier.Min, Selftest) plus enough of the
// contract shape to survive future tightening.
func desc(id string, tierMin int, selftest catalog.SelftestState) *catalog.Descriptor {
	return &catalog.Descriptor{
		ID:       id,
		Tier:     &catalog.Tier{Raw: tierLabel(tierMin), Min: tierMin},
		Selftest: selftest,
	}
}

func tierLabel(min int) string {
	return "L" + string(rune('0'+min)) + " (test fixture)"
}

// TestCheckScopeBelowTierMin: a primitive below its declared minimum tier is
// refused (ReasonScope), and the refusal names both levels and the raw tier.
func TestCheckScopeBelowTierMin(t *testing.T) {
	d := desc("P-001", 2, catalog.SelftestGreen)
	err := CheckScope(d, Scope{Level: 1})
	if err == nil {
		t.Fatal("CheckScope(L1) on a Tier.Min=2 primitive succeeded, want the scope refusal")
	}
	ref, ok := err.(*Refusal)
	if !ok {
		t.Fatalf("error is %T, want *Refusal", err)
	}
	if ref.Reason != ReasonScope {
		t.Fatalf("Reason=%q, want %q", ref.Reason, ReasonScope)
	}
	if ref.Primitive != "P-001" {
		t.Fatalf("Primitive=%q, want P-001 (the typed refusal names the primitive)", ref.Primitive)
	}
	for _, want := range []string{"L1", "L2"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal text %q must name %q", err.Error(), want)
		}
	}
	if ref.Exit() != ExitCode {
		t.Fatalf("Exit()=%d, want %d", ref.Exit(), ExitCode)
	}
}

// TestCheckScopeAtOrAboveMinPasses: at and above the minimum the ladder
// allows (with green selftest so AC-19 stays out of the way).
func TestCheckScopeAtOrAboveMinPasses(t *testing.T) {
	d := desc("P-001", 2, catalog.SelftestGreen)
	for _, level := range []int{2, 3, 4} {
		if err := CheckScope(d, Scope{Level: level}); err != nil {
			t.Fatalf("CheckScope(L%d): %v (at/above Tier.Min must pass)", level, err)
		}
	}
}

// TestCheckScopeSelftestOutsideL0: AC-19 — every non-green selftest state
// refuses above L0 and names the state; L0 scratch always allows (where
// selftest lives); green passes anywhere on the ladder.
func TestCheckScopeSelftestOutsideL0(t *testing.T) {
	states := []catalog.SelftestState{
		catalog.SelftestMissing,
		catalog.SelftestFailing,
		catalog.SelftestUnknown,
	}
	for _, st := range states {
		d := desc("P-002", 0, st)
		err := CheckScope(d, Scope{Level: 1})
		if err == nil {
			t.Fatalf("selftest %q at L1 succeeded, want the AC-19 refusal", st)
		}
		ref := err.(*Refusal)
		if ref.Reason != ReasonSelftest {
			t.Fatalf("selftest %q: Reason=%q, want %q", st, ref.Reason, ReasonSelftest)
		}
		if !strings.Contains(err.Error(), string(st)) {
			t.Fatalf("refusal %q must name the selftest state %q (AC-19 names the state)", err.Error(), st)
		}
		// the same state inside L0 scratch is allowed: that is where selftest runs
		if err := CheckScope(d, Scope{Level: 0, Scratch: true}); err != nil {
			t.Fatalf("selftest %q at L0: %v (L0 always allows — where selftest lives)", st, err)
		}
	}
	d := desc("P-002", 0, catalog.SelftestGreen)
	if err := CheckScope(d, Scope{Level: 3}); err != nil {
		t.Fatalf("green selftest at L3: %v (green runs anywhere on the ladder)", err)
	}
}

// TestCheckScopeNilDescriptor: an unnamed primitive has no declared tier —
// refused, not passed.
func TestCheckScopeNilDescriptor(t *testing.T) {
	err := CheckScope(nil, Scope{Level: 0})
	if err == nil {
		t.Fatal("CheckScope(nil) succeeded, want the scope refusal")
	}
	if err.(*Refusal).Reason != ReasonScope {
		t.Fatalf("Reason=%q, want %q", err.(*Refusal).Reason, ReasonScope)
	}
}

// TestCheckScopeLadderBounds: levels outside L0..L5 refuse.
func TestCheckScopeLadderBounds(t *testing.T) {
	d := desc("P-001", 0, catalog.SelftestGreen)
	for _, level := range []int{-1, 6, 99} {
		err := CheckScope(d, Scope{Level: level})
		if err == nil {
			t.Fatalf("CheckScope(L%d) succeeded, want the ladder-bounds refusal", level)
		}
		if err.(*Refusal).Reason != ReasonScope {
			t.Fatalf("L%d: Reason=%q, want %q", level, err.(*Refusal).Reason, ReasonScope)
		}
	}
}

// TestCheckScopeL5Gate: the real-provider plane needs all three of the
// explicit opt-in, an allowlist entry and a spend cap above zero; the
// refusal names exactly which are missing (AC-17 shape); all three present
// passes.
func TestCheckScopeL5Gate(t *testing.T) {
	d := desc("P-003", 0, catalog.SelftestGreen)

	err := CheckScope(d, Scope{Level: 5})
	if err == nil {
		t.Fatal("bare L5 succeeded, want the L5-gate refusal")
	}
	ref := err.(*Refusal)
	if ref.Reason != ReasonL5Gate {
		t.Fatalf("Reason=%q, want %q", ref.Reason, ReasonL5Gate)
	}
	for _, want := range []string{"--allow-real", "allowlisted", "spend cap"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q must name %q", err.Error(), want)
		}
	}

	// two of three: the missing one is named, the present two are not
	err = CheckScope(d, Scope{Level: 5, AllowReal: true, SpendCapUSD: 5})
	if err == nil {
		t.Fatal("L5 with opt-in+cap but no allowlist succeeded")
	}
	if !strings.Contains(err.Error(), "allowlisted") {
		t.Fatalf("refusal %q must name the missing allowlist", err.Error())
	}
	if strings.Contains(err.Error(), "--allow-real") {
		t.Fatalf("refusal %q names --allow-real though it was given", err.Error())
	}

	// all three: passes (the allowlist leg uses the documented tag —
	// an unlisted value refuses with the tag named, see the L5
	// membership cases below)
	ok := Scope{Level: 5, AllowReal: true, Allowlisted: "aws:sandbox-lab", SpendCapUSD: 5}
	if err := CheckScope(d, ok); err != nil {
		t.Fatalf("L5 with the full gate: %v", err)
	}

	// zero spend cap is not a cap
	err = CheckScope(d, Scope{Level: 5, AllowReal: true, Allowlisted: "aws:sandbox-lab"})
	if err == nil || !strings.Contains(err.Error(), "spend cap") {
		t.Fatalf("L5 without a spend cap: err=%v, want the spend-cap naming", err)
	}
}

// TestCheckScopeL5GateAllowlistMembership: the L5 allowlist leg applies
// the ONE authoritative membership source (internal/cloudallow) — the
// same rule the simulator's AC-17 gate applies. Before MISCHIEF-DF-47
// any non-empty tag (aws:prod-db, "*", whitespace) was admitted; now
// every unlisted value refuses with the tag named, and only the
// documented sandbox tag passes. Proven RED against the old check by
// these exact tags passing the old non-empty gate.
func TestCheckScopeL5GateAllowlistMembership(t *testing.T) {
	d := desc("P-004", 0, catalog.SelftestGreen)
	refused := []string{"aws:prod-db", "prod-fleet-main-host", "*", "  ", "	", " aws:sandbox-lab"}
	for _, tag := range refused {
		err := CheckScope(d, Scope{Level: 5, AllowReal: true, Allowlisted: tag, SpendCapUSD: 5})
		if err == nil {
			t.Fatalf("L5 with unlisted tag %q succeeded, want the allowlist refusal (fail-closed)", tag)
		}
		ref := err.(*Refusal)
		if ref.Reason != ReasonL5Gate {
			t.Fatalf("tag %q: Reason=%q, want %q", tag, ref.Reason, ReasonL5Gate)
		}
		if !strings.Contains(err.Error(), "allowlist") {
			t.Fatalf("refusal for tag %q must name the allowlist: %v", tag, err)
		}
		// the refusal names the offending tag (quoted, so blank/
		// whitespace values are visible)
		if !strings.Contains(err.Error(), strconv.Quote(tag)) {
			t.Fatalf("refusal for tag %q must name the offending tag: %v", tag, err)
		}
		// the present legs (opt-in, cap) are NOT named
		if strings.Contains(err.Error(), "--allow-real") || strings.Contains(err.Error(), "spend cap above zero") {
			t.Fatalf("refusal for tag %q names a satisfied leg: %v", tag, err)
		}
	}
	// the documented tag passes the same gate
	if err := CheckScope(d, Scope{Level: 5, AllowReal: true, Allowlisted: "aws:sandbox-lab", SpendCapUSD: 5}); err != nil {
		t.Fatalf("documented sandbox tag must pass the L5 gate: %v", err)
	}
}

// TestPlanFromArmsNothingOnRefusal: the catalog→run seam — one failing
// primitive and the whole plan refuses, arming nothing (an experiment that
// fails a rail arms nothing).
func TestPlanFromArmsNothingOnRefusal(t *testing.T) {
	cat := &catalog.Catalog{Primitives: map[string]*catalog.Descriptor{
		"P-001": desc("P-001", 2, catalog.SelftestGreen),   // needs L2
		"P-002": desc("P-002", 0, catalog.SelftestFailing), // refuses outside L0
	}}
	declared := []ArmedFault{{Primitive: "P-001"}, {Primitive: "P-002"}}
	set, err := PlanFrom(cat, declared, Scope{Level: 3})
	if err == nil {
		t.Fatal("PlanFrom with one AC-19 primitive succeeded, want the selftest refusal")
	}
	if err.(*Refusal).Reason != ReasonSelftest {
		t.Fatalf("Reason=%q, want %q", err.(*Refusal).Reason, ReasonSelftest)
	}
	if set != nil {
		t.Fatalf("PlanFrom returned an armed set %v on a refusal; nothing may be armed", set.IDs())
	}

	// unknown primitive: refused by name (the typed field carries the id;
	// Error() renders reason+detail)
	set, err = PlanFrom(cat, []ArmedFault{{Primitive: "X-999"}}, Scope{Level: 0})
	if err == nil {
		t.Fatal("PlanFrom(unknown X-999) succeeded, want the refusal")
	}
	if ref, ok := err.(*Refusal); !ok || ref.Primitive != "X-999" {
		t.Fatalf("err=%v (%T), want a *Refusal whose Primitive field names X-999", err, err)
	}

	// all-green plan at a compliant scope: armed, per-run
	set, err = PlanFrom(cat, []ArmedFault{{Primitive: "P-001", Params: map[string]string{"ms": "50"}}}, Scope{Level: 3})
	if err != nil {
		t.Fatalf("PlanFrom compliant: %v", err)
	}
	if got, want := set.IDs(), []string{"P-001"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("IDs=%v, want %v", got, want)
	}
}
