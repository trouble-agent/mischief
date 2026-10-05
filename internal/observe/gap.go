package observe

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Detection-gap rows (AC-14's second half): a missed observe.trouble
// assertion files a structured detection-gap record — the mischief
// board-row shape (battery.Finding's vocabulary, the same rows AC-18
// emits), because that is the ledger the mischief project owns. The row
// is EMITTED as board-vocabulary JSONL, never written onto a live board
// or INTO trouble (observe has no write path at all; filing is the
// operator/foreman merge step, battery's AC-18 precedent).
//
// The row shape trouble's own findings ledger uses is not pinnable in this
// repo (no live API in v0.1 — see package doc), so the shape is defined
// from the PRD (SPEC-12, AC-14) and marked: the LIVE wiring of gap rows
// onto a real trouble instance is v0.2. What v0.1 guarantees is the
// structured, id-unique, priority-vocabularied row set a board can accept
// without re-shaping.

// GapPriorities is the gap row's priority vocabulary (the board's P0..P3
// scale, battery's precedent).
var GapPriorities = map[string]bool{"P0": true, "P1": true, "P2": true, "P3": true}

// GapStatuses is the gap row's status vocabulary.
var GapStatuses = map[string]bool{"pending": true, "in_progress": true, "complete": true, "blocked": true}

// GapRow is one detection-gap finding in the board-row shape. Fields match
// the mischief board's row vocabulary (battery.Finding): id, status,
// title, priority, depends_on + the observe evidence carried grep-able
// (project, primitive, expect_record, window, searched).
type GapRow struct {
	// ID is the board row id ("MSF-OBS-<expect_record>"). Id-unique per
	// file (the ValidateGapRows check).
	ID string `json:"id"`
	// Status is the board's status vocabulary ("pending" — a gap is
	// filed for someone to look at).
	Status string `json:"status"`
	// Title is the one-line finding.
	Title string `json:"title"`
	// Priority is the board's P0..P3 scale.
	Priority string `json:"priority"`
	// DependsOn is the board's dependency list (empty).
	DependsOn []string `json:"depends_on"`
	// Reasoning carries the evidence trail: the observe_result's gap
	// reason and the matrix triple.
	Reasoning string `json:"reasoning"`
	// Project is the owning project (the board the row belongs on;
	// "" stays "" — the field is carried even when unknown, with the
	// unknown named in Reasoning).
	Project string `json:"project"`
	// Observe evidence (grep-able, review-grade).
	Primitive    string `json:"primitive"`
	ExpectRecord string `json:"expect_record"`
	Window       string `json:"window"`
	Searched     int    `json:"searched"`
	TimeToDetect string `json:"time_to_detect"`
	LiveWiring   string `json:"live_wiring"`
}

// gapPriority assigns the gap's priority: the fault LANDED and nothing
// detected it — that is the PRD's "the most expensive false green in this
// class" from the observer's side, so P1 (an adverse finding about the
// product's detection, below data-loss/hang shapes). A declared-not-met
// with no fault context is P2 (a coverage finding).
func gapPriority(fault string) string {
	if fault == "" {
		return "P2"
	}
	return "P1"
}

// Gap builds the detection-gap row for one missed assertion. runID rides
// the reasoning (the row must lead back to the journal that measured it).
func Gap(res Result, project, runID string) GapRow {
	id := res.Decl.ExpectRecord
	if id == "" {
		id = "unnamed"
	}
	return GapRow{
		ID:     "MSF-OBS-" + id,
		Status: "pending",
		Title: fmt.Sprintf("detection gap: %s injected, ledger record %q absent within %s",
			res.faultOrUnknown(), res.Decl.ExpectRecord, res.Decl.Within),
		Priority:     gapPriority(res.Fault),
		DependsOn:    []string{},
		Reasoning:    fmt.Sprintf("run %s | observer %s | %s | %s", runID, ObserverName, res.MatrixRow(), res.Reason),
		Project:      project,
		Primitive:    res.Fault,
		ExpectRecord: res.Decl.ExpectRecord,
		Window:       res.Decl.Within,
		Searched:     res.Searched,
		TimeToDetect: "n/a (gap)",
		LiveWiring:   "v0.2",
	}
}

// FileGaps renders the run's gap rows as board-vocabulary JSONL bytes: one
// JSON object per line, priority-first stable order (the board diff a
// reviewer reads must be reproducible). Empty result set → nil, nil (an
// empty gap file is a shape, "no gaps" ≠ "not checked").
func FileGaps(rows []GapRow) ([]byte, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	sorted := make([]GapRow, len(rows))
	copy(sorted, rows)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority < sorted[j].Priority
		}
		return sorted[i].ID < sorted[j].ID
	})
	var b strings.Builder
	seen := map[string]bool{}
	for i := range sorted {
		if seen[sorted[i].ID] {
			return nil, fmt.Errorf("detection gaps: duplicate row id %q", sorted[i].ID)
		}
		seen[sorted[i].ID] = true
		row, err := json.Marshal(sorted[i])
		if err != nil {
			return nil, fmt.Errorf("detection gaps: %w", err)
		}
		b.Write(row)
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

// ValidateGapRows is the boardctl-style self-check over an emitted gap
// file (the emit/validate same-wire-format rule, battery's AC-18
// precedent): every line a JSON object with id (non-empty), status and
// priority in the board vocabularies, title non-empty, depends_on a list;
// the set id-unique. Names line and field on the first problem.
func ValidateGapRows(jsonl []byte) error {
	lines := strings.Split(strings.TrimRight(string(jsonl), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil // the empty gap file validates
	}
	ids := map[string]bool{}
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			return fmt.Errorf("row %d: empty line", i+1)
		}
		var row struct {
			ID        string   `json:"id"`
			Status    string   `json:"status"`
			Title     string   `json:"title"`
			Priority  string   `json:"priority"`
			DependsOn []string `json:"depends_on"`
		}
		if err := json.Unmarshal([]byte(ln), &row); err != nil {
			return fmt.Errorf("row %d: not a JSON object: %v", i+1, err)
		}
		if row.ID == "" {
			return fmt.Errorf("row %d: id: missing", i+1)
		}
		if !GapStatuses[row.Status] {
			return fmt.Errorf("row %d: status: %q not in the board vocabulary", i+1, row.Status)
		}
		if row.Title == "" {
			return fmt.Errorf("row %d: title: missing", i+1)
		}
		if !GapPriorities[row.Priority] {
			return fmt.Errorf("row %d: priority: %q not in the board vocabulary", i+1, row.Priority)
		}
		if row.DependsOn == nil {
			return fmt.Errorf("row %d: depends_on: missing (a list, possibly empty)", i+1)
		}
		if ids[row.ID] {
			return fmt.Errorf("row %d: duplicate id %q", i+1, row.ID)
		}
		ids[row.ID] = true
	}
	return nil
}
