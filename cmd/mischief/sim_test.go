package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/backend_sim"
)

// TestSimVerbEachShapeProvesInRequestLog runs `mischief sim --shape I-0NN`
// for EVERY layer-I id the simulator covers and asserts the AC-17 landed
// proof: exit 0, the refusal line naming the decision, and a request log
// whose entries include a FAULTED request of the armed shape (read back
// from disk, not from the sim's memory).
func TestSimVerbEachShapeProvesInRequestLog(t *testing.T) {
	writeSanctionMarker(t) // MISCHIEF-DF-67: sim now carries the SPEC-13 gate; the sanctioned posture is the plane's own shape
	writeHealthyLoad(t)    // MISCHIEF-DF-28-R2: sim's landing path is now gated — the AC-8 gate reads the injected posture, not the box's /proc
	for _, id := range simCoveredIDs {
		t.Run(id, func(t *testing.T) {
			log := filepath.Join(t.TempDir(), "sim-requests.log")
			rc := cmdSim([]string{"--shape", id, "--log", log})
			if rc != 0 {
				t.Fatalf("sim %s: exit %d, want 0", id, rc)
			}
			entries, err := backend_sim.ReadRequestLog(log)
			if err != nil {
				t.Fatalf("sim %s: request log read: %v", id, err)
			}
			sh, _ := simShapeFor(id)
			shapeKind := sh.Kind
			found := false
			for _, e := range entries {
				if e.Faulted && e.Shape == shapeKind {
					found = true
				}
			}
			if !found {
				t.Fatalf("sim %s: request log %s carries no faulted %s entry — AC-17 proof did not land", id, log, shapeKind)
			}
		})
	}
}

// TestSimVerbRealGateRedirectWithoutOptIn is the AC-17 refusal wiring at
// the command level: WITHOUT --allow-real and a resource tag the run
// refuses the real plane, names the missing pieces, and STILL lands in the
// simulator (exit 0, faulted log entry present).
func TestSimVerbRealGateRedirectWithoutOptIn(t *testing.T) {
	writeSanctionMarker(t) // MISCHIEF-DF-67 gate
	writeHealthyLoad(t)    // MISCHIEF-DF-28-R2: the AC-8 gate reads the injected posture, not the box's /proc
	log := filepath.Join(t.TempDir(), "sim-requests.log")
	rc := cmdSim([]string{"--shape", "I-012", "--log", log})
	if rc != 0 {
		t.Fatalf("exit %d, want 0 (the refusal lands in the sim, it does not fail)", rc)
	}
	entries, err := backend_sim.ReadRequestLog(log)
	if err != nil {
		t.Fatalf("AC-17 redirect log read: %v", err)
	}
	faulted := false
	for _, e := range entries {
		if e.Faulted {
			faulted = true
		}
	}
	if !faulted {
		t.Fatalf("AC-17 redirect did not reach the simulator (log %s has no faulted entry)", log)
	}
}

// TestSimVerbRealGateStubDecisionWithOptIn: WITH the full triad the gate
// admits "real" and the v0.1 stub refuses the actual cloud call — the
// decision line must say so (never a silent 200).
func TestSimVerbRealGateStubDecisionWithOptIn(t *testing.T) {
	writeSanctionMarker(t) // MISCHIEF-DF-67 gate
	writeHealthyLoad(t)    // MISCHIEF-DF-28-R2: the AC-8 gate reads the injected posture, not the box's /proc
	log := filepath.Join(t.TempDir(), "sim-requests.log")
	rc, out := captureStdout(t, func() int {
		return cmdSim([]string{"--shape", "I-012", "--log", log,
			"--allow-real", "--resource", "aws:sandbox-lab", "--spend-cap", "0.50"})
	})
	if rc != 0 {
		t.Fatalf("exit %d, want 0", rc)
	}
	// MISCHIEF-DF-15: a gate that DECIDES real must not print the refusal
	// wording — the command prints the v0.1 stub-admission line instead.
	if strings.Contains(out, "refused the real plane") {
		t.Fatalf("real decision printed the refusal wording:\n%s", out)
	}
	if !strings.Contains(out, "backend_sim: I-012: real") {
		t.Fatalf("output does not carry the real decision line:\n%s", out)
	}
	if !strings.Contains(out, "ac17: real adapter is a v0.1 documented stub") ||
		!strings.Contains(out, "landing in the simulator") {
		t.Fatalf("output lacks the stub-admission line:\n%s", out)
	}
	// The proof of the run is still the sim request log (the sim received
	// the redirected probe) — assert it landed.
	entries, err := backend_sim.ReadRequestLog(log)
	if err != nil {
		t.Fatalf("redirect proof log read: %v", err)
	}
	faulted := false
	for _, e := range entries {
		if e.Faulted {
			faulted = true
		}
	}
	if !faulted {
		t.Fatalf("redirect proof missing from %s (no faulted entry)", log)
	}
}

// TestSimVerbRealGateMissingPieceStillNamesRefusal: a PARTIAL triad
// (--allow-real without resource tag and cap) still refuses — the output
// names the missing pieces exactly as before (MISCHIEF-DF-15 regression
// guard for the non-real decisions).
func TestSimVerbRealGateMissingPieceStillNamesRefusal(t *testing.T) {
	writeSanctionMarker(t) // MISCHIEF-DF-67 gate
	writeHealthyLoad(t)    // MISCHIEF-DF-28-R2: the AC-8 gate reads the injected posture, not the box's /proc
	log := filepath.Join(t.TempDir(), "sim-requests.log")
	rc, out := captureStdout(t, func() int {
		return cmdSim([]string{"--shape", "I-012", "--log", log, "--allow-real"})
	})
	if rc != 0 {
		t.Fatalf("exit %d, want 0 (the refusal lands in the sim)", rc)
	}
	for _, want := range []string{
		"backend_sim: I-012: simulator:",
		"an allowlisted resource tag",
		"a spend cap above zero",
		"refused the real plane and landed in the simulator",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("partial-triad refusal lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "is a v0.1 documented stub") {
		t.Fatalf("partial triad must not print the stub-admission line:\n%s", out)
	}
}

// TestSimVerbResourceTagAllowlist is the MISCHIEF-DF-47 command-level
// contract: with the full triad, ONLY the documented allowlisted tag
// (aws:sandbox-lab) reaches the real decision; aws:prod-db,
// prod-fleet-main-host, whitespace and "*" each refuse, the output names
// the offending tag AND the allowlist, and the run still lands in the
// simulator (exit 0, faulted log entry — AC-17). Before this change the
// gate admitted any non-empty tag; these refusals are the red proof at
// the operator surface.
func TestSimVerbResourceTagAllowlist(t *testing.T) {
	writeSanctionMarker(t) // MISCHIEF-DF-67 gate
	writeHealthyLoad(t)    // MISCHIEF-DF-28-R2: the AC-8 gate reads the injected posture, not the box's /proc
	admitted := []string{"aws:sandbox-lab"}
	refused := []string{"aws:prod-db", "prod-fleet-main-host", "*", "   "}
	for _, tag := range admitted {
		log := filepath.Join(t.TempDir(), "sim-requests.log")
		rc, out := captureStdout(t, func() int {
			return cmdSim([]string{"--shape", "I-003", "--log", log,
				"--allow-real", "--resource", tag, "--spend-cap", "5"})
		})
		if rc != 0 {
			t.Fatalf("admitted tag %q: exit %d, want 0", tag, rc)
		}
		if !strings.Contains(out, "backend_sim: I-003: real") {
			t.Fatalf("admitted tag %q lacks the real decision line:\n%s", tag, out)
		}
		if !strings.Contains(out, "ac17: real adapter is a v0.1 documented stub") {
			t.Fatalf("admitted tag %q lacks the stub-admission line:\n%s", tag, out)
		}
		if strings.Contains(out, "not on the AC-17 allowlist") {
			t.Fatalf("admitted tag %q must not be refused by the allowlist:\n%s", tag, out)
		}
	}
	for _, tag := range refused {
		log := filepath.Join(t.TempDir(), "sim-requests.log")
		rc, out := captureStdout(t, func() int {
			return cmdSim([]string{"--shape", "I-003", "--log", log,
				"--allow-real", "--resource", tag, "--spend-cap", "5"})
		})
		if rc != 0 {
			t.Fatalf("tag %q: exit %d, want 0 (the refusal lands in the sim)", tag, rc)
		}
		if !strings.Contains(out, "backend_sim: I-003: simulator:") {
			t.Fatalf("tag %q lacks the simulator-decision refusal line:\n%s", tag, out)
		}
		// each branch of the allowlist source names itself: unlisted
		// tags get "not on the AC-17 allowlist", blank/whitespace gets
		// the blank-refusal phrasing — both name the allowlist and the
		// offending value
		wantReason := "not on the AC-17 allowlist"
		if strings.TrimSpace(tag) == "" {
			wantReason = "blank/whitespace is never allowlisted"
		}
		if !strings.Contains(out, wantReason) {
			t.Fatalf("tag %q refusal must name the allowlist reason %q:\n%s", tag, wantReason, out)
		}
		// the refusal names the offending tag (quoted, so whitespace
		// values are visible in operator output)
		if !strings.Contains(out, strconv.Quote(tag)) {
			t.Fatalf("tag %q refusal must name the offending tag:\n%s", tag, out)
		}
		if strings.Contains(out, "backend_sim: I-003: real") {
			t.Fatalf("tag %q must not reach the real decision:\n%s", tag, out)
		}
		// the refusal STILL lands in the simulator with a faulted entry
		entries, err := backend_sim.ReadRequestLog(log)
		if err != nil {
			t.Fatalf("tag %q: log read: %v", tag, err)
		}
		faulted := false
		for _, e := range entries {
			if e.Faulted {
				faulted = true
			}
		}
		if !faulted {
			t.Fatalf("tag %q: refusal did not land in the sim (log %s has no faulted entry)", tag, log)
		}
	}
}

// TestSimVerbPermanentDestroyRefusedOutright: --destroy-class permanent
// refuses everywhere in v0.1 — the companion-faults doctrine — at the
// operator surface: the verb prints the gate's refusal line (the
// RefusalText form naming the permanent destroy) and exits 2 BEFORE the
// simulator starts, so nothing arms, nothing lands and the request log is
// never created ("refuses outright everywhere, simulator included").
func TestSimVerbPermanentDestroyRefusedOutright(t *testing.T) {
	writeSanctionMarker(t) // the SPEC-13 gate (DF-67) runs BEFORE the permanent-destroy refusal; keep the sanctioned posture so the asserted text is the gate's
	log := filepath.Join(t.TempDir(), "sim-requests.log")
	rc, out := captureStdout(t, func() int {
		return cmdSim([]string{"--shape", "I-003", "--log", log, "--destroy-class", "permanent"})
	})
	if rc != 2 {
		t.Fatalf("exit %d, want 2 (the permanent destroy refuses outright)", rc)
	}
	for _, want := range []string{
		"backend_sim: I-003: refused:",
		"permanent",
		"refuses outright",
		"simulator included",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("permanent-destroy refusal lacks %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("refused run created or wrote the request log %s (nothing may arm or land): %v", log, err)
	}
}

// TestSimVerbUnknownDestroyClassRefuses: a --destroy-class value outside
// the closed vocabulary (reversible | permanent) is a usage refusal that
// NAMES the offending value (exit 2) before anything arms — no request
// log, no simulator start.
func TestSimVerbUnknownDestroyClassRefuses(t *testing.T) {
	writeSanctionMarker(t) // usage validation precedes the gate; posture kept explicit for hermeticity
	log := filepath.Join(t.TempDir(), "sim-requests.log")
	rc, stderr := captureStderr(t, func() int {
		return cmdSim([]string{"--shape", "I-003", "--log", log, "--destroy-class", "delete-the-world"})
	})
	if rc != 2 {
		t.Fatalf("exit %d, want 2", rc)
	}
	if !strings.Contains(stderr, "delete-the-world") {
		t.Fatalf("usage refusal does not name the offending value:\n%s", stderr)
	}
	if !strings.Contains(stderr, "mischief sim:") {
		t.Fatalf("usage refusal does not carry the verb prefix:\n%s", stderr)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("refused run created the request log %s: %v", log, err)
	}
}

// TestSimVerbUnknownShapeRefuses: an id outside the sim-covered set is a
// usage refusal (exit 2), never a silent no-op.
func TestSimVerbUnknownShapeRefuses(t *testing.T) {
	if rc := cmdSim([]string{"--shape", "I-999"}); rc != 2 {
		t.Fatalf("exit %d, want 2", rc)
	}
	if rc := cmdSim(nil); rc != 2 {
		t.Fatalf("bare sim exit %d, want 2", rc)
	}
}

// TestSimShapesCoverLayerISimIDs cross-checks the command-level shape
// table against the ids docs/FAULT-CATALOG.md layer I marks `sim · L4`:
// I-003, I-004, I-005, I-006, I-010, I-011, I-012, I-013, I-014. A layer-I
// id gaining a sim mode without a command shape (or vice versa) fails here.
//
// Each id also carries the PROMISED LANDED-PROOF DETAILS the catalog's
// sim-case table commits to: the proof run must land every marker named
// for that shape (I-004 both legs — malformed 5xx AND endless page).
func TestSimShapesCoverLayerISimIDs(t *testing.T) {
	catalogSimIDs := []string{"I-003", "I-004", "I-005", "I-006", "I-010", "I-011", "I-012", "I-013", "I-014"}
	if len(catalogSimIDs) != len(simCoveredIDs) {
		t.Fatalf("sim coverage %v does not match catalog layer-I sim ids %v", simCoveredIDs, catalogSimIDs)
	}
	for _, id := range catalogSimIDs {
		sh, ok := simShapeFor(id)
		if !ok || !sh.Kind.Valid() {
			t.Fatalf("catalog layer-I sim id %s has no command shape", id)
		}
		// per-shape promised-proof details (the catalog's request-log
		// proof column): a shape whose catalog row names a marker must
		// arm the flags that produce it. I-004 promises BOTH the
		// malformed-body 5xx AND the endless-page marker.
		switch id {
		case "I-004":
			if !sh.Malformed || !sh.PaginationBug {
				t.Fatalf("I-004 command shape must arm BOTH catalog legs (malformed body + endless page), got %+v", sh)
			}
			landI004BothMarkers(t, sh)
		}
	}
}

// landI004BothMarkers runs the I-004 sim case against a real simulator and
// asserts BOTH markers land in the durable request log: interleaved
// malformed-body 5xx entries AND endless-page entries — the landed proof
// the catalog row promises, read back from disk (AC-3).
func landI004BothMarkers(t *testing.T, sh backend_sim.Shape) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "sim-i004.log")
	sim := backend_sim.NewSimulator(log)
	if err := sim.Start(); err != nil {
		t.Fatalf("sim start: %v", err)
	}
	defer sim.Close()
	proof, err := simLandWithProof(sim, "I-004", sh, "/fault")
	if err != nil {
		t.Fatalf("I-004 both-legs proof: %v", err)
	}
	for _, marker := range []string{"malformed-body 5xx", "endless-page"} {
		if !strings.Contains(proof, marker) {
			t.Fatalf("I-004 proof line must name %q, got: %s", marker, proof)
		}
	}
	entries, err := backend_sim.ReadRequestLog(log)
	if err != nil {
		t.Fatalf("I-004 request log read: %v", err)
	}
	malformed, endless := 0, 0
	for _, e := range entries {
		if !e.Faulted || e.Shape != backend_sim.ShapeAPI5xx {
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
		t.Fatalf("I-004 request log must carry BOTH legs (malformed-5xx=%d endless-page=%d)", malformed, endless)
	}
}
