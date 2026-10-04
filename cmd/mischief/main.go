// Command mischief is the M1 chassis binary (MSF-014): a single
// CGO_ENABLED=0 Go binary with the plan/status/revert/doctor verbs, the
// reverter daemon, and the stamped --version.
//
// Verbs (PRD §6.1; M1 scope marks the ones this build ships):
//
//	plan     — resolve an experiment, emit the plan, ZERO side effects (AC-1)
//	status   — fold the run dir's journal: armed/held holds, stuck, proofs
//	revert   — measured revert by --all / --hold selector (AC-4/AC-6)
//	doctor   — capability/tier ladder report, refuses nothing extra
//	serve    — the reverter daemon: TTL ownership + boot reconcile + health.json
//	__owner  — the detached TTL owner entry (spawned by the reverter package;
//	           re-execs this binary; not an operator verb)
//	version  — the stamped build identity
//
// Safety floor (PRD §7), enforced on every landing path:
//
//   - no default target (a verb without a resolved target refuses, exit 2);
//   - protected targets refuse at resolution (rails.Resolve, AC-7);
//   - the sanction marker gate runs before any verb that could arm or land
//     (SPEC-13/MSF-020 stub: fail closed, naming host + missing marker);
//   - the load gate refuses to land on a saturated host (AC-8; M1 reads
//     the live numbers and surfaces the measurement).
//
// ch:trace row=MSF-014 spec=docs/prd/mischief-v0.1.md evidence=cmd/mischief/ + Makefile witness=none:no-live-target-run-in-worktree
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/trouble-agent/mischief/internal/catalog"
	"github.com/trouble-agent/mischief/internal/daemon"
	"github.com/trouble-agent/mischief/internal/doctor"
	"github.com/trouble-agent/mischief/internal/journal"
	"github.com/trouble-agent/mischief/internal/plan"
	"github.com/trouble-agent/mischief/internal/rails"
	"github.com/trouble-agent/mischief/internal/reverter"
	"github.com/trouble-agent/mischief/internal/sanction"
)

// gitSha is stamped at build time by the Makefile's bin target:
//
//	-X main.gitSha=$(git rev-parse --short=12 HEAD)
//
// "unknown" means an unstamped build (a plain `go build`); version prints
// it verbatim — never a fabricated sha.
var gitSha = "unknown"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}
	switch args[0] {
	case "plan":
		return cmdPlan(args[1:])
	case "status":
		return cmdStatus(args[1:])
	case "revert":
		return cmdRevert(args[1:])
	case "doctor":
		return cmdDoctor(args[1:])
	case "serve":
		return cmdServe(args[1:])
	case "__owner":
		// The detached TTL owner (reverter.RunOwner). Not listed in usage:
		// it is a spawn target, not an operator verb.
		if err := reverter.RunOwner(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "owner:", err)
			return 1
		}
		return 0
	case "version", "--version":
		fmt.Printf("mischief %s\n", gitSha)
		return 0
	case "help", "-h", "--help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "mischief: unknown verb %q\n\n", args[0])
		usage(os.Stderr)
		return 2
	}
}

func usage(w *os.File) {
	fmt.Fprint(w, `mischief — fault injection for a fleet of Linux daemons (M1 chassis)

Usage:
  mischief plan    -f <exp.yaml> [--json] [--scratch-dir D]
  mischief status  [--dir D]
  mischief revert  (--all | --hold <id>) [--dir D]
  mischief doctor  [--catalog-dir D]
  mischief serve   --dir D          (reverter daemon: TTLs + boot reconcile + health.json)
  mischief version

Safety floor: no default target; protected targets refuse at resolution;
sanction-marker gate (SPEC-13) fail-closed; load gate on every landing path.
`)
}

// defaultRunDir resolves the default run dir (M1: ~/.mischief/runs/<label>),
// overridable by --dir on every journal-touching verb.
func defaultRunDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".mischief", "runs", "default")
}

// ── plan ─────────────────────────────────────────────────────────────────────

func cmdPlan(args []string) int {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	expPath := fs.String("f", "", "experiment YAML (required)")
	catalogDir := fs.String("catalog-dir", "", "catalog dir (default: catalog/faults from cwd upward)")
	scratch := fs.Bool("scratch-dir", false, "report the L0 scratch posture (planning only; run owns the flag)")
	asJSON := fs.Bool("json", false, "emit JSON instead of text")
	skipSanction := fs.Bool("plan-only-sanction-skip", false, "(unimplemented stub flag: reserved for MSF-020; plan never lands faults)")
	_ = scratch
	_ = skipSanction
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *expPath == "" {
		fmt.Fprintln(os.Stderr, "plan: -f <exp.yaml> is required (no default experiment)")
		return 2
	}

	// AC-1: plan is a dry run — the sanction gate still runs (it is a
	// host posture check, not a landing act) but NOTHING is written: the
	// only output is stdout. No run dir, no journal, no target writes.
	if err := sanction.Check(sanction.Options{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	exp, err := journal.Load(*expPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "plan:", err)
		return 1
	}
	raw, err := os.ReadFile(*expPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "plan: read:", err)
		return 1
	}
	cat, err := loadCatalog(*catalogDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "plan:", err)
		return 1
	}
	reg := hostCapabilityRegistry()
	scope := rails.Scope{Level: 0, Scratch: true} // plan previews the L0 scratch posture; run applies its own scope
	p, err := plan.Build(exp, raw, cat, reg, scope)
	if err != nil {
		// The plan still renders (the operator sees what resolved), but
		// the exit is the refusal's (2) — rails.MapExit owns the mapping.
		out := renderPlan(p, *asJSON)
		os.Stdout.Write(out)
		fmt.Fprintln(os.Stderr, "plan:", err)
		return rails.MapExit(err)
	}
	os.Stdout.Write(renderPlan(p, *asJSON))
	// A plan that surfaced rail refusals (unknown primitive, scope below
	// minimum, protected target previewed) exits 2 — the rails exit
	// contract, applied to the report even though plan landed nothing.
	if len(p.Refusals) > 0 {
		return rails.ExitCode
	}
	return 0
}

func renderPlan(p *plan.Plan, asJSON bool) []byte {
	if asJSON && p != nil {
		b, err := json.MarshalIndent(p, "", "  ")
		if err == nil {
			return append(b, '\n')
		}
	}
	return plan.Render(p)
}

// ── status ───────────────────────────────────────────────────────────────────

func cmdStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dir := fs.String("dir", defaultRunDir(), "run dir (journal home)")
	asJSON := fs.Bool("json", false, "emit JSON instead of text")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	store, err := reverter.Open(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "status:", err)
		return 1
	}
	defer store.Close()
	st := reverter.New(store).Status()
	if *asJSON {
		b, err := json.MarshalIndent(st, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "status:", err)
			return 1
		}
		os.Stdout.Write(append(b, '\n'))
		return 0
	}
	fmt.Printf("run_dir: %s\n", *dir)
	fmt.Printf("boot_id: %s\n", st.BootID)
	fmt.Printf("holds: %d\n", len(st.Holds))
	for _, h := range st.Holds {
		line := fmt.Sprintf("  %-16s %-10s %-8s fault=%s target=%s", h.HoldID, h.Status, "", h.FaultID, h.Target)
		if !h.Expiry.IsZero() {
			line += fmt.Sprintf(" ttl_remaining=%s", time.Until(h.Expiry).Round(time.Second))
		}
		if !h.StuckAt.IsZero() {
			line += fmt.Sprintf(" stuck_at=%s", h.StuckAt.Format(time.RFC3339))
		}
		fmt.Println(line)
	}
	fmt.Printf("proofs: %d\n", len(st.History))
	for _, pr := range st.History {
		fmt.Printf("  %-16s %-14s %s role=%s reason=%s detail=%s\n", pr.HoldID, pr.Outcome, pr.FaultID, pr.Role, pr.Reason, pr.Detail)
	}
	return 0
}

// ── revert ───────────────────────────────────────────────────────────────────

func cmdRevert(args []string) int {
	fs := flag.NewFlagSet("revert", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dir := fs.String("dir", defaultRunDir(), "run dir (journal home)")
	all := fs.Bool("all", false, "kill switch: revert every live or stuck hold (AC-6)")
	hold := fs.String("hold", "", "revert one hold by id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !*all && *hold == "" {
		fmt.Fprintln(os.Stderr, "revert: --all or --hold <id> is required (reverting nothing is a report, not a pass)")
		return 2
	}
	if *all && *hold != "" {
		fmt.Fprintln(os.Stderr, "revert: --all and --hold are mutually exclusive")
		return 2
	}
	store, err := reverter.Open(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "revert:", err)
		return 1
	}
	defer store.Close()
	lf := reverter.New(store)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if *all {
		proofs, wall, err := lf.RevertAll(ctx, "kill_switch")
		for _, pr := range proofs {
			fmt.Printf("reverted %-16s outcome=%s fault=%s detail=%s (%s)\n", pr.HoldID, pr.Outcome, pr.FaultID, pr.Detail, pr.Duration.Round(time.Millisecond))
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "revert:", err)
			return 1
		}
		fmt.Printf("kill switch: %d holds reverted, wall=%s (AC-6 budget 2s)\n", len(proofs), wall.Round(time.Millisecond))
		return 0
	}
	pr, err := lf.Revert(ctx, *hold, "direct", reverter.RoleAPI)
	if err != nil {
		fmt.Fprintln(os.Stderr, "revert:", err)
		return 1
	}
	fmt.Printf("reverted %-16s outcome=%s detail=%s duration=%s\n", pr.HoldID, pr.Outcome, pr.Detail, pr.Duration.Round(time.Millisecond))
	if pr.Outcome != reverter.OutcomeReverted {
		return 1
	}
	return 0
}

// ── doctor ───────────────────────────────────────────────────────────────────

func cmdDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	catalogDir := fs.String("catalog-dir", "", "catalog dir (default: catalog/faults from cwd upward)")
	selfCheck := fs.Bool("self-check", false, "run the rails self-checks (sanction marker, load gate, scope ladder) and exit non-zero naming what fails")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rep, err := doctor.Run(*catalogDir, hostCapabilityRegistry())
	if err != nil {
		fmt.Fprintln(os.Stderr, "doctor:", err)
		return 1
	}
	if *selfCheck {
		doctor.RunRailsSelfChecks(rep, rep.Catalog)
	}
	os.Stdout.Write(doctor.Render(rep))
	// doctor refuses nothing extra: a load failure is REPORTED (exit 1 —
	// the doctor could not do its one job), a loaded catalog is exit 0
	// regardless of how many primitives are capability_unavailable. With
	// --self-check, a FAILING rail check is also a non-zero exit (the
	// brief: "exits non-zero naming what fails" — the report above names
	// every failing row before the exit).
	if rep.LoadErr != "" {
		return 1
	}
	if *selfCheck {
		for _, r := range rep.Rails {
			if !r.OK {
				return 1
			}
		}
	}
	return 0
}

// ── serve (the reverter daemon) ──────────────────────────────────────────────

func cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dir := fs.String("dir", defaultRunDir(), "run dir to own (journal + health.json)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	d, err := daemon.New(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "serve:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Printf("mischiefd: run_dir=%s boot=%s health=%s\n", *dir, "folding", filepath.Join(*dir, daemon.HealthFileName))
	if err := d.Run(ctx); err != nil && err != context.Canceled {
		fmt.Fprintln(os.Stderr, "serve:", err)
		return 1
	}
	return 0
}

// ── shared helpers ───────────────────────────────────────────────────────────

func loadCatalog(dir string) (*catalog.Catalog, error) {
	if dir == "" {
		return catalog.LoadDefault()
	}
	return catalog.LoadDir(dir)
}
