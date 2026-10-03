package journal

import (
	"strings"
	"testing"
)

// ch:trace row=MSF-003 spec=docs/SPEC-PLAN.md#SPEC-02 test=internal/journal/scrub_test.go evidence=internal/journal/scrub_test.go witness=none:no-live-host-run-in-worktree
// AC-20: no credential-shaped material survives into journal bytes. These are
// the scrub-unit halves; the write-path (as-persisted) halves are in
// journal_test.go.

// scrubTable: each case's secret span must vanish and the named marker must
// appear. keepCase pins the exact expected output where the replacement form
// is contract (prefix kept for auditability, rule named).
var scrubTable = []struct {
	name   string
	in     string
	marker string // "[REDACTED:<rule>]" expected in the output
	secret string // the substring that must NOT survive
}{
	{"bare-sentry-key", "sentry_key=abc123def", "[REDACTED:header-key]", "abc123def"},
	{"query-sentry-key", "curl -X POST 'http://h/api/1/event/?sentry_key=abc123def' -d '{}'", "[REDACTED:query-key]", "abc123def"},
	{"query-key", "http://h/x?key=secretvalue99", "[REDACTED:query-key]", "secretvalue99"},
	{"header-x-api-key", "x-api-key: SKWJD1234567890abcdef", "[REDACTED:header-key]", "SKWJD1234567890abcdef"},
	{"bearer-jwt", "auth: Bearer eyJhbGciOiJIUzI1NiJ9.abc.def", "[REDACTED:bearer]", "eyJhbGciOiJIUzI1NiJ9.abc.def"},
	{"github-token", "token=ghp_0123456789abcdefghijklmnopqrstuv", "[REDACTED:provider-prefix]", "ghp_0123456789abcdefghijklmnopqrstuv"},
	{"openai-prefix", "key was sk-abcdefghijklmnopqrstuvwxyz123456", "[REDACTED:provider-prefix]", "sk-abcdefghijklmnopqrstuvwxyz123456"},
	{"aws-access-key", "creds AKIAIOSFODNN7EXAMPLE in log", "[REDACTED:provider-prefix]", "AKIAIOSFODNN7EXAMPLE"},
	{"dsn-postgres", "connect postgres://app:s3cret@db.internal:5432/prod", "[REDACTED:dsn]", "s3cret"},
	{"url-credentials", "clone https://user:secretpw@example.com/api", "[REDACTED:url-cred]", "secretpw"},
	// The bare (non-URL, non-header) shapes: a journal Fields value like
	// "password=..." or "token=..." is exactly the credential shape AC-20
	// forbids. (RED before the bare-key rule landed: these passed through
	// verbatim despite the package comment promising them.)
	{"bare-token", "token=abc123def456", "[REDACTED:bare-key]", "abc123def456"},
	{"bare-password", "password=hunter2secret", "[REDACTED:bare-key]", "hunter2secret"},
	{"bare-secret", "secret=deadbeefcafe", "[REDACTED:bare-key]", "deadbeefcafe"},
	{"bare-access-token", "access_token=aaaabbbbccccdddd", "[REDACTED:bare-key]", "aaaabbbbccccdddd"},
	{"bare-api-key", "api_key=xyzsecretvalue12", "[REDACTED:bare-key]", "xyzsecretvalue12"},
	{"bare-key-in-cmd", "run cmd: export API_TOKEN=superfake9999 && curl ...", "[REDACTED:bare-key]", "superfake9999"},
}

// TestScrubRedactsCredentialShapes: the direct scrub() half of AC-20.
func TestScrubRedactsCredentialShapes(t *testing.T) {
	for _, tc := range scrubTable {
		t.Run(tc.name, func(t *testing.T) {
			got := scrub(tc.in)
			if strings.Contains(got, tc.secret) {
				t.Fatalf("scrub(%q) = %q: secret %q survived (AC-20)", tc.in, got, tc.secret)
			}
			if !strings.Contains(got, tc.marker) {
				t.Fatalf("scrub(%q) = %q: want marker %s (the journal must show WHAT was redacted)", tc.in, got, tc.marker)
			}
		})
	}
}

// TestScrubKeepsBenignText: scrub is a credential-shape pass, not a sledge —
// ordinary fault/params text and hex run ids must survive byte-identical.
func TestScrubKeepsBenignText(t *testing.T) {
	benign := []string{
		"mode=truncate service=redis",
		"curl -sf -o /dev/null -w '%{http_code}' 127.0.0.1:7661/health.json",
		"redis-cli XPENDING <stream> <group>",
		"1a2b3c4d5e6f7788", // a hex run id
		"probe set green within 40s",
		"the injector found no live target holding the path",
	}
	for _, s := range benign {
		if got := scrub(s); got != s {
			t.Fatalf("scrub(%q) = %q: benign text must be untouched", s, got)
		}
	}
}

// TestScrubDoesNotRenameEarlierRuleMarkers: rule cascades must be stable —
// once a value is redacted, a later rule must not rewrite the marker (the
// first matching rule owns the naming). RED before the already-redacted
// guard: header-key re-redacted query-key's marker.
func TestScrubDoesNotRenameEarlierRuleMarkers(t *testing.T) {
	already := "?sentry_key=[REDACTED:query-key]"
	if got := scrub(already); got != already {
		t.Fatalf("scrub renamed an existing marker: %q -> %q", already, got)
	}
}

// TestScrubIdempotent: scrub(scrub(x)) == scrub(x) for every table input —
// the write path scrubs per-field AND over the serialised line, so a second
// pass must be a no-op.
func TestScrubIdempotent(t *testing.T) {
	for _, tc := range scrubTable {
		once := scrub(tc.in)
		if twice := scrub(once); twice != once {
			t.Fatalf("%s: scrub not idempotent:\nonce : %s\ntwice: %s", tc.name, once, twice)
		}
	}
}

// TestScrubMarkerForm: the replacement vocabulary is exactly
// [REDACTED:<rule>] (auditable, grep-able, never a bare "***").
func TestScrubMarkerForm(t *testing.T) {
	if got := scrubMarker("query-key"); got != "[REDACTED:query-key]" {
		t.Fatalf("scrubMarker = %q, want [REDACTED:query-key]", got)
	}
}
