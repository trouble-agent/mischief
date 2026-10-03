package reverter

import (
	"regexp"
	"strings"
)

// Scrub-on-write mirror: internal/journal's rule set, byte-for-byte, so the
// reverter journal honours the same AC-20 contract ("no secrets in the
// journal"). The rules are mirrored (not imported) because journal's are
// unexported and this package must not edit files outside its boundary;
// the sibling test pins the two rule sets' names against drift.
//
// markerPrefix is the constant head of every redaction marker.
const markerPrefix = "[REDACTED:"

// scrubRule is one AC-20 credential-shape rule. When keepPrefix is true the
// rule's capture group 1 is non-secret scaffolding (a header name, a query
// parameter name, a URL scheme) and is preserved, with only the secret value
// replaced — the journal stays auditable: it shows WHAT was redacted and why.
type scrubRule struct {
	name       string
	keepPrefix bool
	pattern    *regexp.Regexp
}

// Secret value character classes exclude JSON punctuation (`"`, `,`, `}`,
// `]`) as well as whitespace and quotes: the same rule set runs per field
// AND over the fully serialised line (scrubLine), where a greedy span must
// never eat the line's structural bytes — a span that crossed them would
// rewrite the record into invalid JSON and the write would fail outright.
const secretChars = `[^\s"',}\]]`
const queryValueChars = `[^&\s"',}\]]`

// scrubRules is the AC-20 pattern set: credential SHAPES that must not
// survive into a journal record.
var scrubRules = []scrubRule{
	{name: "bearer", keepPrefix: true, pattern: regexp.MustCompile(`(?i)(bearer\s+)[a-z0-9._~+/-]{8,}[a-z0-9._~+/-=]*`)},
	{name: "auth-header", keepPrefix: true, pattern: regexp.MustCompile(`(?i)((?:authorization|proxy-authorization)\s*[:=]\s*)(` + secretChars + `+)`)},
	{name: "query-key", keepPrefix: true, pattern: regexp.MustCompile(`(?i)([?&](?:key|token|api_key|api-key|apikey|access_token|password|passwd|secret|sentry_key|auth|sig|signature|private_key|client_secret)=)(` + queryValueChars + `{4,})`)},
	{name: "header-key", keepPrefix: true, pattern: regexp.MustCompile(`(?i)((?:x-api-key|x-auth-token|x-access-token|api-key|apikey|sentry_key)\s*[:=]\s*)(` + secretChars + `{4,})`)},
	{name: "provider-prefix", keepPrefix: false, pattern: regexp.MustCompile(`\b(?:sk|pk|rk)-[A-Za-z0-9_-]{12,}|gh[pousr]_[A-Za-z0-9]{20,}|xox[abpor]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16}|AIza[A-Za-z0-9_-]{30,}|glpat-[A-Za-z0-9_-]{15,}|shpat_[A-Za-z0-9]{20,}`)},
	{name: "dsn", keepPrefix: true, pattern: regexp.MustCompile(`(?i)((?:postgres|postgresql|mysql|mariadb|mongodb(?:\+srv)?|redis|amqp|mqtt)://)[^/\s"@]+:[^/\s"@]+@`)},
	{name: "url-cred", keepPrefix: true, pattern: regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^\s/:@""]{2,}:[^\s/@"]{3,}@`)},
	{name: "bare-key", keepPrefix: true, pattern: regexp.MustCompile(`(?i)\b((?:api[-_]?key|api[-_]?token|access[-_]?token|auth[-_]?key|auth[-_]?token|client[-_]?secret|private[-_]?key|sentry[-_]?key|apikey|password|passwd|secret|token)\s*[=:]\s*)(` + secretChars + `{4,})`)},
}

// scrubMarker renders the replacement for one redacted span.
func scrubMarker(rule string) string {
	return markerPrefix + rule + "]"
}

// scrub removes credential-shaped material from s, replacing each secret span
// with [REDACTED:<rule>] (keeping the rule's non-secret prefix when the rule
// declares one). Order matters: dsn before url-cred so the more specific
// marker wins where both match.
//
// A match whose span already carries a marker is left untouched: the first
// matching rule owns the naming (a later rule must not rename
// [REDACTED:query-key] to its own rule), and re-matching a marker must never
// extend the span into the surrounding line bytes.
func scrub(s string) string {
	for _, r := range scrubRules {
		if !r.pattern.MatchString(s) {
			continue
		}
		s = r.pattern.ReplaceAllStringFunc(s, func(m string) string {
			if strings.Contains(m, markerPrefix) {
				return m
			}
			if !r.keepPrefix {
				return scrubMarker(r.name)
			}
			idx := r.pattern.FindStringSubmatchIndex(m)
			if idx == nil || len(idx) < 4 || idx[2] < 0 {
				return scrubMarker(r.name)
			}
			return m[:idx[3]] + scrubMarker(r.name)
		})
	}
	return s
}

// scrubLine is the byte-level backstop over the fully serialised line (the
// JSON-encoded map may still contain a shape the per-field pass missed —
// e.g. a key NAME carrying a secret-like token). It applies the same rule
// set to the whole line.
func scrubLine(b []byte) []byte {
	s := scrub(string(b))
	return []byte(s)
}
