package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/trouble-agent/mischief/internal/catalog"
	"github.com/trouble-agent/mischief/internal/rails"
	"github.com/trouble-agent/mischief/internal/selftest"
)

// selftest.go — the `mischief selftest` verb (MSF-015).
//
// mischief selftest (--all | --primitive <id>) [--dir D] [--catalog-dir D]
//
// Per primitive, on an L0 scratch target the run owns: land, MEASURE the
// descriptor's landed-proof (AC-3), execute the inverse, MEASURE the
// revert (AC-4), byte-compare the pre-state. Pass/skip/fail per
// primitive; --all exits 0 only when every primitive is green-or-skip
// (the M1 exit criterion). A journal record set in the internal/journal
// format lands under <dir>/selftest.jsonl.
//
// ch:trace row=MSF-015 spec=docs/prd/mischief-v0.1.md (§4, §6.1) + .coding-hermes/board/tasks.jsonl (MSF-015) evidence=cmd/mischief/selftest.go + internal/selftest/ witness=none:scratch-only-live-runs

func cmdSelftest(args []string) int {
	fs := flag.NewFlagSet("selftest", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	all := fs.Bool("all", false, "selftest every catalog primitive")
	prim := fs.String("primitive", "", "selftest one primitive by catalog id (e.g. P-001)")
	dir := fs.String("dir", defaultRunDir(), "run dir (journal home for selftest.jsonl)")
	catalogDir := fs.String("catalog-dir", "", "catalog dir (default: catalog/faults from cwd upward)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !*all && *prim == "" {
		fmt.Fprintln(os.Stderr, "selftest: --all or --primitive <id> is required (there is no default primitive)")
		return 2
	}
	if *all && *prim != "" {
		fmt.Fprintln(os.Stderr, "selftest: --all and --primitive are mutually exclusive")
		return 2
	}

	// The catalog grounds the id namespace: --primitive validates against
	// it (an unknown id refuses, exit 2 — the rails' unknown-primitive
	// shape), and the state table (AC-19 wiring) reports per primitive.
	cat, err := loadCatalog(*catalogDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "selftest:", err)
		return 1
	}

	var results []selftest.Result
	switch {
	case *all:
		// the catalog's ids, not the harness's green list: every catalog
		// primitive is exercised (green ones run live; the rest surface
		// as the named skip/fail their honesty demands)
		for _, id := range cat.IDs {
			results = append(results, selftest.RunOnePublic(id))
		}
	default:
		id := strings.TrimSpace(*prim)
		if cat.Get(id) == nil {
			fmt.Fprintf(os.Stderr, "selftest: %q is not in the catalog (%d primitives: %s)\n", id, cat.Len(), strings.Join(cat.IDs, ", "))
			return 2
		}
		results = append(results, selftest.RunOnePublic(id))
	}

	os.Stdout.WriteString(selftest.Print(results))
	summary, exit := selftest.Summarize(results)
	fmt.Println(summary)

	// journal record set (the internal/journal format) — a write failure
	// is reported but does not flip the verdict the measurements earned.
	runID := selftest.JournalRunID(results)
	if path, jerr := selftest.WriteJournal(*dir, selftest.JournalRecords(runID, results)); jerr != nil {
		fmt.Fprintln(os.Stderr, "selftest: journal write failed:", jerr)
	} else {
		fmt.Printf("journal: %s (run_id %s)\n", path, runID)
	}

	// AC-19 wiring, demonstrated live: at L1 the rails must refuse every
	// non-green primitive (the named selftest_not_green refusal) and
	// admit every green one. A violation is a harness bug: it FAILS the
	// run regardless of the per-primitive outcomes (a selftest harness
	// whose refusal gate does not follow the state would certify
	// nothing).
	if wired, werr := printAC19Wiring(cat, results); !wired {
		fmt.Fprintln(os.Stderr, "selftest:", werr)
		if exit == 0 {
			exit = 1
		}
	}

	return exit
}

// printAC19Wiring renders the per-primitive AC-19 gate demonstration with
// the MEASURED state recorded (the task's honesty line: a primitive
// passing selftest may be RECORDED green — recorded here onto the
// in-memory descriptor the rails read; the shipped catalog files are
// never touched). After the recording, CheckScope at L1 (outside L0) must
// refuse every non-green primitive with the selftest_not_green reason and
// admit every green one — the same predicate the run path enforces. A
// violation is a harness bug and fails the run.
func printAC19Wiring(cat *catalog.Catalog, results []selftest.Result) (bool, error) {
	fmt.Println("ac19: measured selftest states recorded; outside L0 (L1 check) the rails admit only green:")
	wired := true
	for _, r := range results {
		d := cat.Get(r.ID)
		if d == nil {
			continue
		}
		live := catalog.SelftestState(selftest.StateOf(r))
		declared := d.Selftest
		d.Selftest = live // the recording: measured state -> the state the predicate reads
		err := rails.CheckScope(d, rails.Scope{Level: 1})
		refused := err != nil
		green := live == catalog.SelftestGreen
		if refused != !green {
			wired = false
			fmt.Printf("  %-8s VIOLATION: live=%s declared-was=%s refused=%t (predicate wants refusal=%t)\n",
				r.ID, live, declared, refused, !green)
			continue
		}
		if refused {
			fmt.Printf("  %-8s REFUSED at L1: %v (live=%s, declared-was=%s)\n", r.ID, err, live, declared)
		} else {
			fmt.Printf("  %-8s ADMITTED at L1 (live=green, declared-was=%s)\n", r.ID, declared)
		}
	}
	if !wired {
		return false, fmt.Errorf("AC-19 wiring violation: the rails' refusal did not follow the measured selftest state")
	}
	return true, nil
}
