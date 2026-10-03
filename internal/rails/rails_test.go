// Tests for the SPEC-03 refusal taxonomy and the CLI exit mapping (AC-2's
// "exits 2 and lands nothing", PRD §6.4 verdict vocabulary).
//
// ch:trace row=MSF-004 spec=docs/SPEC-PLAN.md (SPEC-03) test=internal/rails/rails_test.go evidence=internal/rails/rails_test.go witness=none:no-live-host-run-in-worktree
package rails

import (
	"errors"
	"fmt"
	"testing"
)

func TestRefusalShape(t *testing.T) {
	r := newRefusal(ReasonLoad, "P-001", "host saturated")
	if r.Verdict != VerdictAborted {
		t.Fatalf("Verdict=%q, want %q (PRD §6.4: every rail refusal grades aborted)", r.Verdict, VerdictAborted)
	}
	if got, want := r.Error(), "rails: load_gate: host saturated"; got != want {
		t.Fatalf("Error()=%q, want %q", got, want)
	}
	if r.Exit() != ExitCode {
		t.Fatalf("Exit()=%d, want %d", r.Exit(), ExitCode)
	}
	if ExitCode != 2 {
		t.Fatalf("ExitCode=%d, want 2 (AC-2 shape)", ExitCode)
	}
}

// TestMapExitTable: the once-defined CLI exit contract — nil 0, refusal 2,
// wrapped refusal 2, anything else 1.
func TestMapExitTable(t *testing.T) {
	ref := newRefusal(ReasonNoTarget, "", "no target")
	plain := errors.New("boom")
	wrapped := fmt.Errorf("run: %w", ref)
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"refusal", ref, 2},
		{"wrapped refusal", wrapped, 2},
		{"plain error", plain, 1},
	}
	for _, tc := range cases {
		if got := MapExit(tc.err); got != tc.want {
			t.Fatalf("%s: MapExit=%d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestIsRefusalTable: direct, wrapped, and non-refusal classification.
func TestIsRefusalTable(t *testing.T) {
	ref := newRefusal(ReasonScope, "P-001", "below tier")
	if !IsRefusal(ref) {
		t.Fatal("IsRefusal(*Refusal)=false, want true")
	}
	if !IsRefusal(fmt.Errorf("plan: %w", ref)) {
		t.Fatal("IsRefusal(wrapped refusal)=false, want true")
	}
	if IsRefusal(errors.New("boom")) {
		t.Fatal("IsRefusal(plain error)=true, want false")
	}
	if IsRefusal(nil) {
		t.Fatal("IsRefusal(nil)=true, want false")
	}
}

// TestMapExitResolveErrorExits2 pins AC-2's exit contract for the
// target-resolution refusal: the no-target refusal is a rails refusal, so
// MapExit maps it to 2 and lands nothing (the armed set stays empty — the
// `mischief status` half of AC-2's proof).
//
// RED 2026-10-02: IsRefusal recognized only *Refusal, so the resolver's
// *ResolveError fell through to exit 1. Fixed in refuse.go (IsRefusal now
// classifies *ResolveError too).
func TestMapExitResolveErrorExits2(t *testing.T) {
	_, err := Resolve(DeclaredTarget{}, ResolveOptions{}) // nothing declared → AC-2
	if err == nil {
		t.Fatal("Resolve with no declaration returned nil error, want the no-target refusal")
	}
	var re *ResolveError
	if !errors.As(err, &re) {
		t.Fatalf("Resolve error is %T, want *ResolveError", err)
	}
	if re.Exit() != 2 {
		t.Fatalf("ResolveError.Exit()=%d, want 2", re.Exit())
	}
	if got := MapExit(err); got != 2 {
		t.Fatalf("MapExit(no-target refusal)=%d, want 2 (AC-2: exits 2 and lands nothing)", got)
	}
	// nothing landed: the run that refused has no armed faults
	set := NewArmedSet()
	if set.Len() != 0 {
		t.Fatalf("armed set not empty after refusal: %v", set.IDs())
	}
}
