package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/backend_sim"
	"github.com/trouble-agent/mischief/internal/rails"
)

// MISCHIEF-DF-28-R2: the sim landing path's AC-8 load gate, pinned at the
// command level with an injected reading (the same ambient-filesystem
// discipline the battery/selftest gate tests follow — the suite never
// reads the box's live /proc).

// TestSimVerbSaturatedHostRefusesWithNumbers: at a load reading at/above
// the doctor's gate threshold the sim verb refuses BEFORE anything arms —
// exit 2, the rails' load_gate refusal verbatim (reason load_gate, the
// measured numbers against the gate), no simulator start (the request log
// is never created), no landed-proof line. Before this gate existed sim
// landed a fault at loadavg 27.8 while battery refused and selftest
// skipped (run-12's dogfood class).
func TestSimVerbSaturatedHostRefusesWithNumbers(t *testing.T) {
	writeSanctionMarker(t) // the sanction gate precedes the load gate; keep the sanctioned posture so the asserted refusal is the gate's
	saved := simReadLoad
	simReadLoad = func() (rails.LoadReading, error) {
		return rails.LoadReading{Load1: 27.8, Load5: 30.75, PSIIoSomeAvg10: 3.0}, nil
	}
	t.Cleanup(func() { simReadLoad = saved })

	log := filepath.Join(t.TempDir(), "sim-requests.log")
	rc, stderr := captureStderr(t, func() int {
		return cmdSim([]string{"--shape", "I-003", "--log", log})
	})
	if rc != 2 {
		t.Fatalf("exit %d, want 2 (a saturated host must not land a sim fault)", rc)
	}
	for _, want := range []string{
		"mischief sim:",
		"load_gate",
		"host saturated, fault refused",
		"loadavg 1m 27.80 >= gate 12.00",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("load-gate refusal at the operator surface lacks %q:\n%s", want, stderr)
		}
	}
	// the refusal at threshold: loadavg 1m is the breaching arm (the load5
	// number rides in the same reading; the rails report the arms that
	// breach, and PSI 3.0 is under its ceiling so only the load arm shows)
	if !strings.Contains(stderr, ">= gate 12.00") {
		t.Fatalf("load-gate refusal does not name the measured-vs-gate comparison:\n%s", stderr)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("gate-refused run created or wrote the request log %s (nothing may arm or land): %v", log, err)
	}
}

// TestSimVerbPSIBreachRefuses: the PSI arm alone refuses a landing — the
// loadavg comfortably under the gate, the PSI io some avg10 at/above 50.
// The refusal names the PSI measurement against the gate.
func TestSimVerbPSIBreachRefuses(t *testing.T) {
	writeSanctionMarker(t)
	saved := simReadLoad
	simReadLoad = func() (rails.LoadReading, error) {
		return rails.LoadReading{Load1: 1.2, Load5: 0.9, PSIIoSomeAvg10: 55.0}, nil
	}
	t.Cleanup(func() { simReadLoad = saved })

	log := filepath.Join(t.TempDir(), "sim-requests.log")
	rc, stderr := captureStderr(t, func() int {
		return cmdSim([]string{"--shape", "I-012", "--log", log})
	})
	if rc != 2 {
		t.Fatalf("exit %d, want 2 (PSI io some avg10 55.0 is at/above the 50.0 gate)", rc)
	}
	for _, want := range []string{
		"load_gate",
		"psi io some avg10 55.00% >= gate 50.00%",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("PSI refusal lacks %q:\n%s", want, stderr)
		}
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("gate-refused run created the request log %s: %v", log, err)
	}
}

// TestSimVerbBothArmsBreachedRefusesWithBothNumbers: when both arms
// breach, the refusal reports BOTH measurements (the rails' gate reports
// every enabled arm — the operator sees the full picture, not a redacted
// one).
func TestSimVerbBothArmsBreachedRefusesWithBothNumbers(t *testing.T) {
	writeSanctionMarker(t)
	saved := simReadLoad
	simReadLoad = func() (rails.LoadReading, error) {
		return rails.LoadReading{Load1: 27.8, Load5: 30.75, PSIIoSomeAvg10: 55.0}, nil
	}
	t.Cleanup(func() { simReadLoad = saved })

	rc, stderr := captureStderr(t, func() int {
		return cmdSim([]string{"--shape", "I-003"})
	})
	if rc != 2 {
		t.Fatalf("exit %d, want 2", rc)
	}
	for _, want := range []string{
		"loadavg 1m 27.80 >= gate 12.00",
		"psi io some avg10 55.00% >= gate 50.00%",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("both-arms refusal must name BOTH measurements, lacks %q:\n%s", want, stderr)
		}
	}
}

// TestSimVerbGateUnderThresholdStillLands: just under the ceiling the run
// proceeds exactly as before the gate existed — exit 0, the AC-17
// redirect proof, a faulted log entry. The gate refuses AT/above the
// threshold, not comfortably under it (the rails' >= semantics).
func TestSimVerbGateUnderThresholdStillLands(t *testing.T) {
	writeSanctionMarker(t)
	saved := simReadLoad
	simReadLoad = func() (rails.LoadReading, error) {
		return rails.LoadReading{Load1: 11.99, Load5: 11.5, PSIIoSomeAvg10: 49.9}, nil
	}
	t.Cleanup(func() { simReadLoad = saved })

	log := filepath.Join(t.TempDir(), "sim-requests.log")
	rc, out, _ := captureBoth(t, func() int {
		return cmdSim([]string{"--shape", "I-012", "--log", log})
	})
	if rc != 0 {
		t.Fatalf("exit %d, want 0 (11.99 < 12.0 and 49.9 < 50.0 must still land):\nstderr=%s", rc, out)
	}
	entries, err := backend_sim.ReadRequestLog(log)
	if err != nil {
		t.Fatalf("request log %s read: %v", log, err)
	}
	faulted := false
	for _, e := range entries {
		if e.Faulted && e.Shape == backend_sim.ShapeQuota {
			faulted = true
		}
	}
	if !faulted {
		t.Fatalf("under-threshold run must still land (log %s has no faulted ShapeQuota entry)", log)
	}
}

// TestSimVerbMeasurementErrorRefusesFailClosed: a load measurement that
// FAILS (unreadable /proc) refuses rather than silently gating off — the
// fleet posture: a landing path that cannot say what it measured does not
// run. No request log, exit 2.
func TestSimVerbMeasurementErrorRefusesFailClosed(t *testing.T) {
	writeSanctionMarker(t)
	saved := simReadLoad
	simReadLoad = func() (rails.LoadReading, error) {
		return rails.LoadReading{}, os.ErrPermission
	}
	t.Cleanup(func() { simReadLoad = saved })

	log := filepath.Join(t.TempDir(), "sim-requests.log")
	rc, stderr := captureStderr(t, func() int {
		return cmdSim([]string{"--shape", "I-003", "--log", log})
	})
	if rc != 2 {
		t.Fatalf("exit %d, want 2 (a failed measurement refuses fail-closed, never silently ungated)", rc)
	}
	if !strings.Contains(stderr, "load gate: permission denied") {
		t.Fatalf("measurement-error refusal does not name the failure:\n%s", stderr)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("gate-refused run created the request log %s: %v", log, err)
	}
}