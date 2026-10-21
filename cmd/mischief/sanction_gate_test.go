package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SPEC-13 verb-coverage tests (MSF-032, found by MSF-022): the sanction
// gate must cover EVERY fault-landing verb, not only plan. On this
// unsanctioned test host (no marker, no env) selftest and battery refuse
// fail-closed (exit 2) and write NOTHING into the run dir — battery's gate
// runs before EVERYTHING including --dry-run (plan's posture: a
// host-posture check is not a landing act).
//
// RED discipline: before the gates existed, both tests failed with exit 0
// and a landed matrix (measured on the scheduler host, 2026-10-06). The
// ADMITTED path is covered by the existing verb tests via
// writeSanctionMarker (selftest_test.go) — the plane's own shape.

func TestSelftestRefusesUnsanctionedHost(t *testing.T) {
	t.Setenv("MISCHIEF_SANCTION_FILE", "/nonexistent/sanction-marker")
	dir := t.TempDir()
	code, stderr := captureStderr(t, func() int {
		return run([]string{"selftest", "--all", "--dir", dir})
	})
	if code != 2 {
		t.Fatalf("selftest on unsanctioned host: exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "not sanctioned") {
		t.Fatalf("refusal does not name the sanction check: %s", stderr)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("refused selftest wrote into the run dir: %v", ents)
	}
}

func TestBatteryRefusesUnsanctionedHost(t *testing.T) {
	t.Setenv("MISCHIEF_SANCTION_FILE", "/nonexistent/sanction-marker")
	dir := t.TempDir()
	code, stderr := captureStderr(t, func() int {
		return run([]string{"battery", "--quick", "--dry-run", "--dir", dir})
	})
	if code != 2 {
		t.Fatalf("battery on unsanctioned host: exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "not sanctioned") {
		t.Fatalf("refusal does not name the sanction check: %s", stderr)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("refused battery wrote into the run dir: %v", ents)
	}
}

// TestSimRefusesUnsanctionedHost (MISCHIEF-DF-67): the sim verb is a
// fault-landing surface — it arms a cloud shape, serves the fault and
// writes a durable request log claiming a landed proof — so the SPEC-13
// gate must run BEFORE anything arms, binds or writes. On this
// unsanctioned test host sim refuses (exit 2) with the rails refusal
// naming host_not_sanctioned, and NO request log exists: the landed-proof
// claim of an unsanctioned run is exactly the defect this pins (the gate
// previously covered plan/arm/selftest/battery only — same class MSF-032
// fixed for selftest/battery).
//
// RED discipline: before the gate existed, `sim --shape I-003` on this
// host exited 0, printed the AC-17 landed-proof line and left a faulted
// request log on disk (measured by the dogfood tick, 2026-10-21).
func TestSimRefusesUnsanctionedHost(t *testing.T) {
	t.Setenv("MISCHIEF_SANCTION_FILE", "/nonexistent/sanction-marker")
	log := filepath.Join(t.TempDir(), "sim-requests.log")
	code, stderr := captureStderr(t, func() int {
		return run([]string{"sim", "--shape", "I-003", "--log", log})
	})
	if code != 2 {
		t.Fatalf("sim on unsanctioned host: exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "host_not_sanctioned") {
		t.Fatalf("refusal does not name host_not_sanctioned: %s", stderr)
	}
	if !strings.Contains(stderr, "not sanctioned") {
		t.Fatalf("refusal does not name the sanction check: %s", stderr)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("refused sim created or wrote the request log %s (nothing may arm, bind or write): %v", log, err)
	}
}

// TestSimProvesOnSanctionedHost is the positive arm of the DF-67 gate:
// with a reason-bearing marker (the plane's own provisioning shape) the
// verb still lands its proof — fail-closed, not fail-everything.
func TestSimProvesOnSanctionedHost(t *testing.T) {
	writeSanctionMarker(t)
	log := filepath.Join(t.TempDir(), "sim-requests.log")
	code, stdout := captureStdout(t, func() int {
		return run([]string{"sim", "--shape", "I-003", "--log", log})
	})
	if code != 0 {
		t.Fatalf("sim on sanctioned host: exit %d, want 0\nstdout:\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "ac17-redirect") || !strings.Contains(stdout, "proves it") {
		t.Fatalf("sanctioned sim lost its landed proof line:\n%s", stdout)
	}
}

// TestChaosMatrixPostureNamesHost — the chaos matrix's explicit posture
// (MISCHIEF-DF-67): the verb is evidence-plane (it never executes a
// fault), so it is documented to run WITHOUT the gate — but the artifact
// and stdout must SAY which host posture produced them, so a matrix
// generated on an unsanctioned host can never be mistaken for one a
// sanctioned chaos host would derive. The summary line names the host
// posture explicitly.
func TestChaosMatrixPostureNamesHost(t *testing.T) {
	t.Setenv("MISCHIEF_SANCTION_FILE", "/nonexistent/sanction-marker")
	out := t.TempDir()
	code, outTxt := captureStdout(t, func() int {
		return run([]string{"chaos", "matrix", "--project", "mischief", "--tier-max", "1", "--out", out})
	})
	if code != 0 {
		t.Fatalf("chaos matrix (evidence plane) exit %d, want 0\nstdout:\n%s", code, outTxt)
	}
	if !strings.Contains(outTxt, "unsanctioned chaos host") {
		t.Fatalf("matrix summary does not name the host posture:\n%s", outTxt)
	}
	b, err := os.ReadFile(filepath.Join(out, "matrix.json"))
	if err != nil {
		t.Fatalf("matrix.json: %v", err)
	}
	if !strings.Contains(string(b), `"host_posture"`) {
		t.Fatalf("matrix.json carries no host_posture field (posture must be explicit in the artifact)")
	}
}

// TestChaosMatrixSanctionedPostureStamped pins the sanctioned arm: on a
// sanctioned host the artifact says so (the two postures are
// distinguishable in the artifact, never blank).
func TestChaosMatrixSanctionedPostureStamped(t *testing.T) {
	writeSanctionMarker(t)
	out := t.TempDir()
	code, _ := captureStdout(t, func() int {
		return run([]string{"chaos", "matrix", "--project", "mischief", "--tier-max", "1", "--out", out})
	})
	if code != 0 {
		t.Fatalf("chaos matrix sanctioned exit %d, want 0", code)
	}
	b, err := os.ReadFile(filepath.Join(out, "matrix.json"))
	if err != nil {
		t.Fatalf("matrix.json: %v", err)
	}
	if !strings.Contains(string(b), `"host_posture": "sanctioned"`) {
		t.Fatalf("sanctioned matrix.json does not stamp host_posture=sanctioned:\n%s", string(b))
	}
}
