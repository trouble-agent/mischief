package battery

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trouble-agent/mischief/internal/catalog"
	"github.com/trouble-agent/mischief/internal/journal"
	"github.com/trouble-agent/mischief/internal/selftest"
	"github.com/trouble-agent/mischief/internal/verdict"
)

// runner_test.go — run orchestration against STUB seams (the live harness
// is exercised by matrix_test's TestQuickMatrixReachesEveryCellLive; here
// every grading branch is reached deterministically).

// stubCat is a minimal catalog stand-in: the runner only reads
// Cat.Get(id).Name and the unknown-primitive check (Get == nil).
func stubCat(ids ...string) *catalog.Catalog {
	c := &catalog.Catalog{Primitives: map[string]*catalog.Descriptor{}}
	for _, id := range ids {
		c.Primitives[id] = &catalog.Descriptor{ID: id, Name: "prim-" + id}
		c.IDs = append(c.IDs, id)
	}
	return c
}

// stubRunner wires a runner whose per-cell outcomes come from the table
// (id → selftest.Result).
func stubRunner(cat *catalog.Catalog, outcomes map[string]selftest.Result) *Runner {
	return &Runner{
		Cat: cat,
		executeCell: func(c Cell) selftest.Result {
			if r, ok := outcomes[c.Primitive]; ok {
				return r
			}
			return selftest.Result{ID: c.Primitive, State: "fail", Reason: "no stub outcome"}
		},
		now: func() time.Time { return time.Unix(1700000000, 0) },
	}
}

var (
	stubPass = selftest.Result{ID: "X", State: "pass", LandProof: "landed: proof", RevertProof: "reverted: proof"}
	stubSkip = selftest.Result{ID: "X", State: "skip", Reason: "capability_unavailable: docker API absent"}
)

func cell(id, prim string) Cell {
	return Cell{ID: id, Primitive: prim, Target: Target{Project: "scratch", Name: id, Selector: "host:scratch/x", Scratch: true}}
}

// TestRunGradesPassThroughTheEngine: a measured pass grades recovered via
// verdict.Grade (the engine decides; not a string map), with a green
// recovery axis.
func TestRunGradesPassThroughTheEngine(t *testing.T) {
	rn := stubRunner(stubCat("P-001"), map[string]selftest.Result{"P-001": stubPass})
	res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "P-001")}})
	if err != nil {
		t.Fatal(err)
	}
	c := res.Cells[0]
	if c.Verdict != journal.VerdictRecovered {
		t.Fatalf("verdict %q, want recovered (engine-graded)", c.Verdict)
	}
	if c.Recovery != "recovered" {
		t.Errorf("recovery %q, want recovered", c.Recovery)
	}
	if c.Detection.State != "not_wired" || c.Detection.Reason == "" {
		t.Errorf("detection = %+v, want not_wired WITH reason", c.Detection)
	}
	if len(res.Findings) != 0 {
		t.Errorf("a clean pass filed %d findings, want 0", len(res.Findings))
	}
}

// TestRunGradesSkipAsAbortedWithNamedPiece: a capability skip grades
// aborted (the run did not happen), records the missing piece, and files
// NO finding (a skip is a recorded outcome, never an adverse one).
func TestRunGradesSkipAsAbortedWithNamedPiece(t *testing.T) {
	rn := stubRunner(stubCat("I-002"), map[string]selftest.Result{"I-002": stubSkip})
	res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "I-002")}})
	if err != nil {
		t.Fatal(err)
	}
	c := res.Cells[0]
	if c.Verdict != journal.VerdictAborted {
		t.Fatalf("verdict %q, want aborted", c.Verdict)
	}
	if !strings.Contains(c.Reason, "capability_unavailable") || !strings.Contains(c.Reason, "docker API") {
		t.Errorf("reason does not name the missing piece: %q", c.Reason)
	}
	if c.Recovery != "refused" {
		t.Errorf("recovery %q, want refused", c.Recovery)
	}
	if len(res.Findings) != 0 {
		t.Errorf("a skip filed %d findings, want 0 (a skip is recorded, not adverse)", len(res.Findings))
	}
}

// TestRunGradesNoOpThroughTheEngine: a loop that armed but never proved
// landing grades no_op (AC-3) — the verdict comes from Grade, and the
// finding is a P2 coverage finding (the product was not refuted; the
// coverage was).
func TestRunGradesNoOpThroughTheEngine(t *testing.T) {
	rn := stubRunner(stubCat("Z-1"), map[string]selftest.Result{
		"Z-1": {ID: "Z-1", State: "fail", Reason: "land did not prove: counter stayed 0"},
	})
	res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "Z-1")}})
	if err != nil {
		t.Fatal(err)
	}
	c := res.Cells[0]
	if c.Verdict != journal.VerdictNoOp {
		t.Fatalf("verdict %q, want no_op (AC-3, engine-graded)", c.Verdict)
	}
	if c.Recovery != "not_measured" {
		t.Errorf("recovery %q, want not_measured (nothing landed ⇒ nothing to recover)", c.Recovery)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("no_op filed %d findings, want 1", len(res.Findings))
	}
	if res.Findings[0].Priority != "P2" {
		t.Errorf("no_op finding priority %q, want P2", res.Findings[0].Priority)
	}
}

// TestRunGradesNotRecoveredThroughTheEngine: a landed proof whose revert
// never proved grades not_recovered by the engine's closure rule (rule 7)
// — the finding is P1.
func TestRunGradesNotRecoveredThroughTheEngine(t *testing.T) {
	rn := stubRunner(stubCat("Z-1"), map[string]selftest.Result{
		"Z-1": {ID: "Z-1", State: "fail", Reason: "inverse did not prove (AC-4): state stayed T", LandProof: "landed: proof"},
	})
	res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "Z-1")}})
	if err != nil {
		t.Fatal(err)
	}
	c := res.Cells[0]
	if c.Verdict != journal.VerdictNotRecovered {
		t.Fatalf("verdict %q, want not_recovered", c.Verdict)
	}
	if c.Recovery != "not_recovered" {
		t.Errorf("recovery %q, want not_recovered", c.Recovery)
	}
	if len(res.Findings) != 1 || res.Findings[0].Priority != "P1" {
		t.Fatalf("findings %+v, want one P1", res.Findings)
	}
}

// TestRunGradesCorruptedThroughTheEngine: land + revert proofs fired but
// the pre-state drifted — the integrity oracle reports the drift and rule
// 6 grades corrupted, P0.
func TestRunGradesCorruptedThroughTheEngine(t *testing.T) {
	rn := stubRunner(stubCat("Z-1"), map[string]selftest.Result{
		"Z-1": {ID: "Z-1", State: "fail", Reason: "revert measured but pre-state drifted (AC-4): byte 3", LandProof: "landed: proof", RevertProof: "reverted: proof"},
	})
	res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "Z-1")}})
	if err != nil {
		t.Fatal(err)
	}
	c := res.Cells[0]
	if c.Verdict != journal.VerdictCorrupted {
		t.Fatalf("verdict %q, want corrupted (oracle-reported loss)", c.Verdict)
	}
	if c.Recovery != "not_recovered" {
		t.Errorf("recovery %q, want not_recovered", c.Recovery)
	}
	if len(res.Findings) != 1 || res.Findings[0].Priority != "P0" {
		t.Fatalf("findings %+v, want one P0", res.Findings)
	}
}

// stubDetector is a Detector seam stub.
type stubDetector struct {
	name string
	ok   bool
	err  error
}

func (d *stubDetector) Name() string { return d.name }
func (d *stubDetector) Assert(time.Duration) (bool, error) {
	return d.ok, d.err
}

// TestDetectionAxisDetectedAndGap (the AC-14 seam, both live arms): a
// detector that sees the record records "detected"; one that does not
// records "gap" WITH the reason; a detector error records "error".
func TestDetectionAxisDetectedAndGap(t *testing.T) {
	rn := stubRunner(stubCat("P-001"), map[string]selftest.Result{"P-001": stubPass})
	rn.Detectors = map[string]Detector{
		"c1": &stubDetector{name: "trouble-ledger", ok: true},
		"c2": &stubDetector{name: "trouble-ledger", ok: false},
		"c3": &stubDetector{name: "trouble-ledger", err: errors.New("ledger read: connection refused")},
	}
	res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "P-001"), cell("c2", "P-001"), cell("c3", "P-001")}})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Cells[0].Detection.State; got != "detected" {
		t.Errorf("c1 detection %q, want detected", got)
	}
	gap := res.Cells[1].Detection
	if gap.State != "gap" || gap.Reason == "" {
		t.Errorf("c2 detection = %+v, want gap WITH reason (an unexplained null is junk)", gap)
	}
	if e := res.Cells[2].Detection; e.State != "error" || !strings.Contains(e.Reason, "connection refused") {
		t.Errorf("c3 detection = %+v, want error naming the failure", e)
	}
}

// TestRunRefusals: unknown primitive, non-scratch target, unknown
// selector kind, duplicate cell id, empty matrix — every refusal returns
// before anything executes.
func TestRunRefusals(t *testing.T) {
	rn := stubRunner(stubCat("P-001"), nil)
	if _, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "ZZZ")}}); !isUnknownPrimitive(err) {
		t.Errorf("unknown primitive: err=%v, want ErrUnknownPrimitive", err)
	}
	ns := cell("c1", "P-001")
	ns.Target.Scratch = false
	if _, err := rn.Run(&Matrix{Cells: []Cell{ns}}); !isNonScratch(err) {
		t.Errorf("non-scratch: err=%v, want ErrNonScratchTarget", err)
	}
	bad := cell("c1", "P-001")
	bad.Target.Selector = "vm:/the-cloud"
	if _, err := rn.Run(&Matrix{Cells: []Cell{bad}}); !isUnknownKind(err) {
		t.Errorf("unknown kind: err=%v, want ErrUnknownTargetKind", err)
	}
	if _, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "P-001"), cell("c1", "P-001")}}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("duplicate id: err=%v, want duplicate refusal", err)
	}
	if _, err := rn.Run(&Matrix{}); err == nil || !strings.Contains(err.Error(), "empty matrix") {
		t.Errorf("empty matrix: err=%v, want empty-matrix refusal", err)
	}
}

func isUnknownPrimitive(err error) bool {
	var e *ErrUnknownPrimitive
	return errors.As(err, &e)
}
func isNonScratch(err error) bool {
	var e *ErrNonScratchTarget
	return errors.As(err, &e)
}
func isUnknownKind(err error) bool {
	var e *ErrUnknownTargetKind
	return errors.As(err, &e)
}

// TestJournalRecordsRoundTrip: the record set survives a journal write →
// read (the closed vocabulary is enforced at the write path; flaky
// aggregation surfaces on disagreement).
func TestJournalRecordsRoundTrip(t *testing.T) {
	rn := stubRunner(stubCat("P-001", "Z-1"), map[string]selftest.Result{
		"P-001": stubPass,
		"Z-1":   {ID: "Z-1", State: "fail", Reason: "land did not prove: counter stayed 0"},
	})
	res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "P-001"), cell("c2", "Z-1")}})
	if err != nil {
		t.Fatal(err)
	}
	runID := RunResultRunID(res)
	recs := res.JournalRecords(runID)
	if len(recs) == 0 {
		t.Fatal("no records")
	}
	// a disagreement (recovered vs no_op) aggregates flaky
	var last journal.Record
	for _, r := range recs {
		last = r
	}
	if last.Type != journal.RecordRunClosed {
		t.Fatalf("last record %s, want run_closed", last.Type)
	}
	if last.Verdict != journal.VerdictFlaky {
		t.Errorf("aggregate verdict %q, want flaky (disagreeing cell verdicts)", last.Verdict)
	}
	dir := t.TempDir()
	path, err := WriteJournal(dir, recs)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != len(recs) {
		t.Fatalf("journal lines %d, wrote %d records", len(lines), len(recs))
	}
	// every line must read back through the journal's own validation
	for i, ln := range lines {
		var rec journal.Record
		if err := rec.UnmarshalJSON([]byte(ln)); err != nil {
			t.Errorf("record %d failed the journal read-back validation: %v", i, err)
		}
	}
	// AC-18 evidence half: the journal path rides in the findings
	fb, err := FileFindings(res, runID, path)
	if err != nil || len(fb) == 0 {
		t.Fatalf("findings bytes: %v (%d bytes)", err, len(fb))
	}
	if !strings.Contains(string(fb), path) {
		t.Errorf("findings JSONL does not carry the journal path %q", path)
	}
}

// TestUnanimousAggregate: all-agreeing cells aggregate to the common
// verdict (not flaky).
func TestUnanimousAggregate(t *testing.T) {
	rn := stubRunner(stubCat("P-001"), map[string]selftest.Result{"P-001": stubPass})
	res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "P-001"), cell("c2", "P-001")}})
	if err != nil {
		t.Fatal(err)
	}
	recs := res.JournalRecords("run123")
	last := recs[len(recs)-1]
	if last.Verdict != journal.VerdictRecovered {
		t.Errorf("unanimous aggregate %q, want recovered", last.Verdict)
	}
}

// TestTimelineCarriesMeasuredDuration: the graded timeline stamps the
// measured loop duration into the verdict (t_landed), and the artefact
// renders it.
func TestTimelineCarriesMeasuredDuration(t *testing.T) {
	rn := stubRunner(stubCat("P-001"), map[string]selftest.Result{"P-001": stubPass})
	res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "P-001")}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Cells[0].Timeline.Landed < 0 {
		t.Errorf("t_landed %s is negative (an insane timeline voids the run)", res.Cells[0].Timeline.Landed)
	}
	if err := res.Cells[0].Timeline.Validate(); err != nil {
		t.Errorf("timeline invalid: %v", err)
	}
}

// TestFindingIDsAndProjects: findings carry the owning project and a
// stable id derived from the cell; the parity cells name their legacy
// cell in the row.
func TestFindingIDsAndProjects(t *testing.T) {
	rn := stubRunner(stubCat("Z-1"), map[string]selftest.Result{
		"Z-1": {ID: "Z-1", State: "fail", Reason: "inverse did not prove: state stayed T", LandProof: "landed"},
	})
	c := cell("q7", "Z-1")
	c.LegacyCell = "chaos-shutdown"
	res, err := rn.Run(&Matrix{Cells: []Cell{c}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("findings %d, want 1", len(res.Findings))
	}
	f := res.Findings[0]
	if f.ID != "MSF-BAT-q7" || f.Project != "scratch" || f.LegacyCell != "chaos-shutdown" {
		t.Errorf("finding = %+v", f)
	}
}

// TestArtefactRoundTripFile (write + re-read): the artefact writes
// matrix.json + matrix.md under the dir; the JSON re-reads into the same
// cell count and carries the parity table on a quick run.
func TestArtefactRoundTripFile(t *testing.T) {
	cat, err := catalog.LoadDefault()
	if err != nil {
		t.Fatalf("catalog load: %v", err)
	}
	rn := stubRunner(cat, map[string]selftest.Result{
		"N-012": stubPass,
		"I-002": stubSkip,
		"F-009": {ID: "F-009", State: "fail", Reason: "revert measured but pre-state drifted: byte 3", LandProof: "landed", RevertProof: "reverted"},
		"R-001": stubPass,
	})
	res, err := rn.Run(QuickMatrix())
	if err != nil {
		t.Fatal(err)
	}
	a := BuildArtefact(res, "runabc", "content-v1:test", 1700000000)
	dir := t.TempDir()
	jp, err := WriteArtefact(dir, a)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(jp) != "matrix.json" {
		t.Errorf("artefact path %q", jp)
	}
	if _, err := os.Stat(filepath.Join(dir, "matrix.md")); err != nil {
		t.Errorf("markdown half missing: %v", err)
	}
	b, err := os.ReadFile(jp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "mischief.battery.matrix/v1") {
		t.Errorf("artefact missing schema tag")
	}
	if !strings.Contains(string(b), "chaos-errorpath") {
		t.Errorf("quick artefact missing the parity table")
	}
	if !strings.Contains(string(b), notWired) {
		t.Errorf("artefact missing the detection note (the AC-14 posture is stated once, honestly)")
	}
}

var _ = verdict.Timeline{} // keep the import honest if branches change
