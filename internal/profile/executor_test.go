package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trouble-agent/mischief/internal/catalog"
	"github.com/trouble-agent/mischief/internal/journal"
)

// executor_test.go — the three-value grading contract. The actuator and probe
// seams exist so every arm is reachable in a unit test: the shipped S-004
// scenario can only ever pass (its reference writer retries), so a suite that
// drove only the real actuator could not prove the failed arm exists at all.

// fakeActuator is a scripted actuator: it reports exactly the landing the test
// asks for and never touches the filesystem.
type fakeActuator struct {
	landed       bool
	obs          Observation
	landReason   string
	detach       *Witness
	detachReason string
	applied      int
	detached     int
}

func (f *fakeActuator) Apply(_ context.Context, _ LandRequest) LandResult {
	f.applied++
	return LandResult{Landed: f.landed, Obs: f.obs, Reason: f.landReason,
		Witness: func() *Witness {
			if !f.landed {
				return nil
			}
			return &Witness{Kind: "external", Source: "fake", Evidence: "landed"}
		}()}
}

func (f *fakeActuator) Detach(_ context.Context, _ LandRequest) DetachResult {
	f.detached++
	return DetachResult{Witness: f.detach, Reason: f.detachReason}
}

func okDetach() *Witness {
	return &Witness{Kind: "external", Source: "fake", Evidence: "detached"}
}

// fixture builds a valid profile with one step over the named primitive.
func fixture(t *testing.T, body string) (*Profile, []byte) {
	t.Helper()
	p, err := Parse([]byte(body), "fixture.yaml")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return p, []byte(body)
}

func profileBody(stepBlock string) string {
	return fmt.Sprintf(`schema: mischief.profile/v1
name: fixture
class: test
version: 1.0.0
seed: 3
requires:
  - cc: a C compiler
%s
cause_classes:
  test:broken:
    hint: fix the write path.
`, stepBlock)
}

// catalogFromCorpus loads the repo's real primitives, so the tests resolve
// against the shipped descriptors rather than a hand-made stub.
func catalogFromCorpus(t *testing.T) *catalog.Catalog {
	t.Helper()
	cat, err := catalog.LoadDir(filepath.Join("..", "..", "catalog", "faults"))
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	if cat.Len() == 0 {
		t.Fatal("catalog is empty")
	}
	return cat
}

func runFixture(t *testing.T, body string, opts Options) *Finding {
	t.Helper()
	p, raw := fixture(t, body)
	if opts.Catalog == nil {
		opts.Catalog = catalogFromCorpus(t)
	}
	opts.Profile = p
	opts.Raw = raw
	if opts.RunDir == "" {
		opts.RunDir = t.TempDir()
	}
	if opts.Target.Kind == "" {
		opts.Target = Target{Kind: "scratch", Selector: "scratch:/tmp/x", Path: "/tmp/x"}
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Unix(1700000000, 0) }
	}
	f, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return f
}

// ── passed ──────────────────────────────────────────────────────────────────

func TestRunPassedArm(t *testing.T) {
	act := &fakeActuator{landed: true, detach: okDetach(),
		obs: Observation{TargetPath: "/tmp/x", PayloadLen: 32, OnDisk: 32, Attempts: 2, ShortWrites: 1}}
	runDir := t.TempDir()
	f := runFixture(t, profileBody("steps:\n  - id: s1\n    fault: S-004\n    probe: payload-complete\n"),
		Options{Actuators: map[string]FaultActuator{"S-004": act}, RunDir: runDir,
			Reproduce: "mischief profile run test/fixture --target scratch:/tmp/x --seed 3 --json"})

	if f.Outcome != OutcomePassed {
		t.Fatalf("outcome = %s (want passed); steps=%+v", f.Outcome, f.Steps)
	}
	if f.Outcome.ExitCode() != 0 {
		t.Fatalf("exit = %d", f.Outcome.ExitCode())
	}
	if act.applied != 1 || act.detached != 1 {
		t.Fatalf("actuator applied=%d detached=%d (want 1/1)", act.applied, act.detached)
	}
	// The fault reference must be the RESOLVED descriptor, not the profile's
	// bare token: an agent reading the finding needs the name/backend/tier.
	if f.Steps[0].Fault.Name == "" || f.Steps[0].Fault.Backend != "shim" || f.Steps[0].Fault.Tier != "L1" {
		t.Fatalf("fault ref = %+v", f.Steps[0].Fault)
	}
	if f.Steps[0].Probe == nil || !f.Steps[0].Probe.Passed {
		t.Fatalf("probe = %+v", f.Steps[0].Probe)
	}
	if f.Steps[0].LandedWitness == nil || f.Steps[0].Detached == nil {
		t.Fatalf("witnesses missing: %+v", f.Steps[0])
	}
	if f.RunID == "" || f.RunDir != runDir {
		t.Fatalf("run identity = %q in %q", f.RunID, f.RunDir)
	}
	if f.Reproduce == "" {
		t.Fatal("reproduce is empty — the finding must carry the command that re-runs it")
	}

	// finding.json is byte-identical to what the CLI prints.
	want, err := f.JSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(runDir, "finding.json"))
	if err != nil {
		t.Fatalf("finding.json: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("finding.json differs from the rendered finding")
	}

	recs := readJournal(t, runDir)
	assertTypes(t, recs, journal.RecordRunPlanned, journal.RecordFaultArmed, journal.RecordFaultLanded,
		journal.RecordProbeSample, journal.RecordAssertionResult, journal.RecordFaultReverted,
		journal.RecordFinding, journal.RecordVerdict, journal.RecordRunClosed)
	assertVerdict(t, recs, journal.VerdictRecovered)
	if got, want := recs[len(recs)-1].Type, journal.RecordRunClosed; got != want {
		t.Fatalf("last record = %s (want %s)", got, want)
	}
}

// ── failed ──────────────────────────────────────────────────────────────────

func TestRunFailedArm(t *testing.T) {
	// Landed, but the probe's observation is short: the fault reached the write
	// path and the target did not land the payload.
	act := &fakeActuator{landed: true, detach: okDetach(),
		obs: Observation{TargetPath: "/tmp/x", PayloadLen: 32, OnDisk: 16, Attempts: 1, ShortWrites: 1}}
	runDir := t.TempDir()
	body := profileBody("steps:\n  - id: s1\n    fault: S-004\n    probe: payload-complete\n    cause_class: test:broken\n")
	f := runFixture(t, body, Options{Actuators: map[string]FaultActuator{"S-004": act}, RunDir: runDir})

	if f.Outcome != OutcomeFailed {
		t.Fatalf("outcome = %s (want failed); steps=%+v", f.Outcome, f.Steps)
	}
	if f.Outcome.ExitCode() != 1 {
		t.Fatalf("exit = %d (want 1)", f.Outcome.ExitCode())
	}
	if f.Steps[0].Landed != true {
		t.Fatal("a failed step still landed — the finding must say so")
	}
	if f.CauseClass != "test:broken" || f.Hint != "fix the write path." {
		t.Fatalf("run cause = %q / %q", f.CauseClass, f.Hint)
	}
	assertVerdict(t, readJournal(t, runDir), journal.VerdictNotRecovered)
}

// ── inconclusive ────────────────────────────────────────────────────────────

// TestRunInconclusiveArms is the acceptance-critical table: EVERY way of not
// producing a signal must grade inconclusive WITH the reason named, and none of
// them may grade passed.
func TestRunInconclusiveArms(t *testing.T) {
	processTarget := Target{Kind: "process", Selector: "process:1234"}
	cases := []struct {
		name     string
		body     string
		opts     Options
		extraReq string // extra requires line to inject ("gpu: a GPU")
		wantText string
	}{
		{
			name:     "primitive not in the catalog",
			body:     profileBody("steps:\n  - id: s1\n    fault: Z-999\n    probe: payload-complete\n"),
			wantText: `primitive "Z-999" is not in the loaded catalog`,
		},
		{
			name:     "no actuator for an existing primitive",
			body:     profileBody("steps:\n  - id: s1\n    fault: S-011\n    probe: payload-complete\n"),
			wantText: `no actuator for primitive "S-011"`,
		},
		{
			name:     "unknown requirement token",
			body:     profileBody("steps:\n  - id: s1\n    fault: S-004\n    probe: payload-complete\n"),
			extraReq: "gpu: a GPU",
			wantText: `unknown requirement token "gpu"`,
		},
		{
			name:     "requirement unmet for this target",
			body:     profileBody("steps:\n  - id: s1\n    fault: S-004\n    probe: payload-complete\n"),
			opts:     Options{Target: processTarget},
			extraReq: "scratch-target: under the scratch base",
			wantText: "requirements not met",
		},
		{
			name:     "actuator could not land",
			body:     profileBody("steps:\n  - id: s1\n    fault: S-004\n    probe: payload-complete\n"),
			opts:     Options{Actuators: map[string]FaultActuator{"S-004": &fakeActuator{landed: false, landReason: "rule did not match"}}},
			wantText: "rule did not match",
		},
		{
			name:     "inverse not proven",
			body:     profileBody("steps:\n  - id: s1\n    fault: S-004\n    probe: payload-complete\n"),
			opts:     Options{Actuators: map[string]FaultActuator{"S-004": &fakeActuator{landed: true, detachReason: "next write still short", obs: Observation{PayloadLen: 32, OnDisk: 32}}}},
			wantText: "inverse not proven",
		},
		{
			name:     "undeclared param",
			body:     profileBody("steps:\n  - id: s1\n    fault: S-004\n    params:\n      turbo: \"1\"\n    probe: payload-complete\n"),
			opts:     Options{Actuators: map[string]FaultActuator{"S-004": &fakeActuator{landed: true, detach: okDetach()}}},
			wantText: `param "turbo" is not declared by S-004`,
		},
		{
			name:     "enum param outside the declared values",
			body:     profileBody("steps:\n  - id: s1\n    fault: F-009\n    params:\n      mode: nonsense\n    probe: payload-complete\n"),
			opts:     Options{Actuators: map[string]FaultActuator{"F-009": &fakeActuator{landed: true, detach: okDetach()}}},
			wantText: `param "mode" value "nonsense"`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := c.body
			if c.extraReq != "" {
				body = strings.Replace(body, "  - cc: a C compiler", "  - cc: a C compiler\n  - "+c.extraReq, 1)
			}
			opts := c.opts
			opts.RunDir = t.TempDir()
			f := runFixture(t, body, opts)
			if f.Outcome != OutcomeInconclusive {
				t.Fatalf("outcome = %s (want inconclusive); steps=%+v", f.Outcome, f.Steps)
			}
			if f.Outcome.ExitCode() != 3 {
				t.Fatalf("exit = %d (want 3)", f.Outcome.ExitCode())
			}
			if !strings.Contains(f.Reason, c.wantText) {
				t.Fatalf("reason %q does not name %q", f.Reason, c.wantText)
			}
			assertVerdict(t, readJournal(t, opts.RunDir), journal.VerdictAborted)
		})
	}
}

// TestRunNoRequirementsArrayIsHonest pins that a profile with no requires is
// still graded (no vacuous pass from an empty requirement list) and that the
// scratch target is the only attachable kind.
func TestRunEmptyRequiresStillGrades(t *testing.T) {
	body := `schema: mischief.profile/v1
name: fixture
class: test
version: 1.0.0
steps:
  - id: s1
    fault: S-004
    probe: payload-complete
`
	act := &fakeActuator{landed: true, detach: okDetach(),
		obs: Observation{PayloadLen: 8, OnDisk: 8, Attempts: 2, ShortWrites: 1}}
	f := runFixture(t, body, Options{Actuators: map[string]FaultActuator{"S-004": act}})
	if f.Outcome != OutcomePassed {
		t.Fatalf("outcome = %s", f.Outcome)
	}
	if len(f.Requires) != 0 {
		t.Fatalf("requires = %+v", f.Requires)
	}
}

// TestRunNoStepsNeverPasses pins the empty-set rule directly: a profile that
// graded nothing is inconclusive, never passed (the loader refuses a stepless
// profile, so this covers the fold itself).
func TestFoldWorstOutcome(t *testing.T) {
	cases := []struct {
		name  string
		steps []StepFinding
		want  Outcome
	}{
		{"empty", nil, OutcomeInconclusive},
		{"all passed", []StepFinding{{Outcome: OutcomePassed}, {Outcome: OutcomePassed}}, OutcomePassed},
		{"one failed beats passed", []StepFinding{{Outcome: OutcomePassed}, {Outcome: OutcomeFailed}}, OutcomeFailed},
		{"failed beats inconclusive", []StepFinding{{Outcome: OutcomeInconclusive}, {Outcome: OutcomeFailed}}, OutcomeFailed},
		{"inconclusive beats passed", []StepFinding{{Outcome: OutcomePassed}, {Outcome: OutcomeInconclusive}}, OutcomeInconclusive},
	}
	for _, c := range cases {
		if got := worstOutcome(c.steps); got != c.want {
			t.Fatalf("%s: %s (want %s)", c.name, got, c.want)
		}
	}
}

func TestOutcomeVocabularyAndExitCodes(t *testing.T) {
	for _, c := range []struct {
		o    Outcome
		exit int
	}{{OutcomePassed, 0}, {OutcomeFailed, 1}, {OutcomeInconclusive, 3}, {Outcome("weird"), 3}} {
		if got := c.o.ExitCode(); got != c.exit {
			t.Fatalf("%q exit = %d (want %d)", c.o, got, c.exit)
		}
	}
	if Outcome("weird").Valid() {
		t.Fatal("an undeclared outcome must not validate")
	}
	for _, o := range []Outcome{OutcomePassed, OutcomeFailed, OutcomeInconclusive} {
		if !o.Valid() {
			t.Fatalf("%q must validate", o)
		}
	}
}

func TestProbePayloadCompleteNeverPassesOnAnEmptyExpectation(t *testing.T) {
	pr := probePayloadComplete(Observation{PayloadLen: 0, OnDisk: 0})
	if pr.Passed {
		t.Fatal("a zero-length payload must not grade as met")
	}
	pr = probePayloadComplete(Observation{PayloadLen: 4, OnDisk: 3})
	if pr.Passed {
		t.Fatal("a short file must not grade as met")
	}
	pr = probePayloadComplete(Observation{PayloadLen: 4, OnDisk: 4})
	if !pr.Passed {
		t.Fatal("a complete payload must grade as met")
	}
}

// TestRequirementChecks covers the closed vocabulary against the host.
func TestRequirementChecks(t *testing.T) {
	cases := []struct {
		name   string
		token  string
		target Target
		want   bool
	}{
		{"cc met", ReqCC, Target{Kind: "scratch", Path: "/tmp/x"}, true},
		{"scratch met", ReqScratchTarget, Target{Kind: "scratch", Path: "/tmp/x"}, true},
		{"scratch outside the base", ReqScratchTarget, Target{Kind: "scratch", Path: "/home/kara/x"}, false},
		{"scratch on a non-scratch kind", ReqScratchTarget, Target{Kind: "process", Selector: "process:1"}, false},
		{"scratch relative", ReqScratchTarget, Target{Kind: "scratch", Path: "x"}, false},
		{"writable met", ReqWritableTarget, Target{Kind: "scratch", Path: "/tmp/x"}, true},
		{"writable on a missing dir", ReqWritableTarget, Target{Kind: "scratch", Path: "/tmp/no-such-dir-xyz/f"}, false},
		{"unknown token", "gpu", Target{Kind: "scratch", Path: "/tmp/x"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := checkRequirements([]Requirement{{Token: c.token}}, c.target)
			if len(got) != 1 {
				t.Fatalf("results = %+v", got)
			}
			if got[0].Met != c.want {
				t.Fatalf("met = %t (want %t): %s", got[0].Met, c.want, got[0].Detail)
			}
			if got[0].Detail == "" {
				t.Fatal("an unmet requirement must carry the reason it is unmet")
			}
		})
	}
}

// TestRunJournalIsReplacedOnRerun pins the content-addressed run dir: the same
// profile bytes + seed reuse the dir and REPLACE the journal, so a reader never
// folds two runs (with restarted seqs) out of one file.
func TestRunJournalIsReplacedOnRerun(t *testing.T) {
	body := profileBody("steps:\n  - id: s1\n    fault: S-004\n    probe: payload-complete\n")
	runDir := t.TempDir()
	mk := func() Options {
		return Options{Actuators: map[string]FaultActuator{"S-004": &fakeActuator{landed: true, detach: okDetach(),
			obs: Observation{PayloadLen: 4, OnDisk: 4}}}, RunDir: runDir}
	}
	first := runFixture(t, body, mk())
	second := runFixture(t, body, mk())
	if first.RunID != second.RunID {
		t.Fatalf("same bytes+seed must derive the same run id: %q vs %q", first.RunID, second.RunID)
	}
	recs := readJournal(t, runDir)
	planned := 0
	for _, r := range recs {
		if r.Type == journal.RecordRunPlanned {
			planned++
		}
		if r.Seq >= len(recs) {
			t.Fatalf("seq %d out of range for %d records", r.Seq, len(recs))
		}
	}
	if planned != 1 {
		t.Fatalf("journal holds %d run_planned records (want exactly 1)", planned)
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func readJournal(t *testing.T, runDir string) []journal.Record {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(runDir, "profile.jsonl"))
	if err != nil {
		t.Fatalf("journal: %v", err)
	}
	var recs []journal.Record
	for i, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var r journal.Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("journal line %d: %v (%s)", i, err, line)
		}
		recs = append(recs, r)
	}
	return recs
}

func assertTypes(t *testing.T, recs []journal.Record, want ...journal.RecordType) {
	t.Helper()
	seen := map[journal.RecordType]bool{}
	for _, r := range recs {
		seen[r.Type] = true
	}
	for _, w := range want {
		if !seen[w] {
			t.Fatalf("journal is missing a %s record (has %v)", w, seen)
		}
	}
}

func assertVerdict(t *testing.T, recs []journal.Record, want journal.Verdict) {
	t.Helper()
	for _, r := range recs {
		if r.Type == journal.RecordVerdict {
			if r.Verdict != want {
				t.Fatalf("verdict = %q (want %q)", r.Verdict, want)
			}
			return
		}
	}
	t.Fatal("journal carries no verdict record")
}
