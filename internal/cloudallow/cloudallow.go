// Package cloudallow is the ONE authoritative AC-17 resource-tag
// allowlist: the exact membership source consulted by BOTH real-plane
// gates — the simulator's (backend_sim.RealGate.Decide) and the rails
// ladder's (rails.checkL5Gate). Before this package the two gates each
// checked only "is the tag non-empty", so whitespace, "*" and arbitrary
// production-looking tags were admitted (MISCHIEF-DF-47).
//
// v0.1 admits exactly one documented sandbox tag ("aws:sandbox-lab", the
// example in docs/prd/mischief-v0.1.md and the sim verb's usage text).
// Membership is a fail-closed EXACT string match: no globs, no prefix or
// substring matching, no whitespace trimming, no case folding. The source
// is a compile-time constant — deterministic, no host-specific or
// network-dependent state.
package cloudallow

import (
	"fmt"
	"strings"
)

// SandboxTag is the v0.1 documented sandbox resource tag — the only tag
// the authoritative allowlist admits (docs/prd/mischief-v0.1.md AC-17,
// cmd/mischief sim usage, docs/SPEC-PLAN.md §7 ladder).
const SandboxTag = "aws:sandbox-lab"

// DefaultTags returns the authoritative allowlist, in canonical order.
// Callers must treat it as read-only.
func DefaultTags() []string {
	return []string{SandboxTag}
}

// Allows reports whether tag is an exact entry of the authoritative
// allowlist. Blank/whitespace-only values and every unlisted value —
// including "*" and padded variants of an allowlisted tag — are refused.
func Allows(tag string) bool {
	return CheckReason(tag) == ""
}

// CheckReason returns "" when tag is allowlisted; otherwise it returns
// the refusal reason for the allowlist leg of the AC-17 triad. The reason
// NAMES the offending tag (quoted, so whitespace is visible) and states
// the authoritative allowlist, and it composes into both gates' refusal
// texts as the allowlist piece ("real-provider fault requires <reason>").
func CheckReason(tag string) string {
	if strings.TrimSpace(tag) == "" {
		return fmt.Sprintf("an allowlisted resource tag (got %q: blank/whitespace is never allowlisted)", tag)
	}
	for _, t := range DefaultTags() {
		if tag == t {
			return ""
		}
	}
	return fmt.Sprintf("an allowlisted resource tag (got %q: not on the AC-17 allowlist; allowlisted: %s)",
		tag, strings.Join(DefaultTags(), ", "))
}
