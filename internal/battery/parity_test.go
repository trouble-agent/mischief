package battery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/catalog"
	"github.com/trouble-agent/mischief/internal/journal"
	"github.com/trouble-agent/mischief/internal/selftest"
)

// parity_test.go — MSF-016 (M4): the S-1 replay and the deletion-checklist
// pin, both tied to the PARITY MATRIX.
//
// S-1 (PRD §12) replays QA-TROUBLE-3's false green: a bash cell that graded
// OK from a start command's exit code while the app never started. The
// replacement must grade **no_op** with no landed-proof in the journal, and
// **recovered / not_recovered** once the start (the primitive's landing
// path, in battery terms) is real. These tests execute the REAL quick
// matrix — the same five cells `mischief battery --quick` runs — through
// stub executor seams, so the S-1 shape is proven on the cells that
// replace the bash ones, not on an ad-hoc matrix. The verdicts come from
// the SPEC-05 engine (gradePass/gradeFail collect measured evidence;
// Grade decides); nothing here string-maps a verdict.
//
// The fleet-side deletion checklist (docs/BATTERY-PARITY.md) is pinned by
// TestParityDocPinsTheFiveCellsAndDeletionChecklist so the doc cannot go
// stale against the parity table it documents.

// runQuickWith executes the quick matrix with every primitive's executor
// outcome fixed to r, on a real catalog.
func runQuickWith(t *testing.T, r selftest.Result) *RunResult {
	t.Helper()
	cat, err := catalog.LoadDefault()
	if err != nil {
		t.Fatalf("catalog load: %v", err)
	}
	rn := NewRunner(cat)
	rn.executeCell = func(c Cell) selftest.Result {
		out := r
		out.ID = c.Primitive
		return out
	}
	res, err := rn.Run(QuickMatrix())
	if err != nil {
		t.Fatalf("quick run: %v", err)
	}
	if len(res.Cells) != 5 {
		t.Fatalf("ran %d cells, want 5", len(res.Cells))
	}
	return res
}

// TestS1ReplayNoLandedProofGradesNoOp is the QA-TROUBLE-3 replay: the
// landing path never proves (the analog of "the start command never
// started the app"). Every parity cell must grade no_op — NEVER recovered,
// the false green the old cell produced — the recovery axis must say
// not_measured, every finding must be a P2 coverage finding naming the
// replaced bash cell, and the journal must carry no fault_landed record
// (S-1: "no landed-proof in the journal").
func TestS1ReplayNoLandedProofGradesNoOp(t *testing.T) {
	res := runQuickWith(t, selftest.Result{
		State:  "fail",
		Reason: "land did not prove (AC-3): counter stayed 0",
	})
	for _, c := range res.Cells {
		if c.Verdict != journal.VerdictNoOp {
			t.Errorf("%s (%s, replaces %s): verdict %q, want no_op (S-1: no landed proof ⇒ no_op, never recovered)",
				c.Cell.ID, c.Cell.Primitive, c.Cell.LegacyCell, c.Verdict)
		}
		if c.Recovery != "not_measured" {
			t.Errorf("%s: recovery %q, want not_measured (nothing landed ⇒ nothing to recover)", c.Cell.ID, c.Recovery)
		}
		if !strings.Contains(c.Reason, "landed proof never fired") {
			t.Errorf("%s: reason %q does not name the engine's rule-5 no_op arm (armed but unproven)", c.Cell.ID, c.Reason)
		}
	}

	// The false-green killer, stated outright: no cell grades recovered.
	for _, c := range res.Cells {
		if c.Verdict == journal.VerdictRecovered {
			t.Fatalf("%s graded recovered with NO landed proof — the QA-TROUBLE-3 false green is back", c.Cell.ID)
		}
	}

	// Findings: one P2 coverage finding per cell, each naming the
	// replaced bash cell (the migration's own traceability).
	if len(res.Findings) != 5 {
		t.Fatalf("no_op run filed %d findings, want 5 (one coverage finding per replaced cell)", len(res.Findings))
	}
	for _, f := range res.Findings {
		if f.Priority != "P2" {
			t.Errorf("finding %s priority %q, want P2 (coverage, not the product)", f.ID, f.Priority)
		}
		if f.LegacyCell == "" {
			t.Errorf("finding %s carries no legacy cell name", f.ID)
		}
	}

	// S-1's journal half: no landed-proof record at all.
	for _, rec := range res.JournalRecords("s1-noop") {
		if rec.Type == journal.RecordFaultLanded {
			t.Errorf("journal carries %s with proof %q — a no_op run must have no landed-proof record", rec.Type, rec.Fields["proof"])
		}
	}
}

// TestS1ReplayRealStartGradesRecovered is S-1's second half: with the
// landing path real (a measured landed proof AND a measured revert), the
// same cells grade recovered — engine-graded on the evidence, not mapped
// from an exit code — the journal carries the landed-proof record S-1
// demands, and nothing files a finding.
func TestS1ReplayRealStartGradesRecovered(t *testing.T) {
	res := runQuickWith(t, selftest.Result{
		State:       "pass",
		LandProof:   "landed: proxy hit-ledger delta=1 (measured out-of-band)",
		RevertProof: "reverted: disarm + replay clean, pre-state byte-identical",
	})
	for _, c := range res.Cells {
		if c.Verdict != journal.VerdictRecovered {
			t.Errorf("%s (%s): verdict %q, want recovered (S-1: real start ⇒ recovered/not_recovered, not no_op)",
				c.Cell.ID, c.Cell.Primitive, c.Verdict)
		}
		if c.Recovery != "recovered" {
			t.Errorf("%s: recovery %q, want recovered", c.Cell.ID, c.Recovery)
		}
	}
	if len(res.Findings) != 0 {
		t.Errorf("recovered run filed %d findings, want 0", len(res.Findings))
	}
	var landed, reverted int
	for _, rec := range res.JournalRecords("s1-real") {
		switch rec.Type {
		case journal.RecordFaultLanded:
			landed++
		case journal.RecordFaultReverted:
			reverted++
		}
	}
	if landed != 5 || reverted != 5 {
		t.Errorf("journal carries %d fault_landed / %d fault_reverted records, want 5/5 (the landed-proof journal S-1 cites)", landed, reverted)
	}
}

// TestS1ReplayRealStartBrokenRecoveryGradesNotRecovered is the third S-1
// arm: the start is real (landed proof measured) but the recovery never
// proves — the chaos-shutdown kill-recovery shape. The closure rule (rule
// 7) grades not_recovered, the recovery axis says not_recovered, and the
// findings are P1 (an adverse outcome about the target, not coverage).
func TestS1ReplayRealStartBrokenRecoveryGradesNotRecovered(t *testing.T) {
	res := runQuickWith(t, selftest.Result{
		State:     "fail",
		Reason:    "kill-recovery start failed: container did not come back on the same address",
		LandProof: "landed: fault measured at the target",
	})
	for _, c := range res.Cells {
		if c.Verdict != journal.VerdictNotRecovered {
			t.Errorf("%s (%s): verdict %q, want not_recovered (S-1: real start, broken recovery)", c.Cell.ID, c.Cell.Primitive, c.Verdict)
		}
		if c.Recovery != "not_recovered" {
			t.Errorf("%s: recovery %q, want not_recovered", c.Cell.ID, c.Recovery)
		}
	}
	if len(res.Findings) != 5 {
		t.Fatalf("not_recovered run filed %d findings, want 5", len(res.Findings))
	}
	for _, f := range res.Findings {
		if f.Priority != "P1" {
			t.Errorf("finding %s priority %q, want P1 (adverse outcome on the target)", f.ID, f.Priority)
		}
	}
}

// TestParityDocPinsTheFiveCellsAndDeletionChecklist keeps
// docs/BATTERY-PARITY.md honest against the parity table it documents:
// every legacy cell named, every replacing primitive named, the
// replacement command present, and the deletion checklist present. A doc
// that drifts from the table fails here — the fleet-side deletion edit
// consumes this doc, so a stale one deletes the wrong thing.
func TestParityDocPinsTheFiveCellsAndDeletionChecklist(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "BATTERY-PARITY.md"))
	if err != nil {
		t.Fatalf("read parity doc: %v", err)
	}
	doc := string(b)
	for _, c := range LegacyCellNames() {
		if !strings.Contains(doc, c) {
			t.Errorf("parity doc missing legacy cell %q", c)
		}
	}
	for _, p := range []string{"N-012", "I-002", "F-009", "R-001"} {
		if !strings.Contains(doc, p) {
			t.Errorf("parity doc missing replacing primitive %q", p)
		}
	}
	for _, s := range []string{
		"mischief battery --quick --file-rows", // the replacement command line
		"bunker-qa.sh",                         // the file the checklist deletes from
		"no_op",                                // the S-1 no-landed-proof verdict
		"Deletion checklist",                   // the section the foreman follow-up executes
	} {
		if !strings.Contains(doc, s) {
			t.Errorf("parity doc missing required content %q", s)
		}
	}
}
