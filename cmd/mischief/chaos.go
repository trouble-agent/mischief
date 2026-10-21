package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/trouble-agent/mischief/internal/battery"
	"github.com/trouble-agent/mischief/internal/chaos"
	"github.com/trouble-agent/mischief/internal/sanction"
)

// chaos.go — the `mischief chaos` verb (MSF-017, M6 fleet adoption): the
// operator-free half of the -chaos satellite lane. Three subcommands, one
// per lane duty:
//
//	mischief chaos matrix --project <p> [--tier-max N] [--out D] [--markdown]
//	    Derive the project-scoped fault matrix from the project's OWN
//	    capability data (catalog capability/tier fields probed against the
//	    chaos host's registry). Writes matrix.json (+ matrix.md) under --out
//	    (default: <project>-chaos under the system temp dir). It NEVER
//	    executes a fault and NEVER touches a board.
//	mischief chaos check --lane <lane.yaml>
//	    Verify one instantiated lane: the template contract (isolation
//	    first), then the board symlink evidence — readlink verbatim plus
//	    the full resolution, refused when .coding-hermes/board is a real
//	    directory (a severed lane). Exit 0 with the evidence printed.
//	mischief chaos finding --project <p> --board <dir> --title T --reason R
//	    [--priority P3] [--primitive ID] [--verdict V]
//	    File ONE finding as a board-vocabulary row on the OWNING board
//	    (tasks.jsonl), validated before the append and duplicate-guarded.
//	    This is the no-operator filing path; on a live lane --board is the
//	    primary's board the lane sees through its symlink.
//
// The verb is deliberately dumb (the plane.go discipline): it derives,
// verifies and files — it never spawns, arms a fault, or edits the lane.
//
// ch:trace row=MSF-017 spec=.coding-hermes/board/tasks.jsonl (MSF-017) evidence=cmd/mischief/chaos.go + internal/chaos/ witness=none:verb-derives-and-files-never-executes
func cmdChaos(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "chaos: a subcommand is required (matrix | check | finding)")
		return 2
	}
	switch args[0] {
	case "help", "-h", "--help":
		// MISCHIEF-CLI-001: dispatcher-level help, conventional exit 0
		// on stdout (the subcommand FlagSets answer their own --help
		// via verbParseExit with the flag package's usage).
		fmt.Fprint(os.Stdout, chaosUsage)
		return 0
	case "matrix":
		return cmdChaosMatrix(args[1:])
	case "check":
		return cmdChaosCheck(args[1:])
	case "finding":
		return cmdChaosFinding(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "chaos: unknown subcommand %q (matrix | check | finding)\n", args[0])
		return 2
	}
}

func cmdChaosMatrix(args []string) int {
	fs := flag.NewFlagSet("chaos matrix", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	project := fs.String("project", "", "project name (required; the matrix is scoped to THAT project)")
	tierMax := fs.Int("tier-max", 1, "tier ceiling L0..L5 (default 1: the rootless-bunker workhorse ceiling)")
	catalogDir := fs.String("catalog-dir", "", "catalog dir (default: catalog/faults from cwd upward)")
	out := fs.String("out", "", "output dir (default: <project>-chaos under the system temp dir)")
	asMarkdown := fs.Bool("markdown", false, "print the markdown table to stdout instead of the summary line")
	if err := fs.Parse(args); err != nil {
		return verbParseExit(err)
	}
	if strings.TrimSpace(*project) == "" {
		fmt.Fprintln(os.Stderr, "chaos matrix: --project is required (a project-scoped matrix needs the project; a fleet-wide matrix is refused by design)")
		return 2
	}
	cat, err := loadCatalog(*catalogDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "chaos matrix:", err)
		return 1
	}
	// MISCHIEF-DF-67: the matrix's SPEC-13 posture is EXPLICIT, not
	// enforced. The matrix is evidence-plane — it derives cells from the
	// catalog + registry and writes artifacts; it never arms a fault,
	// never binds a port, never claims a landed proof — so unlike the
	// landing verbs (plan/arm/selftest/battery/sim) it does not refuse on
	// an unsanctioned host. But the artifact and its summary line record
	// WHICH host posture produced them: a matrix derived on the
	// unsanctioned scheduler host can never pass as the artifact a
	// sanctioned chaos host would produce (a NULL with a reason, not a
	// blank).
	posture := chaos.PostureSanctioned
	if err := sanction.Check(sanction.Options{}); err != nil {
		posture = chaos.PostureUnsanctioned
	}
	lane := &chaos.Lane{
		Schema:    chaos.Schema,
		Lane:      *project + "-chaos",
		Project:   *project,
		Namespace: *project + "-chaos-ns",
		Matrix:    chaos.MatrixConf{Scope: chaos.ScopeProject, TierMax: *tierMax},
		Findings:  chaos.FindingsConf{Mode: chaos.ModeRows},
	}
	if err := chaos.ValidateMatrixConf(lane.Matrix.Scope, lane.Matrix.TierMax); err != nil {
		fmt.Fprintln(os.Stderr, "chaos matrix:", err)
		return 2
	}
	m := chaos.ProjectMatrix(*project, cat, hostCapabilityRegistry(), lane, posture)
	outDir := *out
	if outDir == "" {
		outDir = filepath.Join(os.TempDir(), *project+"-chaos")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "chaos matrix:", err)
		return 1
	}
	jp := filepath.Join(outDir, "matrix.json")
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "chaos matrix:", err)
		return 1
	}
	if err := os.WriteFile(jp, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "chaos matrix:", err)
		return 1
	}
	mp := filepath.Join(outDir, "matrix.md")
	if err := os.WriteFile(mp, []byte(chaos.RenderMatrixMarkdown(m)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "chaos matrix:", err)
		return 1
	}
	if *asMarkdown {
		fmt.Print(chaos.RenderMatrixMarkdown(m))
	}
	inc := 0
	for _, c := range m.Cells {
		if c.Status == "included" {
			inc++
		}
	}
	// MISCHIEF-DF-67: the summary names the host posture so an
	// unsanctioned derivation is never mistaken for a sanctioned one.
	fmt.Printf("chaos matrix: %s — %d/%d cells executable at L%d on this %s chaos host (artifact: %s)\n",
		m.Project, inc, len(m.Cells), *tierMax, posture, jp)
	return 0
}

func cmdChaosCheck(args []string) int {
	fs := flag.NewFlagSet("chaos check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	lanePath := fs.String("lane", "", "instantiated lane file (required)")
	laneDir := fs.String("lane-dir", "", "the lane workdir whose .coding-hermes/board is probed (default: the lane file's directory)")
	if err := fs.Parse(args); err != nil {
		return verbParseExit(err)
	}
	if *lanePath == "" {
		fmt.Fprintln(os.Stderr, "chaos check: --lane is required")
		return 2
	}
	l, err := chaos.LoadLane(*lanePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "chaos check:", err)
		return 2
	}
	dir := *laneDir
	if dir == "" {
		dir = filepath.Dir(*lanePath)
	}
	ev, err := chaos.SymlinkEvidence(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "chaos check:", err)
		return 2
	}
	if err := chaos.VerifyAgainst(l, ev); err != nil {
		fmt.Fprintln(os.Stderr, "chaos check:", err)
		return 2
	}
	// The lane consumes the generated matrix artifact in its workdir;
	// when one is present it must agree with the lane (schema, project,
	// scope, catalog_version, and tier_max at or below the lane's
	// declared ceiling — MISCHIEF-DF-19: a tier-3 artifact used to
	// validate green under an L1 lane). Checked against the same lane
	// workdir the symlink evidence probed.
	if err := chaos.ValidateMatrixArtifact(l, dir); err != nil {
		fmt.Fprintln(os.Stderr, "chaos check:", err)
		return 2
	}
	fmt.Printf("chaos lane %s: template contract OK (isolation, sanction, project scope, findings-on-owning-board)\n", l.Lane)
	fmt.Printf("board symlink: %s -> %s (resolves to %s = the primary's board)\n", ev.Link, ev.Raw, ev.Resolved)
	return 0
}

func cmdChaosFinding(args []string) int {
	fs := flag.NewFlagSet("chaos finding", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	project := fs.String("project", "", "owning project (required — the board the row lands on)")
	board := fs.String("board", "", "board dir to file onto (required; the primary's board, seen through the lane's symlink)")
	title := fs.String("title", "", "one-line finding (required)")
	reason := fs.String("reason", "", "deciding evidence (required)")
	priority := fs.String("priority", "P3", "P0..P3 (default P3)")
	primitive := fs.String("primitive", "", "the catalog primitive id, when the finding names one")
	verdict := fs.String("verdict", "", "the graded verdict, when the finding carries one (recovered|degraded|not_recovered|hung|corrupted|no_op|aborted|flaky|void — PRD §6.4, closed)")
	if err := fs.Parse(args); err != nil {
		return verbParseExit(err)
	}
	if *project == "" || *board == "" || strings.TrimSpace(*title) == "" || strings.TrimSpace(*reason) == "" {
		fmt.Fprintln(os.Stderr, "chaos finding: --project, --board, --title and --reason are required")
		return 2
	}
	row := chaos.NewFinding(*project, *title, *reason, *priority, *primitive, *verdict)
	path, n, err := chaos.AppendRows(*board, []battery.Finding{row})
	if err != nil {
		fmt.Fprintln(os.Stderr, "chaos finding:", err)
		return 1
	}
	fmt.Printf("chaos finding: %s filed on %s (%d row)\n", row.ID, path, n)
	return 0
}

// chaosUsage is the dispatcher-level help text for `mischief chaos
// --help` (MISCHIEF-CLI-001); each subcommand's FlagSet documents its own
// flags via the standard usage (verbParseExit).
const chaosUsage = `mischief chaos — the operator-free fault-matrix verb

Usage:
  mischief chaos matrix --project P [--tier-max N] [--out D] [--markdown]   derive the project fault matrix
  mischief chaos check --lane F [--lane-dir D]                              verify one instantiated lane
  mischief chaos finding --project P --board D --title T --reason R [--priority P3] [--primitive ID] [--verdict V]
                                                                            file ONE board-vocabulary finding
`
