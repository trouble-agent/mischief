package profile

import (
	"encoding/json"
	"fmt"
	"strings"
)

// finding.go — the FINDING document: the product of a profile run (PRD §3).
//
// One JSON object is the whole contract with the agent on the other end:
// which profile and version ran, on which resolved target, which fault with
// which params, the independent landing witness, the probe's
// expected-vs-observed pair, the graded outcome, the cause class + steering
// hint when one applies, and the command that reproduces the run. Every field
// is either measured or deliberately empty — the document never carries a
// value the run did not observe.

// TargetInfo is the resolved target as the finding reports it.
type TargetInfo struct {
	// Kind is the resolved selector kind ("scratch", "host", "process", ...).
	Kind string `json:"kind"`
	// Selector is the selector exactly as the operator wrote it.
	Selector string `json:"selector"`
	// Path is the scratch path a `scratch:<path>` selector names (empty for
	// kinds that name no path).
	Path string `json:"path,omitempty"`
}

// FaultRef is the resolved fault a step injected: the primitive's identity as
// the catalog declares it, plus the params the step actually used.
type FaultRef struct {
	// ID is the primitive id ("S-004").
	ID string `json:"id"`
	// Name is the primitive's declared short name.
	Name string `json:"name"`
	// Backend is the primitive's declared injection backend ("shim").
	Backend string `json:"backend"`
	// Tier is the primitive's declared tier string verbatim ("L1"). It is
	// REPORTED, not enforced: see the package NOT-LIST.
	Tier string `json:"tier"`
	// Params is the effective param set (the step's params, defaults filled
	// by the actuator where the descriptor declares none).
	Params map[string]string `json:"params,omitempty"`
}

// Witness is a measured landing (or detach) proof: what was observed, where it
// came from, and the evidence line a reviewer can check.
//
// Kind distinguishes an INDEPENDENT observation (the parent process reading
// the target after the child exited) from INJECTOR-OWNED evidence (the shim's
// own counter). A witness whose only source is injector-owned evidence must
// say so — that is the AC-3 anti-gaming shape.
type Witness struct {
	// Kind is "external" or "injector".
	Kind string `json:"kind"`
	// Source names where the observation came from.
	Source string `json:"source"`
	// Evidence is the measured line (paths, counts, sizes).
	Evidence string `json:"evidence"`
}

// ProbeResult is one probe grading: what the probe expected, what it observed,
// and whether the observation matched.
type ProbeResult struct {
	// Name is the probe's vocabulary name.
	Name string `json:"name"`
	// Expected states the expected condition.
	Expected string `json:"expected"`
	// Observed states the measured condition.
	Observed string `json:"observed"`
	// Passed is true only when Observed satisfies Expected.
	Passed bool `json:"passed"`
}

// RequirementResult is one declared requirement's resolution.
type RequirementResult struct {
	// Requirement is the vocabulary token.
	Requirement string `json:"requirement"`
	// Declared is the profile's own prose for it.
	Declared string `json:"declared,omitempty"`
	// Met is true when the host satisfies it.
	Met bool `json:"met"`
	// Detail is the measurement ("cc -> /usr/bin/cc") or the reason it failed.
	Detail string `json:"detail"`
}

// StepFinding is one step's share of the finding.
type StepFinding struct {
	// ID is the step id.
	ID string `json:"id"`
	// Fault is the resolved fault, empty when the reference did not resolve
	// (the reason then says so).
	Fault FaultRef `json:"fault"`
	// InjectedAt is the RFC3339 time the step's fault was armed (absent when
	// the step never reached arming).
	InjectedAt string `json:"injected_at,omitempty"`
	// Landed is true only when a landing witness was measured.
	Landed bool `json:"landed"`
	// LandedWitness is the measured landing proof (nil when not landed).
	LandedWitness *Witness `json:"landed_witness,omitempty"`
	// Probe is the step's probe grading (nil when the step never graded).
	Probe *ProbeResult `json:"probe,omitempty"`
	// Detached is the measured inverse proof — the fault is gone (nil when
	// the inverse was not proven).
	Detached *Witness `json:"detached,omitempty"`
	// Outcome is the graded three-value outcome for this step.
	Outcome Outcome `json:"outcome"`
	// CauseClass is the profile's declared failure class (only on failure).
	CauseClass string `json:"cause_class,omitempty"`
	// Hint is the profiling steering hint for that class (only on failure).
	Hint string `json:"hint,omitempty"`
	// Reason names why the step did not grade pass/fail (inconclusive only).
	Reason string `json:"reason,omitempty"`
	// DurationMS is the wall time the step took.
	DurationMS int64 `json:"duration_ms"`
}

// Finding is the whole document written to stdout (and to
// <run_dir>/finding.json).
type Finding struct {
	// Schema is FindingSchemaV1.
	Schema string `json:"schema"`
	// Profile is the profile id ("storage/short-write").
	Profile string `json:"profile"`
	// ProfileClass is the profile's class segment.
	ProfileClass string `json:"profile_class"`
	// ProfileVersion is the profile's own version.
	ProfileVersion string `json:"profile_version"`
	// RunID is the content-derived run id.
	RunID string `json:"run_id"`
	// RunDir is where the journal and the injection log live.
	RunDir string `json:"run_dir"`
	// Seed is the seed the run used (declared or overridden).
	Seed int64 `json:"seed"`
	// Target is the resolved target.
	Target TargetInfo `json:"target"`
	// Requires is every declared requirement's resolution.
	Requires []RequirementResult `json:"requires,omitempty"`
	// Steps is one entry per profile step, in profile order.
	Steps []StepFinding `json:"steps"`
	// Outcome is the run's graded outcome (the worst of its steps).
	Outcome Outcome `json:"outcome"`
	// CauseClass is the run-level failure class (failed runs only).
	CauseClass string `json:"cause_class,omitempty"`
	// Hint is the run-level steering hint (failed runs only).
	Hint string `json:"hint,omitempty"`
	// Reason names why the run is inconclusive (inconclusive runs only).
	Reason string `json:"reason,omitempty"`
	// Notes are honest disclosures about this build's posture for the run
	// (unmet-by-bypass rails, executor-local scenario). Never empty when the
	// executor deviates from a declared rail.
	Notes []string `json:"notes,omitempty"`
	// StartedAt / FinishedAt are the run's RFC3339 bounds.
	StartedAt string `json:"started_at"`
	// FinishedAt is the RFC3339 close time.
	FinishedAt string `json:"finished_at"`
	// Reproduce is the exact command that re-runs this profile, seed and
	// target — the field an agent pastes into its next loop iteration.
	Reproduce string `json:"reproduce"`
}

// JSON renders the finding as indented JSON with a trailing newline — the only
// bytes the CLI writes to stdout in --json mode.
func (f *Finding) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("finding json: %w", err)
	}
	return append(b, '\n'), nil
}

// Lines renders the finding for a human (non --json mode). Nothing here is
// load-bearing: the JSON document is the contract.
func (f *Finding) Lines() []string {
	out := []string{
		fmt.Sprintf("profile: %s (%s, v%s)", f.Profile, f.Schema, f.ProfileVersion),
		fmt.Sprintf("target: %s:%s", f.Target.Kind, strings.TrimPrefix(f.Target.Selector, f.Target.Kind+":")),
		fmt.Sprintf("run: %s (seed %d)", f.RunID, f.Seed),
		fmt.Sprintf("run dir: %s", f.RunDir),
	}
	for _, r := range f.Requires {
		mark := "met"
		if !r.Met {
			mark = "UNMET"
		}
		out = append(out, fmt.Sprintf("  requires %s: %s — %s", r.Requirement, mark, r.Detail))
	}
	for _, s := range f.Steps {
		line := fmt.Sprintf("step %s fault=%s landed=%t outcome=%s", s.ID, s.Fault.ID, s.Landed, s.Outcome)
		if s.Probe != nil {
			line += fmt.Sprintf(" probe=%s expected=%q observed=%q", s.Probe.Name, s.Probe.Expected, s.Probe.Observed)
		}
		if s.Reason != "" {
			line += " reason=" + s.Reason
		}
		out = append(out, line)
	}
	for _, n := range f.CauseFor() {
		out = append(out, "  "+n)
	}
	for _, n := range f.Notes {
		out = append(out, "note: "+n)
	}
	out = append(out, fmt.Sprintf("finding: %s (exit %d)", f.Outcome, f.Outcome.ExitCode()))
	out = append(out, "reproduce: "+f.Reproduce)
	return out
}

// CauseFor renders the run-level cause class and hint as lines, when present.
func (f *Finding) CauseFor() []string {
	if f.CauseClass == "" {
		return nil
	}
	lines := []string{"cause: " + f.CauseClass}
	if f.Hint != "" {
		lines = append(lines, "hint: "+f.Hint)
	}
	return lines
}

// worstOutcome folds step outcomes into the run outcome: any failed ⇒ failed,
// else any inconclusive ⇒ inconclusive, else passed. An empty set is
// inconclusive — a run that graded nothing proves nothing.
func worstOutcome(steps []StepFinding) Outcome {
	if len(steps) == 0 {
		return OutcomeInconclusive
	}
	out := OutcomePassed
	for _, s := range steps {
		switch s.Outcome {
		case OutcomeFailed:
			return OutcomeFailed
		case OutcomeInconclusive:
			out = OutcomeInconclusive
		}
	}
	return out
}

// verdictFor maps an outcome onto the journal's closed verdict vocabulary:
// passed ⇒ recovered, failed ⇒ not_recovered (the fault landed and the target
// did NOT stay green), inconclusive ⇒ aborted (the run could not produce a
// signal, which is the selftest harness's verdict for a skip too).
//
// `no_op` is deliberately NOT used for a failed probe: in this vocabulary
// no_op means "the fault never landed", and a failed step landed by
// definition — grading it no_op would claim the opposite of what was measured.
func verdictFor(o Outcome) string {
	switch o {
	case OutcomePassed:
		return "recovered"
	case OutcomeFailed:
		return "not_recovered"
	default:
		return "aborted"
	}
}
