// Package chaos is M6 (MSF-017): the -chaos satellite lane convention —
// how mischief's fault-matrix tooling gets adopted across fleet projects
// with NO operator in the loop.
//
// The convention, in one paragraph: each adopting project gets exactly ONE
// satellite lane named <project>-chaos, in its own namespace, running on
// the project's sanctioned chaos host (never the scheduler's own host).
// The lane's workdir carries .coding-hermes/board as a SYMLINK to the
// primary lane's board dir, so the lane's own board scan sees the
// primary's pending rows. The lane derives a project-scoped fault matrix
// from the project's own capability data (catalog × host registry × tier
// ceiling), executes it on L0 scratch targets, and files every adverse
// outcome as a board-vocabulary row on the OWNING project's board — the
// same rows an operator would have filed, appended by the lane itself.
//
// The isolation contract is ENCODED here, not only in the docs: a lane
// instantiated for the scheduler's own host refuses (ValidateLane), as
// does an unsanctioned host (SPEC-13/MSF-020), a non-project matrix
// scope, and a findings board other than the primary's. The checks run
// before the lane does anything; a refused lane writes nothing.
//
// ch:trace row=MSF-017 spec=.coding-hermes/board/tasks.jsonl (MSF-017) evidence=internal/chaos/ witness=none:lane-never-instantiated-on-a-real-host
package chaos

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/trouble-agent/mischief/internal/battery"
	"github.com/trouble-agent/mischief/internal/sanction"

	"gopkg.in/yaml.v3"
)

// Schema is the lane template's schema tag.
const Schema = "mischief.chaos.lane/v1"

// MatrixSchema is the generated matrix artifact's schema tag.
const MatrixSchema = "mischief.chaos.matrix/v1"

// ScopeProject is the only sanctioned matrix scope: the matrix is scoped
// to THAT project (the row's guard: never a fleet-wide artifact generated
// N times).
const ScopeProject = "project"

// ModeRows is the only sanctioned findings mode: findings land as rows on
// the owning board (no operator merge step).
const ModeRows = "rows"

// SchedulerHost is the canonical name of the fleet's scheduler host. A
// chaos lane instantiated for it refuses — the isolation contract
// (MSF-020/MSF-022: "it may not run a fault on the scheduler's own
// host"). The name is matched case-insensitively against the lane's
// declared host name, and every forbidden host may also be listed
// explicitly in the template's isolation.forbidden_hosts.
const SchedulerHost = "fleet-main-host"

// BoardLink is the board path INSIDE a lane workdir that must be the
// symlink to the primary's board dir (the fleet's symlink convention —
// the scheduler's satellites share the primary's board exactly this way).
const BoardLink = ".coding-hermes/board"

// BoardFile is the board row file the findings path appends to.
const BoardFile = "tasks.jsonl"

// DefaultMarkerPath restates sanction.DefaultMarkerPath for the template
// docs (the marker contract stays owned by internal/sanction).
const DefaultMarkerPath = sanction.DefaultMarkerPath

// Host is the lane's declared chaos host.
type Host struct {
	// Name is the host the lane runs faults on. The scheduler's own host
	// refuses.
	Name string `yaml:"name"`
	// Sanctioned must be true: SPEC-13's marker contract (fail closed).
	Sanctioned bool `yaml:"sanctioned"`
	// MarkerPath is the sanctioned marker file the host provisions.
	MarkerPath string `yaml:"marker_path"`
}

// MatrixConf configures the lane's matrix generation.
type MatrixConf struct {
	// Scope must be "project": the matrix covers THAT project only. Any
	// other value refuses (the row's scope guard, encoded).
	Scope string `yaml:"scope"`
	// TierMax is the lane's tier ceiling (L0..L5): cells at a higher tier
	// are excluded with a named reason. The rootless-bunker workhorse
	// ceiling is 1.
	TierMax int `yaml:"tier_max"`
}

// FindingsConf configures the findings filing path.
type FindingsConf struct {
	// Mode must be "rows": findings land as board rows, no operator.
	Mode string `yaml:"mode"`
	// Board is the board dir findings are filed onto — the PRIMARY's
	// board (which the lane sees through the symlink). It must equal
	// primary_board (validated), so a lane can never file findings
	// somewhere its foreman does not read.
	Board string `yaml:"board"`
}

// Isolation carries the lane's forbidden-host list beyond the canonical
// scheduler host.
type Isolation struct {
	// ForbiddenHosts are host names this lane must never run on
	// (case-insensitive match), in addition to the canonical scheduler
	// host.
	ForbiddenHosts []string `yaml:"forbidden_hosts"`
}

// Lane is one instantiated -chaos satellite lane: the template resolved
// for one project.
type Lane struct {
	// Schema is the template schema tag (mischief.chaos.lane/v1).
	Schema string `yaml:"schema"`
	// Lane is the lane name: <project>-chaos.
	Lane string `yaml:"lane"`
	// Project is the owning project (the lane is scoped to it).
	Project string `yaml:"project"`
	// Namespace is the lane's own namespace id (never shared with the
	// primary or another satellite).
	Namespace string `yaml:"namespace"`
	// Cadence is the lane's tick cadence (informational in the template;
	// the scheduler owns enforcement).
	Cadence string `yaml:"cadence"`
	// PrimaryBoard is the PRIMARY lane's board dir (absolute). The lane's
	// workdir carries .coding-hermes/board → this dir.
	PrimaryBoard string `yaml:"primary_board"`
	// Host is the declared chaos host.
	Host Host `yaml:"host"`
	// Matrix is the matrix generation config.
	Matrix MatrixConf `yaml:"matrix"`
	// Findings is the findings filing config.
	Findings FindingsConf `yaml:"findings"`
	// Isolation is the lane's forbidden-host list.
	Isolation Isolation `yaml:"isolation"`
}

// forbidden reports whether the host name hits a forbidden host
// (case-insensitive: the check is about identity, not spelling).
func (l *Lane) forbidden(name string) bool {
	if strings.EqualFold(strings.TrimSpace(name), SchedulerHost) {
		return true
	}
	for _, h := range l.Isolation.ForbiddenHosts {
		if strings.EqualFold(strings.TrimSpace(h), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// placeholder reports an unsubstituted template token: <PROJECT> and
// friends must never survive into an instantiated lane.
func placeholder(s string) bool {
	return strings.Contains(s, "<") && strings.Contains(s, ">")
}

// ValidateMatrixConf validates the matrix generation config alone (the
// scope guard + the tier ceiling): the piece the `chaos matrix` verb
// needs when no instantiated lane is at hand. ValidateLane runs this as
// part of the full contract.
func ValidateMatrixConf(scope string, tierMax int) error {
	if scope != ScopeProject {
		return fmt.Errorf("chaos matrix: scope %q is not %q (the matrix is scoped to THAT project — never a fleet-wide artifact generated N times)", scope, ScopeProject)
	}
	if tierMax < 0 || tierMax > 5 {
		return fmt.Errorf("chaos matrix: tier_max %d outside L0..L5", tierMax)
	}
	return nil
}

// ValidateLane enforces the template's machine-checked contract. Every
// refusal names the rule it enforces. The checks are ordered
// isolation-first: the scheduler-host and sanction refusals fire before
// anything else is even looked at.
func ValidateLane(l *Lane) error {
	if l == nil {
		return fmt.Errorf("chaos lane: nil")
	}
	if placeholder(l.Lane) || placeholder(l.Project) || placeholder(l.Namespace) || placeholder(l.PrimaryBoard) {
		return fmt.Errorf("chaos lane: unsubstituted placeholder <PLACEHOLDER> token in lane/project/namespace/primary_board — instantiate the template per project, never run it as-is")
	}
	// Isolation contract first: the scheduler's own host is not a lab.
	if l.forbidden(l.Host.Name) {
		return fmt.Errorf("chaos lane %s: host %q is a forbidden host (the scheduler's own host is the protected target, not a chaos host — MSF-020/MSF-022 isolation contract)", l.Lane, l.Host.Name)
	}
	if strings.TrimSpace(l.Host.Name) == "" {
		return fmt.Errorf("chaos lane %s: host.name missing", l.Lane)
	}
	// SPEC-13: the sanction marker is not optional. The lane declares its
	// host sanctioned; the runtime enforces the real marker (sanction.Check)
	// before any landing act — this check refuses a template that even
	// DECLARES an unsanctioned host.
	if !l.Host.Sanctioned {
		return fmt.Errorf("chaos lane %s: host %q is not sanctioned (SPEC-13: a chaos lane runs only on a host carrying the mischief sanction marker)", l.Lane, l.Host.Name)
	}
	if l.Project == "" || l.Lane == "" {
		return fmt.Errorf("chaos lane: project and lane are required")
	}
	if want := l.Project + "-chaos"; l.Lane != want {
		return fmt.Errorf("chaos lane: lane %q is not named for its project (want %q — one lane per project, named for it)", l.Lane, want)
	}
	if strings.TrimSpace(l.Namespace) == "" {
		return fmt.Errorf("chaos lane %s: namespace missing (each lane runs in its own namespace)", l.Lane)
	}
	if !filepath.IsAbs(l.PrimaryBoard) {
		return fmt.Errorf("chaos lane %s: primary_board %q is not absolute (the lane's symlink needs an absolute target)", l.Lane, l.PrimaryBoard)
	}
	if err := ValidateMatrixConf(l.Matrix.Scope, l.Matrix.TierMax); err != nil {
		return fmt.Errorf("chaos lane %s: %v", l.Lane, err)
	}
	if l.Findings.Mode != ModeRows {
		return fmt.Errorf("chaos lane %s: findings.mode %q is not %q (findings land as board rows — no operator in the loop)", l.Lane, l.Findings.Mode, ModeRows)
	}
	if filepath.Clean(l.Findings.Board) != filepath.Clean(l.PrimaryBoard) {
		return fmt.Errorf("chaos lane %s: findings.board %q is not the primary's board %q (findings land on the OWNING project's board)", l.Lane, l.Findings.Board, l.PrimaryBoard)
	}
	return nil
}

// LoadLane loads and validates one instantiated lane file.
func LoadLane(path string) (*Lane, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("chaos lane: %w", err)
	}
	var l Lane
	if err := yaml.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("chaos lane %s: %w", filepath.Base(path), err)
	}
	if l.Schema != Schema {
		return nil, fmt.Errorf("chaos lane %s: schema %q, want %q", filepath.Base(path), l.Schema, Schema)
	}
	if err := ValidateLane(&l); err != nil {
		return nil, err
	}
	return &l, nil
}

// ── symlink evidence ────────────────────────────────────────────────────────

// BoardSymlink is the readlink verification evidence the acceptance row
// asks for: the lane's .coding-hermes/board is a symlink whose resolution
// is recorded, not assumed.
type BoardSymlink struct {
	// Link is the lane-side board path that was probed.
	Link string `json:"link"`
	// Raw is the readlink(2) target verbatim.
	Raw string `json:"raw"`
	// Resolved is the absolute path the link resolves to
	// (filepath.EvalSymlinks).
	Resolved string `json:"resolved"`
	// IsSymlink is true only when the Lstat mode says symlink — a real
	// directory here is a severed lane, never evidence.
	IsSymlink bool `json:"is_symlink"`
}

// SymlinkEvidence probes one lane workdir's board link. It refuses —
// naming the fix — when the path is missing, a REAL directory (a severed
// lane: its board scan would see an empty board and silently do
// nothing), or dangling.
func SymlinkEvidence(laneDir string) (*BoardSymlink, error) {
	link := filepath.Join(laneDir, BoardLink)
	fi, err := os.Lstat(link)
	if err != nil {
		return nil, fmt.Errorf("chaos lane: %s missing — create it as a symlink to the primary's board dir (ln -s)", link)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return nil, fmt.Errorf("chaos lane: %s is a REAL directory, not a symlink — a severed lane's board scan sees an empty board and silently does nothing; replace it with a symlink to the primary's board dir", link)
	}
	raw, err := os.Readlink(link)
	if err != nil {
		return nil, fmt.Errorf("chaos lane: readlink %s: %w", link, err)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		return nil, fmt.Errorf("chaos lane: %s dangles (readlink %q): %w — the primary's board dir must exist", link, raw, err)
	}
	return &BoardSymlink{Link: link, Raw: raw, Resolved: resolved, IsSymlink: true}, nil
}

// VerifyAgainst checks the evidence against the lane's declared primary
// board: the symlink must resolve to exactly that dir.
func VerifyAgainst(l *Lane, ev *BoardSymlink) error {
	if err := ValidateLane(l); err != nil {
		return err
	}
	if !ev.IsSymlink {
		return fmt.Errorf("chaos lane %s: %s is not a symlink", l.Lane, ev.Link)
	}
	if filepath.Clean(ev.Resolved) != filepath.Clean(l.PrimaryBoard) {
		return fmt.Errorf("chaos lane %s: board symlink %s resolves to %q, want the declared primary board %q", l.Lane, ev.Link, ev.Resolved, l.PrimaryBoard)
	}
	return nil
}

// ── findings as rows (the no-operator filing path) ──────────────────────────

// NewFindingID derives a content-addressed board row id for a chaos
// finding: MSF-CHAOS-<hex(sha256(project\x1ftitle\x1treason))[:12]>. Same
// finding content ⇒ same id, so a re-filed duplicate is caught by the
// duplicate-id guard instead of duplicating.
func NewFindingID(project, title, reason string) string {
	h := sha256.Sum256([]byte(project + "\x1f" + title + "\x1f" + reason))
	return "MSF-CHAOS-" + hex.EncodeToString(h[:])[:12]
}

// NewFinding builds one chaos finding as a board-vocabulary row (the
// battery.Finding shape; AppendRows validates and files it). A non-empty
// verdict or reason changes the content id, so re-filed findings with new
// evidence are new rows, and identical re-files hit the duplicate guard.
func NewFinding(project, title, reason, priority, primitive, verdict string) battery.Finding {
	return battery.Finding{
		ID:        NewFindingID(project, title, reason),
		Status:    "pending",
		Title:     title,
		Priority:  priority,
		DependsOn: []string{},
		Project:   project,
		Primitive: primitive,
		Verdict:   verdict,
		Reason:    reason,
	}
}

// AppendRows appends findings as board-vocabulary rows to the OWNING
// board's tasks.jsonl. This is the AC-18 emit path's landing half — the
// rows are validated (battery.ValidateRows, the boardctl-style self-check)
// BEFORE the append, and the duplicate-id guard refuses a re-filed
// finding (an unattended lane's one safety: refuse, never duplicate, and
// write nothing on refusal).
func AppendRows(boardDir string, rows []battery.Finding) (string, int, error) {
	path := filepath.Join(boardDir, BoardFile)
	if len(rows) == 0 {
		return path, 0, nil
	}
	// Validate the rows in the board's wire vocabulary BEFORE reading or
	// writing anything (a P3 finding with a bogus status must leave the
	// board byte-identical).
	var b strings.Builder
	ids := map[string]bool{}
	for i := range rows {
		row := rows[i]
		// The lane's own provenance rides in reasoning (after whatever
		// evidence the caller already put there); empty parts drop out.
		parts := []string{}
		if strings.TrimSpace(row.Reasoning) != "" {
			parts = append(parts, row.Reasoning)
		}
		parts = append(parts, fmt.Sprintf("filed by the %s chaos lane (no operator)", row.Project))
		row.Reasoning = strings.Join(parts, " | ")
		line, err := json.Marshal(row)
		if err != nil {
			return path, 0, fmt.Errorf("findings: %w", err)
		}
		b.Write(line)
		b.WriteByte('\n')
		ids[row.ID] = true
	}
	if err := battery.ValidateRows([]byte(b.String())); err != nil {
		return path, 0, fmt.Errorf("findings: %v", err)
	}

	// The duplicate-id guard reads the board's CURRENT bytes (through the
	// symlink when the lane is wired that way — os functions follow it).
	existing := map[string]bool{}
	if b, err := os.ReadFile(path); err == nil {
		for _, ln := range strings.Split(string(b), "\n") {
			ln = strings.TrimSpace(ln)
			if ln == "" {
				continue
			}
			var probe struct {
				ID string `json:"id"`
			}
			if json.Unmarshal([]byte(ln), &probe) == nil && probe.ID != "" {
				existing[probe.ID] = true
			}
		}
	}
	for id := range ids {
		if existing[id] {
			return path, 0, fmt.Errorf("findings: row %s already on %s — refuse, never duplicate (re-file the finding only with new evidence: a new verdict or reason changes the content id)", id, path)
		}
	}

	if err := os.MkdirAll(boardDir, 0o755); err != nil {
		return path, 0, fmt.Errorf("findings: board dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return path, 0, fmt.Errorf("findings: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(b.String()); err != nil {
		return path, 0, fmt.Errorf("findings: %w", err)
	}
	return path, len(rows), nil
}
