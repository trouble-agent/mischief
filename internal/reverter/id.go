package reverter

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// HoldID derives a hold's content-derived id: hex(sha256(canonical
// hold-declaration bytes))[:16] — the same derivation class as
// internal/journal's RunID (hex(sha256(spec+seed))[:16]) and with the same
// injectivity separator: the fields are joined with explicit newlines so two
// different holds that concatenated identically still hash differently. The
// id is a pure function of the hold's declared content: same fault + inverse
// + check + target + ttl = same id (a duplicate arm of the same hold lands on
// the same line; appendStatus collapses the duplicate states to one); any
// difference in the declaration = a different hold. The TTL enters in
// nanoseconds as decimal text — a duration, not a clock reading.
func HoldID(faultID, target string, ttlNanos int64, inverseDecl, checkDecl string) string {
	h := sha256.New()
	fmt.Fprintf(h, "hold\n%s\n%s\n%d\n%s\n%s\n", faultID, target, ttlNanos, inverseDecl, checkDecl)
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:8]) // [:16] hex chars = first 8 bytes
}

// canonicalJoin renders an ordered field list as one stable string (unit
// separator joined) — the text form used for composite identifiers.
func canonicalJoin(parts ...string) string {
	return strings.Join(parts, "\x1f")
}

// sortStrings is insertion sort — the per-record field counts are small and
// the package avoids importing sort for one caller (same shape as
// internal/journal's record.go).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

var _ = sort.Strings // sort is used by public_surface_test assertions only
