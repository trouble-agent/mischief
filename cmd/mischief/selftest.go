package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/trouble-agent/mischief/internal/catalog"
	"github.com/trouble-agent/mischief/internal/rails"
	"github.com/trouble-agent/mischief/internal/sanction"
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
		return verbParseExit(err)
	}
	if !*all && *prim == "" {
		fmt.Fprintln(os.Stderr, "selftest: --all or --primitive <id> is required (there is no default primitive)")
		return 2
	}
	if *all && *prim != "" {
		fmt.Fprintln(os.Stderr, "selftest: --all and --primitive are mutually exclusive")
		return 2
	}

	// SPEC-13 sanction gate (MSF-032, found by MSF-022): selftest LANDS
	// faults — every covered primitive is armed, landed and reverted on
	// this host — so it is gated exactly like plan: fail-closed BEFORE any
	// catalog work or journal write on an unsanctioned host. The plane
	// provisions the marker on the agent; the scheduler host must stay
	// marker-absent (MSF-020), which makes that host refuse here.
	if err := sanction.Check(sanction.Options{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
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
		// the COVERED corpus (green landables + named skips), not just the
		// catalog's file ids: every primitive this build can exercise is
		// exercised (MSF-010 added the SPEC-09 ids — the sim I-layer rows
		// and the docker node faults — which live in the harness corpus and
		// the docker backend's id namespace rather than as catalog files;
		// the corpus-coverage test keeps the two sets honest against each
		// other). Catalog ids outside the corpus still surface: they are
		// appended and fail with the named "no selftest exists" reason.
		covered := map[string]bool{}
		for _, id := range selftest.CoveredIDs() {
			covered[id] = true
			results = append(results, selftest.RunOnePublic(id))
		}
		for _, id := range cat.IDs {
			if !covered[id] {
				results = append(results, selftest.RunOnePublic(id))
			}
		}
	default:
		id := strings.TrimSpace(*prim)
		if cat.Get(id) == nil {
			fmt.Fprintf(os.Stderr, "selftest: %q is not in the catalog (%d primitives: %s)\n", id, cat.Len(), strings.Join(cat.IDs, ", "))
			return 2
		}
		results = append(results, selftest.RunOnePublic(id))
	}

	// MISCHIEF-DF-38: version-skew hint. The catalog is read at run time;
	// the selftest corpus is compiled INTO the binary. So a catalog NEWER
	// than the binary lands here as an unknown-primitive FAIL — the id is
	// real (the catalog the CLI just loaded declares it) but this build
	// has no actuator for it. Failing is the correct verdict (an
	// unprovable primitive must never pass); what was missing is the
	// cause, and the README's own advice ("run it from a checkout") is
	// exactly what pairs a HEAD catalog with a release binary. The hint
	// goes to stderr — stdout stays the results-and-summary surface.
	if ids := corpusUnknownIDs(results); len(ids) > 0 {
		fmt.Fprint(os.Stderr, versionSkewHint(ids, cat))
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

// corpusUnknownIDs returns the failing result ids this build's compiled-in
// corpus does not cover — the one shape a catalog/binary VINTAGE MISMATCH
// produces (MISCHIEF-DF-38): a catalog id outside the corpus reaches the
// run loop (the CLI loaded the catalog and accepted the id) and the runner
// refuses it as unknown. Sorted, so the hint reads the same for --all and
// for a single --primitive. Membership is tested against the corpus itself
// rather than the runner's reason text — the predicate is the fact, not
// the prose that describes it.
func corpusUnknownIDs(results []selftest.Result) []string {
	covered := map[string]bool{}
	for _, id := range selftest.CoveredIDs() {
		covered[id] = true
	}
	var ids []string
	for _, r := range results {
		if r.State == "fail" && !covered[r.ID] {
			ids = append(ids, r.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// versionSkewHint renders the operator-facing version-skew hint for the
// primitives this build cannot prove (MISCHIEF-DF-38): the two vintages to
// compare — the binary's stamped build (the `version` verb) and the
// catalog's own content-derived version with the directory it was read
// from — plus the remedy, which is to pair the binary with the catalog
// tree of its own release rather than a newer checkout's.
func versionSkewHint(ids []string, cat *catalog.Catalog) string {
	quoted := make([]string, 0, len(ids))
	for _, id := range ids {
		quoted = append(quoted, fmt.Sprintf("%q", id))
	}
	var b strings.Builder
	catWord := "primitives"
	if cat.Len() == 1 {
		catWord = "primitive"
	}
	fmt.Fprintf(&b, "selftest: version skew? %s not in this build's selftest corpus (%d covered ids) but declared by the catalog at %s (%d %s, version %s %s)\n",
		strings.Join(quoted, ", "), len(selftest.CoveredIDs()), cat.SourceDir, cat.Len(), catWord, cat.VersionScheme, cat.Version)
	b.WriteString("  the corpus is compiled into the binary; the catalog is read at run time — a catalog NEWER than this binary fails closed exactly like this\n")
	fmt.Fprintf(&b, "  compare the vintages: `mischief version` reports this binary as %q; the catalog was read from %s\n", gitSha, cat.SourceDir)
	b.WriteString("  remedy: point --catalog-dir at the catalog tree matching this binary's release (`git archive <tag> catalog`), or rebuild from the checkout the catalog came from\n")
	return b.String()
}

// printAC19Wiring renders the per-primitive AC-19 gate demonstration with
// the MEASURED state recorded (the task's honesty line: a primitive
// passing selftest may be RECORDED green — recorded here onto the
// in-memory descriptor the rails read; the shipped catalog files are
// never touched). After the recording, CheckScope one level ABOVE L0 must
// refuse every non-green primitive with the selftest_not_green reason and
// admit every green one — the same predicate the run path enforces.
//
// The check level is the primitive's OWN declared minimum tier when that
// is above L0 (I-002 declares L3/L4: its tier refusal is CORRECT at L1 —
// the AC-19 predicate is about the selftest rail, and a tier-gated
// primitive would read as a false violation if forced to L1). For the
// L0-tier primitives (the whole rootless set) the check stays at L1.
// A violation is a harness bug and fails the run.
func printAC19Wiring(cat *catalog.Catalog, results []selftest.Result) (bool, error) {
	fmt.Println("ac19: measured selftest states recorded; outside L0 the rails admit only green:")
	wired := true
	for _, r := range results {
		d := cat.Get(r.ID)
		if d == nil {
			continue
		}
		level := 1
		if d.Tier != nil && d.Tier.Min > level {
			level = d.Tier.Min
		}
		live := catalog.SelftestState(selftest.StateOf(r))
		declared := d.Selftest
		d.Selftest = live // the recording: measured state -> the state the predicate reads
		err := rails.CheckScope(d, rails.Scope{Level: level})
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
