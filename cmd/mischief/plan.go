package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/trouble-agent/mischief/internal/journal"
	"github.com/trouble-agent/mischief/internal/plan"
	"github.com/trouble-agent/mischief/internal/rails"
	"github.com/trouble-agent/mischief/internal/sanction"
)

// ── plan ─────────────────────────────────────────────────────────────────────

// cmdPlan is the M1 plan verb: resolve an experiment against the catalog and
// the rails and print what would land, what proves it landed and how it is
// reverted.
//
// Side-effect posture (AC-1). The SHIPPED invocation — `mischief plan -f
// exp.yaml` — writes nothing: no run dir, no journal, no target writes. That
// is the release criterion's own proof clause ("plan on a running target
// leaves status empty, the journal file absent and the target's own
// checksum/pid set byte-identical"), it is the PRD verb table's "No side
// effects.", and it is internal/plan's documented contract ("It does not
// land, arm or journal: zero side effects by construction").
//
// MISCHIEF-DF-37: the README's Option A walkthrough promised a run dir and a
// journal entry that plan never wrote, so a consumer auditing what plan did
// found an empty journal and could not tell "nothing happened" from "record
// lost". The fix keeps AC-1 intact by making the record EXPLICIT: pass
// --journal-dir D and plan records the preview it printed — one run_planned
// record (the journal package's own "the resolved plan" type) in
// D/plan.jsonl, carrying the run_id the render printed, the experiment, the
// resolved fault set, the target and the refusal count. Without the flag the
// verb still touches nothing, so an operator can have the audit trail
// without a preview that mutates the default run dir, and the README now
// says exactly that.
func cmdPlan(args []string) int {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	expPath := fs.String("f", "", "experiment YAML (required)")
	catalogDir := fs.String("catalog-dir", "", "catalog dir (default: catalog/faults from cwd upward)")
	asJSON := fs.Bool("json", false, "emit JSON instead of text")
	// MISCHIEF-CLI-002: the shipped usage/docs advertise the path-valued
	// form (--scratch-dir D), so the flag IS path-valued. Plan only
	// DECLARES the dir — it never creates, reads or writes it (AC-1); the
	// run contract owns the actual scratch dir ("plan reports, run
	// enforces"). A valueless --scratch-dir is a usage error naming the
	// flag (never the old "invalid boolean value" type confusion).
	scratchDir := fs.String("scratch-dir", "", "scratch dir the plan declares for the L0 preview (verbatim, never created by plan; run owns the flag)")
	// MISCHIEF-DF-37: the opt-in evidence flag. Empty (the default) keeps
	// the verb side-effect free (AC-1); a value names the RUN DIR the
	// preview record is appended to as <dir>/plan.jsonl.
	journalDir := fs.String("journal-dir", "", "run dir to RECORD the plan preview into (<dir>/plan.jsonl); default: none — plan writes nothing (AC-1)")
	skipSanction := fs.Bool("plan-only-sanction-skip", false, "(unimplemented stub flag: reserved for MSF-020; plan never lands faults)")
	_ = skipSanction
	if err := fs.Parse(args); err != nil {
		return verbParseExit(err)
	}
	if *expPath == "" {
		fmt.Fprintln(os.Stderr, "plan: -f <exp.yaml> is required (no default experiment)")
		return 2
	}

	// AC-1: plan is a dry run — the sanction gate still runs (it is a
	// host posture check, not a landing act) but NOTHING is written here:
	// the sanction check itself writes nothing (SPEC-13), and the only
	// write plan can make is the --journal-dir record below.
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
	scope := rails.Scope{Level: 0, Scratch: true, ScratchDir: *scratchDir} // plan previews the L0 scratch posture; run applies its own scope
	p, err := plan.Build(exp, raw, cat, reg, scope)
	if err != nil {
		// The plan still renders (the operator sees what resolved), but
		// the exit is the refusal's (2) — rails.MapExit owns the mapping.
		out := renderPlan(p, *asJSON)
		os.Stdout.Write(out)
		fmt.Fprintln(os.Stderr, "plan:", err)
		// A refused preview is still a preview the operator asked to
		// record: the record is written best-effort (the refusal exit
		// owns the code) and a write failure is reported, never silent.
		if *journalDir != "" && p != nil {
			if path, werr := writePlanJournal(*journalDir, p); werr != nil {
				fmt.Fprintln(os.Stderr, "plan: journal write failed:", werr)
			} else {
				fmt.Fprintf(os.Stderr, "plan: journal: %s (run_id %s)\n", path, p.RunID)
			}
		}
		return rails.MapExit(err)
	}
	os.Stdout.Write(renderPlan(p, *asJSON))
	if *journalDir != "" {
		path, werr := writePlanJournal(*journalDir, p)
		if werr != nil {
			// The record was REQUESTED: a missing one is a failed write,
			// not a footnote — report it and exit non-zero (the report
			// itself already reached the operator on stdout).
			fmt.Fprintln(os.Stderr, "plan: journal write failed:", werr)
			return 1
		}
		// --json keeps stdout machine-readable: the note goes to stderr
		// there (the run_id is already in the JSON payload).
		if *asJSON {
			fmt.Fprintf(os.Stderr, "plan: journal: %s (run_id %s)\n", path, p.RunID)
		} else {
			fmt.Printf("journal: %s (run_id %s)\n", path, p.RunID)
		}
	}
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

// planJournalFile is the per-verb evidence file inside the run dir, the
// same convention selftest.jsonl and battery.jsonl follow: one verb's
// record set per file, so a reader of a run journal reads a plan journal
// unchanged (internal/journal owns the record format).
const planJournalFile = "plan.jsonl"

// planJournalRecords renders the plan preview's record set: exactly one
// run_planned record — the journal package's own type for "the resolved
// plan (no side effects, AC-1)" — carrying the plan ACTION: the
// content-derived run id the render printed, the experiment, the seed, the
// catalog version, the resolved target, the resolved fault set and the
// refusal tally. One record, seq 0: plan makes one act, and the record says
// which one.
func planJournalRecords(p *plan.Plan) []journal.Record {
	ids := make([]string, 0, len(p.Faults))
	for _, f := range p.Faults {
		ids = append(ids, f.Primitive)
	}
	sort.Strings(ids) // the render's own order: the plan is byte-identical (AC-1)
	reasons := make([]string, 0, len(p.Refusals))
	for _, r := range p.Refusals {
		reasons = append(reasons, string(r))
	}
	sort.Strings(reasons)

	fields := map[string]string{
		"kind":            "plan",
		"mode":            "preview",
		"experiment":      p.ExperimentID,
		"seed":            strconv.FormatInt(p.Seed, 10),
		"catalog_version": p.CatalogVersion,
		"target_kind":     string(p.Target.Kind),
		"target_selector": p.Target.Selector,
		"faults":          strings.Join(ids, ","),
		"faults_count":    strconv.Itoa(len(ids)),
		"refusals":        strconv.Itoa(len(p.Refusals)),
	}
	if len(reasons) > 0 {
		fields["refusal_reasons"] = strings.Join(reasons, ",")
	}
	// SPEC-03: a protected target is recorded when the plan resolved one
	// under an explicit operator override ("the journal records it").
	if p.Target.Protected != "" {
		fields["target_protected"] = string(p.Target.Protected)
	}
	// The declared scratch dir rides verbatim when the invocation declared
	// one (MISCHIEF-CLI-002) — plan never touched the path.
	if p.ScratchDir != "" {
		fields["scratch_dir"] = p.ScratchDir
	}

	return []journal.Record{{
		Type:   journal.RecordRunPlanned,
		RunID:  p.RunID,
		Seq:    0,
		TS:     time.Now().Unix(),
		Fields: fields,
	}}
}

// writePlanJournal appends the plan record set to <dir>/plan.jsonl and
// returns the path. The write path is the journal package's own: every line
// is serialised by Record.MarshalJSON, so the write-time validation (closed
// record vocabulary, run id present) and the scrub pass (AC-20: no
// credential-shaped material leaves the process) ride on it, and the file is
// fsynced before the path is reported — the record is durable when the verb
// claims it.
func writePlanJournal(dir string, p *plan.Plan) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, planJournalFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	recs := planJournalRecords(p)
	for i := range recs {
		b, err := recs[i].MarshalJSON()
		if err != nil {
			return "", err
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			return "", err
		}
	}
	return path, f.Sync()
}
