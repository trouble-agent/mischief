package selftest

// entry.go — the verb-level entry points the CLI consumes.

import (
	"fmt"
	"sort"

	"github.com/trouble-agent/mischief/internal/doctor"
	"github.com/trouble-agent/mischief/internal/rails"
)

// ReadLoad is the AC-8 measurement seam (MISCHIEF-DF-28; the doctor's own
// package-level-seam pattern): production = rails.ReadLoad (the live
// /proc/loadavg + /proc/pressure/io read); tests point it at an injected
// LoadReading — a test never depends on, or perturbs, the box it runs on
// (ambient-filesystem discipline). The runner consults it once per
// primitive, BEFORE the land+revert loop: the gate is the rails' last
// admission rail, and a refusal means nothing landed.
var ReadLoad = rails.ReadLoad

// newGate builds the run gate from the doctor's thresholds (load 12.0,
// PSI 50.0 — the same numbers doctor reports, so the doctor's grade and
// the selftest's refusal agree on where the ceiling is).
func newGate() (*rails.LoadGate, error) {
	return rails.NewLoadGate(rails.LoadGateConfig{
		LoadThreshold: doctor.LoadThreshold,
		PSIThreshold:  doctor.PSIThreshold,
	})
}

// gateReason renders one gate refusal as a result reason: the refusal's
// own text (reason load_gate, the measured numbers vs the gate, verdict
// aborted) — the recorded outcome carries the measurement, per AC-8.
func gateReason(err error) string {
	return fmt.Sprintf("load_gate: %v", err)
}

// refused is the result a gate-refused primitive records: the AC-11 skip
// shape (a recorded outcome, never a pass — the AC-19 set stays honest),
// graded aborted by the journal (the rails' verdict for "this run did not
// happen"). Nothing armed, nothing landed, nothing reverted.
func refused(id, reason string) Result {
	return Result{ID: id, State: "skip", Reason: reason}
}

// RunOnePublic is the exported single-primitive run (the CLI's
// --primitive path).
func RunOnePublic(id string) Result { return newRunner().RunOne(id) }

// RunAllPublic is the exported all-primitives run (the CLI's --all path).
func RunAllPublic() []Result { return newRunner().RunAll() }

// Print renders the result lines (sorted by id).
func Print(res []Result) string {
	sorted := make([]Result, len(res))
	copy(sorted, res)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	out := ""
	for _, r := range sorted {
		out += r.String() + "\n"
	}
	return out
}
