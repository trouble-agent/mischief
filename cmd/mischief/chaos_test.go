package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/battery"
	"github.com/trouble-agent/mischief/internal/chaos"
)

// chaos_test.go — CLI-level tests for the `mischief chaos` verb (MSF-017):
// usage refusals, the project-scoped matrix artifact, the check verb's
// symlink evidence, and the findings filing path on a SCRATCH board (the
// live board is never touched by a test).

// instantiatedLaneYAML is one valid instantiated lane (board dir spliced
// in at both primary_board and findings.board).
const instantiatedLaneYAML = `schema: mischief.chaos.lane/v1
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

func TestChaosVerbRequiresSubcommand(t *testing.T) {
	code, stderr := captureStderr(t, func() int { return run([]string{"chaos"}) })
	if code != 2 {
		t.Fatalf("bare chaos exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "matrix") {
		t.Fatalf("refusal does not name the subcommands: %s", stderr)
	}
}

func TestChaosMatrixRequiresProject(t *testing.T) {
	code, stderr := captureStderr(t, func() int { return run([]string{"chaos", "matrix"}) })
	if code != 2 {
		t.Fatalf("projectless matrix exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "--project") {
		t.Fatalf("refusal does not name --project: %s", stderr)
	}
}

// The scope guard is encoded, not documentary: the generator refuses to
// render anything but a project-scoped matrix.
func TestChaosValidateMatrixConfRefusesFleetScope(t *testing.T) {
	if err := chaos.ValidateMatrixConf("fleet", 1); err == nil {
		t.Fatal("fleet scope accepted by the generator's own config check")
	}
	if err := chaos.ValidateMatrixConf("project", 9); err == nil {
		t.Fatal("tier_max 9 accepted")
	}
	if err := chaos.ValidateMatrixConf("project", 1); err != nil {
		t.Fatalf("valid conf refused: %v", err)
	}
}

// TestChaosMatrixGeneratesProjectScoped is the M6 wiring proof: the
// derived matrix is THE PROJECT'S, stamped mischief.chaos.matrix/v1, and
// both halves (json + md) land under --out.
func TestChaosMatrixGeneratesProjectScoped(t *testing.T) {
	out := t.TempDir()
	code, outTxt := captureStdout(t, func() int {
		return run([]string{"chaos", "matrix", "--project", "mischief", "--tier-max", "1", "--out", out})
	})
	if code != 0 {
		t.Fatalf("matrix exit %d\nstdout:\n%s", code, outTxt)
	}
	b, err := os.ReadFile(filepath.Join(out, "matrix.json"))
	if err != nil {
		t.Fatalf("matrix.json: %v", err)
	}
	var m struct {
		Schema  string `json:"schema"`
		Project string `json:"project"`
		Scope   string `json:"scope"`
		Cells   []struct {
			Primitive string `json:"primitive"`
			Status    string `json:"status"`
			Reason    string `json:"reason"`
		} `json:"cells"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("matrix.json not JSON: %v", err)
	}
	if m.Schema != "mischief.chaos.matrix/v1" {
		t.Errorf("schema %q", m.Schema)
	}
	if m.Project != "mischief" || m.Scope != "project" {
		t.Errorf("project %q scope %q — the artifact must be project-scoped", m.Project, m.Scope)
	}
	if len(m.Cells) == 0 {
		t.Fatal("zero cells — the real in-repo catalog produced nothing")
	}
	// Every cell carries a status; every non-included cell names why.
	for _, c := range m.Cells {
		if c.Status == "" {
			t.Errorf("cell %s has no status", c.Primitive)
		}
		if c.Status != "included" && strings.TrimSpace(c.Reason) == "" {
			t.Errorf("excluded cell %s carries no reason", c.Primitive)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "matrix.md")); err != nil {
		t.Errorf("matrix.md missing: %v", err)
	}
}

func TestChaosCheckVerifiesInstantiatedLane(t *testing.T) {
	primary := t.TempDir()
	laneDir := t.TempDir()
	link := filepath.Join(laneDir, ".coding-hermes", "board")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(primary, link); err != nil {
		t.Fatal(err)
	}
	lanePath := filepath.Join(laneDir, "chaos-lane.yaml")
	body := strings.ReplaceAll(instantiatedLaneYAML, "%s", primary)
	if err := os.WriteFile(lanePath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := captureStdout(t, func() int {
		return run([]string{"chaos", "check", "--lane", lanePath})
	})
	if code != 0 {
		t.Fatalf("check exit %d\nstdout:\n%s", code, out)
	}
	// The readlink verification evidence is the point: the raw target AND
	// the resolution must both be printed.
	if !strings.Contains(out, "readlink") && !strings.Contains(out, "->") {
		t.Errorf("check output carries no readlink evidence:\n%s", out)
	}
	if !strings.Contains(out, primary) {
		t.Errorf("check output does not name the primary board %q:\n%s", primary, out)
	}
}

func TestChaosCheckRefusesSeveredLane(t *testing.T) {
	laneDir := t.TempDir()
	link := filepath.Join(laneDir, ".coding-hermes", "board")
	if err := os.MkdirAll(link, 0o755); err != nil { // a real dir, not a symlink
		t.Fatal(err)
	}
	lanePath := filepath.Join(laneDir, "chaos-lane.yaml")
	body := strings.ReplaceAll(instantiatedLaneYAML, "%s", t.TempDir())
	if err := os.WriteFile(lanePath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stderr := captureStderr(t, func() int { return run([]string{"chaos", "check", "--lane", lanePath}) })
	if code == 0 {
		t.Fatal("severed lane passed check")
	}
	if !strings.Contains(stderr, "symlink") {
		t.Errorf("refusal does not name the symlink contract: %s", stderr)
	}
}

// TestChaosFindingFilesSampleRow is the mechanical findings-as-rows
// demonstration, on a SCRATCH board (never the live board): one finding,
// filed as a validated board-vocabulary row, read back and asserted.
func TestChaosFindingFilesSampleRow(t *testing.T) {
	board := t.TempDir()
	code, out := captureStdout(t, func() int {
		return run([]string{"chaos", "finding",
			"--project", "mischief",
			"--board", board,
			"--title", "battery finding: N-012 on mischief (recovered) — sample demonstration row",
			"--reason", "quick matrix cell q1 graded recovered; the detector seam is not wired (future work)",
			"--priority", "P3",
			"--primitive", "N-012",
		})
	})
	if code != 0 {
		t.Fatalf("finding exit %d\nstdout:\n%s", code, out)
	}
	b, err := os.ReadFile(filepath.Join(board, "tasks.jsonl"))
	if err != nil {
		t.Fatalf("board file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("board has %d rows, want exactly the 1 demo row", len(lines))
	}
	var row struct {
		ID       string   `json:"id"`
		Status   string   `json:"status"`
		Title    string   `json:"title"`
		Priority string   `json:"priority"`
		Depends  []string `json:"depends_on"`
		Project  string   `json:"project"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &row); err != nil {
		t.Fatalf("row not JSON: %v", err)
	}
	if !strings.HasPrefix(row.ID, "MSF-CHAOS-") {
		t.Errorf("row id %q lacks the chaos prefix", row.ID)
	}
	if row.Status != "pending" || row.Priority != "P3" || row.Project != "mischief" {
		t.Errorf("row fields: %+v", row)
	}
	// The filed row must pass the same validator the battery's emit path
	// uses (one wire format, one self-check).
	if err := battery.ValidateRows(b); err != nil {
		t.Errorf("filed row fails the board validator: %v", err)
	}
}

// A second identical filing is REFUSED (duplicate guard), and the board
// stays at one row.
func TestChaosFindingRefusesDuplicate(t *testing.T) {
	board := t.TempDir()
	args := []string{"chaos", "finding",
		"--project", "mischief", "--board", board,
		"--title", "duplicate probe", "--reason", "same reason twice",
	}
	if code, _ := captureStdout(t, func() int { return run(args) }); code != 0 {
		t.Fatal("first filing failed")
	}
	code, stderr := captureStderr(t, func() int { return run(args) })
	if code == 0 {
		t.Fatal("duplicate filing accepted")
	}
	if !strings.Contains(stderr, "already on") {
		t.Errorf("refusal does not name the duplicate guard: %s", stderr)
	}
	b, _ := os.ReadFile(filepath.Join(board, "tasks.jsonl"))
	if got := len(strings.Split(strings.TrimRight(string(b), "\n"), "\n")); got != 1 {
		t.Errorf("board has %d rows after the refused duplicate, want 1", got)
	}
}
