package battery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/selftest"
)

// filing_test.go — the AC-18 half: findings render as board-vocabulary
// JSONL, the validate self-check accepts exactly the rows the filer emits
// (and refuses malformed ones), and a clean battery writes an EMPTY
// findings file (no findings ≠ not run).

func failingRun(t *testing.T) *RunResult {
	t.Helper()
	rn := stubRunner(stubCat("Z-1", "Z-2"), map[string]selftest.Result{
		"Z-1": {ID: "Z-1", State: "fail", Reason: "inverse did not prove: state stayed T", LandProof: "landed: proof"},
		"Z-2": {ID: "Z-2", State: "fail", Reason: "revert measured but pre-state drifted: byte 3", LandProof: "landed", RevertProof: "reverted"},
	})
	res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "Z-1"), cell("c2", "Z-2")}})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// TestFileFindingsBoardVocabulary: every emitted row carries the mischief
// board's fields (id, status, title, priority, depends_on) with values in
// the board's closed vocabularies, and passes the in-package validate
// (the boardctl-style self-check).
func TestFileFindingsBoardVocabulary(t *testing.T) {
	res := failingRun(t)
	b, err := FileFindings(res, res.RunID, "/run/dir/battery.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateRows(b); err != nil {
		t.Fatalf("the filer's own output failed the validator: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("rows %d, want 2 (one per failed cell)", len(lines))
	}
	// P0 (corrupted) sorts before P1 (not_recovered): the board diff a
	// reviewer reads is priority-first and reproducible
	if !strings.Contains(lines[0], `"priority":"P0"`) {
		t.Errorf("first row is not the P0: %s", lines[0])
	}
	for _, ln := range lines {
		for _, field := range []string{`"id":"MSF-BAT-`, `"status":"pending"`, `"title":"`, `"depends_on":[]`, `"reasoning":"battery run `} {
			if !strings.Contains(ln, field) {
				t.Errorf("row missing %s: %s", field, ln)
			}
		}
	}
	// AC-9 (MSF-031): every row carries the run id as its own field and
	// names it in the reasoning line — the correlation key the journal
	// records and the CLI output carry too.
	for _, ln := range lines {
		if !strings.Contains(ln, `"run_id":"`) {
			t.Errorf("row missing the AC-9 run_id field: %s", ln)
		}
		if !strings.Contains(ln, `journal: /run/dir/battery.jsonl`) {
			t.Errorf("row reasoning lost the AC-18 journal path: %s", ln)
		}
	}
}

// TestValidateRowsRefusals: the validator refuses each malformation by
// name — this is the self-check AC-18 is judged on.
func TestValidateRowsRefusals(t *testing.T) {
	good := `{"id":"MSF-BAT-x","status":"pending","title":"t","priority":"P1","depends_on":[]}` + "\n"
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"not json", "hello\n", "not a JSON object"},
		{"missing id", `{"status":"pending","title":"t","priority":"P1","depends_on":[]}` + "\n", "id: missing"},
		{"bad status", `{"id":"x","status":"done-ish","title":"t","priority":"P1","depends_on":[]}` + "\n", "status: \"done-ish\" not in the board vocabulary"},
		{"missing title", `{"id":"x","status":"pending","priority":"P1","depends_on":[]}` + "\n", "title: missing"},
		{"bad priority", `{"id":"x","status":"pending","title":"t","priority":"urgent","depends_on":[]}` + "\n", "priority: \"urgent\" not in the board vocabulary"},
		{"duplicate id", good + good, "duplicate id"},
		{"empty line mid-file", good + "\n" + good, "empty line"},
	}
	for _, tc := range cases {
		err := ValidateRows([]byte(tc.in))
		if err == nil {
			t.Errorf("%s: validated, want refusal naming %q", tc.name, tc.want)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: refusal %q does not name %q", tc.name, err, tc.want)
		}
	}
	if err := ValidateRows(nil); err != nil {
		t.Errorf("empty findings file must validate: %v", err)
	}
}

// TestWriteFindingsEmptyIsAFile: a clean run writes findings.jsonl EMPTY
// (zero bytes) — "no findings" is distinguishable from "not run".
func TestWriteFindingsEmptyIsAFile(t *testing.T) {
	rn := stubRunner(stubCat("P-001"), map[string]selftest.Result{"P-001": stubPass})
	res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "P-001")}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p, n, err := WriteFindings(dir, res, res.RunID, "/j/battery.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("clean run filed %d rows, want 0", n)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 0 {
		t.Errorf("clean run's findings.jsonl is %d bytes, want an empty file", st.Size())
	}
	if filepath.Base(p) != "findings.jsonl" {
		t.Errorf("findings path %q", p)
	}
}

// TestWriteFindingsNonEmpty: a failing run writes one JSONL row per
// finding, and the written file passes the validator (write-path and
// check agree on the wire format).
func TestWriteFindingsNonEmpty(t *testing.T) {
	res := failingRun(t)
	dir := t.TempDir()
	p, n, err := WriteFindings(dir, res, res.RunID, "/j/battery.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("rows %d, want 2", n)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateRows(b); err != nil {
		t.Fatalf("written findings failed validation: %v", err)
	}
}
