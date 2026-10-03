package reverter

import (
	"strings"
	"testing"
	"time"
)

// TestScrubRulesMirrorJournal (drift pin): this package mirrors
// internal/journal's scrub rule set byte-for-byte (its rules are unexported
// and the row forbids touching files outside internal/reverter/). The rule
// NAMES and their ORDER are the contract the journals share — a rename or
// reorder on either side must fail this test so the drift is a decision,
// not an accident.
func TestScrubRulesMirrorJournal(t *testing.T) {
	want := []string{"bearer", "auth-header", "query-key", "header-key", "provider-prefix", "dsn", "url-cred", "bare-key"}
	if len(scrubRules) != len(want) {
		t.Fatalf("rule count %d, want %d (mirror drift vs internal/journal)", len(scrubRules), len(want))
	}
	for i, n := range want {
		if scrubRules[i].name != n {
			t.Fatalf("rule[%d] = %s, want %s (mirror drift vs internal/journal)", i, scrubRules[i].name, n)
		}
	}
}

// TestScrubRedactsCredentialShapes: the AC-20 shapes are redacted with
// named markers, and the non-secret scaffolding survives (auditable: the
// journal shows WHAT was redacted and why). The fixtures are ASSEMBLED at
// runtime so no credential-shaped literal exists in this file (the repo's
// gitleaks allowlist covers internal/journal tests only — this package must
// never need one).
func TestScrubRedactsCredentialShapes(t *testing.T) {
	prefix := string([]byte{'s', 'k', '-'}) // "sk-" without the literal
	gh := string([]byte{'g', 'h', 'p', '_'})
	cases := []struct {
		name    string
		in      string
		wantHas []string
		wantNo  string
	}{
		{
			name:    "bearer header",
			in:      "curl -H 'Authorization: Bearer " + prefix + "AbcD1234567890xyz' host",
			wantHas: []string{"[REDACTED:bearer]"},
			wantNo:  "AbcD1234567890xyz",
		},
		{
			name:    "provider key prefix",
			in:      "the key is " + prefix + "proj-AbcD1234567890 done",
			wantHas: []string{"[REDACTED:provider-prefix]"},
			wantNo:  "AbcD1234567890",
		},
		{
			name:    "github token prefix",
			in:      "push with " + gh + "Abcdefghijklmnopqrst done",
			wantHas: []string{"[REDACTED:provider-prefix]"},
			wantNo:  "Abcdefghijklmnopqrst",
		},
		{
			name:    "query parameter key",
			in:      "https://sentry.io/api/1/store/?sentry_key=abcdef1234567890",
			wantHas: []string{"[REDACTED:query-key]"},
			wantNo:  "abcdef1234567890",
		},
		{
			name:    "bare assignment in a field value",
			in:      "export API_TOKEN=supersecretvalue123",
			wantHas: []string{"[REDACTED:bare-key]"},
			wantNo:  "supersecretvalue123",
		},
		{
			name:    "dsn credentials",
			in:      "postgres://alice:hunter2pw@db.internal:5432/app",
			wantHas: []string{"[REDACTED:dsn]"},
			wantNo:  "hunter2pw",
		},
		{
			name:    "plain paths survive untouched",
			in:      "restore-file path=/tmp/mischief/scratch/x backup=/tmp/mischief/scratch/x.bak",
			wantHas: []string{"path=/tmp/mischief/scratch/x"},
			wantNo:  "[REDACTED",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := scrub(tc.in)
			for _, w := range tc.wantHas {
				if !strings.Contains(out, w) {
					t.Fatalf("scrub(%q) = %q, want it to contain %q", tc.in, out, w)
				}
			}
			if tc.wantNo != "" && strings.Contains(out, tc.wantNo) {
				t.Fatalf("scrub(%q) = %q, secret material %q survived", tc.in, out, tc.wantNo)
			}
			// idempotent: scrubbing a scrubbed line changes nothing (the
			// marker is never renamed or extended).
			if out2 := scrub(out); out2 != out {
				t.Fatalf("scrub not idempotent: %q -> %q", out, out2)
			}
		})
	}
}

// TestJournalCarriesNoCredentialMaterial (the AC-20 end-to-end half): a
// secret entering through ANY caller field — fault id, inverse params,
// check params, extra — is absent from the journal BYTES as persisted. The
// write itself must still succeed (scrub replaces, never fails).
func TestJournalCarriesNoCredentialMaterial(t *testing.T) {
	st, lf := newTestStore(t)
	secret := string([]byte{'s', 'k'}) + "-proj-SecretValue0987654321"
	hold := Hold{
		FaultID: "P-SCRUB-" + secret,
		Target:  "file:/tmp/mischief-scrub/live?token=" + secret,
		Inverse: Decl{Kind: "restore-file", Params: map[string]string{"path": "/tmp/live", "backup": secret}},
		Check:   Decl{Kind: "file-matches-backup", Params: map[string]string{"path": "/tmp/live", "backup": secret}},
		TTL:     time.Minute,
	}
	id, err := lf.Arm(hold)
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	if err := lf.Land(id); err != nil {
		t.Fatalf("land: %v", err)
	}
	if _, err := lf.Revert(testContext(), id, "direct", RoleAPI); err != nil {
		t.Fatalf("revert: %v", err)
	}
	markers := 0
	for i, line := range journalLines(t, st.Dir) {
		if strings.Contains(line, secret) {
			t.Fatalf("line %d carries the raw secret: %s", i, line)
		}
		if strings.Contains(line, "[REDACTED:") {
			markers++
		}
		if err := validJSONLine(line); err != nil {
			t.Fatalf("line %d is not valid JSON after scrub: %v (%s)", i, err, line)
		}
	}
	// The arm line carried the secret through fault id, target and params —
	// the redaction must be VISIBLE somewhere (the journal shows WHAT was
	// redacted and why), not merely silent.
	if markers == 0 {
		t.Fatal("no [REDACTED:*] marker anywhere in the journal; scrub pass never fired")
	}
}

// validJSONLine checks one journal line parses (the scrub pass must never
// rewrite a line into invalid JSON).
func validJSONLine(line string) error {
	var v map[string]any
	return jsonUnmarshalStrict([]byte(line), &v)
}
