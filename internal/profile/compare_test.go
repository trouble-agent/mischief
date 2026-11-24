package profile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/journal"
)

// compare_test.go — the PROF-031 determinism + compare contracts:
//
//  1. Two runs of the same profile bytes at the same seed write BYTE-IDENTICAL
//     journals (the injection log a compare diffs), and a different seed
//     changes the journal (the pin is not vacuous).
//  2. `CompareRunDirs` pairs steps by id between two runs of the same profile
//     and reports failed->passed for the step whose controlled target was
//     FIXED between the runs, unchanged for the rest.
//  3. Transition vocabulary: unchanged, and the not-graded pseudo-outcome for
//     a step a killed run never graded.
//  4. Refusals: a missing journal, a profile mismatch.

// controlledActuator is a scripted actuator that grades against a CONTROL FILE
// the test owns: `<step>.state` under dir holds "fixed" or anything else. It
// is the stand-in for the target code under test — flipping the file between
// two runs is "the fix lands".
type controlledActuator struct {
	dir string
}

func (c *controlledActuator) Apply(_ context.Context, req LandRequest) LandResult {
	state, err := os.ReadFile(filepath.Join(c.dir, req.Step.ID+".state"))
	fixed := err == nil && strings.TrimSpace(string(state)) == "fixed"
	onDisk := int64(16) // a short on-disk payload: the probe breaks
	if fixed {
		onDisk = 32 // the whole payload landed: the probe holds
	}
	return LandResult{
		Landed: true,
		Obs: Observation{TargetPath: req.Target.Path, PayloadLen: 32,
			OnDisk: onDisk, Attempts: 2, ShortWrites: 1},
		Witness: &Witness{Kind: "external", Source: "fixture", Evidence: "landed"},
	}
}

func (c *controlledActuator) Detach(context.Context, LandRequest) DetachResult {
	return DetachResult{Witness: okDetach()}
}

// twoStepBody is a two-step profile so the compare has something unchanged
// beside the flipping step.
func twoStepBody() string {
	return profileBody("steps:\n" +
		"  - id: s1\n    fault: S-004\n    probe: payload-complete\n    cause_class: test:broken\n" +
		"  - id: s2\n    fault: S-004\n    probe: payload-complete\n")
}

// runTwoStepsInto runs the two-step fixture into an isolated run dir (same
// bytes, same seed every call).
func runTwoStepsInto(t *testing.T, states, runDir string, opts Options) {
	t.Helper()
	act := &controlledActuator{dir: states}
	opts.Actuators = map[string]FaultActuator{"S-004": act}
	opts.RunDir = runDir
	runFixture(t, twoStepBody(), opts)
}

func setState(t *testing.T, states, step, val string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(states, step+".state"), []byte(val+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSameSeedRunsWriteIdenticalJournals(t *testing.T) {
	states := t.TempDir()
	setState(t, states, "s1", "fixed")
	setState(t, states, "s2", "fixed")

	dirA, dirB := t.TempDir(), t.TempDir()
	runTwoStepsInto(t, states, dirA, Options{})
	runTwoStepsInto(t, states, dirB, Options{})

	ja, err := os.ReadFile(filepath.Join(dirA, "profile.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	jb, err := os.ReadFile(filepath.Join(dirB, "profile.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(ja) != string(jb) {
		t.Fatalf("same profile + same seed must write identical journals (a=%d bytes, b=%d bytes, differ)\nA: %s\nB: %s",
			len(ja), len(jb), ja, jb)
	}

	// The timestamp contract underneath that byte-identity: monotone, and the
	// run_planned record discloses the seed-derived logical clock.
	recs := readJournal(t, dirA)
	prev := int64(-1)
	for _, r := range recs {
		if r.TS <= prev {
			t.Fatalf("ts is not monotone: %d after %d", r.TS, prev)
		}
		prev = r.TS
	}
	for _, r := range recs {
		if r.Type == journal.RecordRunPlanned && r.Fields["journal_clock"] == "" {
			t.Fatal("run_planned must disclose the journal clock (seed-derived, not wall clock)")
		}
	}

	// The pin is not vacuous: a different seed changes the journal (the run id
	// and the clock base both derive from it).
	dirC := t.TempDir()
	runTwoStepsInto(t, states, dirC, Options{Seed: 9999})
	jc, err := os.ReadFile(filepath.Join(dirC, "profile.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(ja) == string(jc) {
		t.Fatal("a different seed must not reproduce the same journal")
	}
}

func TestProfileCompareFailedToPassedForTheFixedStep(t *testing.T) {
	states := t.TempDir()
	setState(t, states, "s1", "broken")
	setState(t, states, "s2", "fixed")

	dirA, dirB := t.TempDir(), t.TempDir()
	runTwoStepsInto(t, states, dirA, Options{})

	// "The fix lands": s1's controlled target is repaired between the runs.
	setState(t, states, "s1", "fixed")
	runTwoStepsInto(t, states, dirB, Options{})

	rep, err := CompareRunDirs(dirA, dirB)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Profile != "test/fixture" {
		t.Fatalf("profile = %q (want test/fixture)", rep.Profile)
	}
	found := map[string]StepTransition{}
	for _, tr := range rep.Steps {
		found[tr.Step] = tr
	}
	if got := found["s1"].Transition; got != TransitionFailedToPassed {
		t.Fatalf("s1 transition = %q (want %s); report=%+v", got, TransitionFailedToPassed, rep.Steps)
	}
	if !found["s1"].Changed || found["s1"].From != "failed" || found["s1"].To != "passed" {
		t.Fatalf("s1 = %+v", found["s1"])
	}
	if got := found["s2"].Transition; got != transitionUnchanged {
		t.Fatalf("s2 transition = %q (want %s)", got, transitionUnchanged)
	}
	if found["s2"].Changed {
		t.Fatalf("s2 must not count as changed: %+v", found["s2"])
	}
	if rep.Changed != 1 || rep.Unchanged != 1 {
		t.Fatalf("counts = %d changed / %d unchanged (want 1/1)", rep.Changed, rep.Unchanged)
	}
	lines := strings.Join(rep.Lines(), "\n")
	if !strings.Contains(lines, TransitionFailedToPassed) || !strings.Contains(lines, "unchanged (passed)") {
		t.Fatalf("report lines do not name the transitions:\n%s", lines)
	}

	// The report is directional: comparing the after-run against the
	// before-run inverts every flip.
	back, err := CompareRunDirs(dirB, dirA)
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range back.Steps {
		if tr.Step == "s1" && tr.Transition != TransitionPassedToFailed {
			t.Fatalf("reversed s1 transition = %q (want %s)", tr.Transition, TransitionPassedToFailed)
		}
	}
}

func TestTransitionVocabulary(t *testing.T) {
	for _, c := range []struct {
		name string
		from string
		to   string
		want string
	}{
		{"fix landed", "failed", "passed", "failed->passed"},
		{"regression", "passed", "failed", "passed->failed"},
		{"same outcome", "passed", "passed", "unchanged"},
		{"same inconclusive", "inconclusive", "inconclusive", "unchanged"},
		{"degraded to inconclusive", "passed", "inconclusive", "passed->inconclusive"},
		{"recovered signal", "inconclusive", "passed", "inconclusive->passed"},
		{"killed then graded", "", "passed", "not-graded->passed"},
		{"graded then killed", "failed", "", "failed->not-graded"},
		{"no signal either way", "", "", "unchanged"},
	} {
		if got := transitionOf(c.from, c.to); got != c.want {
			t.Fatalf("%s: %q (want %q)", c.name, got, c.want)
		}
	}
}

func TestCompareRefusals(t *testing.T) {
	if _, err := CompareRunDirs(filepath.Join(t.TempDir(), "absent"), filepath.Join(t.TempDir(), "absent2")); err == nil {
		t.Fatal("a missing run dir must refuse")
	}
	// Empty journal file.
	empty := t.TempDir()
	if err := os.WriteFile(filepath.Join(empty, "profile.jsonl"), []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if _, err := CompareRunDirs(empty, other); err == nil || !strings.Contains(err.Error(), "holds no records") {
		t.Fatalf("empty journal must refuse by name: %v", err)
	}

	// Profile mismatch: two hand-built journals for different profiles.
	dA, dB := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(dA, "profile.jsonl"), []byte(`{"type":"run_planned","run_id":"abcd0000abcd0000","fields":{"kind":"profile","profile":"a/one","seed":"3","steps":"s1=S-004"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dB, "profile.jsonl"), []byte(`{"type":"run_planned","run_id":"abcd0000abcd0000","fields":{"kind":"profile","profile":"b/two","seed":"3","steps":"s1=S-004"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CompareRunDirs(dA, dB); err == nil || !strings.Contains(err.Error(), "profile mismatch") {
		t.Fatalf("profile mismatch must refuse: %v", err)
	}

	// A step declared but never graded pairs with the not-graded pseudo-outcome.
	if err := os.WriteFile(filepath.Join(dB, "profile.jsonl"), []byte(`{"type":"run_planned","run_id":"abcd0000abcd0000","fields":{"kind":"profile","profile":"a/one","seed":"3","steps":"s1=S-004,s2=S-004"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dA, "profile.jsonl"), []byte(`{"type":"run_planned","run_id":"abcd0000abcd0000","fields":{"kind":"profile","profile":"a/one","seed":"3","steps":"s1=S-004"}}
{"type":"finding","run_id":"abcd0000abcd0000","fields":{"step":"s1","outcome":"passed"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := CompareRunDirs(dA, dB)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Steps) != 2 || rep.Steps[0].Transition != "passed->"+transitionNotGraded || !rep.Steps[0].Changed {
		t.Fatalf("s1 = %+v (want %s, changed)", rep.Steps[0], "passed->"+transitionNotGraded)
	}
	if rep.Steps[1].Transition != transitionUnchanged || rep.Steps[1].Changed {
		t.Fatalf("s2 = %+v (want unchanged, not changed: neither run graded it)", rep.Steps[1])
	}
}

func TestReadRunStateKilledRunKeepsGradedSteps(t *testing.T) {
	// A journal truncated mid-step: the finding record for s1 exists, s2 only
	// has its probe sample. ReadRunState must surface both.
	dir := t.TempDir()
	body := `{"type":"run_planned","run_id":"abcd0000abcd0000","fields":{"kind":"profile","profile":"a/one","seed":"5","steps":"s1=S-004,s2=S-004"}}
{"type":"run_planned","run_id":"abcd0000abcd0000","fields":{"kind":"other"}}
{"type":"fault_armed","run_id":"abcd0000abcd0000","fields":{"step":"s1","primitive":"S-004"}}
{"type":"finding","run_id":"abcd0000abcd0000","fields":{"step":"s1","outcome":"failed","cause_class":"test:broken"}}
{"type":"fault_armed","run_id":"abcd0000abcd0000","fields":{"step":"s2","primitive":"S-004"}}
{"type":"probe_sample","run_id":"abcd0000abcd0000","fields":{"step":"s2","probe":"payload-complete","observed":"short"}}`
	if err := os.WriteFile(filepath.Join(dir, "profile.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := ReadRunState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Profile != "a/one" || st.Seed == nil || *st.Seed != 5 {
		t.Fatalf("state header = %q seed=%+v", st.Profile, st.Seed)
	}
	if len(st.Steps) != 2 {
		t.Fatalf("steps = %+v (want 2)", st.Steps)
	}
	if st.Steps[0].Outcome != "failed" || st.Steps[1].Probe != "payload-complete" {
		t.Fatalf("steps = %+v", st.Steps)
	}
	// The second run_planned (kind=other) must not append two phantom slots
	// from a malformed steps field parse — it carried no steps field. Assert
	// no step named "" or a bare fault leaked in.
	for _, s := range st.Steps {
		if s.Step == "" || s.Fault != "S-004" {
			t.Fatalf("phantom step slot leaked: %+v", s)
		}
	}
}
