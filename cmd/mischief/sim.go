package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/trouble-agent/mischief/internal/backend_sim"
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
// the redirect path PROVES itself in the sim's request log. destroy_class
// =permanent refuses outright everywhere (the companion-faults doctrine).
//
// Usage:
//
//	mischief sim --shape I-003 [--path /fault] [--log PATH]
//	mischief sim --shape I-012 --allow-real --resource aws:sandbox-lab --spend-cap 0.50
//
// ch:trace row=MSF-010 spec=docs/SPEC-PLAN.md#SPEC-09 wave=.coding-hermes/waves/mischief-foreman-2026-10-05-10-40-40.json#MSF-010 test=cmd/mischief evidence=cmd/mischief/sim.go witness=none:layer-I-shape-exercised-via-selfCall-request-log
func cmdSim(args []string) int {
	fs := flag.NewFlagSet("sim", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	shapeID := fs.String("shape", "", "the layer-I catalog id to simulate (I-003, I-004, I-005, I-006, I-010, I-011, I-012, I-013, I-014)")
	path := fs.String("path", "/fault", "request path the sim serves")
	logPath := fs.String("log", "", "sim request log path (default <tmpdir>/mischief-sim-<shape>.log)")
	allowReal := fs.Bool("allow-real", false, "AC-17 opt-in: admit the real-provider plane (v0.1: gate decides; adapter is a stub)")
	resource := fs.String("resource", "", "allowlisted resource tag (AC-17 triad)")
	spendCap := fs.Float64("spend-cap", 0, "spend cap in USD (AC-17 triad)")
	if err := fs.Parse(args); err != nil {
		return 2
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

	// The AC-17 gate decides BEFORE anything arms: the triad
	// (--allow-real AND resource tag AND spend cap) is what separates a
	// real-plane admission from a forced simulator redirect. Refusals do
	// not error out of sim mode — they ARE the sim-mode proof path.
	gate := backend_sim.RealGate{
		AllowReal:   *allowReal,
		ResourceTag: *resource,
		SpendCapUSD: *spendCap,
	}
	decision, detail := gate.Decide("reversible")

	log := *logPath
	if log == "" {
		log = filepath.Join(os.TempDir(), fmt.Sprintf("mischief-sim-%s.log", *shapeID))
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
	fmt.Println(proof)
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
