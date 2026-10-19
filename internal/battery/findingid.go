package battery

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// findingid.go — the ONE id-format seam for every finding emitter (the
// battery's deriveFindings via BatteryFindingID, and the chaos lane's
// NewFindingID, which composes from the same primitives). MISCHIEF-DF-43:
// both emitters previously minted ids the fleet board tooling refuses —
// the battery templated the raw cell id (MSF-BAT-mischief/F-009: a slash,
// lowercase), the chaos lane appended lowercase hex. boardctl
// create/update enforce ^[A-Z][A-Z0-9]+(-[A-Z0-9]+)+$ and refuse both
// shapes, so a filed finding could never be closed or edited without
// --force.
//
// The contract here: uppercase hyphen-separated tokens for the
// human-meaningful identity parts (project, cell/primitive), uppercase
// hex for the content-addressed tail. Same inputs ⇒ same id (idempotent
// re-files hit the board's duplicate guard), different inputs ⇒ different
// ids (the hash tail separates same-named cells across projects), and the
// whole id always matches the fleet regex.

// BoardIDRegex is the fleet board tooling's row-id contract
// (`boardctl`): an initial capital letter, then capitals/digits, with at
// least one hyphen-separated group. Emitters self-check against this so a
// malformed id never reaches the board (the validator refuses; we never
// need --force).
const BoardIDRegex = `^[A-Z][A-Z0-9]+(-[A-Z0-9]+)+$`

var boardIDRE = regexp.MustCompile(BoardIDRegex)

// ValidBoardID reports whether id matches the fleet board tooling's row-id
// format. This is the emitter-side copy of the boardctl contract — board
// internals are not imported.
func ValidBoardID(id string) bool { return boardIDRE.MatchString(id) }

// NormalizeIDTokens uppercases the parts and folds every non-alphanumeric
// run to a single hyphen, trimming leading/trailing hyphens — the
// uppercase hyphen-separated token form the board regex admits
// (mischief/F-009 → MISCHIEF-F-009).
func NormalizeIDTokens(parts ...string) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteByte('-')
		}
		prevHyphen := true // swallow a leading hyphen
		for _, r := range strings.ToUpper(p) {
			switch {
			case r >= 'A' && r <= 'Z' || r >= '0' && r <= '9':
				b.WriteRune(r)
				prevHyphen = false
			default:
				if !prevHyphen {
					b.WriteByte('-')
					prevHyphen = true
				}
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// ContentHash is the content-addressed tail: the first 12 hex chars of
// sha256 over the \x1f-joined parts, UPPERCASE (the lowercase form was
// MISCHIEF-DF-43's chaos half). Same content ⇒ same hash; any content
// change ⇒ a different id.
func ContentHash(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return strings.ToUpper(hex.EncodeToString(h[:])[:12])
}

// FindingID builds a board-compatible finding id from the emitter family
// and the finding's source identity:
//
//	MSF-<FAMILY>-<NORMALIZED-TOKENS>-<CONTENTHASH12>
//
// e.g. FindingID("BAT", "mischief", "mischief/F-009") =
// MSF-BAT-MISCHIEF-MISCHIEF-F-009-057852623DE7.
func FindingID(family string, parts ...string) string {
	return "MSF-" + family + "-" + NormalizeIDTokens(parts...) + "-" + ContentHash(parts...)
}

// checkFindingID is the emit-time self-check: an id that somehow fails the
// fleet contract is a programming error, failed loudly at the seam rather
// than filed to a board that would refuse it.
func checkFindingID(id string) error {
	if !ValidBoardID(id) {
		return fmt.Errorf("finding id %q does not match the fleet board id format %s", id, BoardIDRegex)
	}
	return nil
}

// BatteryFindingID is the battery emitter's id: family BAT, sourced from
// the owning project and the cell id (which is <project>/<primitive> on
// the full matrix, qN on the quick one). Both parts ride the normalized
// token form and the hash tail, so mischief/F-009 and a hypothetical
// second project's F-009 cell never collide.
func BatteryFindingID(project, cellID string) string {
	id := FindingID("BAT", project, cellID)
	if err := checkFindingID(id); err != nil {
		panic(err) // a programmatic id shape, not runtime data
	}
	return id
}
