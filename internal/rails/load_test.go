// Tests for the load gate (AC-8): a saturated host refuses, and the refusal
// text names BOTH the threshold and the measured number. Readings are
// injected — the test never reads the real /proc of the box it runs on
// (ambient-filesystem discipline; ReadLoad's procfs side is exercised
// separately through the package readFile seam).
//
// ch:trace row=MSF-004 spec=docs/SPEC-PLAN.md (SPEC-03) test=internal/rails/load_test.go evidence=internal/rails/load_test.go witness=none:no-live-host-run-in-worktree
package rails

import (
	"errors"
	"strings"
	"testing"
)

// TestLoadGateCheckRefusesWithNumbers: AC-8's contract — over the threshold
// refuses with a text carrying the measured value AND the gate value, on
// each arm, alone and together.
func TestLoadGateCheckRefusesWithNumbers(t *testing.T) {
	gate, err := NewLoadGate(LoadGateConfig{LoadThreshold: 8.0, PSIThreshold: 30.0})
	if err != nil {
		t.Fatalf("NewLoadGate: %v", err)
	}

	cases := []struct {
		name     string
		reading  LoadReading
		wantText []string // every fragment the refusal must carry
	}{
		{
			name:    "load over threshold",
			reading: LoadReading{Load1: 19.35, Load5: 12.1, PSIIoSomeAvg10: 1.2},
			wantText: []string{
				"19.35",   // the measured number (AC-8: "the reason names the measurement")
				"8.00",    // the threshold
				"loadavg", // which arm breached
				"refused", // the verdict of the gate
			},
		},
		{
			name:     "psi over threshold",
			reading:  LoadReading{Load1: 1.0, Load5: 0.9, PSIIoSomeAvg10: 42.5},
			wantText: []string{"42.50", "30.00", "psi"},
		},
		{
			name:     "both arms over",
			reading:  LoadReading{Load1: 19.35, Load5: 18.0, PSIIoSomeAvg10: 88.1},
			wantText: []string{"19.35", "8.00", "88.10", "30.00", "psi"},
		},
	}
	for _, tc := range cases {
		err := gate.Check(tc.reading)
		if err == nil {
			t.Fatalf("%s: Check(%v) passed a saturated reading, want the load refusal", tc.name, tc.reading)
		}
		if !IsRefusal(err) {
			t.Fatalf("%s: error %T is not a rails refusal", tc.name, err)
		}
		ref := err.(*Refusal)
		if ref.Reason != ReasonLoad {
			t.Fatalf("%s: Reason=%q, want %q", tc.name, ref.Reason, ReasonLoad)
		}
		if ref.Verdict != VerdictAborted {
			t.Fatalf("%s: Verdict=%q, want %q (AC-8: refused with aborted)", tc.name, ref.Verdict, VerdictAborted)
		}
		for _, want := range tc.wantText {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("%s: refusal text %q must name %q (AC-8: measured numbers vs the gate)", tc.name, err.Error(), want)
			}
		}
		if MapExit(err) != 2 {
			t.Fatalf("%s: MapExit(load refusal)=%d, want 2", tc.name, MapExit(err))
		}
	}
}

// TestLoadGateCheckPassesUnderThreshold: strictly under every enabled arm
// passes; at-the-threshold refuses (the gate is a ceiling, >= refuses).
func TestLoadGateCheckPassesUnderThreshold(t *testing.T) {
	gate, err := NewLoadGate(LoadGateConfig{LoadThreshold: 8.0, PSIThreshold: 30.0})
	if err != nil {
		t.Fatalf("NewLoadGate: %v", err)
	}
	if err := gate.Check(LoadReading{Load1: 7.99, Load5: 40.0, PSIIoSomeAvg10: 29.99}); err != nil {
		t.Fatalf("Check under every ceiling: %v (5m and unmeasured arms must not breach)", err)
	}
	if err := gate.Check(LoadReading{Load1: 8.0, PSIIoSomeAvg10: -1}); err == nil {
		t.Fatal("Check at exactly the load ceiling passed; >= the threshold refuses")
	}
	if err := gate.Check(LoadReading{Load1: 0.1, PSIIoSomeAvg10: 30.0}); err == nil {
		t.Fatal("Check at exactly the PSI ceiling passed; >= the threshold refuses")
	}
}

// TestLoadGateUnmeasuredPSINeverBreaches: PSI = -1 means "not measured";
// a missing measurement must not fabricate a breach (evidence-first: the
// gate refuses on numbers, not on absence).
func TestLoadGateUnmeasuredPSINeverBreaches(t *testing.T) {
	gate, err := NewLoadGate(LoadGateConfig{LoadThreshold: 8.0, PSIThreshold: 0.5})
	if err != nil {
		t.Fatalf("NewLoadGate: %v", err)
	}
	if err := gate.Check(LoadReading{Load1: 0.2, PSIIoSomeAvg10: -1}); err != nil {
		t.Fatalf("unmeasured PSI breached the gate: %v", err)
	}
}

// TestLoadGateArms: a disabled load arm must not refuse, and a gate with no
// arms at all is a misconfiguration that refuses at construction rather
// than passing everything (fail closed).
func TestLoadGateArms(t *testing.T) {
	psiOnly, err := NewLoadGate(LoadGateConfig{PSIThreshold: 30.0})
	if err != nil {
		t.Fatalf("NewLoadGate(psi-only): %v", err)
	}
	if err := psiOnly.Check(LoadReading{Load1: 500, PSIIoSomeAvg10: 1.0}); err != nil {
		t.Fatalf("disabled load arm refused: %v", err)
	}
	loadOnly, err := NewLoadGate(LoadGateConfig{LoadThreshold: 4.0, PSIThreshold: -1})
	if err != nil {
		t.Fatalf("NewLoadGate(load-only): %v", err)
	}
	if err := loadOnly.Check(LoadReading{Load1: 1.0, PSIIoSomeAvg10: 99.9}); err != nil {
		t.Fatalf("disabled PSI arm refused: %v", err)
	}
	_, err = NewLoadGate(LoadGateConfig{LoadThreshold: 0, PSIThreshold: -1})
	if err == nil {
		t.Fatal("a gate with no arms constructed; want the misconfiguration refusal (fail closed)")
	}
	if !IsRefusal(err) {
		t.Fatalf("misconfiguration error %T is not a rails refusal", err)
	}
}

// TestReadLoadViaSeam: ReadLoad parses /proc shapes through the package's
// readFile seam — never the box's real /proc numbers. A full loadavg line
// with a PSI body parses both arms; a missing PSI line reports -1 and the
// gate then treats it as unmeasured.
func TestReadLoadViaSeam(t *testing.T) {
	orig := readFile
	t.Cleanup(func() { readFile = orig })

	readFile = func(path string) (string, error) {
		switch path {
		case "/proc/loadavg":
			return "2.41 1.90 1.75 3/1234 5678\n", nil
		case "/proc/pressure/io":
			return "some avg10=12.34 avg60=8.00 avg300=4.00 total=12345\nfull avg10=1.00 avg60=0.50 avg300=0.10 total=678\n", nil
		}
		return "", errors.New("unexpected path " + path)
	}
	r, err := ReadLoad()
	if err != nil {
		t.Fatalf("ReadLoad: %v", err)
	}
	if r.Load1 != 2.41 || r.Load5 != 1.90 {
		t.Fatalf("loadavg parsed %.2f/%.2f, want 2.41/1.90", r.Load1, r.Load5)
	}
	if r.PSIIoSomeAvg10 != 12.34 {
		t.Fatalf("PSI some avg10 parsed %.2f, want 12.34", r.PSIIoSomeAvg10)
	}

	// PSI file missing → unmeasured (-1), loadavg still valid
	readFile = func(path string) (string, error) {
		if path == "/proc/loadavg" {
			return "0.10 0.20 0.30 1/2 3\n", nil
		}
		return "", errors.New("no such file")
	}
	r, err = ReadLoad()
	if err != nil {
		t.Fatalf("ReadLoad without PSI: %v", err)
	}
	if r.PSIIoSomeAvg10 != -1 {
		t.Fatalf("missing PSI reported %.2f, want -1 (unmeasured)", r.PSIIoSomeAvg10)
	}

	// loadavg missing is a hard error — the gate cannot say what it measured
	readFile = func(string) (string, error) { return "", errors.New("no proc") }
	if _, err := ReadLoad(); err == nil {
		t.Fatal("ReadLoad without loadavg succeeded; a gate that cannot measure must error")
	}
}
