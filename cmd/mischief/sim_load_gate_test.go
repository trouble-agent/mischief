package main

// sim_load_gate_test.go — the AC-8 load gate on the `mischief sim` landing
// path (MISCHIEF-DF-65). The verb arms a cloud shape, serves the fault and
// writes a durable request log claiming a landed proof — so the gate must
// run BEFORE anything arms, binds or writes, exactly as it does on the
// battery and selftest landing paths (MISCHIEF-DF-28). Readings are
// INJECTED via the backend_sim package ReadLoad seam (ambient-filesystem
// discipline — the suite never reads the box's real /proc numbers); the
// gate itself is left REAL (rails.LoadGate grades the pinned reading, so
// the refusal text is the production text).
//
// RED discipline: before the gate existed, a saturated-host sim run
// exited 0 with a landed fault and a written request log (dogfood run-26:
// battery refused at loadavg 21.44 while sim landed I-003 in the same
// minute). These tests pin the refusal arm and the admitted arm at the
// operator surface.
//
// ch:trace row=MISCHIEF-DF-65 spec=docs/SPEC-PLAN.md#SPEC-03 test=cmd/mischief/sim_load_gate_test.go evidence=cmd/mischief/sim.go witness=none:injected-readings-no-live-run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/backend_sim"
	"github.com/trouble-agent/mischief/internal/rails"
)

// pinSimLoad pins the backend_sim ReadLoad seam (the seam cmdSim's gate
// consults) to the given reading + err for the test's duration.
func pinSimLoad(t *testing.T, r rails.LoadReading, err error) {
	t.Helper()
	old := backend_sim.ReadLoad
	backend_sim.ReadLoad = func() (rails.LoadReading, error) { return r, err }
	t.Cleanup(func() { backend_sim.ReadLoad = old })
}

// pinHealthySimLoad is the pre-existing verb tests' posture (the
// battery_test.go writeHealthyLoad shape): the tests that reach the
// landing path measure the AC-17/usage/sanction machinery, not the box's
// real /proc, and a saturated CI host must not red them — MISCHIEF-DF-65
// wired the load gate under every cmdSim landing, so a test that reaches
// the sim pins a healthy injected reading (the gate's refusal arms are
// covered by the injected-reading tests above).
func pinHealthySimLoad(t *testing.T) {
	t.Helper()
	pinSimLoad(t, rails.LoadReading{Load1: 1.0, Load5: 0.8, PSIIoSomeAvg10: 2.0}, nil)
}

// TestSimVerbRefusesSaturatedHost: over the load ceiling ⇒ exit 2 with
// the rails refusal naming the measured numbers vs the gate, and NOTHING
// arms, binds or writes — the request log is never created (the sim's
// log is the landed-proof channel; a refused run must not produce one).
func TestSimVerbRefusesSaturatedHost(t *testing.T) {
	writeSanctionMarker(t) // the SPEC-13 gate passes; the load gate is what must refuse
	log := filepath.Join(t.TempDir(), "sim-requests.log")
	pinSimLoad(t, rails.LoadReading{Load1: 21.44, Load5: 18.2, PSIIoSomeAvg10: 3.0}, nil)
	rc, out := captureStderr(t, func() int {
		return cmdSim([]string{"--shape", "I-003", "--log", log})
	})
	if rc != 2 {
		t.Fatalf("saturated-host sim exit %d, want 2 (the load gate refuses; before MISCHIEF-DF-65 this landed the fault at exit 0)\nstdout/stderr:\n%s", rc, out)
	}
	for _, want := range []string{"load_gate", "21.44", "12.00", "refused"} {
		if !strings.Contains(out, want) {
			t.Fatalf("refusal must name %q (AC-8: measured numbers vs the gate):\n%s", want, out)
		}
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("refused run created or wrote the request log %s (nothing may arm, bind or write): %v", log, err)
	}
}

// TestSimVerbPsiArmRefuses: the PSI arm alone breaches ⇒ refused (both
// gate arms live on the sim landing path).
func TestSimVerbPsiArmRefuses(t *testing.T) {
	writeSanctionMarker(t)
	log := filepath.Join(t.TempDir(), "sim-requests.log")
	pinSimLoad(t, rails.LoadReading{Load1: 1.0, Load5: 0.9, PSIIoSomeAvg10: 88.5}, nil)
	rc, out := captureStderr(t, func() int {
		return cmdSim([]string{"--shape", "I-003", "--log", log})
	})
	if rc != 2 {
		t.Fatalf("PSI-saturated sim exit %d, want 2\noutput:\n%s", rc, out)
	}
	if !strings.Contains(out, "load_gate") || !strings.Contains(out, "88.50") {
		t.Fatalf("refusal must name the psi arm and its measurement:\n%s", out)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("refused run created the request log %s: %v", log, err)
	}
}

// TestSimVerbUnreadableLoadFailsClosed: a measurement error refuses exit
// 2 (fail closed — the gate cannot say what it measured, so nothing
// lands), naming the gate and the error.
func TestSimVerbUnreadableLoadFailsClosed(t *testing.T) {
	writeSanctionMarker(t)
	log := filepath.Join(t.TempDir(), "sim-requests.log")
	pinSimLoad(t, rails.LoadReading{}, os.ErrPermission)
	rc, out := captureStderr(t, func() int {
		return cmdSim([]string{"--shape", "I-003", "--log", log})
	})
	if rc != 2 {
		t.Fatalf("unreadable-measurement sim exit %d, want 2 (fail closed)\noutput:\n%s", rc, out)
	}
	if !strings.Contains(out, "load gate") {
		t.Fatalf("measurement-failure refusal must name the gate:\n%s", out)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("refused run created the request log %s: %v", log, err)
	}
}

// TestSimVerbHealthyReadingProceeds: under every ceiling ⇒ exit 0 and the
// AC-17 landed proof (the gate must not over-refuse a healthy host — the
// sim's landing semantics are unchanged behind the gate).
func TestSimVerbHealthyReadingProceeds(t *testing.T) {
	writeSanctionMarker(t)
	log := filepath.Join(t.TempDir(), "sim-requests.log")
	pinSimLoad(t, rails.LoadReading{Load1: 1.0, Load5: 0.8, PSIIoSomeAvg10: 2.0}, nil)
	rc, out := captureStdout(t, func() int {
		return cmdSim([]string{"--shape", "I-003", "--log", log})
	})
	if rc != 0 {
		t.Fatalf("healthy-host sim exit %d, want 0\nstdout/stderr:\n%s", rc, out)
	}
	if !strings.Contains(out, "ac17-redirect") || !strings.Contains(out, "proves it") {
		t.Fatalf("healthy sim lost its landed proof line:\n%s", out)
	}
	entries, err := backend_sim.ReadRequestLog(log)
	if err != nil {
		t.Fatalf("request log read: %v", err)
	}
	faulted := false
	for _, e := range entries {
		if e.Faulted && e.Shape == backend_sim.ShapeRateLimit {
			faulted = true
		}
	}
	if !faulted {
		t.Fatalf("healthy run did not land (log %s carries no faulted api-rate-limit entry)", log)
	}
}

// TestSimVerbUnknownShapeRefusedBeforeGate: the load gate is the LAST
// rail — an id outside the sim-covered set fails for ITS reason (usage,
// exit 2) even on a saturated host, and the gate is never consulted
// (an unknown shape is not a load-gate refusal; the measurement seam
// must stay untouched so the refusal text cannot be masked).
func TestSimVerbUnknownShapeRefusedBeforeGate(t *testing.T) {
	writeSanctionMarker(t)
	consulted := false
	old := backend_sim.ReadLoad
	backend_sim.ReadLoad = func() (rails.LoadReading, error) {
		consulted = true
		return rails.LoadReading{Load1: 99}, nil
	}
	t.Cleanup(func() { backend_sim.ReadLoad = old })

	rc, out := captureStderr(t, func() int {
		return cmdSim([]string{"--shape", "I-999"})
	})
	if rc != 2 {
		t.Fatalf("unknown shape exit %d, want 2", rc)
	}
	if !strings.Contains(out, "unknown layer-I shape") {
		t.Fatalf("expected the usage refusal, got:\n%s", out)
	}
	if consulted {
		t.Fatal("the load gate was consulted for an unknown shape — usage refusals are not a load-gate matter")
	}
}

// TestSimVerbPermanentDestroyRefusedBeforeGate: the permanent-destroy
// policy refusal keeps ITS reason on a saturated host too — policy
// refusals are not load-gate refusals, and the gate is not consulted for
// a request that refuses outright (nothing would land either way).
func TestSimVerbPermanentDestroyRefusedBeforeGate(t *testing.T) {
	writeSanctionMarker(t)
	log := filepath.Join(t.TempDir(), "sim-requests.log")
	consulted := false
	old := backend_sim.ReadLoad
	backend_sim.ReadLoad = func() (rails.LoadReading, error) {
		consulted = true
		return rails.LoadReading{Load1: 99}, nil
	}
	t.Cleanup(func() { backend_sim.ReadLoad = old })

	rc, out := captureStdout(t, func() int {
		return cmdSim([]string{"--shape", "I-003", "--log", log, "--destroy-class", "permanent"})
	})
	if rc != 2 {
		t.Fatalf("permanent destroy exit %d, want 2", rc)
	}
	if !strings.Contains(out, "refuses outright") {
		t.Fatalf("expected the permanent-destroy refusal, got:\n%s", out)
	}
	if consulted {
		t.Fatal("the load gate was consulted for an outright policy refusal — the gate is the LAST rail")
	}
}
