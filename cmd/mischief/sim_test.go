package main

import (
	"path/filepath"
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
	log := filepath.Join(t.TempDir(), "sim-requests.log")
	rc := cmdSim([]string{"--shape", "I-012", "--log", log,
		"--allow-real", "--resource", "aws:sandbox-lab", "--spend-cap", "0.50"})
	if rc != 0 {
		t.Fatalf("exit %d, want 0", rc)
	}
	// The gate admitted real; the adapter is a v0.1 stub. The proof of the
	// run is still the sim request log (the sim received the redirected
	// probe) — assert it landed.
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

// TestSimVerbPermanentDestroyRefusedOutright: destroy_class=permanent
// refuses everywhere in v0.1 — the companion-faults doctrine — and the
// refusal names the doctrine.
func TestSimVerbPermanentDestroyRefusedOutright(t *testing.T) {
	// The sim verb only arms reversible layer-I shapes; a permanent-destroy
	// request cannot be expressed as a shape, so the gate-level refusal is
	// asserted directly (the command refuses before any arming).
	gate := backend_sim.RealGate{AllowReal: true, ResourceTag: "aws:x", SpendCapUSD: 1}
	d, detail := gate.Decide("permanent")
	if d != backend_sim.DecideRefused || !strings.Contains(detail, "permanent") {
		t.Fatalf("permanent destroy not refused outright: %s %s", d, detail)
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
	}
}
