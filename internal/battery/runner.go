package battery

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/trouble-agent/mischief/internal/catalog"
	"github.com/trouble-agent/mischief/internal/journal"
	"github.com/trouble-agent/mischief/internal/selftest"
	"github.com/trouble-agent/mischief/internal/verdict"
)

// Detector is the AC-14 detection-assertion seam. A cell's declared
// observer (today: trouble's incident ledger; the real ledger integration
// is FUTURE WORK) answers one question: did the expected record appear
// within the declared window? The runner records the answer as the cell's
// detection entry — including the reason when no detector is wired — and
// never infers detection from anything else.
//
// The v0.1 build wires NO real detector. A nil Detector on a cell is the
// honest "not wired" state and is recorded as such (NotWired, with the
// reason naming that the trouble integration is future work).
type Detector interface {
	// Name identifies the observer ("trouble-ledger").
	Name() string
	// Assert answers whether the expected record appeared within the
	// window. ok=false with err=nil means "checked, record absent within
	// the window" (a detection gap, AC-14); err != nil means the check
	// itself could not run.
	Assert(window time.Duration) (ok bool, err error)
}

// DetectionEntry is one cell's recorded detection outcome (the
// detection axis of the matrix artefact).
type DetectionEntry struct {
	// Observer is the detector that was consulted ("" when none wired).
	Observer string `json:"observer,omitempty"`
	// State: "detected" (the record appeared in-window), "gap" (checked,
	// record absent — the AC-14 detection gap), "not_wired" (no detector
	// on this cell), "error" (the check itself failed).
	State string `json:"state"`
	// Reason carries the gap detail / not-wired reason / error text. A
	// null axis always carries a reason (Bane's null-with-reason law).
	Reason string `json:"reason"`
	// Within is the measured time from land to the observed record, when
	// detected.
	Within string `json:"within,omitempty"`
}

// notWired is the reason text a nil-detector cell records. One string,
// quoted by tests, so the wording is part of the contract.
const notWired = "no detector wired on this cell: the trouble-ledger integration is future work (the Detector seam is the AC-14 assertion point)"

// recoveryEntry derives one cell's recovery axis from the graded verdict
// (the SPEC-05 vocabulary decides; nothing is string-mapped here that the
// engine did not decide). The rule:
//
//	recovered / degraded  → "recovered" (the probe set came back within
//	                        budget; degraded says so with a cost)
//	not_recovered / hung  → "not_recovered"
//	corrupted             → "not_recovered" (loss graded before recovery)
//	no_op                 → "not_measured" (nothing landed, so there was
//	                        nothing to recover — measured, not assumed)
//	aborted               → "refused" (rails refused before arming)
//	void                  → "not_measured" (the control was red: the
//	                        harness, not the target, is the finding)
//	flaky                 → "flaky" (N runs disagree — recovery itself
//	                        was unstable)
func recoveryEntry(v journal.Verdict) string {
	switch v {
	case journal.VerdictRecovered, journal.VerdictDegraded:
		return "recovered"
	case journal.VerdictNotRecovered, journal.VerdictHung, journal.VerdictCorrupted:
		return "not_recovered"
	case journal.VerdictNoOp, journal.VerdictVoid:
		return "not_measured"
	case journal.VerdictAborted:
		return "refused"
	case journal.VerdictFlaky:
		return "flaky"
	default:
		return "not_measured"
	}
}

// CellResult is one executed matrix cell.
type CellResult struct {
	// Cell is the executed cell.
	Cell Cell `json:"cell"`
	// Verdict is the SPEC-05 graded verdict (the closed journal
	// vocabulary value).
	Verdict journal.Verdict `json:"verdict"`
	// Reason is the engine's deciding rule, verbatim.
	Reason string `json:"reason"`
	// LandProof / RevertProof are the measured proofs ("" when the step
	// never ran, e.g. a capability skip).
	LandProof   string `json:"land_proof,omitempty"`
	RevertProof string `json:"revert_proof,omitempty"`
	// SelftestState is the selftest result state this cell's execution
	// produced: "pass" | "skip" | "fail" (the land+revert harness's own
	// vocabulary; a skip is recorded, never silently a pass).
	SelftestState string `json:"selftest_state"`
	// SkipReason names the missing piece when SelftestState == "skip"
	// (AC-11 shape).
	SkipReason string `json:"skip_reason,omitempty"`
	// FaultName is the primitive's human name from the catalog ("" when
	// the runner had no catalog — a stub-runner test).
	FaultName string `json:"fault_name,omitempty"`
	// Detection is the cell's detection entry (AC-14).
	Detection DetectionEntry `json:"detection"`
	// Recovery is the cell's recovery entry (derived from the verdict by
	// the table above).
	Recovery string `json:"recovery"`
	// Duration is the measured wall time of the cell's execute loop.
	Duration time.Duration `json:"-"`
	// Timeline is the four-timing model stamped on the graded result.
	Timeline verdict.Timeline `json:"timeline"`
}

// RunResult is one battery run: the matrix, its cell results, and the
// file-findings derived from it.
type RunResult struct {
	// Quick is true when the run executed the parity matrix.
	Quick bool `json:"quick"`
	// Matrix is the executed matrix (cells with their legacy annotations).
	Matrix *Matrix `json:"-"`
	// Cells is the per-cell outcome, in matrix order.
	Cells []CellResult `json:"cells"`
	// Findings is the finding set derived from the cell outcomes (the
	// board-row feed; empty when nothing found).
	Findings []Finding `json:"findings,omitempty"`
	// RunID is the content-derived run id, stamped by the runner's
	// caller (cmd: battery.RunResultRunID) BEFORE findings are rendered
	// or the artefact is built — the AC-9 correlation key every hop
	// (journal record, CLI output, findings row) must carry (MSF-031).
	RunID string `json:"run_id,omitempty"`
	// StartedAt / FinishedAt are unix seconds.
	StartedAt  int64 `json:"started_at"`
	FinishedAt int64 `json:"finished_at"`
}

// Counts renders the outcome census line.
func (r *RunResult) Counts() string {
	pass, skip, fail := 0, 0, 0
	for _, c := range r.Cells {
		switch c.SelftestState {
		case "pass":
			pass++
		case "skip":
			skip++
		default:
			fail++
		}
	}
	return fmt.Sprintf("battery: %d pass, %d skip, %d fail (of %d cells)", pass, skip, fail, len(r.Cells))
}

// AggregateVerdict returns the run's aggregate verdict (verdict.Aggregate
// over the per-cell results: unanimous → the common verdict,
// disagreement → flaky, zero cells → void) — the SAME value the journal's
// verdict/run_closed records carry, exposed so the CLI hop can print the
// aggregate verdict with the run id (AC-9: the aggregate value survives
// to the output, MSF-031).
func (r *RunResult) AggregateVerdict() journal.Verdict {
	graded := make([]verdict.Result, 0, len(r.Cells))
	for i := range r.Cells {
		graded = append(graded, verdict.Result{
			Verdict:  r.Cells[i].Verdict,
			Reason:   r.Cells[i].Reason,
			Timeline: r.Cells[i].Timeline,
		})
	}
	return verdict.Aggregate(graded).Verdict
}

// findingVerdicts is the set of verdicts that file a finding (a finding is
// a measured adverse outcome, never a pass and never a clean skip).
func findingVerdict(v journal.Verdict) bool {
	switch v {
	case journal.VerdictRecovered:
		return false
	default:
		return true
	}
}

// deriveFindings converts failed verdicts into findings (AC-18's feed).
// A clean skip (capability_unavailable) does NOT file a finding — it is a
// recorded unavailable actuator, not an adverse outcome — but a FAIL does,
// and so does every adverse verdict on a cell that ran.
func (r *RunResult) deriveFindings() {
	for i := range r.Cells {
		c := &r.Cells[i]
		switch {
		case c.SelftestState == "skip":
			// recorded skip: not a finding, the artefact carries it
			continue
		case c.Verdict == journal.VerdictRecovered:
			continue
		case !findingVerdict(c.Verdict):
			continue
		}
		r.Findings = append(r.Findings, Finding{
			ID: fmt.Sprintf("MSF-BAT-%s", c.Cell.ID),
			// RunID is the run id stamped later by FileFindings (the
			// id is derived after the run; the filer renders at emit
			// time when it is known) — AC-9's correlation key.
			RunID:    r.RunID,
			Priority: priorityFor(c.Verdict),
			Title: fmt.Sprintf("battery finding: %s on %s (%s) — %s",
				c.Cell.Primitive, c.Cell.Target.Project, c.Verdict, firstLine(c.Reason)),
			Status:     "pending",
			DependsOn:  []string{},
			Project:    c.Cell.Target.Project,
			Primitive:  c.Cell.Primitive,
			LegacyCell: c.Cell.LegacyCell,
			Verdict:    c.Verdict.String(),
			Reason:     c.Reason,
			// JournalPath is filled by the runner after the journal write
			// (AC-18: the journal path rides in the row's reasoning).
		})
	}
}

// priorityFor maps a verdict to the mischief board's P0..P3 scale: data
// loss and hangs are P0 (an outage shape), a never-landed fault is P2 (the
// finding is about coverage, not the product), everything else adverse is
// P1.
func priorityFor(v journal.Verdict) string {
	switch v {
	case journal.VerdictCorrupted, journal.VerdictHung:
		return "P0"
	case journal.VerdictNoOp, journal.VerdictVoid:
		return "P2"
	case journal.VerdictAborted:
		return "P3"
	default:
		return "P1"
	}
}

// firstLine returns the first line of s, trimmed (finding titles stay
// one line).
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// Runner executes a matrix. The zero value is not usable; NewRunner wires
// the real dependencies (tests inject seams).
type Runner struct {
	// Cat is the loaded catalog (matrix primitive ids resolve against it).
	Cat *catalog.Catalog
	// Detectors maps cell id → the wired detector (nil map / nil entry =
	// not wired; the honest recorded state).
	Detectors map[string]Detector
	// executeCell is the seam that executes one cell's land+prove+revert
	// loop. Production wires the selftest runner's single-primitive path
	// (the SAME harness — no second actuator); tests inject a stub.
	executeCell func(c Cell) selftest.Result
	// now is the clock seam.
	now func() time.Time
}

// NewRunner wires the production runner: the selftest harness (RunOnePublic)
// as the per-cell executor, the wall clock.
func NewRunner(cat *catalog.Catalog) *Runner {
	return &Runner{
		Cat:         cat,
		executeCell: func(c Cell) selftest.Result { return selftest.RunOnePublic(c.Primitive) },
		now:         time.Now,
	}
}

// ErrUnknownPrimitive is the refusal for a matrix cell whose primitive is
// not in the loaded catalog (rails unknown-primitive shape).
type ErrUnknownPrimitive struct{ ID string }

func (e *ErrUnknownPrimitive) Error() string {
	return fmt.Sprintf("primitive %q is not in the loaded catalog", e.ID)
}

// ErrNonScratchTarget is the M1 refusal for a matrix target that does not
// declare scratch (NOT-LIST: no non-scratch targets at M1).
type ErrNonScratchTarget struct{ Cell, Selector string }

func (e *ErrNonScratchTarget) Error() string {
	return fmt.Sprintf("cell %s: target %q does not declare scratch — M1 executes on L0 scratch targets only", e.Cell, e.Selector)
}

// ErrUnknownTargetKind is the refusal for a selector whose kind prefix is
// not one of the rails' five target classes.
type ErrUnknownTargetKind struct{ Cell, Selector string }

func (e *ErrUnknownTargetKind) Error() string {
	return fmt.Sprintf("cell %s: selector %q does not name a known target kind (process|cgroup|container|host|cloud)", e.Cell, e.Selector)
}

// knownTargetKind mirrors plan.kindFor's prefix table (the rails' five
// classes). The battery resolves nothing at M1 — it REFUSES a selector
// outside the known classes so a typo'd selector cannot silently become
// "host".
func knownTargetKind(selector string) bool {
	for _, p := range []string{"process:", "pid:", "cgroup:", "container:", "host:", "cloud:"} {
		if strings.HasPrefix(selector, p) {
			return true
		}
	}
	return false
}

// Run executes the matrix. Validation refusals (unknown primitive,
// non-scratch target, unknown selector kind, empty matrix) return before
// anything executes — a matrix that validates partially is refused whole.
func (rn *Runner) Run(m *Matrix) (*RunResult, error) {
	if rn.executeCell == nil || rn.now == nil {
		return nil, fmt.Errorf("battery runner misconfigured: nil seam")
	}
	if m == nil || len(m.Cells) == 0 {
		return nil, fmt.Errorf("empty matrix: a battery with no cells refuses (nothing to measure is not a pass)")
	}
	seen := map[string]bool{}
	for _, c := range m.Cells {
		if rn.Cat != nil && rn.Cat.Get(c.Primitive) == nil {
			return nil, &ErrUnknownPrimitive{ID: c.Primitive}
		}
		if !c.Target.Scratch {
			return nil, &ErrNonScratchTarget{Cell: c.ID, Selector: c.Target.Selector}
		}
		if !knownTargetKind(c.Target.Selector) {
			return nil, &ErrUnknownTargetKind{Cell: c.ID, Selector: c.Target.Selector}
		}
		if seen[c.ID] {
			return nil, fmt.Errorf("duplicate cell id %q", c.ID)
		}
		seen[c.ID] = true
	}

	res := &RunResult{Quick: m.Quick, Matrix: m, StartedAt: rn.now().Unix()}
	for _, c := range m.Cells {
		start := rn.now()
		st := rn.executeCell(c)
		dur := rn.now().Sub(start)

		cr := CellResult{
			Cell:          c,
			SelftestState: st.State,
			LandProof:     st.LandProof,
			RevertProof:   st.RevertProof,
			SkipReason:    st.Reason,
			Duration:      dur,
		}
		if d := rn.Cat.Get(c.Primitive); rn.Cat != nil && d != nil {
			cr.FaultName = d.Name
		}
		switch st.State {
		case "pass":
			// Both proofs measured: grade through the SPEC-05 engine on
			// the measured evidence. Control: the harness's own pass IS
			// the green control (the pre/post byte-compare ran inside the
			// loop); the baseline and post probe samples are the proofs
			// themselves, recorded as samples so Grade grades MEASURED
			// evidence, not an assertion.
			cr.Verdict, cr.Reason, cr.Timeline = gradePass(st)
			cr.Recovery = recoveryEntry(cr.Verdict)
			cr.Detection = rn.detect(c)
		case "skip":
			// capability_unavailable, recorded (AC-11): grades aborted —
			// the rails' verdict for "this run did not happen" — and the
			// recovery/detection axes carry their reasons.
			cr.Verdict = journal.VerdictAborted
			cr.Reason = "capability_unavailable: " + st.Reason
			cr.Recovery = recoveryEntry(cr.Verdict)
			cr.Detection = DetectionEntry{State: "not_wired", Reason: notWired}
		default: // "fail"
			// The land+revert loop measured a defect. Grade through the
			// engine with the measured failure shape: a landing that did
			// not prove is no_op (AC-3); a proof without a revert is
			// not_recovered (closure rule 1); a drift-after-revert is
			// corrupted.
			cr.Verdict, cr.Reason, cr.Timeline = gradeFail(st)
			cr.Recovery = recoveryEntry(cr.Verdict)
			cr.Detection = rn.detect(c)
		}
		res.Cells = append(res.Cells, cr)
	}
	res.FinishedAt = rn.now().Unix()
	// The run id is a pure function of the finished result — the runner
	// derives it once here so EVERY consumer (journal records, the CLI's
	// verdict line, findings rows) reads the same value off the result
	// instead of re-deriving or forgetting (AC-9's correlation key,
	// MSF-031).
	res.RunID = RunResultRunID(res)
	res.deriveFindings()
	return res, nil
}

// detect consults the cell's wired detector, if any, and records the
// outcome — including the not-wired reason when none is wired. The AC-14
// seam in one place.
func (rn *Runner) detect(c Cell) DetectionEntry {
	if rn.Detectors == nil || rn.Detectors[c.ID] == nil {
		return DetectionEntry{State: "not_wired", Reason: notWired}
	}
	d := rn.Detectors[c.ID]
	ok, err := d.Assert(detectWindow)
	switch {
	case err != nil:
		return DetectionEntry{State: "error", Reason: err.Error(), Observer: d.Name()}
	case ok:
		return DetectionEntry{State: "detected", Reason: "", Observer: d.Name(), Within: detectWindow.String()}
	default:
		return DetectionEntry{State: "gap", Reason: fmt.Sprintf("observer %q: the expected record did not appear within %s", d.Name(), detectWindow), Observer: d.Name()}
	}
}

// detectWindow is the declared AC-14 window for a wired detector at M1.
const detectWindow = 60 * time.Second

// proofProbe is the single out-of-band probe the battery declares per cell:
// its observed value is the measured proof text, its OK flag whether the
// proof fired. Evidence collection keeps to ONE probe so the engine's
// baseline rule (every declared probe answered green before the fault) and
// its post-revert probe track grade the SAME set the harness measured.
func proofProbe(name string) verdict.Probe { return verdict.Probe{Name: name} }

func proofSample(name, proof string) verdict.Sample {
	return verdict.Sample{Probe: proofProbe(name), OK: proof != "", Observed: proof}
}

// gradePass grades a measured pass through the SPEC-05 engine. The
// land+revert proofs become the evidence (collected, not asserted); Grade
// decides the verdict. The declared integrity oracle is the harness's own
// pre/post byte-compare — declared so AC-15 does not refuse recovered, and
// enforceable: a drifted compare never reaches this path (gradeFail's
// oracle arm), and a verdict record without the oracle would be an AC-15
// refusal by the engine's own rule.
func gradePass(st selftest.Result) (journal.Verdict, string, verdict.Timeline) {
	tl := verdict.Timeline{Landed: st.Duration}
	probe := proofProbe("land-and-revert-proof")
	green := verdict.Sample{Probe: probe, OK: true, Observed: "land: " + st.LandProof + " | revert: " + st.RevertProof}
	ev := verdict.Evidence{
		Landed:   st.LandProof != "",
		Reverted: st.RevertProof != "",
		Timeline: tl,
		Baseline: []verdict.Sample{green},
		Post:     []verdict.Sample{green},
	}
	plan := verdict.Plan{
		Probes:         []verdict.Probe{probe},
		IntegrityNames: []string{"prestate-byte-compare"},
		ControlGreen:   true,
	}
	res := verdict.Grade(plan, ev)
	return res.Verdict, res.Reason, res.Timeline
}

// gradeFail grades a measured harness/cell failure through the SPEC-05
// engine, mapping the measured shape to the evidence shape the engine's
// closure rules consume (the engine decides the verdict — no string map):
//
//   - never landed (empty land proof on a run loop)      → no_op (AC-3)
//   - landed, revert proof never fired                   → not_recovered
//   - landed, reverted, but the pre-state drifted        → corrupted
//
// A skip is not a fail and never reaches this path.
func gradeFail(st selftest.Result) (journal.Verdict, string, verdict.Timeline) {
	tl := verdict.Timeline{Landed: st.Duration}
	probe := proofProbe("land-and-revert-proof")
	plan := verdict.Plan{
		Probes:         []verdict.Probe{probe},
		IntegrityNames: []string{"prestate-byte-compare"},
		ControlGreen:   true,
	}
	// The pre-fault baseline was green (prepare succeeded — a failed
	// prepare never reaches the land step): the observed value names what
	// was measured.
	baseline := verdict.Sample{Probe: probe, OK: true, Observed: "prepare: pre-state captured"}
	if st.LandProof == "" {
		ev := verdict.Evidence{Timeline: tl, Baseline: []verdict.Sample{baseline}}
		res := verdict.Grade(plan, ev)
		return res.Verdict, fmt.Sprintf("cell failed: %s (%s)", st.Reason, res.Reason), res.Timeline
	}
	if st.RevertProof == "" {
		// Landed but the revert never proved: rule 7 grades
		// not_recovered; the post sample records the dead probe.
		ev := verdict.Evidence{
			Timeline: tl,
			Landed:   true,
			Reverted: false,
			Baseline: []verdict.Sample{baseline},
			Post:     []verdict.Sample{proofSample(probe.Name, "")},
		}
		res := verdict.Grade(plan, ev)
		return res.Verdict, fmt.Sprintf("cell failed: %s (%s)", st.Reason, res.Reason), res.Timeline
	}
	// Both proofs fired but the harness graded fail: the pre-state drifted
	// (the drift detail is st.Reason). The integrity oracle REPORTS the
	// drift and rule 6 grades corrupted before recovery is considered.
	ev := verdict.Evidence{
		Timeline: tl,
		Landed:   true,
		Reverted: true,
		Baseline: []verdict.Sample{baseline},
		Post:     []verdict.Sample{proofSample(probe.Name, st.RevertProof)},
		Oracles:  []verdict.OracleReport{{Name: "prestate-byte-compare", Err: fmt.Errorf("%s", st.Reason)}},
	}
	res := verdict.Grade(plan, ev)
	return res.Verdict, fmt.Sprintf("cell failed: %s (%s)", st.Reason, res.Reason), res.Timeline
}

// JournalRecords renders the run's journal record set (the
// internal/journal format, SPEC-02's lifecycle vocabulary):
//
//	run_planned     — once: kind battery, quick flag, cell ids
//	fault_landed    — per cell with a measured land proof
//	fault_reverted  — per cell with a measured revert proof (AC-4)
//	verdict         — once: the run's aggregate (verdict.Aggregate over
//	                  per-cell results: unanimous → the common verdict,
//	                  disagreement → flaky)
//	run_closed      — once: the counts
func (r *RunResult) JournalRecords(runID string) []journal.Record {
	now := time.Now().Unix()
	recs := make([]journal.Record, 0, 2*len(r.Cells)+3)
	seq := 0
	add := func(rec *journal.Record) {
		rec.RunID, rec.Seq, rec.TS = runID, seq, now
		seq++
		recs = append(recs, *rec)
	}

	cellIDs := make([]string, 0, len(r.Cells))
	for _, c := range r.Cells {
		cellIDs = append(cellIDs, c.Cell.ID)
	}
	add(&journal.Record{Type: journal.RecordRunPlanned, Fields: map[string]string{
		"kind":     "battery",
		"quick":    fmt.Sprintf("%t", r.Quick),
		"cells":    strings.Join(cellIDs, ","),
		"projects": strings.Join(r.Projects(), ","),
	}})

	var graded []verdict.Result
	for i := range r.Cells {
		c := &r.Cells[i]
		if c.LandProof != "" {
			add(&journal.Record{Type: journal.RecordFaultLanded, Fields: map[string]string{
				"cell": c.Cell.ID, "primitive": c.Cell.Primitive, "proof": c.LandProof,
			}})
		}
		if c.RevertProof != "" {
			add(&journal.Record{Type: journal.RecordFaultReverted, Fields: map[string]string{
				"cell": c.Cell.ID, "primitive": c.Cell.Primitive, "proof": c.RevertProof,
			}})
		}
		graded = append(graded, verdict.Result{
			Verdict:  c.Verdict,
			Reason:   c.Reason,
			Timeline: c.Timeline,
		})
	}

	agg := verdict.Aggregate(graded)
	add(&journal.Record{Type: journal.RecordVerdict, Verdict: agg.Verdict,
		Fields: map[string]string{"reason": agg.Reason, "cells": fmt.Sprintf("%d", len(r.Cells))}})
	add(&journal.Record{Type: journal.RecordRunClosed, Verdict: agg.Verdict,
		Fields: map[string]string{"kind": "battery", "findings": fmt.Sprintf("%d", len(r.Findings)), "cells": fmt.Sprintf("%d", len(r.Cells))}})
	return recs
}

// Projects returns the matrix's distinct target projects, sorted.
func (r *RunResult) Projects() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range r.Cells {
		if !seen[c.Cell.Target.Project] {
			seen[c.Cell.Target.Project] = true
			out = append(out, c.Cell.Target.Project)
		}
	}
	sort.Strings(out)
	return out
}

// RunID derives the battery run's content id (the journal.RunID family:
// hex(sha256("seed\\n"+seed+"\\n"+bytes))[:16]) over the canonical cell
// list. Same cell set and outcomes ⇒ same id (reproducibility); the id
// distinguishes this run's journal in the run dir.
func RunResultRunID(r *RunResult) string {
	var b strings.Builder
	for _, c := range r.Cells {
		fmt.Fprintf(&b, "%s\x1f%s\x1f%s\x1f%s\n", c.Cell.ID, c.Cell.Primitive, c.Cell.Target.Project, c.Verdict)
	}
	return journal.RunID([]byte(b.String()), r.StartedAt)
}
