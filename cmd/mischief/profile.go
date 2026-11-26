// profile.go — the `profile` verb family and its `run` subcommand (PROF-003).
//
// `mischief plan` resolves and renders a profile BY DESIGN and writes nothing;
// `mischief profile run` is the surface that actually applies the faults,
// writes a run dir and emits a graded finding. Splitting them keeps the
// preview-only guarantee of plan intact while giving the agent loop
// (inject → read finding → fix → re-run) a real executor.
//
// Contract (PRD §4): `mischief profile run <class>/<name> --target <selector>
// --json [--seed N]`. With --json the process writes ONE JSON document to
// stdout and nothing else — every human note goes to stderr, because an agent
// piping the run into `jq` must never have to skip a banner. Exit codes are
// the graded outcome: 0 passed, 1 failed, 3 inconclusive. A refusal (bad
// argument, unsanctioned host, protected target, missing profile) is exit 2
// and lands nothing.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/trouble-agent/mischief/internal/profile"
	"github.com/trouble-agent/mischief/internal/rails"
	"github.com/trouble-agent/mischief/internal/sanction"
)

// cmdProfile dispatches the profile subcommands.
func cmdProfile(args []string) int {
	if len(args) == 0 {
		profileUsage(os.Stderr)
		return 2
	}
	switch args[0] {
	case "run":
		return cmdProfileRun(args[1:])
	case "list":
		return cmdProfileList(args[1:])
	case "compare":
		return cmdProfileCompare(args[1:])
	case "help", "-h", "--help":
		profileUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "profile: unknown subcommand %q (want run, list, compare)\n\n", args[0])
		profileUsage(os.Stderr)
		return 2
	}
}

// profileUsage prints the profile verb family's help.
func profileUsage(w *os.File) {
	fmt.Fprint(w, `mischief profile — apply a testing profile and grade the target

Usage:
  mischief profile run  <class>/<name> --target <selector> [--json] [--seed N] [--dir D] [--profiles-dir D] [--catalog-dir D]
  mischief profile list [--profiles-dir D]
  mischief profile compare <run-dir-a> <run-dir-b> [--json]

run loads catalog/profiles/<class>/<name>.yaml, resolves every step's fault
against catalog/faults/, applies each step in order, runs the step's probe and
writes a graded finding to stdout plus a run dir (<dir>/<run_id>/).

compare reads the two run dirs' journals (profile.jsonl), pairs steps by id
between two runs of the SAME profile, and reports the outcome transition per
probe: failed->passed (the fix landed), passed->failed (a regression),
unchanged. Run the same profile twice with the same --seed and DIFFERENT --dir
roots (a run dir is content-addressed, so re-running into one --dir replaces
the previous run's journal), then hand the two run dirs to compare.

Exit codes (the graded outcome):
  0  passed        the fault landed and the probe held
  1  failed        the fault landed and the probe broke
  3  inconclusive  no signal: a missing primitive, an unmet requirement, no
                   actuator, or a target this build cannot attach to
  2  refused       usage, sanction, protected target or missing profile —
                   nothing was landed

--target takes the same selector grammar as arm/plan. Two kinds are
attachable in this build:
  scratch:<path>                       the executor spawns its own scenario
                                       writer under an LD_PRELOAD shim (S-004)
  dbdurability:<fsync|none>:<path>     the executor builds and spawns a
                                       controlled commit-log database child
                                       at <path> with that commit discipline
                                       (S-010 fsync lie; <path> must be
                                       absolute and under the scratch base)
any other kind grades inconclusive.
`)
}

// cmdProfileList lists the profile ids under the profiles tree.
func cmdProfileList(args []string) int {
	fs := flag.NewFlagSet("profile list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	profilesDir := fs.String("profiles-dir", "", "catalog/profiles dir (default: resolved from cwd)")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: mischief profile list [--profiles-dir D]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return verbParseExit(err)
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "profile list: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	dir, err := profilesDirOr(*profilesDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "profile list:", err)
		return 2
	}
	ids, err := profile.List(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "profile list:", err)
		return 1
	}
	for _, id := range ids {
		fmt.Println(id)
	}
	return 0
}

// cmdProfileRun is the executor verb.
func cmdProfileRun(args []string) int {
	fs := flag.NewFlagSet("profile run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	target := fs.String("target", "", "fault target selector (required)")
	asJSON := fs.Bool("json", false, "emit the finding JSON on stdout and nothing else")
	seed := fs.Int64("seed", 0, "seed override (default: the profile's declared seed)")
	dir := fs.String("dir", profileRunDir(), "run dir (the run id becomes a subdirectory)")
	profilesDir := fs.String("profiles-dir", "", "catalog/profiles dir (default: resolved from cwd)")
	catalogDir := fs.String("catalog-dir", "", "catalog/faults dir (default: resolved from cwd)")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: mischief profile run <class>/<name> --target <selector> [--json] [--seed N] [--dir D] [--profiles-dir D] [--catalog-dir D]")
		fs.PrintDefaults()
	}
	// Flags may appear before OR after the profile id (`profile run <id>
	// --target X` is the documented shape), but the flag package stops at the
	// first non-flag token. Consume each positional and re-parse the rest, so
	// the documented command line parses as written.
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return verbParseExit(err)
		}
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}

	// Usage refusals come before any read, gate or write: a bare invocation
	// must land nothing and name what is missing.
	if len(positional) == 0 {
		fmt.Fprintln(os.Stderr, "profile run: missing profile id (want <class>/<name>, e.g. storage/short-write)")
		return 2
	}
	if len(positional) > 1 {
		fmt.Fprintf(os.Stderr, "profile run: unexpected argument %q (the profile is one positional id)\n", positional[1])
		return 2
	}
	if strings.TrimSpace(*target) == "" {
		fmt.Fprintln(os.Stderr, "profile run: missing --target (no default target: name the selector explicitly)")
		return 2
	}
	class, name, err := profile.SplitID(positional[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "profile run:", err)
		return 2
	}
	// --seed 0 is a legal seed, so explicitness is read from the parsed flags
	// rather than from the value (the fs.Visit rule).
	seedExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "seed" {
			seedExplicit = true
		}
	})

	// Resolution rails first (protected targets refuse before anything is
	// read or written), then the sanction gate (SPEC-13: this verb lands
	// faults), matching plan/selftest/battery/arm.
	decl := rails.DeclaredTarget{Kind: declaredTargetKind(*target), Selector: *target}
	if _, err := rails.Resolve(decl, rails.ResolveOptions{SelfPID: 0, SelfExe: ""}); err != nil {
		fmt.Fprintln(os.Stderr, verbErr("profile run", err))
		return rails.MapExit(err)
	}
	if err := sanction.Check(sanction.Options{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	pDir, err := profilesDirOr(*profilesDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "profile run:", err)
		return 2
	}
	p, raw, err := profile.Load(pDir, class, name)
	if err != nil {
		fmt.Fprintln(os.Stderr, "profile run:", err)
		return 2
	}

	cat, err := loadCatalog(*catalogDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "profile run:", err)
		return 2
	}

	effectiveSeed := p.Seed
	if seedExplicit {
		effectiveSeed = *seed
	}

	tgt := profile.Target{Kind: "scratch", Selector: *target, Path: scratchPath(*target)}
	if disc, path, ok := dbDurabilityTarget(*target); ok {
		tgt = profile.Target{Kind: "dbdurability", Selector: *target, Path: path, Discipline: disc}
	} else if decl.Kind != rails.TargetHost {
		tgt = profile.Target{Kind: string(decl.Kind), Selector: *target}
	}

	runIDPreview := profile.RunID(raw, effectiveSeed)
	runDir := filepath.Join(*dir, runIDPreview)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "profile run: run dir:", err)
		return 1
	}

	reproduce := fmt.Sprintf("mischief profile run %s --target %s --seed %d --json", p.ID(), *target, effectiveSeed)
	finding, err := profile.Run(context.Background(), profile.Options{
		Profile:   p,
		Raw:       raw,
		Seed:      effectiveSeed,
		Target:    tgt,
		Catalog:   cat,
		RunDir:    runDir,
		Reproduce: reproduce,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "profile run:", err)
		return 1
	}

	if *asJSON {
		doc, err := finding.JSON()
		if err != nil {
			fmt.Fprintln(os.Stderr, "profile run:", err)
			return 1
		}
		if _, err := os.Stdout.Write(doc); err != nil {
			fmt.Fprintln(os.Stderr, "profile run: stdout:", err)
			return 1
		}
		// Every human-facing note goes to stderr so --json stdout stays one
		// parseable document.
		fmt.Fprintf(os.Stderr, "profile run: %s — journal %s\n", finding.Outcome, filepath.Join(runDir, "profile.jsonl"))
	} else {
		for _, line := range finding.Lines() {
			fmt.Println(line)
		}
	}
	return finding.Outcome.ExitCode()
}

// cmdProfileCompare is the before/after verb (PROF-031): two run dirs of the
// same profile in, the per-step outcome transitions out. dir A is the
// before-run, dir B the after-run — the transition tokens are directional
// (failed->passed means the fix landed; passed->failed means a regression).
func cmdProfileCompare(args []string) int {
	fs := flag.NewFlagSet("profile compare", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "emit the compare report as ONE JSON document on stdout")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: mischief profile compare <run-dir-a> <run-dir-b> [--json]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return verbParseExit(err)
	}
	// Flags may trail the two positional run dirs; consume each positional and
	// re-parse the rest, so `compare <a> <b> --json` parses as written.
	var positional []string
	rest := fs.Args()
	for {
		if err := fs.Parse(rest); err != nil {
			return verbParseExit(err)
		}
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}
	if len(positional) != 2 {
		fmt.Fprintln(os.Stderr, "profile compare: want exactly two run dirs (the before-run dir, then the after-run dir), e.g. <dir-a>/<run_id> <dir-b>/<run_id>")
		return 2
	}
	report, err := profile.CompareRunDirs(positional[0], positional[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "profile compare:", err)
		return 2
	}
	if *asJSON {
		doc, err := report.JSON()
		if err != nil {
			fmt.Fprintln(os.Stderr, "profile compare:", err)
			return 1
		}
		if _, err := os.Stdout.Write(doc); err != nil {
			fmt.Fprintln(os.Stderr, "profile compare: stdout:", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "profile compare: %d changed, %d unchanged\n", report.Changed, report.Unchanged)
		return 0
	}
	for _, line := range report.Lines() {
		fmt.Println(line)
	}
	return 0
}

// profileRunDir is the default run dir for profile runs.
func profileRunDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".mischief", "runs", "profile")
}

// profilesDirOr resolves the profiles tree: an explicit flag wins, otherwise
// catalog/profiles is found from the working directory upward.
func profilesDirOr(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return explicit, nil
	}
	return profile.FindDefaultDir()
}

// scratchPath extracts the path of a `scratch:<path>` selector; any other
// selector yields "" (the executor then grades it inconclusive rather than
// silently faulting a path the operator did not name).
func scratchPath(selector string) string {
	rest, ok := strings.CutPrefix(selector, "scratch:")
	if !ok {
		return ""
	}
	return strings.TrimSpace(rest)
}

// dbDurabilityTarget parses a `dbdurability:<discipline>:<path>` selector —
// the controlled commit-log database the S-010 fsync-lie actuator builds and
// spawns as a child. ok is true for any selector carrying the prefix (a
// malformed one keeps the kind so the executor's durability-target
// requirement names what is wrong: a broken discipline or a missing path
// grades inconclusive, never a refusal that hides the selector's shape).
func dbDurabilityTarget(selector string) (discipline, path string, ok bool) {
	rest, isDur := strings.CutPrefix(selector, "dbdurability:")
	if !isDur {
		return "", "", false
	}
	disc, pathRest, found := strings.Cut(rest, ":")
	if !found {
		return strings.TrimSpace(rest), "", true
	}
	return strings.TrimSpace(disc), strings.TrimSpace(pathRest), true
}

// profilePreviewID derives the run id the CLI names the run dir with, using the
// same content+seed derivation the executor uses (journal.RunID), so the dir
// the CLI creates is the dir the finding reports.
func profilePreviewID(raw []byte, seed int64) string {
	return profile.RunID(raw, seed)
}
