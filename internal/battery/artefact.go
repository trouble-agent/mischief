package battery

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// artefact.go — the detection-matrix artefact writer (S-5): fault ×
// detection × recovery, written as JSON + a markdown table under the run
// dir. The artefact is the fleet-facing product: one battery over N
// projects emits it, the quick run's copy names every legacy bash cell it
// replaces, and it is the evidence the fleet-side deletion (the
// docs/BATTERY-PARITY.md checklist) cites when the five bash cells are
// removed from bunker-qa.sh.
//
// The artefact never grades: it renders what the run measured (verdicts
// from the SPEC-05 engine, detection from the Detector seam, recovery from
// the verdict table in runner.go).

// Artefact is the detection-matrix artefact's JSON shape.
type Artefact struct {
	// Schema is the artefact schema tag ("mischief.battery.matrix/v1").
	Schema string `json:"schema"`
	// RunID is the content-derived run id.
	RunID string `json:"run_id"`
	// Quick is true when this artefact is the --quick parity matrix.
	Quick bool `json:"quick"`
	// GeneratedAt is the unix second the artefact was written.
	GeneratedAt int64 `json:"generated_at"`
	// CatalogVersion is the content version of the catalog the run
	// resolved against.
	CatalogVersion string `json:"catalog_version"`
	// Projects are the distinct owning projects the matrix covered.
	Projects []string `json:"projects"`
	// Cells is the fault × detection × recovery table, one row per cell.
	Cells []ArtefactCell `json:"cells"`
	// LegacyParity is populated on a --quick artefact: the five bash
	// cells and the primitive replacing each, in bunker-qa.sh order.
	LegacyParity []LegacyCell `json:"legacy_parity,omitempty"`
	// DetectionNote states the AC-14 posture of this build honestly
	// (every non-wired cell carries the same reason on its row; this
	// note says it once for the reader).
	DetectionNote string `json:"detection_note"`
}

// ArtefactCell is one matrix row: fault × detection × recovery.
type ArtefactCell struct {
	// CellID is the stable cell id.
	CellID string `json:"cell_id"`
	// Project is the owning project.
	Project string `json:"project"`
	// Fault is the primitive id; FaultName the human name.
	Fault     string `json:"fault"`
	FaultName string `json:"fault_name"`
	// LegacyCell is the bunker-qa.sh cell replaced ("" outside --quick).
	LegacyCell string `json:"legacy_cell,omitempty"`
	// Verdict is the graded verdict (closed vocabulary).
	Verdict string `json:"verdict"`
	// Detection is the detection axis (the AC-14 entry).
	Detection DetectionEntry `json:"detection"`
	// Recovery is the recovery axis.
	Recovery string `json:"recovery"`
	// Reason is the deciding rule text.
	Reason string `json:"reason"`
}

// BuildArtefact renders the artefact struct from a run result.
func BuildArtefact(r *RunResult, runID, catalogVersion string, now int64) *Artefact {
	a := &Artefact{
		Schema:         "mischief.battery.matrix/v1",
		RunID:          runID,
		Quick:          r.Quick,
		GeneratedAt:    now,
		CatalogVersion: catalogVersion,
		Projects:       r.Projects(),
		DetectionNote:  notWired,
	}
	for i := range r.Cells {
		c := &r.Cells[i]
		a.Cells = append(a.Cells, ArtefactCell{
			CellID:     c.Cell.ID,
			Project:    c.Cell.Target.Project,
			Fault:      c.Cell.Primitive,
			FaultName:  faultName(r, c),
			LegacyCell: c.Cell.LegacyCell,
			Verdict:    c.Verdict.String(),
			Detection:  c.Detection,
			Recovery:   c.Recovery,
			Reason:     c.Reason,
		})
	}
	if r.Quick {
		a.LegacyParity = LegacyCells()
	}
	return a
}

// faultName returns the row's fault name (stamped by the runner from the
// catalog; the artefact renders only).
func faultName(r *RunResult, c *CellResult) string {
	if c.FaultName != "" {
		return c.FaultName
	}
	return "unknown"
}

// WriteArtefact writes the artefact under dir: matrix.json (the machine
// form) and matrix.md (the markdown table). Both are created 0644 inside
// the run dir; the run dir itself is created 0700. Returns the JSON path.
func WriteArtefact(dir string, a *Artefact) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("artefact: empty run dir")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("artefact: run dir: %w", err)
	}
	jp := filepath.Join(dir, "matrix.json")
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return "", fmt.Errorf("artefact: %w", err)
	}
	if err := os.WriteFile(jp, append(b, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("artefact: %w", err)
	}
	mp := filepath.Join(dir, "matrix.md")
	if err := os.WriteFile(mp, []byte(renderMarkdown(a)), 0o644); err != nil {
		return "", fmt.Errorf("artefact: %w", err)
	}
	return jp, nil
}

// renderMarkdown renders the artefact's markdown table (the human half of
// the artefact: one row per cell, the axes as columns).
func renderMarkdown(a *Artefact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# mischief battery — fault × detection × recovery matrix\n\n")
	fmt.Fprintf(&b, "- run id: `%s`\n", a.RunID)
	fmt.Fprintf(&b, "- mode: %s\n", map[bool]string{true: "--quick (parity matrix)", false: "full matrix"}[a.Quick])
	fmt.Fprintf(&b, "- generated: %s\n", time.Unix(a.GeneratedAt, 0).UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "- catalog version: `%s`\n", a.CatalogVersion)
	fmt.Fprintf(&b, "- projects: %s\n", strings.Join(a.Projects, ", "))
	fmt.Fprintf(&b, "\n%s\n\n", a.DetectionNote)

	if a.Quick {
		b.WriteString("## Legacy parity (S-5)\n\n")
		b.WriteString("The five bunker-qa.sh chaos cells this run replaces (superseded since MSF-016: the fleet-side deletion executes the docs/BATTERY-PARITY.md checklist):\n\n")
		b.WriteString("| legacy cell | replacing primitive | mapping |\n|---|---|---|\n")
		for _, lc := range a.LegacyParity {
			fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", lc.Cell, lc.Primitive, lc.Replaces)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Matrix\n\n")
	b.WriteString("| cell | project | fault | legacy cell | verdict | detection | recovery |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	for _, c := range a.Cells {
		det := c.Detection.State
		if c.Detection.Reason != "" && c.Detection.State != "detected" {
			// the reason is the row's honest payload: keep it short on
			// the table line, full in the JSON
			det = fmt.Sprintf("%s (%s)", c.Detection.State, truncate(c.Detection.Reason, 80))
		}
		fmt.Fprintf(&b, "| `%s` | %s | `%s` | %s | %s | %s | %s |\n",
			c.CellID, c.Project, c.Fault,
			legacyCellText(c.LegacyCell),
			c.Verdict, det, c.Recovery)
	}
	return b.String()
}

func legacyCellText(s string) string {
	if s == "" {
		return "—"
	}
	return "`" + s + "`"
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// ── findings filing (AC-18) ─────────────────────────────────────────────────

// Finding is one adverse battery outcome rendered in the OWNING board's
// row vocabulary. The mischief board (tasks.jsonl) row shape is:
// id, status, title, priority, depends_on — plus the battery's reasoning
// fields the AC requires (the journal path rides in `reasoning`).
type Finding struct {
	// ID is the board row id (MSF-BAT-<cell>).
	ID string `json:"id"`
	// Status is the board's status vocabulary ("pending").
	Status string `json:"status"`
	// Title is the one-line finding.
	Title string `json:"title"`
	// Priority is the board's P0..P3 scale.
	Priority string `json:"priority"`
	// DependsOn is the board's dependency list (empty).
	DependsOn []string `json:"depends_on"`
	// Reasoning carries the AC-18 journal path and the deciding rule.
	Reasoning string `json:"reasoning"`
	// Project is the owning project (the board the row belongs on).
	Project string `json:"project"`
	// RunID is the run id the finding's journal records carry — the
	// AC-9 correlation key: the journal, the CLI output and this row all
	// name the same run (MSF-031). Set by deriveFindings from the run
	// result; empty only on a hand-built Finding (tests, external tools).
	RunID      string `json:"run_id,omitempty"`
	Primitive  string `json:"primitive"`
	LegacyCell string `json:"legacy_cell,omitempty"`
	Verdict    string `json:"verdict"`
	Reason     string `json:"reason"`
}

// findingStatuses / findingPriorities are the board's closed vocabularies
// (read from the mischief board's own rows: status ∈ pending|in_progress|
// complete|blocked, priority ∈ P0|P1|P2|P3).
var (
	findingStatuses    = map[string]bool{"pending": true, "in_progress": true, "complete": true, "blocked": true}
	findingPriorities  = map[string]bool{"P0": true, "P1": true, "P2": true, "P3": true}
	findingPriorityOrd = map[string]int{"P0": 0, "P1": 1, "P2": 2, "P3": 3}
)

// FileFindings renders the run's findings as the board-row JSONL byte
// form: one JSON object per line, in the board's field vocabulary. The
// bytes are what `boardctl validate` (or this package's ValidateRows)
// consumes — emitting and validating are the same wire format.
// runID is the AC-9 correlation key stamped on every row (MSF-031: the
// journal, the CLI output and the rows all name the same run).
func FileFindings(r *RunResult, runID, journalPath string) ([]byte, error) {
	if len(r.Findings) == 0 {
		return nil, nil
	}
	var b strings.Builder
	rows := make([]Finding, len(r.Findings))
	copy(rows, r.Findings)
	// stable order: priority (P0 first), then cell id — the board diff a
	// reviewer reads must be reproducible
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Priority != rows[j].Priority {
			return findingPriorityOrd[rows[i].Priority] < findingPriorityOrd[rows[j].Priority]
		}
		return rows[i].ID < rows[j].ID
	})
	for i := range rows {
		rows[i].RunID = runID
		rows[i].Reasoning = fmt.Sprintf("battery run %s journal: %s | verdict %s | %s",
			runID, journalPath, rows[i].Verdict, rows[i].Reason)
		row, err := json.Marshal(rows[i])
		if err != nil {
			return nil, fmt.Errorf("findings: %w", err)
		}
		b.Write(row)
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

// WriteFindings writes the findings JSONL to <dir>/findings.jsonl (the
// AC-18 artefact; empty file when nothing was found — a clean battery
// writes the empty file rather than no file, so "no findings" is
// distinguishable from "not run"). runID rides every emitted row (AC-9).
func WriteFindings(dir string, r *RunResult, runID, journalPath string) (string, int, error) {
	b, err := FileFindings(r, runID, journalPath)
	if err != nil {
		return "", 0, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", 0, fmt.Errorf("findings: run dir: %w", err)
	}
	p := filepath.Join(dir, "findings.jsonl")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return "", 0, fmt.Errorf("findings: %w", err)
	}
	n := 0
	if len(b) > 0 {
		n = strings.Count(strings.TrimRight(string(b), "\n"), "\n") + 1
	}
	return p, n, nil
}

// ValidateRows is the `boardctl validate`-style self-check (AC-18): every
// line must be a JSON object carrying the mischief board's row fields —
// id (non-empty), status (in the board's status vocabulary), title
// (non-empty), priority (P0..P3), depends_on (a list) — and the whole set
// must be id-unique. Returns the first problem, naming line and field.
func ValidateRows(jsonl []byte) error {
	lines := strings.Split(strings.TrimRight(string(jsonl), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil // an empty findings file validates (no rows is a shape)
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
		switch {
		case row.ID == "":
			return fmt.Errorf("row %d: id: missing", i+1)
		case !findingStatuses[row.Status]:
			return fmt.Errorf("row %d: status: %q not in the board vocabulary", i+1, row.Status)
		case row.Title == "":
			return fmt.Errorf("row %d: title: missing", i+1)
		case !findingPriorities[row.Priority]:
			return fmt.Errorf("row %d: priority: %q not in the board vocabulary", i+1, row.Priority)
		}
		if ids[row.ID] {
			return fmt.Errorf("row %d: duplicate id %q", i+1, row.ID)
		}
		ids[row.ID] = true
	}
	return nil
}
