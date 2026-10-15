package chaos

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trouble-agent/mischief/internal/battery"
)

// lane_test.go — the -chaos satellite lane template contract (M6, MSF-017):
//
//   - one lane per project, named <project>-chaos, own namespace;
//   - the lane's board scan sees the PRIMARY's board through the symlink
//     (a lane-owned board dir is a severed lane and refuses);
//   - findings land as rows on the OWNING project's board — no operator;
//   - the isolation contract is ENCODED in the template, not just the
//     docs: a lane whose host is the scheduler's own host refuses, and so
//     does an unsanctioned one (SPEC-13 / MSF-020);
//   - the scope guard is ENCODED: matrix.scope is "project" — a fleet or
//     all-projects scope refuses (never a fleet-wide artefact generated
//     N times).
//
// A finding is one adverse outcome filed as a board-vocabulary row
// (battery.Finding) appended to the owning board's tasks.jsonl.

// validLane returns an instantiated, valid lane for project "mischief".
func validLane(t *testing.T) Lane {
	t.Helper()
	return Lane{
		Schema:       Schema,
		Lane:         "mischief-chaos",
		Project:      "mischief",
		Namespace:    "mischief-chaos-ns",
		Cadence:      "every 6h",
		PrimaryBoard: t.TempDir(),
		Host:         Host{Name: "bunker-qa-1", Sanctioned: true, MarkerPath: DefaultMarkerPath},
		Matrix:       MatrixConf{Scope: ScopeProject, TierMax: 1},
		Findings:     FindingsConf{Mode: ModeRows, Board: ""},
		Isolation:    Isolation{ForbiddenHosts: []string{"fleet-main-host"}},
	}
}

// ── ValidateLane: the template's machine-checked contract ───────────────────

func TestValidateLaneAcceptsInstantiated(t *testing.T) {
	l := validLane(t)
	l.Findings.Board = l.PrimaryBoard
	if err := ValidateLane(&l); err != nil {
		t.Fatalf("valid lane refused: %v", err)
	}
}

// TestValidateLaneRefusesSchedulerHost is the isolation contract, encoded:
// a chaos lane may not run a fault on the scheduler's own host. The
// refusal names the host and the rule (SPEC-13 / MSF-020).
func TestValidateLaneRefusesSchedulerHost(t *testing.T) {
	l := validLane(t)
	l.Findings.Board = l.PrimaryBoard
	l.Host.Name = "fleet-main-host" // the forbidden scheduler host
	err := ValidateLane(&l)
	if err == nil {
		t.Fatal("lane on the scheduler's own host was accepted — the isolation contract is not encoded")
	}
	for _, want := range []string{"fleet-main-host", "scheduler"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal missing %q: %v", want, err)
		}
	}
}

// Case-insensitive host matching: the forbidden list is about identity,
// not spelling.
func TestValidateLaneRefusesSchedulerHostCaseInsensitive(t *testing.T) {
	l := validLane(t)
	l.Findings.Board = l.PrimaryBoard
	l.Host.Name = "Fleet-Main-Host"
	if err := ValidateLane(&l); err == nil {
		t.Fatal("case-variant scheduler host accepted")
	}
}

// ── isolation rule 1 must also catch the REAL hostname (MISCHIEF-DF-18) ─────

// resolvedHostname is the test's view of this machine's hostname — the
// same os.Hostname the isolation rule resolves.
func resolvedHostname(t *testing.T) string {
	t.Helper()
	h, err := os.Hostname()
	if err != nil || strings.TrimSpace(h) == "" {
		t.Skipf("os.Hostname unavailable (%v) — self-host matching cannot be exercised", err)
	}
	return strings.TrimSpace(h)
}

// The scheduler's own host is not only the literal placeholder name: a
// lane declaring THIS machine's resolved hostname must refuse too — the
// check runs on the machine, so "the scheduler's own host" is resolvable
// at validation time, not just a name in a template.
func TestValidateLaneRefusesRealHostname(t *testing.T) {
	host := resolvedHostname(t)
	l := validLane(t)
	l.Findings.Board = l.PrimaryBoard
	l.Host.Name = host
	err := ValidateLane(&l)
	if err == nil {
		t.Fatalf("lane declaring this machine's hostname %q accepted — isolation rule 1 matches only the literal placeholder", host)
	}
	if !strings.Contains(err.Error(), "scheduler") {
		t.Errorf("refusal does not name the isolation contract: %v", err)
	}
}

// Spelling variants of the real hostname refuse too: the rule is about
// identity, not spelling (case; FQDN vs short DNS label).
func TestValidateLaneRefusesRealHostnameVariants(t *testing.T) {
	host := resolvedHostname(t)
	variants := []string{
		strings.ToUpper(host),     // case variant
		host + ".lab.example.com", // FQDN form of a short name
	}
	if i := strings.IndexByte(host, '.'); i >= 0 { // short-label forms of an FQDN
		variants = append(variants, host[:i], host[:i]+".other.example.com")
	}
	for _, v := range variants {
		l := validLane(t)
		l.Findings.Board = l.PrimaryBoard
		l.Host.Name = v
		if err := ValidateLane(&l); err == nil {
			t.Errorf("hostname variant %q accepted (resolved host %q)", v, host)
		}
	}
}

// A genuinely foreign host still passes — the rule widens to the real
// hostname, not to everything.
func TestValidateLaneAcceptsForeignHost(t *testing.T) {
	foreign := "nowhere-" + resolvedHostname(t) + "-lab" // cannot equal the resolved host or the literal placeholder
	l := validLane(t)
	l.Findings.Board = l.PrimaryBoard
	l.Host.Name = foreign
	if err := ValidateLane(&l); err != nil {
		t.Fatalf("foreign host %q refused: %v", foreign, err)
	}
}

func TestValidateLaneRefusesUnsanctionedHost(t *testing.T) {
	l := validLane(t)
	l.Findings.Board = l.PrimaryBoard
	l.Host.Sanctioned = false
	err := ValidateLane(&l)
	if err == nil {
		t.Fatal("unsanctioned lane host accepted (SPEC-13: the sanction marker is not optional)")
	}
	if !strings.Contains(err.Error(), "sanction") {
		t.Errorf("refusal does not name the sanction contract: %v", err)
	}
}

// TestValidateLaneRefusesFleetWideScope is the row's scope guard, encoded:
// the matrix is scoped to THAT project — a fleet-wide scope refuses.
func TestValidateLaneRefusesFleetWideScope(t *testing.T) {
	l := validLane(t)
	l.Findings.Board = l.PrimaryBoard
	l.Matrix.Scope = "fleet"
	err := ValidateLane(&l)
	if err == nil {
		t.Fatal("fleet-wide matrix scope accepted — never a fleet-wide artefact generated N times")
	}
	if !strings.Contains(err.Error(), "project") {
		t.Errorf("refusal does not name the project scope: %v", err)
	}
}

// One lane per project, named for it: <project>-chaos.
func TestValidateLaneRefusesWrongLaneName(t *testing.T) {
	l := validLane(t)
	l.Findings.Board = l.PrimaryBoard
	l.Lane = "other-chaos"
	if err := ValidateLane(&l); err == nil {
		t.Fatal("lane not named <project>-chaos accepted")
	}
}

// Findings land on the OWNING board — a findings.board that is not the
// primary's board would file rows somewhere no foreman reads.
func TestValidateLaneRefusesFindingsOffBoard(t *testing.T) {
	l := validLane(t)
	l.Findings.Board = "/somewhere/else"
	if err := ValidateLane(&l); err == nil {
		t.Fatal("findings board differing from the primary board accepted")
	}
}

// A template is not an instantiation: unsubstituted <PLACEHOLDER> tokens
// refuse (the committed template must never pass check as-is).
func TestValidateLaneRefusesPlaceholder(t *testing.T) {
	l := validLane(t)
	l.Findings.Board = l.PrimaryBoard
	l.Project = "<PROJECT>"
	err := ValidateLane(&l)
	if err == nil {
		t.Fatal("unsubstituted placeholder accepted")
	}
	if !strings.Contains(err.Error(), "placeholder") {
		t.Errorf("refusal does not name the placeholder: %v", err)
	}
}

func TestValidateLaneRefusesEmptyNamespace(t *testing.T) {
	l := validLane(t)
	l.Findings.Board = l.PrimaryBoard
	l.Namespace = ""
	if err := ValidateLane(&l); err == nil {
		t.Fatal("lane without its own namespace accepted")
	}
}

func TestValidateLaneRefusesRelativeBoard(t *testing.T) {
	l := validLane(t)
	l.PrimaryBoard = "relative/board"
	l.Findings.Board = "relative/board"
	if err := ValidateLane(&l); err == nil {
		t.Fatal("relative primary board accepted (the symlink needs an absolute target)")
	}
}

// ── LoadLane: the YAML template parses and validates ────────────────────────

const instantiatedTemplate = `schema: mischief.chaos.lane/v1
lane: mischief-chaos
project: mischief
namespace: mischief-chaos-ns
cadence: every 6h
primary_board: %s
host:
  name: bunker-qa-1
  sanctioned: true
  marker_path: /etc/mischief-sanction
matrix:
  scope: project
  tier_max: 1
findings:
  mode: rows
  board: %s
isolation:
  forbidden_hosts: [fleet-main-host]
`

func TestLoadLaneRoundTrip(t *testing.T) {
	board := t.TempDir()
	path := filepath.Join(t.TempDir(), "chaos-lane.yaml")
	body := strings.ReplaceAll(instantiatedTemplate, "%s", board)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := LoadLane(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := ValidateLane(l); err != nil {
		t.Fatalf("loaded lane invalid: %v", err)
	}
	if l.Project != "mischief" || l.Lane != "mischief-chaos" || l.Matrix.TierMax != 1 {
		t.Errorf("round trip lost fields: %+v", l)
	}
	if len(l.Isolation.ForbiddenHosts) != 1 || l.Isolation.ForbiddenHosts[0] != "fleet-main-host" {
		t.Errorf("forbidden hosts lost: %v", l.Isolation.ForbiddenHosts)
	}
}

func TestLoadLaneRefusesGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chaos-lane.yaml")
	if err := os.WriteFile(path, []byte("\t\tnot: [yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLane(path); err == nil {
		t.Fatal("garbage lane file accepted")
	}
}

// ── Symlink evidence: the lane's board scan sees the PRIMARY's board ────────

func TestSymlinkEvidenceOK(t *testing.T) {
	primary := t.TempDir()
	laneDir := t.TempDir()
	link := filepath.Join(laneDir, ".coding-hermes", "board")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(primary, link); err != nil {
		t.Fatal(err)
	}
	ev, err := SymlinkEvidence(laneDir)
	if err != nil {
		t.Fatalf("symlink evidence: %v", err)
	}
	if ev.Raw != primary && ev.Raw != "." && !strings.HasSuffix(filepath.Clean(ev.Raw), filepath.Base(primary)) {
		t.Errorf("raw readlink target unexpected: %q", ev.Raw)
	}
	if ev.Resolved != primary {
		t.Errorf("resolved %q, want the primary board %q", ev.Resolved, primary)
	}
}

// A lane-owned REAL board dir is a severed lane: its board scan would see
// an empty board and silently do nothing. Refuse, naming the fix.
func TestSymlinkEvidenceRefusesSeveredBoard(t *testing.T) {
	laneDir := t.TempDir()
	link := filepath.Join(laneDir, ".coding-hermes", "board")
	if err := os.MkdirAll(link, 0o755); err != nil { // a real dir, not a symlink
		t.Fatal(err)
	}
	_, err := SymlinkEvidence(laneDir)
	if err == nil {
		t.Fatal("real board dir accepted — a severed lane must refuse")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("refusal does not name the symlink contract: %v", err)
	}
}

func TestSymlinkEvidenceRefusesDangling(t *testing.T) {
	laneDir := t.TempDir()
	link := filepath.Join(laneDir, ".coding-hermes", "board")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(laneDir, "gone"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := SymlinkEvidence(laneDir); err == nil {
		t.Fatal("dangling symlink accepted")
	}
}

func TestSymlinkEvidenceRefusesMissing(t *testing.T) {
	laneDir := t.TempDir()
	if _, err := SymlinkEvidence(laneDir); err == nil {
		t.Fatal("missing .coding-hermes/board accepted")
	}
}

// VerifyAgainst ties the symlink to the lane's declared primary board.
func TestVerifyAgainstMatches(t *testing.T) {
	primary := t.TempDir()
	laneDir := t.TempDir()
	link := filepath.Join(laneDir, ".coding-hermes", "board")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(primary, link); err != nil {
		t.Fatal(err)
	}
	ev, err := SymlinkEvidence(laneDir)
	if err != nil {
		t.Fatal(err)
	}
	l := validLane(t)
	l.PrimaryBoard = primary
	l.Findings.Board = primary
	if err := VerifyAgainst(&l, ev); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyAgainstMismatch(t *testing.T) {
	laneDir := t.TempDir()
	other := t.TempDir() // a DIFFERENT project's board
	link := filepath.Join(laneDir, ".coding-hermes", "board")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	ev, err := SymlinkEvidence(laneDir)
	if err != nil {
		t.Fatal(err)
	}
	l := validLane(t)
	l.PrimaryBoard = t.TempDir() // declared primary is yet another dir
	l.Findings.Board = l.PrimaryBoard
	if err := VerifyAgainst(&l, ev); err == nil {
		t.Fatal("symlink to a foreign board accepted")
	}
}

// ── Findings as rows: the no-operator filing path ───────────────────────────

func TestAppendRowsFilesAndReadsBack(t *testing.T) {
	board := t.TempDir()
	row := battery.Finding{
		ID: "MSF-CHAOS-deadbeef", Status: "pending", Title: "t",
		Priority: "P2", DependsOn: []string{}, Project: "mischief",
		Reasoning: "filed by mischief-chaos",
	}
	path, n, err := AppendRows(board, []battery.Finding{row})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if n != 1 {
		t.Fatalf("appended %d rows, want 1", n)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := battery.ValidateRows(b); err != nil {
		t.Fatalf("board bytes do not validate: %v", err)
	}
	if !strings.Contains(string(b), "MSF-CHAOS-deadbeef") {
		t.Errorf("row id missing from %s", path)
	}
}

// The duplicate-id guard is the one safety an unattended writer needs: a
// lane that re-files the same finding must refuse, not duplicate.
func TestAppendRowsRefusesDuplicateID(t *testing.T) {
	board := t.TempDir()
	row := battery.Finding{
		ID: "MSF-CHAOS-deadbeef", Status: "pending", Title: "t",
		Priority: "P2", DependsOn: []string{}, Project: "mischief",
	}
	if _, _, err := AppendRows(board, []battery.Finding{row}); err != nil {
		t.Fatalf("first append: %v", err)
	}
	_, n, err := AppendRows(board, []battery.Finding{row})
	if err == nil {
		t.Fatal("duplicate id accepted")
	}
	if n != 0 {
		t.Fatalf("duplicate append wrote %d rows", n)
	}
	b, _ := os.ReadFile(filepath.Join(board, "tasks.jsonl"))
	if got := strings.Count(strings.TrimRight(string(b), "\n"), "\n") + 1; got != 1 {
		t.Errorf("board has %d rows after the refused duplicate, want 1", got)
	}
}

// Vocabulary violations refuse BEFORE anything is written (the same
// wire-format rule the battery's emit path enforces).
func TestAppendRowsRefusesBadVocabulary(t *testing.T) {
	board := t.TempDir()
	row := battery.Finding{
		ID: "MSF-CHAOS-cafe", Status: "bogus", Title: "t",
		Priority: "P2", DependsOn: []string{}, Project: "mischief",
	}
	if _, n, err := AppendRows(board, []battery.Finding{row}); err == nil {
		t.Fatal("bad status accepted")
	} else if n != 0 {
		t.Fatalf("refused append wrote %d rows", n)
	}
	if b, err := os.ReadFile(filepath.Join(board, "tasks.jsonl")); err == nil && len(b) > 0 {
		t.Errorf("refused append left bytes: %q", b)
	}
}

func TestNewFindingIDIsContentDerivedAndStable(t *testing.T) {
	a := NewFindingID("mischief", "title", "reason")
	b := NewFindingID("mischief", "title", "reason")
	c := NewFindingID("mischief", "title", "other")
	if a != b {
		t.Errorf("same content, different ids: %q vs %q", a, b)
	}
	if a == c {
		t.Error("different content, same id")
	}
	if !strings.HasPrefix(a, "MSF-CHAOS-") {
		t.Errorf("id %q lacks the MSF-CHAOS- prefix", a)
	}
}

// ── matrix artifact vs the lane (MISCHIEF-DF-19) ────────────────────────────
//
// The lane consumes the matrix.json in its workdir. check verifies the
// artifact the lane will RUN, not only the lane's declaration: schema,
// project, scope, catalog_version, and tier_max at or below the lane's
// declared ceiling. A matrix generated at tier-max 3 used to validate
// green under an L1 lane.

// writeMatrixArtifact marshals one MatrixArtifact into dir/matrix.json —
// the file a real `chaos matrix --out <laneDir>` run leaves behind.
func writeMatrixArtifact(t *testing.T, dir string, m *MatrixArtifact) {
	t.Helper()
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, MatrixFile), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// validArtifact returns an artifact matching the validLane's declaration
// (project mischief, scope project, tier_max 1, a named catalog).
func validArtifact(t *testing.T) *MatrixArtifact {
	t.Helper()
	return &MatrixArtifact{
		Schema:         MatrixSchema,
		Project:        "mischief",
		Scope:          ScopeProject,
		TierMax:        1,
		CatalogVersion: "test-catalog-v1",
		GeneratedAt:    time.Now().Unix(),
		Cells:          []ProjectCell{{Primitive: "P-001", Name: "sigstop", Capability: "none", TierRaw: "L1", TierMin: 1, Status: "included"}},
	}
}

// The row's core refusal: an artifact above the lane's ceiling is named —
// tier_max, both numbers, and the lane — never a green check.
func TestValidateMatrixArtifactRefusesTierAboveCeiling(t *testing.T) {
	l := validLane(t)
	dir := t.TempDir()
	a := validArtifact(t)
	a.TierMax = 3
	writeMatrixArtifact(t, dir, a)
	err := ValidateMatrixArtifact(&l, dir)
	if err == nil {
		t.Fatal("tier-3 artifact accepted under an L1 lane — the tick body would run tier-3 faults on an L1-scoped lane")
	}
	for _, want := range []string{"tier_max", "3", "1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal missing %q: %v", want, err)
		}
	}
}

// An artifact matching the lane's ceiling validates green.
func TestValidateMatrixArtifactAcceptsAtCeiling(t *testing.T) {
	l := validLane(t)
	dir := t.TempDir()
	writeMatrixArtifact(t, dir, validArtifact(t))
	if err := ValidateMatrixArtifact(&l, dir); err != nil {
		t.Fatalf("ceiling-matching artifact refused: %v", err)
	}
}

// Below the ceiling is not above it: a narrower artifact is safe.
func TestValidateMatrixArtifactAcceptsBelowCeiling(t *testing.T) {
	l := validLane(t)
	dir := t.TempDir()
	a := validArtifact(t)
	a.TierMax = 0
	writeMatrixArtifact(t, dir, a)
	if err := ValidateMatrixArtifact(&l, dir); err != nil {
		t.Fatalf("below-ceiling artifact refused: %v", err)
	}
}

// No artifact yet is not an error: a fresh lane workdir has nothing for
// the lane to consume (the file's absence is checked first).
func TestValidateMatrixArtifactSkipsMissing(t *testing.T) {
	l := validLane(t)
	if err := ValidateMatrixArtifact(&l, t.TempDir()); err != nil {
		t.Fatalf("missing artifact refused: %v", err)
	}
}

// Every artifact field the row names refuses with the offending field
// named: schema, project, scope, catalog_version.
func TestValidateMatrixArtifactRefusesOffendingFields(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*MatrixArtifact)
		want   string
	}{
		{"schema", func(a *MatrixArtifact) { a.Schema = "mischief.chaos.matrix/v2" }, "schema"},
		{"project", func(a *MatrixArtifact) { a.Project = "other-project" }, "project"},
		{"scope", func(a *MatrixArtifact) { a.Scope = "fleet" }, "scope"},
		{"catalog_version", func(a *MatrixArtifact) { a.CatalogVersion = "" }, "catalog_version"},
	}
	for _, tc := range cases {
		l := validLane(t)
		dir := t.TempDir()
		a := validArtifact(t)
		tc.mutate(a)
		writeMatrixArtifact(t, dir, a)
		err := ValidateMatrixArtifact(&l, dir)
		if err == nil {
			t.Errorf("%s: tampered artifact accepted", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: refusal does not name the offending field %q: %v", tc.name, tc.want, err)
		}
	}
}

// A corrupt artifact is a refusal, not a silent skip: the check must not
// read "cannot parse" as "nothing to validate".
func TestValidateMatrixArtifactRefusesGarbage(t *testing.T) {
	l := validLane(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, MatrixFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMatrixArtifact(&l, dir); err == nil {
		t.Fatal("garbage artifact accepted")
	}
}

// ── matrix-generator tests live in matrix_test.go ──────────────────────────
