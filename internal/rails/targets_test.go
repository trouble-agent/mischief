// Tests for target resolution: AC-2 (no default target), AC-7 (protected
// list, refused by default; explicit override recorded), and the structural
// exclusions that refuse unconditionally (PRD §7).
//
// ch:trace row=MSF-004 spec=docs/SPEC-PLAN.md (SPEC-03) test=internal/rails/targets_test.go evidence=internal/rails/targets_test.go witness=none:no-live-host-run-in-worktree
package rails

import (
	"errors"
	"strings"
	"testing"
)

// TestResolveNoTargetTable: AC-2 — no declaration means refusal, whatever
// shape the non-declaration takes; the refusal names its exit and there is
// no default target, ever.
func TestResolveNoTargetTable(t *testing.T) {
	cases := []struct {
		name string
		decl DeclaredTarget
	}{
		{"zero value", DeclaredTarget{}},
		{"kind only", DeclaredTarget{Kind: TargetProcess}},
		{"blank selector", DeclaredTarget{Kind: TargetProcess, Selector: "   "}},
		{"selector only", DeclaredTarget{Selector: "pid:4242"}},
	}
	for _, tc := range cases {
		tgt, err := Resolve(tc.decl, ResolveOptions{})
		if err == nil {
			t.Fatalf("%s: Resolve succeeded with %v, want the no-target refusal", tc.name, tc.decl)
		}
		var re *ResolveError
		if !errors.As(err, &re) {
			t.Fatalf("%s: error is %T, want *ResolveError", tc.name, err)
		}
		if re.Reason != ReasonNoTarget {
			t.Fatalf("%s: Reason=%q, want %q", tc.name, re.Reason, ReasonNoTarget)
		}
		if re.Exit() != 2 {
			t.Fatalf("%s: Exit()=%d, want 2", tc.name, re.Exit())
		}
		if tgt != (Target{}) {
			t.Fatalf("%s: resolved target %v on a refusal, want zero (nothing landed)", tc.name, tgt)
		}
		if !strings.Contains(err.Error(), "no target") || !strings.Contains(err.Error(), "exit 2") {
			t.Fatalf("%s: refusal text %q must name the condition and the exit (CLI quotes it)", tc.name, err.Error())
		}
	}
}

// TestResolveProtectedTable: AC-7 — the protected list is refused by default
// on every kind that can name it, and the refusal names the plane and the
// doctrine reason.
func TestResolveProtectedTable(t *testing.T) {
	cases := []struct {
		selector string
		want     ProtectedKind
	}{
		{"protected:scheduler", ProtectedScheduler},
		{"process:scheduler", ProtectedScheduler},
		{"container:gateway", ProtectedGateway},
		{"host:memory-daemon", ProtectedMemoryDaemon},
		{"protected=memory-daemon", ProtectedMemoryDaemon},
	}
	for _, tc := range cases {
		decl := DeclaredTarget{Kind: TargetProcess, Selector: tc.selector}
		_, err := Resolve(decl, ResolveOptions{})
		if err == nil {
			t.Fatalf("%s: Resolve succeeded, want the protected refusal", tc.selector)
		}
		var re *ResolveError
		if !errors.As(err, &re) {
			t.Fatalf("%s: error is %T, want *ResolveError", tc.selector, err)
		}
		if re.Reason != ReasonProtected {
			t.Fatalf("%s: Reason=%q, want %q", tc.selector, re.Reason, ReasonProtected)
		}
		if re.Protected != tc.want {
			t.Fatalf("%s: named %q, want %q", tc.selector, re.Protected, tc.want)
		}
		if !strings.Contains(err.Error(), tc.want.String()) || !strings.Contains(err.Error(), protectedReasons) {
			t.Fatalf("%s: refusal text %q must name the plane and the doctrine reason", tc.selector, err.Error())
		}
	}
}

// TestResolveProtectedAllowedRecords: AC-7's override path — the explicit
// operator flag resolves the SAME protected target, and the resolution says
// so (Protected set, ProtectedAllowed=true — what the journal records).
// This asserts the resolution REPORTS the override, never that the run was
// permitted silently: without the flag the identical declaration refuses
// (covered by TestResolveProtectedTable).
func TestResolveProtectedAllowedRecords(t *testing.T) {
	decl := DeclaredTarget{Kind: TargetProcess, Selector: "container:gateway"}
	tgt, err := Resolve(decl, ResolveOptions{AllowProtected: true})
	if err != nil {
		t.Fatalf("Resolve with AllowProtected: %v", err)
	}
	if !tgt.IsProtected() {
		t.Fatal("resolved target does not report IsProtected; the override must stay visible")
	}
	if tgt.Protected != ProtectedGateway {
		t.Fatalf("Protected=%q, want %q", tgt.Protected, ProtectedGateway)
	}
	if !tgt.ProtectedAllowed {
		t.Fatal("ProtectedAllowed=false on the explicit override; the journal would have nothing to record")
	}
}

// TestResolveProtectedNearMiss: the protected match is on selector
// components, never inside a longer word — "gateway-test" is not the
// gateway.
func TestResolveProtectedNearMiss(t *testing.T) {
	tgt, err := Resolve(DeclaredTarget{Kind: TargetProcess, Selector: "process:gateway-test"}, ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve(gateway-test): %v (a component match must not eat longer words)", err)
	}
	if tgt.IsProtected() {
		t.Fatalf("gateway-test resolved as protected (%q); near-miss leaked", tgt.Protected)
	}
}

// TestResolveStructuralExclusionTable: PID 1, kernel threads and mischief's
// own tree refuse unconditionally — no override exists (PRD §7 "never, by
// construction"). AllowProtected is set in every case to prove the
// structural rail does not honor it.
func TestResolveStructuralExclusionTable(t *testing.T) {
	opts := ResolveOptions{AllowProtected: true, SelfPID: 424242}
	cases := []struct {
		decl DeclaredTarget
		want string
	}{
		{DeclaredTarget{Kind: TargetProcess, Selector: "pid:1"}, "pid 1"},
		{DeclaredTarget{Kind: TargetProcess, Selector: "1"}, "pid 1"},
		{DeclaredTarget{Kind: TargetProcess, Selector: "pid:424242"}, "own process tree"},
		{DeclaredTarget{Kind: TargetProcess, Selector: "kernel"}, "kernel thread"},
		{DeclaredTarget{Kind: TargetProcess, Selector: "[kworker/0:1]"}, "kernel thread"},
	}
	for _, tc := range cases {
		tgt, err := Resolve(tc.decl, opts)
		if err == nil {
			t.Fatalf("%q: Resolve succeeded, want the structural refusal", tc.decl.Selector)
		}
		var re *ResolveError
		if !errors.As(err, &re) {
			t.Fatalf("%q: error is %T, want *ResolveError", tc.decl.Selector, err)
		}
		if re.Reason != ReasonExcluded {
			t.Fatalf("%q: Reason=%q, want %q", tc.decl.Selector, re.Reason, ReasonExcluded)
		}
		if !strings.Contains(re.Excluded, tc.want) {
			t.Fatalf("%q: Excluded=%q, want it to name %q", tc.decl.Selector, re.Excluded, tc.want)
		}
		if !strings.Contains(err.Error(), "unconditionally") {
			t.Fatalf("%q: refusal text %q must say the exclusion is unconditional", tc.decl.Selector, err.Error())
		}
		if tgt != (Target{}) {
			t.Fatalf("%q: resolved %v on a refusal, want zero", tc.decl.Selector, tgt)
		}
	}
}

// TestResolveOrdinaryTargetPasses: an ordinary pid resolves clean — the
// rails refuse the named shapes, not everything.
func TestResolveOrdinaryTargetPasses(t *testing.T) {
	tgt, err := Resolve(DeclaredTarget{Kind: TargetProcess, Selector: "pid:4242"}, ResolveOptions{SelfPID: 424242})
	if err != nil {
		t.Fatalf("Resolve(pid:4242): %v", err)
	}
	if tgt.IsProtected() || tgt.PID != 0 {
		t.Fatalf("ordinary target resolved with Protected=%q PID=%d; want clean", tgt.Protected, tgt.PID)
	}
}
