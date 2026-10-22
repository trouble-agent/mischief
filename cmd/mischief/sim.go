package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/trouble-agent/mischief/internal/backend_sim"
	"github.com/trouble-agent/mischief/internal/sanction"
)

// cmdSim implements the `mischief sim` verb — the layer-I cloud-shape
// simulator as an operator surface (SPEC-09 / MSF-010).
//
// Simulate mode arms one cloud shape, issues ONE real request against it,
// and prints the AC-17 landed-proof line backed by the sim's request log
// (the same proof channel the battery uses). This is the safe default: a
// run WITHOUT --allow-real and a resource tag refuses the real plane and
// lands here — the command wiring of backend_sim.LandInSimOnRefusal.
//
// Real mode (--allow-real + --resource + --spend-cap) applies the AC-17
// gate explicitly: in v0.1 the real adapter is a documented stub, so the
// gate's decision (admit-refused-by-stub | redirect-to-sim) is printed and
// the redirect path PROVES itself in the sim's request log.
//
// --destroy-class names the requested fault's destroy class (closed
// vocabulary: reversible | permanent; default reversible). The value is
// validated BEFORE anything arms and feeds the gate directly: outside the
// vocabulary is a usage refusal naming the offending value (exit 2), and
// destroy_class=permanent refuses outright everywhere in v0.1 (the
// companion-faults doctrine — the refusal the PRD's L5 row and
// docs/FAULT-CATALOG.md promise): the gate's RefusalText line prints and
// the verb exits 2 BEFORE the simulator starts — no request log, nothing
// armed, nothing landed ("simulator included").
//
// Usage:
//
//	mischief sim --shape I-003 [--path /fault] [--log PATH]
//	mischief sim --shape I-003 --destroy-class permanent   (refused outright: exit 2)
//	mischief sim --shape I-012 --allow-real --resource aws:sandbox-lab --spend-cap 0.50
//
// ch:trace row=MSF-010 spec=docs/SPEC-PLAN.md#SPEC-09 wave=.coding-hermes/waves/mischief-foreman-2026-10-05-10-40-40.json#MSF-010 test=cmd/mischief evidence=cmd/mischief/sim.go witness=none:layer-I-shape-exercised-via-selfCall-request-log
// cmdSim — MISCHIEF-DF-67: the SPEC-13 gate runs AFTER usage validation
// and BEFORE the AC-17 decision — an unsanctioned host refuses with
// host_not_sanctioned, exit 2, nothing armed, nothing bound, no request
// log; sanctioned hosts get the AC-17 redirect semantics unchanged.
//
// ch:trace row=MISCHIEF-DF-67 spec=docs/prd/mischief-v0.1.md#spec-13 wave=.coding-hermes/waves/mischief-foreman-2026-10-21-18-18-09.json#3 test=cmd/mischief/sanction_gate_test.go doc=docs/TEST-PLANE.md prompt=/tmp/mischief_brief_MISCHIEF-DF-67.txt evidence=docs/verification/MISCHIEF-DF-67.md witness=none:unsanctioned-scratch-only verdict=none:pending-foreman-judge commit=none:pending-foreman-trace memory=none:not-written
//
// ch:trace row=MISCHIEF-DF-65 spec=docs/SPEC-PLAN.md#SPEC-03 wave=mischief-foreman-2026-10-22-07-05-13.json#MISCHIEF-DF-65 test=cmd/mischief/sim_load_gate_test.go evidence=internal/backend_sim/load_gate.go witness=none:injected-readings-no-live-run
// cmdSim — MISCHIEF-DF-65: the AC-8 load gate runs AFTER the usage and
// policy refusals keep their own reasons and BEFORE anything arms, binds
// or writes — the same last-admission-rail battery (DF-28) and selftest
// run before their loops. A saturated host refuses exit 2 with the rails
// refusal (the measured numbers vs the gate), the simulator never starts
// and the request log is never created; a healthy host keeps the AC-17
// redirect semantics unchanged.
func cmdSim(args []string) int {
	fs := flag.NewFlagSet("sim", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	shapeID := fs.String("shape", "", "the layer-I catalog id to simulate (I-003, I-004, I-005, I-006, I-010, I-011, I-012, I-013, I-014)")
	path := fs.String("path", "/fault", "request path the sim serves")
	logPath := fs.String("log", "", "sim request log path (default <tmpdir>/mischief-sim-<shape>.log)")
	destroyClass := fs.String("destroy-class", "reversible", "the requested fault's destroy class, closed vocabulary reversible | permanent (permanent refuses outright in v0.1)")
	allowReal := fs.Bool("allow-real", false, "AC-17 opt-in: admit the real-provider plane (v0.1: gate decides; adapter is a stub)")
	resource := fs.String("resource", "", "allowlisted resource tag (AC-17 triad)")
	spendCap := fs.Float64("spend-cap", 0, "spend cap in USD (AC-17 triad)")
	if err := fs.Parse(args); err != nil {
		return verbParseExit(err)
	}

	if *shapeID == "" {
		fmt.Fprintln(os.Stderr, "mischief sim: --shape is required (one of I-003, I-004, I-005, I-006, I-010, I-011, I-012, I-013, I-014)")
		return 2
	}
	shape, ok := simShapeFor(*shapeID)
	if !ok {
		fmt.Fprintf(os.Stderr, "mischief sim: unknown layer-I shape %q — the simulator covers I-003, I-004, I-005, I-006, I-010, I-011, I-012, I-013, I-014\n", *shapeID)
		return 2
	}

	// --destroy-class is a closed vocabulary validated BEFORE anything
	// arms (PRD L5: reversible, non-destructive verbs only). The gate's
	// permanent check is case-insensitive, so the vocabulary check matches
	// that shape; canonicalize so exactly one spelling reaches the gate.
	switch {
	case strings.EqualFold(*destroyClass, "reversible"):
		*destroyClass = "reversible"
	case strings.EqualFold(*destroyClass, "permanent"):
		*destroyClass = "permanent"
	default:
		fmt.Fprintf(os.Stderr, "mischief sim: unknown --destroy-class %q — the closed vocabulary is reversible | permanent\n", *destroyClass)
		return 2
	}

	// MISCHIEF-DF-67: the SPEC-13 host-admission gate, same shape as
	// plan/arm/selftest/battery (the MSF-032 class again — sim was the
	// 4th+ fault-landing surface outside the gate). Fail closed BEFORE
	// anything arms, binds or writes: the request log below is a
	// landed-proof claim, and an unsanctioned host must never produce
	// one. Usage validation runs first (usage refusals are not a
	// sanction matter); from here on a refusal exits 2 and the only
	// filesystem contact this verb makes is the marker read.
	if err := sanction.Check(sanction.Options{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	// The AC-17 gate decides BEFORE anything arms: the triad
	// (--allow-real AND resource tag AND spend cap) is what separates a
	// real-plane admission from a forced simulator redirect. Refusals do
	// not error out of sim mode — they ARE the sim-mode proof path.
	gate := backend_sim.RealGate{
		AllowReal:   *allowReal,
		ResourceTag: *resource,
		SpendCapUSD: *spendCap,
	}
	decision, detail := gate.Decide(*destroyClass)

	log := *logPath
	if log == "" {
		log = filepath.Join(os.TempDir(), fmt.Sprintf("mischief-sim-%s.log", *shapeID))
	}

	// The permanent destroy refuses OUTRIGHT — simulator included — so the
	// verb exits before the simulator starts: no request log created, no
	// shape armed, no request landed. This is the documented refusal, not
	// a sim-mode redirect.
	if decision == backend_sim.DecideRefused {
		fmt.Println(backend_sim.RefusalText(*shapeID, decision, detail))
		return 2
	}

	// MISCHIEF-DF-65: the AC-8 load gate, the rails' LAST admission rail —
	// the same check battery (DF-28) and selftest run before their loops,
	// at the same doctor thresholds (load 12.0, PSI 50.0). It runs AFTER
	// the usage refusals and the permanent-destroy policy refusal keep
	// their own reasons (a mis-typed shape and a policy refusal are not a
	// load-gate matter) and BEFORE anything arms, binds or writes: the
	// request log below is a landed-proof claim, and a saturated host must
	// never produce one. A breach refuses exit 2 with the rails refusal
	// verbatim (reason load_gate, the measured numbers vs the gate); an
	// unreadable measurement fails closed the same way.
	if err := backend_sim.CheckLoadGate(); err != nil {
		fmt.Fprintf(os.Stderr, "mischief sim: %s: %v\n", *shapeID, err)
		return 2
	}

	sim := backend_sim.NewSimulator(log)
	if err := sim.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "mischief sim: start: %v\n", err)
		return 1
	}
	defer sim.Close()

	proof, err := simLandWithProof(sim, *shapeID, shape, simPathFor(shape, *path))
	if err != nil {
		fmt.Fprintf(os.Stderr, "mischief sim: %v\n", err)
		return 1
	}

	fmt.Println(backend_sim.RefusalText(*shapeID, decision, detail))
	if decision == backend_sim.DecideReal {
		// MISCHIEF-DF-15: the full AC-17 triad passed, so the gate ADMITS
		// the real plane — the v0.1 stub is the only reason this still
		// lands in the simulator. The refusal wording ("refused the real
		// plane") contradicted the decision; print the admission instead.
		fmt.Println("ac17: real adapter is a v0.1 documented stub — landing in the simulator")
	} else {
		fmt.Println(proof)
	}
	fmt.Printf("request log: %s\n", log)
	return 0
}

// simShapeFor maps a layer-I catalog id to the simulator Shape that
// produces it. The mapping is the command-level contract with
// docs/FAULT-CATALOG.md layer I — every sim-covered id has exactly one
// default shape here (ShapeStatus/Burst etc. stay at defaults; operators
// vary them via future flags, not by editing this table).
func simShapeFor(id string) (backend_sim.Shape, bool) {
	switch strings.ToUpper(id) {
	case "I-003":
		return backend_sim.Shape{Kind: backend_sim.ShapeRateLimit, Burst: 3, RetryAfter: 2 * time.Second}, true
	case "I-004":
		return backend_sim.Shape{Kind: backend_sim.ShapeAPI5xx, Burst: 3, Malformed: true, PaginationBug: true}, true
	case "I-005":
		return backend_sim.Shape{Kind: backend_sim.ShapeEventualLag, LagWindow: 5 * time.Second}, true
	case "I-006":
		return backend_sim.Shape{Kind: backend_sim.ShapeObjectStore, ExpiredPresign: true}, true
	case "I-010":
		return backend_sim.Shape{Kind: backend_sim.ShapeAuthExpiry}, true
	case "I-011":
		return backend_sim.Shape{Kind: backend_sim.ShapeMetadata, MetadataMode: "timeout"}, true
	case "I-012":
		return backend_sim.Shape{Kind: backend_sim.ShapeQuota, QuotaResource: "instances"}, true
	case "I-013":
		return backend_sim.Shape{Kind: backend_sim.ShapeSnapshotStale, StaleSeconds: 120}, true
	case "I-014":
		return backend_sim.Shape{Kind: backend_sim.ShapeDNSPropagation, StaleSeconds: 30, Burst: 0}, true
	}
	return backend_sim.Shape{}, false
}

// simPathFor picks the request path each shape's handler actually faults
// on: eventual-lag needs a PUT (acknowledged write) then a GET, object
// store faults on GET, DNS propagation faults on the resolver-b query.
// Other shapes fault on any path, so the operator's --path passes through.
func simPathFor(sh backend_sim.Shape, operatorPath string) string {
	switch sh.Kind {
	case backend_sim.ShapeEventualLag:
		return "/kv/ac17-probe"
	case backend_sim.ShapeObjectStore:
		return "/obj/ac17-probe"
	case backend_sim.ShapeDNSPropagation:
		return "/dns/resolve?name=api.internal&resolver=resolver-b"
	}
	return operatorPath
}

// simCoveredIDs is the operator-facing list of sim-covered layer-I ids,
// used by usage and by the catalog cross-check test.
var simCoveredIDs = []string{"I-003", "I-004", "I-005", "I-006", "I-010", "I-011", "I-012", "I-013", "I-014"}

// simLandWithProof drives the AC-17 redirect with the per-shape request
// choreography the proof requires: LandInSimOnRefusal issues ONE GET, but
// eventual-lag faults only on a GET that follows a seeded PUT. For that
// shape we seed via the sim's exported SelfPut (a real acknowledged write
// against the running sim), issue the proof GET ourselves, and reuse the
// shared durable-log proof read. Every other shape delegates.
func simLandWithProof(sim *backend_sim.Simulator, primitive string, sh backend_sim.Shape, path string) (string, error) {
	if err := sim.Arm(sh); err != nil {
		return "", fmt.Errorf("backend_sim: %s: arm: %w", primitive, err)
	}
	switch sh.Kind {
	case backend_sim.ShapeEventualLag:
		// write-then-read choreography: the acknowledged PUT seeds the
		// lag window, the proof GET inside the window serves stale.
		if err := sim.SelfPut(path); err != nil {
			return "", fmt.Errorf("backend_sim: %s: seed PUT: %w", primitive, err)
		}
		st, _, err := sim.SelfCall(path)
		if err != nil {
			return "", fmt.Errorf("backend_sim: %s: sim request: %w", primitive, err)
		}
		return simProofFromLog(sim, primitive, path, sh.Kind, st)
	case backend_sim.ShapeDNSPropagation:
		// the proof path carries a query string the log's Path field
		// drops — drive the request and use the prefix-tolerant proof.
		st, _, err := sim.SelfCall(path)
		if err != nil {
			return "", fmt.Errorf("backend_sim: %s: sim request: %w", primitive, err)
		}
		return simProofFromLog(sim, primitive, path, sh.Kind, st)
	case backend_sim.ShapeAPI5xx:
		if sh.Malformed && sh.PaginationBug {
			// the catalog's I-004 case arms BOTH legs: drive the full
			// burst (interleaved malformed 5xx / endless-page) and print
			// a proof line naming BOTH markers — one probe would land
			// only one of the two promised faults.
			return simI004BothLegsProof(sim, primitive, path, sh)
		}
	}
	return backend_sim.LandInSimOnRefusal(sim, primitive, sh, path)
}

// simProofFromLog mirrors LandInSimOnRefusal's proof leg: read the durable
// request log back from disk and assert a faulted entry of the armed shape
// landed for the proof request (the log, not memory, is the evidence).
func simProofFromLog(sim *backend_sim.Simulator, primitive, path string, kind backend_sim.ShapeKind, st int) (string, error) {
	entries, err := backend_sim.ReadRequestLog(sim.LogPath())
	if err != nil {
		return "", fmt.Errorf("backend_sim: %s: request log read: %w", primitive, err)
	}
	for _, e := range entries {
		// r.URL.Path carries no query string, so a proof path like
		// /dns/resolve?resolver=b logs as /dns/resolve — match on the
		// logged path being the query-free prefix of the proof path.
		if (e.Path == path || strings.HasPrefix(path, e.Path)) && e.Faulted && e.Shape == kind && e.Status == st {
			return fmt.Sprintf("ac17-redirect: %s refused the real plane and landed in the simulator: %s -> %d (%s) — request log %s proves it",
				primitive, path, st, e.Detail, sim.LogPath()), nil
		}
	}
	return "", fmt.Errorf("backend_sim: %s: sim served %s -> %d but the request log at %s carries no faulted %s entry — the landed-proof did not fire (AC-3)",
		primitive, path, st, sim.LogPath(), kind)
}

// simI004BothLegsProof drives I-004's interleaved burst end to end and
// prints the AC-17 landed-proof line naming BOTH catalog markers: the
// malformed-body 5xx entries AND the endless-page marker. Each leg must
// land (a burst serving only one leg is a failed proof — the catalog
// promises both), and the log path in the line is the durable evidence.
func simI004BothLegsProof(sim *backend_sim.Simulator, primitive, path string, sh backend_sim.Shape) (string, error) {
	burst := sh.Burst
	if burst <= 0 {
		burst = 3
	}
	statuses := make([]int, 0, burst)
	for i := 0; i < burst; i++ {
		st, _, err := sim.SelfCall(path)
		if err != nil {
			return "", fmt.Errorf("backend_sim: %s: sim request %d: %w", primitive, i+1, err)
		}
		statuses = append(statuses, st)
	}
	entries, err := backend_sim.ReadRequestLog(sim.LogPath())
	if err != nil {
		return "", fmt.Errorf("backend_sim: %s: request log read: %w", primitive, err)
	}
	malformed, endless := 0, 0
	for _, e := range entries {
		if !e.Faulted || e.Shape != backend_sim.ShapeAPI5xx || e.Path != path {
			continue
		}
		if e.Status >= 500 && strings.Contains(e.Detail, "malformed body") {
			malformed++
		}
		if e.Status == 200 && strings.Contains(e.Detail, "endless page") {
			endless++
		}
	}
	if malformed == 0 || endless == 0 {
		return "", fmt.Errorf(
			"backend_sim: %s: I-004 proof incomplete — served %v but the request log at %s carries malformed-5xx=%d endless-page=%d (both catalog markers must land; AC-3)",
			primitive, statuses, sim.LogPath(), malformed, endless)
	}
	return fmt.Sprintf(
		"ac17-redirect: %s refused the real plane and landed in the simulator: %s -> %s interleaved (%d malformed-body 5xx + %d endless-page) — request log %s proves it",
		primitive, path, statusSummary(statuses), malformed, endless, sim.LogPath()), nil
}

// statusSummary renders the burst's status sequence compactly
// ("200,500,200" — the mixed wire surface the operator expects to see).
func statusSummary(statuses []int) string {
	parts := make([]string, len(statuses))
	for i, st := range statuses {
		parts[i] = strconv.Itoa(st)
	}
	return strings.Join(parts, ",")
}
