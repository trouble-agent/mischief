// Rails self-check tests (MSF-014 deliverable 6): the doctor's
// sanction-marker, load-gate and scope-ladder arms, and the report's
// deterministic render over them.
//
// ch:trace row=MSF-014 spec=docs/prd/mischief-v0.1.md evidence=cmd/mischief/ + Makefile witness=none:no-live-target-run-in-worktree
package doctor

import (
	"bytes"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/catalog"
)

// railsCatalog builds the two-descriptor synthetic catalog the ladder arm
// needs (d-002 carries a declared-but-unavailable NET_ADMIN capability, the
// same shape testCatalog in doctor_test.go pins).
func railsCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	dir := t.TempDir()
	desc := `id: D-001
name: rails-test-none
what: inject nothing (rails arm fixture)
breaks: nothing
params: {mode: str}
landed_proof: {kind: test, check: "always"}
inverse: {action: noop}
capability: none
tier: L1
backend: helper
maturity: planned
`
	descNet := strings.Replace(desc, "D-001", "D-002", 1)
	descNet = strings.Replace(descNet, "capability: none", "capability: NET_ADMIN", 1)
	descNet = strings.Replace(descNet, "tier: L1", "tier: L2", 1)
	for name, body := range map[string]string{"d-001.yaml": desc, "d-002.yaml": descNet} {
		if err := osWriteFile(dir+"/"+name, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	cat, err := catalog.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

// TestRailsSelfChecksGreenOnSanctionedHost: with the sanction seam pinned
// to admit and the load seam pinned to a healthy reading, all three rails
// rows report OK with a measured detail, in PRD §7's rail order.
func TestRailsSelfChecksGreenOnSanctionedHost(t *testing.T) {
	restoreSanction := pinSanction(t, nil)
	defer restoreSanction()
	restoreLoad := pinLoad(t, railsReading{load1: 0.42, load5: 0.31, psi: 3.2})
	defer restoreLoad()

	rep := &Report{}
	RunRailsSelfChecks(rep, railsCatalog(t))
	if len(rep.Rails) != 3 {
		t.Fatalf("rails rows: %d, want 3", len(rep.Rails))
	}
	wantOrder := []string{"sanction_marker", "load_gate", "scope_ladder"}
	for i, w := range wantOrder {
		if rep.Rails[i].Check != w {
			t.Fatalf("rails row %d = %s, want %s (PRD §7 rail order)", i, rep.Rails[i].Check, w)
		}
		if !rep.Rails[i].OK {
			t.Fatalf("rails row %s failed on a sanctioned host: %s", w, rep.Rails[i].Detail)
		}
	}
	// the load row must name its measured numbers (AC-8's evidence rule)
	if !strings.Contains(rep.Rails[1].Detail, "0.42") {
		t.Fatalf("load row does not name the measured loadavg: %s", rep.Rails[1].Detail)
	}
}

// TestRailsSelfChecksSanctionFailNamesHost: an unsanctioned host fails the
// sanction row with the gate's full message (host + missing halves), and
// the other rows still report (a fail never aborts the report).
func TestRailsSelfChecksSanctionFailNamesHost(t *testing.T) {
	restore := pinSanction(t, errSanctionRefusal)
	defer restore()
	restoreLoad := pinLoad(t, railsReading{load1: 0.5, load5: 0.4, psi: 1})
	defer restoreLoad()

	rep := &Report{}
	RunRailsSelfChecks(rep, railsCatalog(t))
	san := rep.Rails[0]
	if san.OK {
		t.Fatal("sanction row reports OK on an unsanctioned host")
	}
	if !strings.Contains(san.Detail, "not sanctioned") || !strings.Contains(san.Detail, "karaHermes") {
		t.Fatalf("sanction fail does not name host + reason: %s", san.Detail)
	}
	if !rep.Rails[1].OK || !rep.Rails[2].OK {
		t.Fatalf("a sanction fail must not abort the other rails rows: %+v", rep.Rails)
	}
	// render: the FAIL is visible, named
	out := Render(rep)
	if !bytes.Contains(out, []byte("FAIL")) || !bytes.Contains(out, []byte("sanction_marker")) {
		t.Fatalf("render does not name the failing sanction row:\n%s", out)
	}
}

// TestRailsSelfChecksLoadGateBreach: a reading at/over a threshold fails
// the load row with the measured-vs-gate numbers (AC-8).
func TestRailsSelfChecksLoadGateBreach(t *testing.T) {
	restoreSanction := pinSanction(t, nil)
	defer restoreSanction()
	restoreLoad := pinLoad(t, railsReading{load1: 14.0, load5: 9.0, psi: 10})
	defer restoreLoad()

	rep := &Report{}
	RunRailsSelfChecks(rep, railsCatalog(t))
	lg := rep.Rails[1]
	if lg.OK {
		t.Fatal("load row reports OK over the gate")
	}
	if !strings.Contains(lg.Detail, "14.00") || !strings.Contains(lg.Detail, "12") {
		t.Fatalf("load-gate fail does not name measured vs gate: %s", lg.Detail)
	}
}

// TestRailsSelfChecksLadderViolation: a synthetic catalog carrying an
// out-of-ladder tier fails the scope_ladder row NAMING the offending
// primitive (the report is the evidence, not a count).
func TestRailsSelfChecksLadderViolation(t *testing.T) {
	restoreSanction := pinSanction(t, nil)
	defer restoreSanction()
	restoreLoad := pinLoad(t, railsReading{load1: 0.5, load5: 0.4, psi: 1})
	defer restoreLoad()

	cat := railsCatalog(t)
	// synthesize the violation in memory (the loader refuses it on disk —
	// which is exactly why the arm must be fed a hand-built catalog)
	bad := *cat.Primitives["D-002"]
	bad.Tier = &catalog.Tier{Raw: "L9", Min: 9}
	cat.Primitives["D-002"] = &bad

	rep := &Report{}
	RunRailsSelfChecks(rep, cat)
	ladder := rep.Rails[2]
	if ladder.OK {
		t.Fatal("scope_ladder row reports OK with an L9 tier in the catalog")
	}
	if !strings.Contains(ladder.Detail, "D-002") {
		t.Fatalf("ladder violation does not name the primitive: %s", ladder.Detail)
	}
}

// TestRailsSelfChecksEmptyCatalogNoPanic: a hand-built Report with no
// catalog is a no-op (RunRailsSelfChecks refuses nothing, reports nothing).
func TestRailsSelfChecksEmptyCatalogNoPanic(t *testing.T) {
	rep := &Report{}
	RunRailsSelfChecks(rep, nil)
	if len(rep.Rails) != 0 {
		t.Fatalf("nil catalog produced rails rows: %+v", rep.Rails)
	}
}
