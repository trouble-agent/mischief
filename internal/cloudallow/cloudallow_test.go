package cloudallow

import (
	"strconv"
	"strings"
	"testing"
)

// cloudallow_test.go — the allowlist source itself is the contract under
// test: one tag, exact match, fail-closed on blank/whitespace, wildcard
// and every unlisted value.

// TestAllowsOnlyTheDocumentedSandboxTag: the membership surface admits
// exactly the documented tag and nothing else.
func TestAllowsOnlyTheDocumentedSandboxTag(t *testing.T) {
	if !Allows(SandboxTag) {
		t.Fatalf("the documented tag %q must be allowlisted", SandboxTag)
	}
	if got := CheckReason(SandboxTag); got != "" {
		t.Fatalf("CheckReason(%q) = %q, want empty", SandboxTag, got)
	}
	tags := DefaultTags()
	if len(tags) != 1 || tags[0] != SandboxTag {
		t.Fatalf("DefaultTags() = %v, want exactly [%s]", tags, SandboxTag)
	}
}

// TestRefusesBlankWhitespaceWildcardAndUnlisted: every value the old
// non-empty-only gates admitted but the allowlist must refuse.
func TestRefusesBlankWhitespaceWildcardAndUnlisted(t *testing.T) {
	cases := []struct {
		tag        string
		wantNamed  bool // the offending tag itself must appear in the reason
		wantQuoted bool // reason must quote the tag so whitespace is visible
	}{
		{tag: ""},
		{tag: "   ", wantQuoted: true},
		{tag: "\t\n", wantQuoted: true},
		{tag: "*", wantNamed: true},
		{tag: "aws:prod-db", wantNamed: true},
		{tag: "prod-fleet-main-host", wantNamed: true},
		{tag: " aws:sandbox-lab", wantNamed: true}, // padded: NOT the listed tag
		{tag: "aws:sandbox-lab ", wantNamed: true}, // padded: NOT the listed tag
		{tag: "AWS:SANDBOX-LAB", wantNamed: true},  // exact match only: no case folding
		{tag: "aws:sandbox-lab*", wantNamed: true}, // no glob/prefix semantics
	}
	for _, tc := range cases {
		if Allows(tc.tag) {
			t.Fatalf("Allows(%q) = true, want refused (fail-closed)", tc.tag)
		}
		reason := CheckReason(tc.tag)
		if reason == "" {
			t.Fatalf("CheckReason(%q) = empty, want a named refusal", tc.tag)
		}
		if tc.wantNamed && !strings.Contains(reason, tc.tag) {
			t.Fatalf("refusal for %q must name the tag: %s", tc.tag, reason)
		}
		if tc.wantQuoted && !strings.Contains(reason, strconv.Quote(tc.tag)) {
			t.Fatalf("refusal for %q must quote the tag: %s", tc.tag, reason)
		}
		if !strings.Contains(reason, "allowlist") {
			t.Fatalf("refusal for %q must name the allowlist: %s", tc.tag, reason)
		}
	}
}
