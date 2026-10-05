package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/trouble-agent/mischief/internal/ops"
)

// ops.go — the SPEC-11 operator verbs (MSF-012):
//
//	mischief install    [--user U] [--prefix P] [--dry-run] [--no-drop-in]
//	mischief uninstall  [--prefix P] [--dry-run] [--keep-count N] [--keep-days D]
//	mischief audit      [--prefix P] [--json]
//	mischief retention  --apply [--runs-dir D] [--keep-count N] [--keep-days D] [--dry-run]
//
// SAFETY (PRD §7 + SPEC-11):
//
//   - install NEVER elevates: it writes the artefacts the caller already
//     has the rights for, and refuses (fail closed) over a foreign file;
//   - the privileged surface is exactly one sudoers drop-in generated from
//     the canonical verb table (internal/ops.VerbTable) — no setuid binary
//     exists anywhere in the design;
//   - every verb is dry-runnable: --dry-run prints the exact plan and
//     touches nothing (the tests run the plan engine against temp roots);
//   - real-mode install validates the drop-in with visudo before it lands
//     and refuses when visudo is unavailable — an unchecked drop-in never
//     installs;
//   - this dev host is NOT an install target: the acceptance runs are
//     plan-only + temp-root executions in tests. witness=none.
//
// ch:trace row=MSF-012 spec=docs/SPEC-PLAN.md#SPEC-11 evidence=cmd/mischief/ops.go + internal/ops/ witness=none:no-real-install-run-on-dev-host

// visudoPath is the visudo binary used to validate a real drop-in write.
const visudoPath = "/usr/sbin/visudo"

func cmdInstall(args []string) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	user := fs.String("user", "", "sudoers grantee account (default: mischief)")
	prefix := fs.String("prefix", ops.DefaultPrefix, "mischief state prefix")
	dryRun := fs.Bool("dry-run", false, "print the exact plan; touch nothing")
	noDropIn := fs.Bool("no-drop-in", false, "plan/install the manifest only (the drop-in needs root)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	opts := ops.InstallOpts{
		SudoersUser: *user,
		Prefix:      *prefix,
		DryRun:      *dryRun,
		SkipDropIn:  *noDropIn,
	}
	if !*dryRun && !*noDropIn {
		// Real drop-in write: validate with visudo first (fail closed
		// when unavailable). The dev host has no visudo — a real install
		// belongs on the sanctioned host's provisioning step.
		if _, err := os.Stat(visudoPath); err != nil {
			fmt.Fprintf(os.Stderr, "install: refusing a real drop-in write without %s (validate first, or pass --dry-run / --no-drop-in)\n", visudoPath)
			return 1
		}
		opts.ValidateDropIn = true
	}
	plan, err := ops.PlanInstall(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "install:", err)
		return 1
	}
	fmt.Print(plan.Render())
	if plan.DryRun {
		return 0
	}
	sums, err := plan.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, "install:", err)
		return 1
	}
	for path, sum := range sums {
		fmt.Printf("installed %-42s sha256=%s\n", path, sum)
	}
	return 0
}

func cmdUninstall(args []string) int {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	prefix := fs.String("prefix", ops.DefaultPrefix, "mischief state prefix")
	runsDir := fs.String("runs-dir", "", "journal runs dir to expire per the retention policy (empty: derive under $HOME)")
	keepCount := fs.String("keep-count", "", "retention: keep at most N newest run dirs")
	keepDays := fs.String("keep-days", "", "retention: expire run dirs older than D days")
	dryRun := fs.Bool("dry-run", false, "print the exact plan; touch nothing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pol, err := ops.ParseRetentionFlags(*keepCount, *keepDays)
	if err != nil {
		fmt.Fprintln(os.Stderr, "uninstall:", err)
		return 2
	}
	rd := *runsDir
	if rd == "" {
		rd = defaultRunsDir()
	}
	opts := ops.InstallOpts{Prefix: *prefix, DryRun: *dryRun}
	plan, err := ops.PlanUninstall(opts, rd, pol)
	if err != nil {
		fmt.Fprintln(os.Stderr, "uninstall:", err)
		return 1
	}
	fmt.Print(plan.Render())
	if plan.DryRun {
		return 0
	}
	removed, err := plan.ExecuteUninstall()
	if err != nil {
		fmt.Fprintln(os.Stderr, "uninstall:", err)
		return 1
	}
	for _, p := range removed {
		fmt.Printf("removed %s\n", p)
	}
	fmt.Printf("uninstall: %d artefact(s) removed\n", len(removed))
	return 0
}

func cmdAudit(args []string) int {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	prefix := fs.String("prefix", ops.DefaultPrefix, "mischief state prefix to survey")
	user := fs.String("user", "", "sudoers grantee the expected drop-in was rendered for (default: mischief)")
	asJSON := fs.Bool("json", false, "emit JSON (the operator diff surface)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	a, err := ops.RunAudit(ops.AuditOpts{Prefix: *prefix, SudoersUser: *user})
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit:", err)
		return 1
	}
	// Exit: drift or a setuid finding is a non-zero audit result (1) —
	// the operator's diff said NO. Both output forms carry the same
	// verdict (the JSON is a surface, not a separate policy).
	verdict := 0
	if !a.DropInMatches || len(a.SetuidBinaries) > 0 {
		verdict = 1
	}
	if *asJSON {
		os.Stdout.Write(ops.RenderAuditJSON(a))
		return verdict
	}
	// text form: the same facts, operator-readable
	fmt.Printf("drop-in: %s installed=%v matches_verb_table=%v\n", a.DropInPath, a.DropInInstalled, a.DropInMatches)
	if a.DropInDiff != "" {
		fmt.Println(a.DropInDiff)
	}
	fmt.Printf("helper manifest: %s installed=%v matches=%v\n", a.HelperManifestPath, a.HelperManifestInstalled, a.HelperManifestMatches)
	if len(a.SetuidBinaries) == 0 {
		fmt.Println("setuid binaries in prefix: none (expected — the design ships none)")
	} else {
		fmt.Printf("setuid binaries in prefix: %d (DESIGN SHIPS NONE — investigate)\n", len(a.SetuidBinaries))
		for _, s := range a.SetuidBinaries {
			fmt.Println("  " + s)
		}
	}
	fmt.Printf("verbs (%d):\n", len(a.Verbs))
	for _, v := range a.Verbs {
		grant := "delegated(rootless)"
		switch {
		case v.GrantedInDropIn && v.WildcardGrant:
			grant = "granted (WILDCARD — deliberate, measured)"
		case v.GrantedInDropIn:
			grant = "granted"
		case v.Exec == "sudo":
			grant = "NOT granted"
		}
		fmt.Printf("  %-20s %-8s %-24s %s\n      %s\n", v.ID, v.Exec, v.Binary, grant, v.Reason)
	}
	for _, n := range a.Notes {
		fmt.Println("note: " + n)
	}
	return verdict
}

func cmdRetention(args []string) int {
	fs := flag.NewFlagSet("retention", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	apply := fs.Bool("apply", false, "apply the rotation (default: report only)")
	runsDir := fs.String("runs-dir", "", "journal runs dir (default: derived under $HOME)")
	keepCount := fs.String("keep-count", "", "keep at most N newest run dirs (default: 20)")
	keepDays := fs.String("keep-days", "", "expire run dirs older than D days (default: 30)")
	dryRun := fs.Bool("dry-run", false, "print the exact plan; touch nothing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pol, err := ops.ParseRetentionFlags(*keepCount, *keepDays)
	if err != nil {
		fmt.Fprintln(os.Stderr, "retention:", err)
		return 2
	}
	rd := *runsDir
	if rd == "" {
		rd = defaultRunsDir()
	}
	plan, err := ops.PlanRetention(rd, pol)
	if err != nil {
		fmt.Fprintln(os.Stderr, "retention:", err)
		return 1
	}
	if !*apply && !*dryRun {
		// report-only: same plan, clearly labelled
		plan.DryRun = true
	}
	if *dryRun {
		plan.DryRun = true
	}
	fmt.Print(plan.Render())
	if plan.DryRun {
		return 0
	}
	removed, err := ops.ExecuteRetention(rd, plan)
	if err != nil {
		fmt.Fprintln(os.Stderr, "retention:", err)
		return 1
	}
	for _, p := range removed {
		fmt.Printf("rotated-away %s\n", p)
	}
	fmt.Printf("retention: %d run dir(s) expired\n", len(removed))
	return 0
}

// defaultRunsDir is the journal home's parent: ~/.mischief/runs (the run
// DIRS are what retention rotates).
func defaultRunsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return home + "/.mischief/runs"
}

// opsUsageFooter extends the main usage with the SPEC-11 verbs.
func opsUsageFooter() string {
	var b strings.Builder
	b.WriteString(`
  mischief install    [--user U] [--prefix P] [--dry-run] [--no-drop-in]
  mischief uninstall  [--prefix P] [--dry-run] [--keep-count N] [--keep-days D]
  mischief audit      [--prefix P] [--json]
  mischief retention  [--apply] [--runs-dir D] [--keep-count N] [--keep-days D] [--dry-run]
`)
	return b.String()
}

var _ = visudoPath
