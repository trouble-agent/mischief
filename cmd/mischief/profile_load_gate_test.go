package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/rails"
)

// MISCHIEF-RV-005: the `profile run` landing path's AC-8 load gate, pinned
// at the command level with an injected reading (the same ambient-filesystem
// discipline the battery/selftest/sim gate tests follow — the suite never
// reads the box's live /proc).

// TestProfileRunSaturatedHostRefusesBeforeLanding: at a load reading at/above
// the doctor's gate threshold the profile run verb refuses BEFORE anything is
// written or armed — exit 2, the rails' load_gate refusal verbatim (reason
// load_gate, the measured numbers against the gate), no run dir created, no
// journal, no fault step landed. The gate sits after profile resolution
// (a missing-profile refusal stays a missing-profile refusal) and before the
// run dir is created (fail closed, nothing written).
func TestProfileRunSaturatedHostRefusesBeforeLanding(t *testing.T) {
	sanctionEnv(t) // the sanction gate precedes the load gate; keep the sanctioned posture so the asserted refusal is the gate's
	saved := profileReadLoad
	profileReadLoad = func() (rails.LoadReading, error) {
		return rails.LoadReading{Load1: 27.8, Load5: 30.75, PSIIoSomeAvg10: 3.0}, nil
	}
	t.Cleanup(func() { profileReadLoad = saved })

	dir := t.TempDir()
	rc, stderr := captureStderr(t, func() int {
		return run([]string{"profile", "run", "storage/short-write",
			"--target", "process:1", "--json", "--seed", "7", "--dir", dir})
	})
	if rc != 2 {
		t.Fatalf("exit %d, want 2 (a saturated host must not land a profile fault); stderr=%s", rc, stderr)
	}
	for _, want := range []string{
		"profile run:",
		"rails: load_gate:",
		"host saturated, fault refused",
		"loadavg 1m 27.80 >= gate 12.00",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("load-gate refusal at the operator surface lacks %q:\n%s", want, stderr)
		}
	}
	// Nothing written: the gate sits before the run dir is created, so the
	// --dir root must still be empty (no run id subdir, no journal, no
	// fault events).
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("gate-refused run wrote into the --dir root (nothing may be written or landed): %v", entries)
	}
}

// TestProfileRunHealthyLoadProceedsPastTheGate: the same invocation under an
// injected HEALTHY reading proceeds normally — the run path continues past
// the gate, the executor grades the unattachable target inconclusive (exit
// 3), stdout is one JSON finding naming a run dir that exists. This is the
// gate's admit arm: healthy host, faults may land.
func TestProfileRunHealthyLoadProceedsPastTheGate(t *testing.T) {
	sanctionEnv(t)
	writeHealthyLoad(t)

	dir := t.TempDir()
	rc, stdout := captureStdout(t, func() int {
		return run([]string{"profile", "run", "storage/short-write",
			"--target", "process:1", "--json", "--seed", "7", "--dir", dir})
	})
	if rc != 3 {
		t.Fatalf("exit %d, want 3 (a healthy host proceeds past the gate to the graded outcome); stdout=%s", rc, stdout)
	}
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not one JSON document:\n%s", stdout)
	}
	var f struct {
		Outcome string `json:"outcome"`
		RunDir  string `json:"run_dir"`
	}
	if err := json.Unmarshal([]byte(stdout), &f); err != nil {
		t.Fatal(err)
	}
	if f.Outcome != "inconclusive" || f.RunDir == "" {
		t.Fatalf("finding = %+v", f)
	}
	if _, err := os.Stat(f.RunDir); err != nil {
		t.Fatalf("run dir %s missing after a healthy-load run (the landing machinery must proceed): %v", f.RunDir, err)
	}
}
