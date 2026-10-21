package battery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trouble-agent/mischief/internal/selftest"
)

// detection_honesty_test.go — MISCHIEF-DF-44: the detection axis of the
// battery matrix is NOT measured in this build (no production detector is
// constructed anywhere; every shipped artefact row reads not_wired with
// the named reason). These tests pin that honest state so it cannot
// silently revert in either direction:
//
//   - a change that starts CLAIMING detection (a "detected" row, a docs
//     or artefact sentence that sells detection as measured) fails here
//     until a real detector is wired AND the artefacts/docs change with
//     it consciously;
//   - a change that LOSES the honesty (the not-wired reason dropped, the
//     artefact note emptied, an unnamed observer slipping a "detected"
//     past the render) fails here.
//
// The pins deliberately couple the production constructor, the run
// output, the artefact render and the committed evidence: all four must
// move together or not at all.

// TestNewRunnerWiresNoDetector: the production constructor (the only
// constructor cmd/mischief/battery.go uses) wires NO detector. If a real
// trouble-ledger integration lands, this test fails on purpose — the
// constructor change must arrive together with the artefact/docs
// reconciliation (docs/BATTERY-PARITY.md, the DetectionNote), never
// alone.
func TestNewRunnerWiresNoDetector(t *testing.T) {
	rn := NewRunner(stubCat("X"))
	if len(rn.Detectors) != 0 {
		t.Fatalf("NewRunner wired %d detector(s); the honest v0.1 posture wires none (detection NOT measured — wiring one requires the docs/artefact reconciliation in the same change)", len(rn.Detectors))
	}
}

// TestQuickMatrixEveryCellRecordsNotWiredWithReason: every cell of the
// shipped --quick parity matrix records detection.state = "not_wired"
// WITH the contract reason — never a bare null (an unexplained null is
// junk) and never a state that implies a measurement happened.
func TestQuickMatrixEveryCellRecordsNotWiredWithReason(t *testing.T) {
	m := QuickMatrix()
	if len(m.Cells) == 0 {
		t.Fatal("quick matrix has no cells")
	}
	outcomes := map[string]selftestResultStub{}
	for _, c := range m.Cells {
		outcomes[c.Primitive] = selftestResultStub{}
	}
	rn := stubRunner(stubCat(primitiveIDs(m)...), stubPassOutcomes(m))
	res, err := rn.Run(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Cells) != len(m.Cells) {
		t.Fatalf("ran %d cells, want %d", len(res.Cells), len(m.Cells))
	}
	for _, c := range res.Cells {
		if c.Detection.State != "not_wired" {
			t.Errorf("%s: detection.state = %q, want not_wired (no detector is wired in this build — any other state claims a measurement that did not happen)", c.Cell.ID, c.Detection.State)
		}
		if c.Detection.Reason != notWired {
			t.Errorf("%s: detection.reason = %q, want the contract reason %q", c.Cell.ID, c.Detection.Reason, notWired)
		}
		if c.Detection.Observer != "" {
			t.Errorf("%s: detection.observer = %q, want empty (no observer exists)", c.Cell.ID, c.Detection.Observer)
		}
	}
}

// TestArtefactRendersStatesDistinctly: the artefact is the fleet-facing
// product — it must (a) state the not-measured posture once in
// DetectionNote, (b) carry the state vocabulary so not_measured /
// detected / gap / error are distinguishable on the markdown table, and
// (c) never render a bare "detected" without naming the observer.
func TestArtefactRendersStatesDistinctly(t *testing.T) {
	rn := stubRunner(stubCat("P-001"), map[string]selftest.Result{"P-001": stubPass})
	rn.Detectors = map[string]Detector{
		"c1": &stubDetector{name: "trouble-ledger", ok: true},
	}
	res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "P-001")}})
	if err != nil {
		t.Fatal(err)
	}
	a := BuildArtefact(res, "runid-test", "cat-test", time.Now().Unix())
	if !strings.Contains(a.DetectionNote, "not_wired") && !strings.Contains(a.DetectionNote, "no detector") {
		t.Errorf("DetectionNote %q does not state the not-wired posture", a.DetectionNote)
	}
	md := renderMarkdown(a)
	if !strings.Contains(md, a.DetectionNote) {
		t.Errorf("markdown omits the DetectionNote (the posture must be stated on the human artefact too)")
	}
	// The detected row names its observer — a bare "detected" that does
	// not say WHO observed is a fake-looking green.
	if !strings.Contains(md, "detected (trouble-ledger)") {
		t.Errorf("markdown does not render the detected row with its observer name; got table rows without %q", "detected (trouble-ledger)")
	}
	// The vocabulary legend distinguishes the four states.
	for _, want := range []string{"not_wired", "gap", "error"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown legend/table missing the %q state (states must stay distinguishable on the shipped artefact)", want)
		}
	}
}

// TestUnnamedObserverCannotClaimDetection: a detector whose Name() is
// empty cannot claim any detection outcome — its entries are recorded as
// "error" naming the defect, never "detected" (an unnamed green is a
// false-green detection claim in the making).
func TestUnnamedObserverCannotClaimDetection(t *testing.T) {
	rn := stubRunner(stubCat("P-001"), map[string]selftest.Result{"P-001": stubPass})
	rn.Detectors = map[string]Detector{
		"c1": &stubDetector{name: "", ok: true},
	}
	res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "P-001")}})
	if err != nil {
		t.Fatal(err)
	}
	d := res.Cells[0].Detection
	if d.State != "error" {
		t.Errorf("unnamed observer produced detection.state = %q, want error (an unnamed observer cannot claim a detection outcome)", d.State)
	}
	if d.Reason == "" || !strings.Contains(d.Reason, "observer") {
		t.Errorf("unnamed-observer entry carries reason %q, want text naming the observer defect", d.Reason)
	}
}

// TestCommittedQuickArtefactsPinNotWiredEvidence: the committed live-run
// artefacts under docs/battery/ are the shipped evidence — every cell in
// every committed matrix.json must read not_wired with its reason and the
// artefact must carry the detection note. A future artefact that claims
// "detected" fails here until the detection wiring is real and the docs
// reconciliation lands with it.
func TestCommittedQuickArtefactsPinNotWiredEvidence(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "docs", "battery", "quick-*", "matrix.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no committed quick artefacts under docs/battery/ — the not-measured evidence is missing")
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		var a struct {
			Cells []struct {
				CellID    string `json:"cell_id"`
				Detection struct {
					State    string `json:"state"`
					Reason   string `json:"reason"`
					Observer string `json:"observer"`
				} `json:"detection"`
			} `json:"cells"`
			DetectionNote string `json:"detection_note"`
		}
		if err := json.Unmarshal(b, &a); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if a.DetectionNote == "" {
			t.Errorf("%s: detection_note empty (the artefact must state its detection posture)", p)
		}
		if len(a.Cells) == 0 {
			t.Errorf("%s: no cells", p)
		}
		for _, c := range a.Cells {
			if c.Detection.State != "not_wired" {
				t.Errorf("%s: cell %s detection.state = %q, want not_wired (committed evidence claims a detection measurement no run made)", p, c.CellID, c.Detection.State)
			}
			if c.Detection.Reason == "" {
				t.Errorf("%s: cell %s not_wired entry carries no reason", p, c.CellID)
			}
		}
	}
}

// stubDetector is a minimal Detector stand-in — DECLARED IN
// runner_test.go (this package's shared stub; redeclaring it here would
// break the build). This file only consumes it.

// stubPassOutcomes builds the all-pass outcome map a quick matrix needs
// (the detection pin is orthogonal to the verdict axis).
func stubPassOutcomes(m *Matrix) map[string]selftest.Result {
	out := map[string]selftest.Result{}
	for _, c := range m.Cells {
		out[c.Primitive] = selftest.Result{ID: c.Primitive, State: "pass", LandProof: "landed: proof", RevertProof: "reverted: proof"}
	}
	return out
}

// primitiveIDs lists the distinct primitive ids a matrix resolves against
// the catalog.
func primitiveIDs(m *Matrix) []string {
	seen := map[string]bool{}
	var ids []string
	for _, c := range m.Cells {
		if !seen[c.Primitive] {
			seen[c.Primitive] = true
			ids = append(ids, c.Primitive)
		}
	}
	return ids
}

// selftestResultStub is a named placeholder keeping the outcomes map type
// simple (the map is only used for membership above).
type selftestResultStub struct{}
