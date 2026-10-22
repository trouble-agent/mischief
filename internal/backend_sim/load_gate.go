package backend_sim

import (
	"fmt"

	"github.com/trouble-agent/mischief/internal/doctor"
	"github.com/trouble-agent/mischief/internal/rails"
)

// load_gate.go — the AC-8 load gate on the sim landing path
// (MISCHIEF-DF-65). battery and selftest land behind the rails' load gate
// (MISCHIEF-DF-28: a saturated host records the refusal, nothing arms);
// the `mischief sim` verb armed and fired the cloud fault directly, so a
// host at loadavg 21.44 watched battery refuse while sim landed the fault
// in the same minute (dogfood run-26's discriminating pair). The gate
// here is the SAME check those paths run: rails.LoadGate at the doctor's
// thresholds (load 12.0, PSI 50.0 — the numbers doctor reports, so the
// doctor's grade and the sim's refusal agree), consulted BEFORE anything
// arms, binds or writes: the request log below the landing is a
// landed-proof claim, and a saturated host must never produce one.
//
// Rails order: the load gate is the LAST admission rail. The sim verb's
// usage refusals, the SPEC-13 sanction gate and the permanent-destroy
// policy refusal all keep their own reasons — a mis-typed shape refuses
// as a usage refusal and a permanent destroy refuses as a policy refusal
// even on a saturated host — and only a request that would otherwise land
// is bounded by the gate.

// ReadLoad is the AC-8 measurement seam (the doctor/battery/selftest
// package-level-seam pattern): production = rails.ReadLoad (the live
// /proc/loadavg + /proc/pressure/io read); tests point it at an injected
// LoadReading — a test never depends on, or perturbs, the box it runs on
// (ambient-filesystem discipline).
var ReadLoad = rails.ReadLoad

// CheckLoadGate measures the host and grades the reading against the
// load gate. A breach returns the rails refusal verbatim (reason
// load_gate, the measured numbers vs the gate — AC-8's "the reason names
// the measurement"); an unreadable measurement fails closed with the
// gate named (a gate that cannot say what it measured does not run).
// nil means the host is under every enabled ceiling and the landing may
// proceed.
func CheckLoadGate() error {
	reading, err := ReadLoad()
	if err != nil {
		return fmt.Errorf("load gate: %w", err)
	}
	gate, err := rails.NewLoadGate(rails.LoadGateConfig{
		LoadThreshold: doctor.LoadThreshold,
		PSIThreshold:  doctor.PSIThreshold,
	})
	if err != nil {
		// unreachable with the doctor's constants (both arms enabled);
		// a misconfigured gate refuses loudly, never silently ungated
		return err
	}
	return gate.Check(reading)
}

// ch:trace row=MISCHIEF-DF-65 spec=docs/SPEC-PLAN.md#SPEC-03 wave=mischief-foreman-2026-10-22-07-05-13.json#MISCHIEF-DF-65 test=internal/backend_sim/load_gate_test.go evidence=cmd/mischief/sim.go witness=none:injected-readings-no-live-run
