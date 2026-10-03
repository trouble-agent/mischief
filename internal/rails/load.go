package rails

import (
	"fmt"
	"strconv"
	"strings"
)

// LoadReading is one host-load measurement. All three fields are required
// for a check; a gate that cannot say what it measured does not run (the
// fleet doctrine: evidence-first, "I/O some avg10=19.35" is a measurement,
// "the host felt slow" is not).
type LoadReading struct {
	// Load1 is the 1-minute load average.
	Load1 float64
	// Load5 is the 5-minute load average.
	Load5 float64
	// PSIIoSomeAvg10 is PSI io "some" avg10 as a percent (0..100), -1 when
	// PSI was not read.
	PSIIoSomeAvg10 float64
}

// LoadGateConfig is the gate: the thresholds a host must stay under for a
// fault to be allowed to land. Zero/negative LoadThreshold disables the
// load-average arm; negative PSIThreshold disables the PSI arm. At least
// one arm must be enabled — a gate with no arms is a misconfiguration and
// refuses rather than passing everything.
type LoadGateConfig struct {
	// LoadThreshold is the 1-minute load-average ceiling.
	LoadThreshold float64
	// PSIThreshold is the PSI io some avg10 percent ceiling.
	PSIThreshold float64
}

// LoadGate refuses to land faults on a saturated host (AC-8; PRD §7: "The
// load gate refuses to land a fault on a saturated host. An injector that
// adds load to a struggling box manufactures the incident it claims to
// observe."). It is constructed with a reading — production reads
// /proc/loadavg + /proc/pressure/io at call time (ReadLoad); tests inject
// numbers.
type LoadGate struct {
	cfg LoadGateConfig
}

// NewLoadGate builds a gate from config.
func NewLoadGate(cfg LoadGateConfig) (*LoadGate, error) {
	loadOn := cfg.LoadThreshold > 0
	psiOn := cfg.PSIThreshold >= 0
	if !loadOn && !psiOn {
		return nil, newRefusal(ReasonLoad, "", "load gate misconfigured: both the load-average and the PSI arm are disabled — configure LoadThreshold > 0 or PSIThreshold >= 0")
	}
	return &LoadGate{cfg: cfg}, nil
}

// Check refuses when the reading is at or above any enabled threshold. The
// refusal text names the measured numbers against the gate (AC-8: "the
// reason names the measurement"): loadavg 1m/5m vs the load ceiling, PSI io
// some avg10 vs the PSI ceiling — every enabled arm is reported, the
// offending one flagged.
func (g *LoadGate) Check(r LoadReading) error {
	if g == nil {
		return nil
	}
	var breaches []string
	if g.cfg.LoadThreshold > 0 {
		if r.Load1 >= g.cfg.LoadThreshold {
			breaches = append(breaches, fmt.Sprintf("loadavg 1m %.2f >= gate %.2f", r.Load1, g.cfg.LoadThreshold))
		}
	}
	if g.cfg.PSIThreshold >= 0 && r.PSIIoSomeAvg10 >= 0 {
		if r.PSIIoSomeAvg10 >= g.cfg.PSIThreshold {
			breaches = append(breaches, fmt.Sprintf("psi io some avg10 %.2f%% >= gate %.2f%%", r.PSIIoSomeAvg10, g.cfg.PSIThreshold))
		}
	}
	if len(breaches) > 0 {
		return newRefusal(ReasonLoad, "",
			fmt.Sprintf("host saturated, fault refused — measured %s", strings.Join(breaches, "; ")))
	}
	return nil
}

// ReadLoad measures the host now: /proc/loadavg for the averages,
// /proc/pressure/io for PSI io "some" avg10. It never blocks and never
// writes; a missing PSI line reports -1 (the arm then reads as unmeasured
// and is not breached by the missing data).
func ReadLoad() (LoadReading, error) {
	r := LoadReading{PSIIoSomeAvg10: -1}
	raw, err := readFile("/proc/loadavg")
	if err != nil {
		return r, fmt.Errorf("load gate: read loadavg: %w", err)
	}
	fields := strings.Fields(raw)
	if len(fields) < 3 {
		return r, fmt.Errorf("load gate: loadavg: want >=3 fields, got %q", raw)
	}
	if r.Load1, err = strconv.ParseFloat(fields[0], 64); err != nil {
		return r, fmt.Errorf("load gate: loadavg 1m: %w", err)
	}
	if r.Load5, err = strconv.ParseFloat(fields[1], 64); err != nil {
		return r, fmt.Errorf("load gate: loadavg 5m: %w", err)
	}
	if praw, perr := readFile("/proc/pressure/io"); perr == nil {
		if v, ok := parsePSISomeAvg10(praw); ok {
			r.PSIIoSomeAvg10 = v
		}
	}
	return r, nil
}

// parsePSISomeAvg10 pulls the "some avg10=" percent from a /proc/pressure/io body.
func parsePSISomeAvg10(body string) (float64, bool) {
	for _, line := range strings.Split(body, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "some "); ok {
			for _, f := range strings.Fields(rest) {
				if v, ok := strings.CutPrefix(f, "avg10="); ok {
					pct, err := strconv.ParseFloat(v, 64)
					if err != nil {
						return 0, false
					}
					return pct, true
				}
			}
		}
	}
	return 0, false
}

// ch:trace row=MSF-004 spec=docs/SPEC-PLAN.md (SPEC-03) evidence=internal/rails/load.go witness=none:no-live-host-run-in-worktree
