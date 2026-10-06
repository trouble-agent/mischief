package battery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/journal"
	"github.com/trouble-agent/mischief/internal/selftest"
)

// ac9_test.go — AC-9 completion (board row MSF-031): a fault run's verdict
// value must survive all three hops IDENTICALLY —
//
//	journal record → CLI output → findings row,
//
// with the run id visible on every hop so the three records correlate.
// The green-but-meaningless shapes (no_op, corrupted, flaky) are
// explicitly in the sweep: a run that did nothing cannot grade as
// recovered.
//
// Hop boundaries the M1 harness honestly has (asserted here, not
// papered over):
//
//   - the battery's pass shape declares no recovery budget, so
//     degraded is unreachable through the runner — the value's hop is
//     pinned engine-side (verdict/journal packages) and in the findings
//     priority table (TestAC9FindingsPriorityTable);
//   - a recorded skip (aborted) is NOT a finding by contract (a
//     capability_unavailable is a recorded unavailable actuator, not an
//     adverse outcome) — its journal hop is asserted, the findings hop
//     is asserted ABSENT;
//   - void/hung are engine-graded shapes the selftest harness cannot
//     produce (the harness's own control is the loop's byte-compare);
//     their findings hop is the priority table.

// ac9FindingsRows reads an emitted findings.jsonl into row maps (the
// board's read side of the AC-18 wire format).
func ac9FindingsRows(t *testing.T, path string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("findings: %v", err)
	}
	var rows []map[string]any
	for i, ln := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("findings row %d: %v (%s)", i, err, ln)
		}
		rows = append(rows, m)
	}
	return rows
}

// ac9Str reads a string field off a findings row, failing with the row
// when the field is absent or not a string.
func ac9Str(t *testing.T, row map[string]any, field string) string {
	t.Helper()
	v, ok := row[field].(string)
	if !ok {
		t.Fatalf("findings row missing string field %q: %v", field, row)
	}
	return v
}

// ac9JournalRecords writes recs through the real write path (the
// validate + scrub gate) and reads every line back through the journal's
// own UnmarshalJSON validation.
func ac9JournalRecords(t *testing.T, recs []journal.Record) []journal.Record {
	t.Helper()
	dir := t.TempDir()
	path, err := WriteJournal(dir, recs)
	if err != nil {
		t.Fatalf("journal write: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("journal read: %v", err)
	}
	var back []journal.Record
	for i, ln := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		var r journal.Record
		if err := r.UnmarshalJSON([]byte(ln)); err != nil {
			t.Fatalf("journal record %d failed the write-path validation: %v", i, err)
		}
		back = append(back, r)
	}
	return back
}

// ac9VerdictRecord returns the journal's verdict record.
func ac9VerdictRecord(t *testing.T, recs []journal.Record) journal.Record {
	t.Helper()
	for i := range recs {
		if recs[i].Type == journal.RecordVerdict {
			return recs[i]
		}
	}
	t.Fatal("no verdict record in the journal")
	return journal.Record{}
}

// TestAC9JournalAndFindingsHopsAgree (AC-9, hops 1 and 3): for every
// adverse verdict the battery grades per cell, the journal's verdict
// record and the findings row carry the SAME verdict value, and both
// carry the same run id. The shapes drive the engine's closure rules —
// the verdict is graded, never string-mapped:
//
//	not_recovered   landed, revert proof never fired (rule 7)
//	corrupted       landed+reverted, pre-state drifted (rule 6)
//	no_op           land never proved (rule 5, AC-3)
func TestAC9JournalAndFindingsHopsAgree(t *testing.T) {
	cases := []struct {
		name string
		id   string
		res  selftest.Result
		want journal.Verdict
	}{
		{
			name: "not_recovered via the revert rule",
			id:   "Z-N",
			res: selftest.Result{
				ID: "Z-N", State: "fail",
				Reason: "inverse did not prove: state stayed T", LandProof: "landed: proof",
			},
			want: journal.VerdictNotRecovered,
		},
		{
			name: "corrupted via the oracle rule",
			id:   "Z-C",
			res: selftest.Result{
				ID: "Z-C", State: "fail",
				Reason: "revert measured but pre-state drifted: byte 3", LandProof: "landed", RevertProof: "reverted",
			},
			want: journal.VerdictCorrupted,
		},
		{
			name: "no_op via the AC-3 rule",
			id:   "Z-O",
			res: selftest.Result{
				ID: "Z-O", State: "fail", Reason: "land did not prove: counter stayed 0",
			},
			want: journal.VerdictNoOp,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rn := stubRunner(stubCat(tc.id), map[string]selftest.Result{tc.id: tc.res})
			res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", tc.id)}})
			if err != nil {
				t.Fatal(err)
			}
			if got := res.Cells[0].Verdict; got != tc.want {
				t.Fatalf("shape graded %q, want %q (the proof shape must drive the engine there)", got, tc.want)
			}
			if len(res.Findings) != 1 {
				t.Fatalf("adverse verdict %s filed %d findings, want 1", tc.want, len(res.Findings))
			}

			runID := RunResultRunID(res)

			// Hop 1: the journal (real write path, validated read-back).
			recs := ac9JournalRecords(t, res.JournalRecords(runID))
			vrec := ac9VerdictRecord(t, recs)
			if vrec.Verdict != tc.want {
				t.Fatalf("AC-9 hop 1: journal verdict %q, want %q", vrec.Verdict, tc.want)
			}
			if vrec.RunID != runID {
				t.Fatalf("AC-9 hop 1: journal run_id %q, want %q", vrec.RunID, runID)
			}

			// Hop 3: the findings row (real writer, board read-back).
			dir := t.TempDir()
			fpath, n, err := WriteFindings(dir, res, runID, filepath.Join(dir, "battery.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Fatalf("findings rows %d, want 1", n)
			}
			rows := ac9FindingsRows(t, fpath)
			if ac9Str(t, rows[0], "verdict") != tc.want.String() {
				t.Fatalf("AC-9 hop 3: findings verdict %q, want %q", rows[0]["verdict"], tc.want)
			}
			if ac9Str(t, rows[0], "run_id") != runID {
				t.Fatalf("AC-9 hop 3: findings run_id %v (want %q) — a row without the run id cannot be correlated to its journal", rows[0]["run_id"], runID)
			}
			// The reasoning line still carries the journal path (AC-18).
			if !strings.Contains(ac9Str(t, rows[0], "reasoning"), "battery.jsonl") {
				t.Fatalf("findings row lost the AC-18 journal path: %v", rows[0]["reasoning"])
			}
		})
	}
}

// TestAC9AbortedSkipsJournalOnly (AC-9, the aborted shape): a capability
// skip grades aborted on the journal hop (the value survives), and files
// NO findings row by contract — a recorded unavailable actuator is not
// an adverse outcome. The CLI hop (the printed SKIP line carrying the
// verdict) is pinned at the cmd level.
func TestAC9AbortedSkipsJournalOnly(t *testing.T) {
	rn := stubRunner(stubCat("Z-A"), map[string]selftest.Result{
		"Z-A": {ID: "Z-A", State: "skip", Reason: "capability_unavailable: docker API absent"},
	})
	res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "Z-A")}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Cells[0].Verdict != journal.VerdictAborted {
		t.Fatalf("skip graded %q, want aborted", res.Cells[0].Verdict)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("a recorded skip filed %d findings, want 0 (not an adverse outcome)", len(res.Findings))
	}
	runID := RunResultRunID(res)
	recs := ac9JournalRecords(t, res.JournalRecords(runID))
	if vrec := ac9VerdictRecord(t, recs); vrec.Verdict != journal.VerdictAborted || vrec.RunID != runID {
		t.Fatalf("AC-9 aborted hop: journal verdict %q run_id %q, want aborted/%s", vrec.Verdict, vrec.RunID, runID)
	}
}

// TestAC9FlakyAggregateHops (AC-9, the flaky shape): two cells whose
// verdicts disagree aggregate to flaky on the journal hop (verdict +
// run_closed records AND the aggregate the CLI prints); the run files
// the adverse cell's row and the row stays the CELL's truth (its own
// verdict + the shared run id), never the aggregate's.
func TestAC9FlakyAggregateHops(t *testing.T) {
	rn := stubRunner(stubCat("P-001", "Z-1"), map[string]selftest.Result{
		"P-001": stubPass,
		"Z-1":   {ID: "Z-1", State: "fail", Reason: "land did not prove: counter stayed 0"},
	})
	res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "P-001"), cell("c2", "Z-1")}})
	if err != nil {
		t.Fatal(err)
	}
	runID := RunResultRunID(res)

	if agg := res.AggregateVerdict(); agg != journal.VerdictFlaky {
		t.Fatalf("AggregateVerdict() = %q, want flaky (disagreeing cells)", agg)
	}
	recs := ac9JournalRecords(t, res.JournalRecords(runID))

	var crec *journal.Record
	vrec := ac9VerdictRecord(t, recs)
	for i := range recs {
		if recs[i].Type == journal.RecordRunClosed {
			crec = &recs[i]
		}
	}
	if crec == nil {
		t.Fatal("missing run_closed record")
	}
	if vrec.Verdict != journal.VerdictFlaky || crec.Verdict != journal.VerdictFlaky {
		t.Fatalf("AC-9 flaky hop: verdict record %q / run_closed %q, want flaky", vrec.Verdict, crec.Verdict)
	}

	dir := t.TempDir()
	fpath, n, err := WriteFindings(dir, res, runID, "j.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	rows := ac9FindingsRows(t, fpath)
	if n != 1 || len(rows) != 1 {
		t.Fatalf("findings rows %d (read %d), want 1 (the no_op cell)", n, len(rows))
	}
	if ac9Str(t, rows[0], "verdict") != journal.VerdictNoOp.String() {
		t.Fatalf("findings row verdict %v, want the cell's own %s", rows[0]["verdict"], journal.VerdictNoOp)
	}
	if ac9Str(t, rows[0], "run_id") != runID {
		t.Fatalf("findings row run_id %v, want %q", rows[0]["run_id"], runID)
	}
}

// TestAC9FindingsPriorityTable: the findings priority is a pure function
// of the verdict — corrupted and hung P0, no_op and void P2, aborted P3,
// every other adverse verdict P1 — so every vocabulary value, including
// the shapes the M1 harness cannot produce live (void, hung, degraded),
// has a defined findings hop.
func TestAC9FindingsPriorityTable(t *testing.T) {
	for _, tc := range []struct {
		v    journal.Verdict
		want string
	}{
		{journal.VerdictCorrupted, "P0"},
		{journal.VerdictHung, "P0"},
		{journal.VerdictNotRecovered, "P1"},
		{journal.VerdictDegraded, "P1"},
		{journal.VerdictFlaky, "P1"},
		{journal.VerdictNoOp, "P2"},
		{journal.VerdictVoid, "P2"},
		{journal.VerdictAborted, "P3"},
	} {
		if got := priorityFor(tc.v); got != tc.want {
			t.Errorf("priorityFor(%s) = %q, want %q", tc.v, got, tc.want)
		}
	}
}
