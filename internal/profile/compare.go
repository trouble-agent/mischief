package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/trouble-agent/mischief/internal/journal"
)

// compare.go — the `profile compare` engine (PROF-031): two run dirs in, one
// per-probe transition report out.
//
// `mischief profile run` writes a run dir whose journal (profile.jsonl) is the
// injection log; the same profile bytes + seed produce a RUN-ID-derived,
// byte-identical journal (the clock is seed-derived, see executor.go), so a
// fix loop can hold two runs apart with two --dir roots and then ask what
// CHANGED. This file reads the two journals, pairs steps by id between two
// runs of the SAME profile, and emits the outcome transition per step in the
// finding's three-value vocabulary:
//
//	failed->passed    the fix landed: the probe that broke in the before-run
//	                  now holds (the flip that proves a fix)
//	passed->failed    the before-run held and the after-run broke: a
//	                  regression, or the fix removed protection
//	unchanged         the step graded the same outcome in both runs
//	passed->inconclusive, inconclusive->passed, ... — any other pair of the
//	                  three-value outcomes, named the same way
//	not-graded->…     a step whose journal never graded it (a killed run) is
//	                  named with the not-graded pseudo-outcome
//
// The journal is the read surface, not finding.json: the journal exists (and
// is exactly reproducible) even for a run that was killed mid-flight, and it
// is the artifact whose byte-identity the determinism contract pins.

// CompareSchemaV1 is the compare report document schema.
const CompareSchemaV1 = "mischief.compare/v1"

// transitionUnchanged is the token for a step that graded the same outcome in
// both runs.
const transitionUnchanged = "unchanged"

// notGradedish is the pseudo-outcome for a step the journal never graded.
const transitionNotGraded = "not-graded"

// TransitionFailedToPassed / TransitionPassedToFailed are the two flip tokens
// the fix loop looks for.
const (
	TransitionFailedToPassed = "failed->passed"
	TransitionPassedToFailed = "passed->failed"
)

// StepState is one step's grading state as one journal recorded it. Outcome
// is empty when the journal never graded the step (a killed run); Fault is
// empty when the journal never armed it.
type StepState struct {
	Step    string `json:"step"`
	Fault   string `json:"fault,omitempty"`
	Probe   string `json:"probe,omitempty"`
	Outcome string `json:"outcome,omitempty"`
}

// RunState is one run dir's per-step grading state, read from its journal.
// Seed is nil for older run dirs whose run_planned carried no seed field.
type RunState struct {
	RunID   string      `json:"run_id"`
	Profile string      `json:"profile,omitempty"`
	Seed    *int64      `json:"seed,omitempty"`
	Steps   []StepState `json:"steps"`
}

// ReadRunState reads one run dir's journal (profile.jsonl) into the run's
// per-step state. Step ids the run declared but never journaled (a killed
// run) are still present, with an empty Outcome — the pairing must not lose a
// step just because the run died before it graded.
func ReadRunState(runDir string) (*RunState, error) {
	path := filepath.Join(runDir, "profile.jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("journal: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) == 1 && strings.TrimSpace(lines[0]) == "" {
		return nil, fmt.Errorf("%s holds no records", path)
	}
	st := &RunState{}
	steps := map[string]*StepState{}
	ensure := func(id string) *StepState {
		return ensureStep(steps, &st.Steps, id)
	}
	for i, line := range lines {
		var r journal.Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, i+1, err)
		}
		if st.RunID == "" {
			st.RunID = r.RunID
		}
		switch r.Type {
		case journal.RecordRunPlanned:
			if r.Fields == nil {
				continue
			}
			if p := r.Fields["profile"]; p != "" && st.Profile == "" {
				st.Profile = p
			}
			if s := r.Fields["seed"]; s != "" && st.Seed == nil {
				if v, err := strconv.ParseInt(s, 10, 64); err == nil {
					st.Seed = &v
				}
			}
			// Pre-seed the declared step set: a run_planned "steps" field is
			// "id=fault" pairs, so a step never journaled still pairs.
			for _, pair := range strings.Split(r.Fields["steps"], ",") {
				id, fault, _ := strings.Cut(pair, "=")
				if strings.TrimSpace(id) == "" {
					continue
				}
				s := ensure(id)
				if s.Fault == "" && fault != "" {
					s.Fault = fault
				}
			}
		case journal.RecordFaultArmed:
			if r.Fields == nil {
				continue
			}
			if s := ensure(r.Fields["step"]); s != nil {
				if p := r.Fields["primitive"]; p != "" && s.Fault == "" {
					s.Fault = p
				}
			}
		case journal.RecordProbeSample, journal.RecordAssertionResult:
			if r.Fields == nil {
				continue
			}
			if s := ensure(r.Fields["step"]); s != nil {
				if p := r.Fields["probe"]; p != "" && s.Probe == "" {
					s.Probe = p
				}
			}
		case journal.RecordFinding:
			if r.Fields == nil {
				continue
			}
			if s := ensure(r.Fields["step"]); s != nil && s.Outcome == "" {
				s.Outcome = strings.TrimSpace(r.Fields["outcome"])
			}
		}
	}
	// Sync the map state back into the slice (ensure mutates the map copy).
	for i := range st.Steps {
		if s, ok := steps[st.Steps[i].Step]; ok {
			st.Steps[i] = *s
		}
	}
	return st, nil
}

// ensureStep appends (or returns) a step slot keyed by id, preserving
// encounter order. An empty id returns nil: a record without a step field
// carries nothing pairable, and dropping it silently is better than parking a
// phantom step slot.
func ensureStep(steps map[string]*StepState, order *[]StepState, id string) *StepState {
	if id == "" {
		return nil
	}
	if s, ok := steps[id]; ok {
		return s
	}
	s := &StepState{Step: id}
	steps[id] = s
	*order = append(*order, *s)
	return s
}

// StepTransition is one step's before/after pair.
type StepTransition struct {
	Step       string `json:"step"`
	Fault      string `json:"fault,omitempty"`
	Probe      string `json:"probe,omitempty"`
	From       string `json:"from,omitempty"`
	To         string `json:"to,omitempty"`
	Transition string `json:"transition"`
	Changed    bool   `json:"changed"`
}

// CompareReport is the compare product: every step paired by id between two
// runs of the same profile, plus the flip counts.
type CompareReport struct {
	Schema    string           `json:"schema"`
	Profile   string           `json:"profile"`
	RunIDA    string           `json:"run_id_a"`
	RunIDB    string           `json:"run_id_b"`
	Steps     []StepTransition `json:"steps"`
	Changed   int              `json:"changed"`
	Unchanged int              `json:"unchanged"`
}

// transitionOf names one step's outcome transition in the finding's
// three-value vocabulary plus the not-graded pseudo-outcome.
func transitionOf(from, to string) string {
	a := orNotGraded(from)
	b := orNotGraded(to)
	if a == b {
		return transitionUnchanged
	}
	return a + "->" + b
}

func orNotGraded(o string) string {
	if o == "" {
		return transitionNotGraded
	}
	return o
}

// CompareRunDirs reads two run dirs and reports the per-step outcome
// transitions between them. dirA is the BEFORE run, dirB the AFTER run. The
// two journals must belong to the same profile id — pairing steps across two
// different profiles would make "failed->passed" mean nothing.
func CompareRunDirs(dirA, dirB string) (*CompareReport, error) {
	a, err := ReadRunState(dirA)
	if err != nil {
		return nil, fmt.Errorf("run a (%s): %w", dirA, err)
	}
	b, err := ReadRunState(dirB)
	if err != nil {
		return nil, fmt.Errorf("run b (%s): %w", dirB, err)
	}
	if a.Profile != "" && b.Profile != "" && a.Profile != b.Profile {
		return nil, fmt.Errorf("profile mismatch: run a is %q, run b is %q — compare pairs two runs of the SAME profile", a.Profile, b.Profile)
	}
	rep := &CompareReport{
		Schema:  CompareSchemaV1,
		Profile: firstNonEmpty(a.Profile, b.Profile),
		RunIDA:  a.RunID,
		RunIDB:  b.RunID,
	}
	byIDA := indexSteps(a)
	byIDB := indexSteps(b)
	// Pair in A's order first, then B-only ids in B's order, so the report
	// order is deterministic and read-independent.
	seen := map[string]bool{}
	for _, s := range a.Steps {
		if seen[s.Step] {
			continue
		}
		seen[s.Step] = true
		rep.append(fromPair(byIDA, byIDB, s.Step))
	}
	for _, s := range b.Steps {
		if seen[s.Step] {
			continue
		}
		seen[s.Step] = true
		rep.append(fromPair(byIDA, byIDB, s.Step))
	}
	for _, t := range rep.Steps {
		if t.Changed {
			rep.Changed++
		} else {
			rep.Unchanged++
		}
	}
	return rep, nil
}

func fromPair(byIDA, byIDB map[string]*StepState, id string) StepTransition {
	from, to := "", ""
	fault, probe := "", ""
	if s := byIDA[id]; s != nil {
		from, fault, probe = s.Outcome, s.Fault, s.Probe
	}
	if s := byIDB[id]; s != nil {
		to = s.Outcome
		if fault == "" {
			fault = s.Fault
		}
		if probe == "" {
			probe = s.Probe
		}
	}
	tr := StepTransition{Step: id, From: from, To: to, Probe: probe, Fault: fault,
		Transition: transitionOf(from, to)}
	tr.Changed = tr.Transition != transitionUnchanged
	return tr
}

func (r *CompareReport) append(t StepTransition) { r.Steps = append(r.Steps, t) }

func indexSteps(st *RunState) map[string]*StepState {
	m := make(map[string]*StepState, len(st.Steps))
	for i := range st.Steps {
		m[st.Steps[i].Step] = &st.Steps[i]
	}
	return m
}

// Lines renders the report for a human (non --json mode): one line per step,
// changed steps first (they are what a fix loop reads), then unchanged ones.
func (r *CompareReport) Lines() []string {
	var changed, unchanged []string
	for _, t := range r.Steps {
		if t.Transition == transitionUnchanged {
			detail := orNotGraded(t.From)
			if t.From == "" && t.To == "" {
				detail = "neither run graded this step"
			}
			unchanged = append(unchanged, fmt.Sprintf("  step %s probe=%s unchanged (%s)",
				t.Step, orNone(t.Probe), detail))
			continue
		}
		changed = append(changed, fmt.Sprintf("  step %s probe=%s %s (was: %s, now: %s)",
			t.Step, orNone(t.Probe), t.Transition, orNotGraded(t.From), orNotGraded(t.To)))
	}
	out := []string{
		fmt.Sprintf("compare: %s (%s)", firstNonEmpty(r.Profile, "(unknown profile)"), r.Schema),
		fmt.Sprintf("run a: %s", r.RunIDA),
		fmt.Sprintf("run b: %s", r.RunIDB),
	}
	out = append(out, changed...)
	out = append(out, unchanged...)
	out = append(out, fmt.Sprintf("transitions: %d changed, %d unchanged of %d step(s)",
		r.Changed, r.Unchanged, len(r.Steps)))
	return out
}

// JSON renders the report as indented JSON with a trailing newline — the
// only bytes the CLI writes to stdout in --json mode.
func (r *CompareReport) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("compare json: %w", err)
	}
	return append(b, '\n'), nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func orNone(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
