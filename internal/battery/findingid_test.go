package battery

import (
	"regexp"
	"strings"
	"testing"
)

// findingid_test.go — MISCHIEF-DF-43: the ONE id-format seam both finding
// emitters share. The fleet board tooling (boardctl) refuses row ids that
// do not match ^[A-Z][A-Z0-9]+(-[A-Z0-9]+)+$ — the emitters used to mint
// MSF-BAT-mischief/F-009 (slash, lowercase) and MSF-CHAOS-<lowercase hex>,
// so a filed finding could never be closed or edited without --force.
// These tests pin the real contract: valid shape, deterministic output,
// project/cell identity carried in uppercase hyphen-separated tokens,
// uppercase hex content tail, old shapes rejected.

// TestFindingIDBoardCompatible — the exact shape: matches the fleet regex,
// carries the uppercase normalized project/cell tokens, and ends in the
// 12-char uppercase hex content tail. The goldens are pinned byte-exact so
// a format drift is a test failure, not a silent board refusal.
func TestFindingIDBoardCompatible(t *testing.T) {
	cases := []struct {
		name           string
		family         string
		parts          []string
		want           string // pinned golden ("" = shape-only)
		wantNormalized string // the normalized tokens must appear verbatim
	}{
		{
			name:           "mischief/F-009 (the observed malformed shape)",
			family:         "BAT",
			parts:          []string{"mischief", "mischief/F-009"},
			want:           "MSF-BAT-MISCHIEF-MISCHIEF-F-009-057852623DE7",
			wantNormalized: "MISCHIEF-F-009",
		},
		{
			name:           "quick cell q7 on scratch",
			family:         "BAT",
			parts:          []string{"scratch", "q7"},
			want:           "MSF-BAT-SCRATCH-Q7-64185AAA7620",
			wantNormalized: "SCRATCH-Q7",
		},
		{
			name:           "chaos content address (project token + content hash)",
			family:         "CHAOS",
			parts:          []string{"mischief"},
			want:           "MSF-CHAOS-MISCHIEF-D3CD17514917",
			wantNormalized: "MISCHIEF",
		},
		{
			name:           "empty project still yields a valid id",
			family:         "BAT",
			parts:          []string{"", "q1"},
			want:           "MSF-BAT-Q1-07C34EBDEB3D",
			wantNormalized: "Q1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FindingID(tc.family, tc.parts...)
			if tc.want != "" && got != tc.want {
				t.Errorf("FindingID(%q, %v) = %q, want golden %q", tc.family, tc.parts, got, tc.want)
			}
			if !ValidBoardID(got) {
				t.Errorf("id %q fails the fleet board regex %s", got, BoardIDRegex)
			}
			if !strings.Contains(got, tc.wantNormalized) {
				t.Errorf("id %q lost the normalized tokens %q", got, tc.wantNormalized)
			}
			// the content tail: 12 uppercase hex chars
			tail := got[len(got)-12:]
			if !regexp.MustCompile(`^[0-9A-F]{12}$`).MatchString(tail) {
				t.Errorf("id %q tail %q is not 12 uppercase hex chars", got, tail)
			}
			// deterministic: same inputs, same id
			if again := FindingID(tc.family, tc.parts...); again != got {
				t.Errorf("non-deterministic: %q vs %q", got, again)
			}
		})
	}
}

// TestFindingIDDeterministicAndDistinct — same content ⇒ same id (an
// idempotent re-file must hit the duplicate guard, not mint a twin);
// any content change ⇒ a different id.
func TestFindingIDDeterministicAndDistinct(t *testing.T) {
	a := FindingID("BAT", "mischief", "mischief/F-009")
	b := FindingID("BAT", "mischief", "mischief/F-009")
	if a != b {
		t.Errorf("same inputs, different ids: %q vs %q", a, b)
	}
	others := []string{
		FindingID("BAT", "mischief", "mischief/F-008"),
		FindingID("BAT", "crier", "mischief/F-009"),
		FindingID("CHAOS", "mischief", "mischief/F-009"),
	}
	for i, o := range others {
		if a == o {
			t.Errorf("variant %d collided with the base id %q", i, a)
		}
	}
}

// TestBatteryFindingIDRepresentativeCells — the battery emitter's wrapper
// over representative matrix cells, including the exact cell id the
// dogfood run observed (mischief/F-009): board-compatible, deterministic,
// project-scoped (a second project's same-named cell does not collide).
func TestBatteryFindingIDRepresentativeCells(t *testing.T) {
	f009 := BatteryFindingID("mischief", "mischief/F-009")
	if f009 != "MSF-BAT-MISCHIEF-MISCHIEF-F-009-057852623DE7" {
		t.Errorf("BatteryFindingID(mischief, mischief/F-009) = %q, want the pinned golden", f009)
	}
	if !ValidBoardID(f009) {
		t.Errorf("%q fails the fleet board regex", f009)
	}
	if again := BatteryFindingID("mischief", "mischief/F-009"); again != f009 {
		t.Errorf("non-deterministic: %q vs %q", f009, again)
	}
	other := BatteryFindingID("crier", "mischief/F-009")
	if other == f009 {
		t.Error("a second project's same-named cell collided")
	}
	if !ValidBoardID(other) {
		t.Errorf("%q fails the fleet board regex", other)
	}
}

// TestOldIDShapesRejected — the shapes the emitters used to emit are NOT
// valid under the fleet contract; the emit-time self-check refuses them.
func TestOldIDShapesRejected(t *testing.T) {
	for _, id := range []string{
		"MSF-BAT-mischief/F-009",  // the observed slash form
		"MSF-BAT-q7",              // lowercase quick-cell form (single group, lowercase)
		"MSF-CHAOS-2dfb1056d701",  // the lowercase hex the chaos lane really emitted
		"MSF-CHAOS-deadbeef",      // lowercase hex test fixture
		"msf-bat-MISCHIEF-F-009",  // lowercase prefix
		"MSF-BAT-MISCHIEF-F-009-", // trailing hyphen
		"MSF_BAT_MISCHIEF",        // underscores
	} {
		if ValidBoardID(id) {
			t.Errorf("old/invalid shape %q passed the fleet regex", id)
		}
		if err := checkFindingID(id); err == nil {
			t.Errorf("checkFindingID(%q) accepted an id the board refuses", id)
		} else if !strings.Contains(err.Error(), "fleet board id format") {
			t.Errorf("refusal does not name the contract: %v", err)
		}
	}
}
