package selftest

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/catalog"
)

// loadCatalogForTest loads the in-repo catalog for corpus coverage tests.
// It delegates to catalog.LoadDefault so repo-root resolution lives in ONE
// place: a duplicate walk-up here would drift from the loader's own boundary
// (and re-open the out-of-repo defect for every corpus coverage test).
func loadCatalogForTest() (*catalog.Catalog, error) {
	return catalog.LoadDefault()
}

// TestEverySkipLandableNamesItsPiece: each recorded-skip landable must
// carry a non-empty missing-piece reason (a skip that cannot say why is
// a bug this package refuses). MSF-010 moved I-002 onto the docker-backed
// coldReturnLandable, so it is no longer in the pure-skip set — the pure
// skips are the ones coveredSkips still names minus I-002.
func TestEverySkipLandableNamesItsPiece(t *testing.T) {
	for _, id := range coveredSkips {
		if id == "I-002" {
			continue // docker-backed now (coldReturnLandable)
		}
		dir := t.TempDir()
		l := buildLandable(id, dir)
		sk, ok := l.(*skipLandable)
		if !ok {
			t.Fatalf("%s built %T, want *skipLandable", id, l)
		}
		msg, missing := sk.capability()
		if !missing || msg == "" {
			t.Fatalf("%s skip landable: missing=%t msg=%q", id, missing, msg)
		}
	}
}

// TestColdReturnLandableGatesOnDocker: I-002's landable skips with a named
// piece when the daemon is absent, and never claims capability without
// evidence (fail-closed probe).
func TestColdReturnLandableGatesOnDocker(t *testing.T) {
	l := buildLandable("I-002", t.TempDir())
	if _, ok := l.(*coldReturnLandable); !ok {
		t.Fatalf("I-002 built %T, want *coldReturnLandable", l)
	}
	msg, missing := l.capability()
	if !missing {
		return // daemon present: the live loop covers the rest
	}
	if msg == "" {
		t.Fatal("cold-return capability gate skipped without naming the missing piece")
	}
}

// TestBuildLandableUnknownFails: an unknown id builds the refusal
// landable, whose prepare refuses (fail, not skip).
func TestBuildLandableUnknownFails(t *testing.T) {
	l := buildLandable("ZZZ-999", t.TempDir())
	if _, ok := l.(*refusalLandable); !ok {
		t.Fatalf("unknown id built %T, want *refusalLandable", l)
	}
	if _, err := l.prepare(); err == nil {
		t.Fatal("refusal landable's prepare did not refuse")
	}
}

// TestNetHelperMissingMeasured: the N-001 missing-piece text reflects the
// host (it names sudo when absent, the L2 target when present) — never a
// bare "unavailable".
func TestNetHelperMissingMeasured(t *testing.T) {
	msg := netHelperMissing()
	if msg == "" {
		t.Fatal("netHelperMissing produced an empty missing-piece text")
	}
	if _, err := exec.LookPath("sudo"); err == nil {
		if !strings.Contains(msg, "L2-scoped namespace target") {
			t.Fatalf("host has sudo but the text does not name the L2 target gap: %q", msg)
		}
	} else if !strings.Contains(msg, "sudo") {
		t.Fatalf("host lacks sudo but the text does not name it: %q", msg)
	}
}
