package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/trouble-agent/mischief/internal/battery"
)

// battery.go — the `mischief battery` verb (SPEC-10, MSF-011).
//
// mischief battery [--quick] [--project <p>]... [--primitive <id>]...
//                  [--out <dir>] [--dir <run-dir>] [--file-rows]
//                  [--catalog-dir D] [--dry-run]
//
// Runs the fault × target matrix, grades every cell through the SPEC-05
// verdict engine on the selftest harness's measured land+revert proofs,
// and emits under --out (default: the run dir):
//
//	matrix.json / matrix.md — the fault × detection × recovery artefact
//	                          (S-5); the --quick artefact names the five
//	                          bunker-qa.sh cells each quick cell replaces
//	battery.jsonl           — the run journal (SPEC-02 record format)
//	findings.jsonl          — AC-18 board rows for every adverse verdict,
//	                          in the mischief board's row vocabulary,
//	                          the journal path riding in `reasoning`
//
// --quick runs the reduced parity matrix: exactly the five legacy bash
// chaos cells (chaos-disconnect, chaos-shutdown, chaos-corruption,
// chaos-resource, chaos-errorpath), each named alongside the catalog
// primitive that replaces it. MSF-016 (M4): parity is proven live
// (docs/BATTERY-PARITY.md + the committed run artefact) and the five bash
// cells are SUPERSEDED — their fleet-side deletion is the checklist in
// docs/BATTERY-PARITY.md (bunker-qa.sh is outside this repo).
//
// --file-rows emits findings.jsonl (AC-18's artefact). The rows are
// EMITTED, not appended onto a live board: landing them on the owning
// project's board is the operator/foreman merge step; the in-package
// validator (the boardctl-validate-style self-check) runs on every emit.
//
// AC-14 honesty: no cell carries a live trouble-ledger detector in v0.1 —
// every detection entry records the named not-wired reason; the Detector
// seam is the assertion point the real integration plugs into.
//
// ch:trace row=MSF-011 spec=docs/SPEC-PLAN.md#SPEC-10 evidence=internal/battery/ + cmd/mischief/battery.go witness=none:scratch-only-live-runs

func cmdBattery(args []string) int {
	fs := flag.NewFlagSet("battery", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	quick := fs.Bool("quick", false, "run the parity matrix: the five bunker-qa.sh chaos cells, each replaced by its catalog primitive")
	projects := fs.String("project", "", "owning project(s), comma-separated (full matrix; default: scratch)")
	prims := fs.String("primitive", "", "primitive id(s), comma-separated (full matrix; default: every catalog primitive)")
	out := fs.String("out", "", "artefact dir (default: the run dir)")
	dir := fs.String("dir", defaultRunDir(), "run dir (journal + artefact home)")
	fileRows := fs.Bool("file-rows", false, "emit findings.jsonl (AC-18 board rows for adverse verdicts)")
	catalogDir := fs.String("catalog-dir", "", "catalog dir (default: catalog/faults from cwd upward)")
	dryRun := fs.Bool("dry-run", false, "print the resolved matrix and exit (nothing executes)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !*quick && *projects == "" && *prims == "" {
		fmt.Fprintln(os.Stderr, "battery: nothing to run — pass --quick (the parity matrix), or --project/--primitive for the full matrix")
		return 2
	}

	cat, err := loadCatalog(*catalogDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "battery:", err)
		return 1
	}

	var m *battery.Matrix
	switch {
	case *quick:
		m = battery.QuickMatrix()
		if *prims != "" || *projects != "" {
			fmt.Fprintln(os.Stderr, "battery: --quick ignores --project/--primitive (the parity matrix is fixed: the five bunker-qa.sh cells)")
		}
	default:
		var plist, prlist []string
		if *projects != "" {
			plist = strings.Split(*projects, ",")
		}
		if *prims != "" {
			prlist = strings.Split(*prims, ",")
		} else {
			prlist = cat.IDs // the full matrix defaults to the whole catalog
		}
		if len(plist) == 0 {
			plist = []string{"scratch"}
		}
		m = battery.DefaultMatrix(plist, prlist)
	}

	if *dryRun {
		mode := "full"
		if m.Quick {
			mode = "--quick (parity)"
		}
		fmt.Printf("battery %s: %d cells\n", mode, len(m.Cells))
		for _, c := range m.Cells {
			legacy := ""
			if c.LegacyCell != "" {
				legacy = fmt.Sprintf("  [replaces %s]", c.LegacyCell)
			}
			fmt.Printf("  %-6s %-6s project=%s%s\n", c.ID, c.Primitive, c.Target.Project, legacy)
		}
		return 0
	}

	// Runner: the production executor (the selftest harness's land+prove+
	// revert loop per cell; no second actuator). Detectors stay unwired —
	// the honest AC-14 posture this build ships.
	rn := battery.NewRunner(cat)
	res, err := rn.Run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "battery:", err)
		return 2
	}

	// Journal first: the findings' reasoning carries the journal path
	// (AC-18), so the path must exist before rows are rendered. The run
	// id is already stamped on the result by the runner (Run derives it
	// from the finished result); re-derive here and assert the same
	// value — a mismatch would break the AC-9 correlation.
	runID := battery.RunResultRunID(res)
	if res.RunID != runID {
		fmt.Fprintln(os.Stderr, "battery: run id drifted between the runner and the CLI derivation:", res.RunID, runID)
		return 1
	}
	journalPath, jerr := battery.WriteJournal(*dir, res.JournalRecords(runID))
	if jerr != nil {
		fmt.Fprintln(os.Stderr, "battery: journal write failed:", jerr)
		// findings may still render, but without the AC-18 path — fail
		// the run: a finding whose evidence is unreachable is not filable
		return 1
	}

	outDir := *out
	if outDir == "" {
		outDir = *dir
	}
	artefactPath, err := battery.WriteArtefact(outDir, battery.BuildArtefact(res, runID, cat.Version, res.FinishedAt))
	if err != nil {
		fmt.Fprintln(os.Stderr, "battery:", err)
		return 1
	}

	// Print the per-cell lines (verdict vocabulary verbatim, AC-9's CLI
	// hop) and the summary.
	for i := range res.Cells {
		c := &res.Cells[i]
		legacy := ""
		if c.Cell.LegacyCell != "" {
			legacy = fmt.Sprintf(" [replaces %s]", c.Cell.LegacyCell)
		}
		switch c.SelftestState {
		case "pass":
			fmt.Printf("%-6s %-6s PASS  %s%s\n", c.Cell.ID, c.Cell.Primitive, c.Verdict, legacy)
		case "skip":
			fmt.Printf("%-6s %-6s SKIP  %s — %s%s\n", c.Cell.ID, c.Cell.Primitive, c.Verdict, c.SkipReason, legacy)
		default:
			fmt.Printf("%-6s %-6s FAIL  %s — %s%s\n", c.Cell.ID, c.Cell.Primitive, c.Verdict, firstReasonLine(c.Reason), legacy)
		}
	}
	fmt.Println(res.Counts())
	// AC-9's CLI hop (MSF-031): the run's AGGREGATE verdict value and the
	// run id print together — the same aggregate the journal's verdict
	// record carries and the same id every journal record and findings
	// row names, so the three hops correlate by grep on one line.
	fmt.Printf("verdict: %s (run %s)\n", res.AggregateVerdict(), runID)

	exit := 0
	// Findings (AC-18): emitted + validated when --file-rows.
	if *fileRows {
		p, n, err := battery.WriteFindings(outDir, res, runID, journalPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "battery:", err)
			return 1
		}
		// the boardctl-validate-style self-check on the EMITTED bytes
		if b, rerr := os.ReadFile(p); rerr == nil {
			if verr := battery.ValidateRows(b); verr != nil {
				fmt.Fprintln(os.Stderr, "battery: findings self-check FAILED:", verr)
				exit = 1
			}
		}
		if n > 0 {
			fmt.Printf("findings: %d row(s) → %s (board vocabulary, validated)\n", n, p)
		} else {
			fmt.Printf("findings: none (empty %s)\n", p)
		}
	}

	fmt.Printf("artefact: %s (+ matrix.md)\njournal: %s (run_id %s)\n", artefactPath, journalPath, runID)
	return exit
}

// firstReasonLine keeps a FAIL line to its first reason line.
func firstReasonLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
