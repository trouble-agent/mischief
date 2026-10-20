// Command mischief is the M1 chassis binary (MSF-014): a single
// CGO_ENABLED=0 Go binary with the plan/status/revert/doctor verbs, the
// reverter daemon, and the stamped --version.
//
// Verbs (PRD §6.1; M1 scope marks the ones this build ships):
//
//	plan     — resolve an experiment, emit the plan, ZERO side effects
//	           (AC-1: nothing is written; --journal-dir D opts IN to
//	           recording the preview as D/plan.jsonl — see plan.go)
//	status   — fold the run dir's journal: armed/held holds, stuck, proofs
//	revert   — measured revert by --all / --hold selector (AC-4/AC-6);
//	           --retry re-executes the recorded inverse of a terminal
//	           revert_failed hold (one more measured attempt, AC-4 grading)
//	doctor   — capability/tier ladder report, refuses nothing extra
//	serve    — the reverter daemon: TTL ownership + boot reconcile + health.json
//	arm      — write-ahead a reverter hold (fault + inverse/check decls + TTL;
//	           SPEC-04: the inverse is durable before any fault may land)
//	__owner  — the detached TTL owner entry (spawned by the reverter package;
//	           re-execs this binary; not an operator verb)
//	version  — the stamped build identity
//
// Safety floor (PRD §7), enforced on every landing path:
//
//   - no default target (a verb without a resolved target refuses, exit 2);
//   - protected targets refuse at resolution (rails.Resolve, AC-7);
//   - the sanction marker gate runs before any verb that could arm or land
//     (SPEC-13/MSF-020; since MSF-032 the gate covers plan + selftest +
//     battery — the fault-landing verbs — fail closed, naming host and
//     missing marker);
//   - the load gate refuses to land on a saturated host (AC-8; M1 reads
//     the live numbers and surfaces the measurement).
//
// ch:trace row=MSF-014 spec=docs/prd/mischief-v0.1.md evidence=cmd/mischief/ + Makefile witness=none:no-live-target-run-in-worktree
package main

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/trouble-agent/mischief/internal/reverter"
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
	case "install":
		return cmdInstall(args[1:])
	case "uninstall":
		return cmdUninstall(args[1:])
	case "audit":
		return cmdAudit(args[1:])
	case "retention":
		return cmdRetention(args[1:])
	case "selftest":
		return cmdSelftest(args[1:])
	case "plane":
		return cmdPlane(args[1:])
	case "battery":
		return cmdBattery(args[1:])
	case "chaos":
		return cmdChaos(args[1:])
	case "sim":
		return cmdSim(args[1:])
	case "serve":
		return cmdServe(args[1:])
	case "arm":
		// DF-05: the write-ahead verb — arm a reverter hold (fault,
		// inverse, check, TTL) into the run dir's journal. The write-ahead
		// half of the loop that serve/owner/revert complete.
		return cmdArm(args[1:])
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

// verbParseExit maps a FlagSet parse error to the verb's exit code.
// flag.ErrHelp — the conventional --help / -h request — is exit 0 with
// usage text (the flag package prints the FlagSet usage itself); every
// other parse error stays a usage error (exit 2). MISCHIEF-CLI-001.
func verbParseExit(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 2
}

// usage prints the top-level help. The verb list mirrors README.md's
// `cmd/mischief/` docs row: core fault-loop verbs first (plan · status ·
// arm · revert · doctor · selftest · serve), then the v0.1 helper/ops
// verbs, then the evidence verbs. selftest is the M1 exit criterion
// (README: "exit 0 only green-or-skip") — it must stay discoverable here
// (MISCHIEF-DF-20: the README advertised it, the usage text did not).
func usage(w *os.File) {
	fmt.Fprint(w, `mischief — fault injection for a fleet of Linux daemons (M1 chassis)

Usage:
  mischief plan      -f <exp.yaml> [--json] [--scratch-dir D] [--journal-dir D]  (writes nothing without --journal-dir)
  mischief status    [--dir D] [--all]  (active holds: armed/stuck; --all lists terminal holds too)
  mischief arm       --fault ID --target SEL --ttl D --inverse-kind K --inverse-params k=v,.. --check-kind K --check-params k=v,.. [--dir D]
  mischief revert    (--all | --hold <id> [--retry]) [--dir D]
  mischief doctor    [--catalog-dir D] [--json] [--self-check]  (exit 1 = a check FAILED: catalog load, or a --self-check rail)
  mischief selftest  (--all | --primitive <id>) [--dir D] [--catalog-dir D]  (exit 0 only when green-or-skip — the M1 exit criterion)
  mischief serve     --dir D          (reverter daemon: TTLs + boot reconcile + health.json)
  mischief install   [--user U] [--prefix P] [--dry-run] [--no-drop-in]
  mischief uninstall [--prefix P] [--dry-run] [--keep-count N] [--keep-days D]
  mischief audit     [--prefix P] [--json]
  mischief retention [--apply] [--runs-dir D] [--keep-count N] [--keep-days D] [--dry-run]
  mischief battery   [--quick] [--project P] [--primitive ID] [--out D] [--file-rows] [--dry-run]
  mischief chaos     matrix --project P [--tier-max N] [--out D] | check --lane F | finding --project P --board D --title T --reason R
  mischief sim       --shape I-00N [--path P] [--log F] [--allow-real --resource TAG --spend-cap USD]
  mischief plane     parse-agent-id | record | selftest-verdict | fold   (bunker test-plane evidence CLI)
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
// cmdPlan, renderPlan and the --journal-dir preview record live in plan.go
// (one verb per file: the ops.go / selftest.go / arm.go convention).

// runDirAbsent reports whether a run dir path does not exist. The CLI
// checks this BEFORE reverter.Open because Open is a CREATING open: it
// MkdirAlls the dir and mints a boot.json identity on first open, so an
// absent --dir used to answer with invented clean state — a minted
// boot_id over holds: 0 / proofs: 0, and a kill-switch sweep of zero
// holds graded green (MISCHIEF-DF-60: a typo'd or rotated run dir was
// indistinguishable from a verified-clean kill switch). Only the
// IsNotExist case is decided here: a present dir (empty or not) and any
// other stat error keep the store's own handling.
func runDirAbsent(dir string) bool {
	_, err := os.Stat(dir)
	return os.IsNotExist(err)
}

// ── status ───────────────────────────────────────────────────────────────────

func cmdStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dir := fs.String("dir", defaultRunDir(), "run dir (journal home)")
	asJSON := fs.Bool("json", false, "emit JSON instead of text")
	// MISCHIEF-DF-54: the default surface is the ACTIVE holds (armed or
	// stuck) — the contract this verb has always documented — so a clean
	// `revert --all` reads back as "holds: 0" and AC-6's own proof step is
	// satisfiable. Terminal holds (reverted / revert_failed) are facts:
	// they stay in the proof history below, and --all lists them here.
	all := fs.Bool("all", false, "include terminal holds (reverted / revert_failed) in the listing")
	if err := fs.Parse(args); err != nil {
		return verbParseExit(err)
	}
	// MISCHIEF-DF-60: an absent run dir is a state, not a count. The
	// creating Open would mint a boot.json and answer holds: 0 / proofs: 0
	// over an invented boot_id — a typo'd --dir indistinguishable from a
	// verified-clean run dir. Name the state instead and refuse.
	if runDirAbsent(*dir) {
		fmt.Printf("run_dir: %s\n", *dir)
		fmt.Println("state: absent")
		return 1
	}
	store, err := reverter.Open(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "status:", err)
		return 1
	}
	defer store.Close()
	lf := reverter.New(store)
	st := lf.Status()
	if *all {
		st = lf.StatusAll()
	}
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
		// ttl_remaining is a LIVE field: only a hold that still owes an
		// inverse has a TTL left to burn. A terminal row keeps its
		// arm-time expiry in the fold, which is exactly how a reverted hold
		// came to print a live-looking countdown (MISCHIEF-DF-54) — so the
		// gate is the status, not the expiry.
		if h.Status.Active() && !h.Expiry.IsZero() {
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
	// MISCHIEF-DF-52: a terminal revert_failed hold is NOT a dead end. The
	// retry re-executes the recorded inverse as one more MEASURED attempt
	// (same inverse, same check, same AC-4 grading) and appends a new proof,
	// so the failed attempt and the retry's outcome both stay on the record.
	retry := fs.Bool("retry", false, "re-execute the recorded inverse of a terminal revert_failed hold (requires --hold; exit 0 only if the retry measures reverted)")
	if err := fs.Parse(args); err != nil {
		return verbParseExit(err)
	}
	if *retry && *all {
		fmt.Fprintln(os.Stderr, "revert: --retry and --all are mutually exclusive (--retry re-executes ONE hold's recorded inverse)")
		return 2
	}
	if *retry && *hold == "" {
		fmt.Fprintln(os.Stderr, "revert: --retry requires --hold <id>")
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
	// MISCHIEF-DF-60: the kill switch must not grade a directory that is
	// not there as a verified-clean sweep. The creating Open would MkdirAll
	// the absent path, sweep zero holds and print the green summary; name
	// the absent state on stderr instead and refuse (no `kill switch:`
	// line on stdout, nothing created). Mirrors status's absent refusal.
	if runDirAbsent(*dir) {
		fmt.Fprintf(os.Stderr, "revert: run dir %s absent (nothing to revert)\n", *dir)
		return 1
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
		// DF-51: a proof graded revert_failed is a LIVE fault — the sweep
		// continued (every fault got its proof, the journal keeps the
		// evidence) but the kill switch must not report success. Successful
		// proofs are counted separately and ANY outcome that is not a
		// measured reverted — revert_failed included, and a malformed or
		// unknown outcome value too — fails the verb. Mirrors the --hold
		// path, which already exits non-zero on the same outcome.
		if failed := countRevertFailures(proofs); failed > 0 {
			fmt.Printf("kill switch: %d holds reverted, %d revert_failed, wall=%s (AC-6 budget 2s)\n", len(proofs)-failed, failed, wall.Round(time.Millisecond))
			fmt.Fprintln(os.Stderr, "revert: kill switch sweep did not fully revert — at least one fault may still be live")
			return 1
		}
		fmt.Printf("kill switch: %d holds reverted, wall=%s (AC-6 budget 2s)\n", len(proofs), wall.Round(time.Millisecond))
		return 0
	}
	// The retry is the ONLY path that admits a terminal revert_failed hold
	// (Revert refuses it and points at this flag); the plain path is
	// unchanged, including its `direct` reason on the proof. A retry proof
	// carries reason=retry, so the journal distinguishes the two attempts.
	var pr reverter.Proof
	if *retry {
		pr, err = lf.RevertRetry(ctx, *hold, "retry", reverter.RoleAPI)
	} else {
		pr, err = lf.Revert(ctx, *hold, "direct", reverter.RoleAPI)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "revert:", err)
		return 1
	}
	if *retry {
		fmt.Printf("retried %-16s outcome=%s detail=%s duration=%s\n", pr.HoldID, pr.Outcome, pr.Detail, pr.Duration.Round(time.Millisecond))
	} else {
		fmt.Printf("reverted %-16s outcome=%s detail=%s duration=%s\n", pr.HoldID, pr.Outcome, pr.Detail, pr.Duration.Round(time.Millisecond))
	}
	if pr.Outcome != reverter.OutcomeReverted {
		return 1
	}
	return 0
}

// countRevertFailures counts sweep proofs that do NOT attest a measured
// revert: the declared revert_failed outcome, and any outcome value outside
// the closed vocabulary (a malformed or unknown proof must never count as
// green by accident — the sweep is only as good as its worst proof).
func countRevertFailures(proofs []reverter.Proof) int {
	n := 0
	for _, pr := range proofs {
		if pr.Outcome != reverter.OutcomeReverted || !pr.Outcome.Valid() {
			n++
		}
	}
	return n
}

// ── doctor ───────────────────────────────────────────────────────────────────

func cmdDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	catalogDir := fs.String("catalog-dir", "", "catalog dir (default: catalog/faults from cwd upward)")
	selfCheck := fs.Bool("self-check", false, "run the rails self-checks (sanction marker, load gate, scope ladder) and exit non-zero naming what fails")
	asJSON := fs.Bool("json", false, "emit the report as JSON (rows with per-primitive missing pieces + the SPEC-11 privileged-surface audit)")
	opsPosture := fs.Bool("ops", true, "include the SPEC-11 privileged-surface posture (drop-in/manifest/grants per capability)")
	opsPrefix := fs.String("prefix", "", "mischief state prefix for the ops posture (default: the ops package default)")
	if err := fs.Parse(args); err != nil {
		return verbParseExit(err)
	}
	rep, err := doctor.Run(*catalogDir, hostCapabilityRegistry())
	if err != nil {
		fmt.Fprintln(os.Stderr, "doctor:", err)
		return 1
	}
	if *selfCheck {
		doctor.RunRailsSelfChecks(rep, rep.Catalog)
	}
	// SPEC-11 posture (MSF-012): per-primitive available/absent PLUS the
	// missing piece (AC-11 upgrade), derived from the privilege audit.
	if *opsPosture {
		doctor.RunOps(rep, doctor.OpsOptions{Prefix: *opsPrefix})
		doctor.AppendOpsRows(rep)
	}
	doctor.WriteOut(rep, *asJSON || rep.JSON)
	// doctor refuses nothing extra: a load failure is REPORTED (exit 1 —
	// the doctor could not do its one job), a loaded catalog is exit 0
	// regardless of how many primitives are capability_unavailable. With
	// --self-check, a FAILING rail check is also a non-zero exit (the
	// brief: "exits non-zero naming what fails" — the report above names
	// every failing row before the exit). MSF-035: this exit contract is
	// pinned by TestDoctorFailsOnMissingCatalog / TestDoctorGreenOnLoadedCatalog.
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
		return verbParseExit(err)
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
