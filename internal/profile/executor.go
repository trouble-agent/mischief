package profile

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/trouble-agent/mischief/internal/catalog"
	"github.com/trouble-agent/mischief/internal/journal"
)

// executor.go — the run loop: requirements, per-step resolution, landing,
// probing, grading.
//
// The order of refusal inside a step is the doctrine, and it is deliberate:
// resolve the primitive, then the actuator, then the params, then land, then
// probe, then detach. Every branch that cannot produce a signal returns
// OutcomeInconclusive WITH a reason; none of them returns passed. The only two
// paths to `passed` are (a) a measured landing witness and (b) a probe whose
// observation matched its expectation.

// Target is the resolved fault target, as the caller resolved it (the CLI
// resolves selectors through internal/rails, so a protected or malformed
// selector never reaches the executor).
type Target struct {
	// Kind is the resolved selector kind: "scratch" (L0 scratch file) and
	// "dbdurability" (controlled commit-log database) are the kinds this
	// build can attach to; anything else grades inconclusive.
	Kind string
	// Selector is the selector as the operator wrote it.
	Selector string
	// Path is the file path the selector names (scratch:<path>, or the
	// database file a dbdurability selector names).
	Path string
	// Discipline is the commit discipline a dbdurability selector names
	// ("fsync" or "none"); empty for other kinds.
	Discipline string
}

// LandRequest is everything an actuator is given for one step.
type LandRequest struct {
	// Step is the profile step being applied.
	Step Step
	// Desc is the resolved primitive descriptor.
	Desc *catalog.Descriptor
	// Target is the resolved target.
	Target Target
	// Dir is the run dir; actuators own a subdirectory of it for their
	// generated sources and injection log.
	Dir string
	// RunID is the content-derived run id (scenario bytes derive from it).
	RunID string
	// Seed is the run seed.
	Seed int64
}

// Observation is what an actuator MEASURED about the target after injecting.
// The probe grades it; the actuator never decides the outcome itself.
type Observation struct {
	// TargetPath is the file the scenario wrote.
	TargetPath string
	// PayloadLen is the number of bytes the scenario was asked to write.
	PayloadLen int
	// OnDisk is the number of bytes the target file held afterwards.
	OnDisk int64
	// Attempts is the number of write(2) calls the scenario issued.
	Attempts int
	// ShortWrites is the number of write(2) calls that returned fewer bytes
	// than they were given (the fault's own observable).
	ShortWrites int
	// Acks is the number of commits the database child acknowledged (its own
	// report — the commit-durable probe's input).
	Acks int
	// Syncs is the number of fsync/fdatasync calls on the database file the
	// child issued before its acks (its own report).
	Syncs int
	// SyncFails is the number of those sync calls that returned an error.
	SyncFails int
	// StableRecords is the number of commit records the PARENT read from the
	// database file after the commit child exited — the external loss
	// witness the injector does not author.
	StableRecords int
	// ClaimedDurable is the number of records the child's recovery report
	// claims durable.
	ClaimedDurable int
	// Lost is the number of records the child's recovery report names lost.
	Lost int
	// LieHits is the shim's fsync-lie hit count (injector corroboration).
	LieHits int
	// Note carries actuator detail the probe's line should include.
	Note string
	// ErrnoObserved records a fault independently observed in the target report.
	ErrnoObserved bool
	// ErrnoHandled records that the target recovered or reported the fault.
	ErrnoHandled bool
}

// LandResult is one actuator's landing report.
type LandResult struct {
	// Landed is true only when a landing witness was measured.
	Landed bool
	// Witness is the measured landing proof (required when Landed).
	Witness *Witness
	// Obs is the observation the probe grades.
	Obs Observation
	// Reason names why the fault did not land (required when !Landed).
	Reason string
}

// DetachResult is one actuator's inverse report.
type DetachResult struct {
	// Witness is the measured inverse proof (nil when not proven).
	Witness *Witness
	// Reason names why the inverse was not proven.
	Reason string
}

// FaultActuator injects ONE primitive and proves the injection went away.
// Implementations are keyed by primitive id; a primitive with no entry grades
// inconclusive naming the missing actuator.
type FaultActuator interface {
	// Apply injects the fault, runs the scenario and reports the measurement.
	Apply(ctx context.Context, req LandRequest) LandResult
	// Detach proves the fault is gone (the descriptor's inverse).
	Detach(ctx context.Context, req LandRequest) DetachResult
}

// ProbeFunc grades one observation against a probe's expectation.
type ProbeFunc func(obs Observation) ProbeResult

// DefaultActuators is the shipped actuator set. S-004 (short write) and
// S-010 (fsync lie) are the primitives this build can actually inject; every
// other catalog primitive grades inconclusive naming the gap (see the
// package NOT-LIST).
func DefaultActuators() map[string]FaultActuator {
	return map[string]FaultActuator{
		"S-004": &ShortWrite{},
		"S-010": &FsyncLie{},
	}
}

// DefaultProbes is the shipped probe set (the closed vocabulary in probe.go).
func DefaultProbes() map[string]ProbeFunc {
	return map[string]ProbeFunc{
		ProbePayloadComplete: probePayloadComplete,
		ProbeCommitDurable:   probeCommitDurable,
		ProbeErrnoHandled:    probeErrnoHandled,
	}
}

// Options configures one profile run.
type Options struct {
	// Profile is the loaded profile (already schema-validated).
	Profile *Profile
	// Raw is the exact profile bytes, for the content-derived run id.
	Raw []byte
	// Seed is the effective seed (CLI override or the profile's declared one).
	Seed int64
	// Target is the resolved target.
	Target Target
	// Catalog is the loaded primitive catalog.
	Catalog *catalog.Catalog
	// RunDir is the run dir to create (the caller names it).
	RunDir string
	// Actuators overrides the actuator set (tests); nil uses DefaultActuators.
	Actuators map[string]FaultActuator
	// Probes overrides the probe set (tests); nil uses DefaultProbes.
	Probes map[string]ProbeFunc
	// Now is the clock (tests); nil uses time.Now.
	Now func() time.Time
	// Reproduce is the exact command the finding carries.
	Reproduce string
}

// runJournal appends the run's record set to <run_dir>/profile.jsonl as it
// happens, so a killed run still leaves its evidence.
//
// The journal is the INJECTION LOG a compare diffs between a before-run and
// an after-run (PROF-031), so its bytes must depend on nothing but the
// profile bytes, the seed and the run's graded reality. That is why the clock
// is seed-derived and logical (base + seq, see journalClockBase): a record's
// ts is a pure function of the seed and the record's position, never of the
// wall clock. Wall-clock bounds for the run live in finding.json
// (started_at / finished_at / injected_at), which is NOT part of the
// byte-identity contract.
type runJournal struct {
	f      *os.File
	path   string
	runID  string
	seq    int
	baseTS int64
}

// newRunJournal opens <dir>/profile.jsonl for the run. The dir is
// CONTENT-ADDRESSED (it is the run id), so a re-run of the same profile bytes
// at the same seed replaces the journal rather than appending a second record
// set with a restarted seq — one dir holds exactly one run's records.
// baseTS is the logical clock base derived from the run id and seed.
func newRunJournal(dir, runID string, baseTS int64) (*runJournal, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("run dir: %w", err)
	}
	path := filepath.Join(dir, "profile.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("journal: %w", err)
	}
	return &runJournal{f: f, path: path, runID: runID, baseTS: baseTS}, nil
}

// Path is the journal file path.
func (j *runJournal) Path() string { return j.path }

// add appends one record. The journal package validates and scrubs on write,
// so an out-of-vocabulary value cannot enter the file. The record's ts is the
// logical clock: base + seq (base derived from the run id + seed), so two
// same-seed runs write byte-identical journals.
func (j *runJournal) add(t journal.RecordType, v journal.Verdict, fields map[string]string) error {
	r := journal.Record{Type: t, RunID: j.runID, Seq: j.seq, TS: j.baseTS + int64(j.seq), Verdict: v, Fields: fields}
	b, err := r.MarshalJSON()
	if err != nil {
		return fmt.Errorf("journal record %s: %w", t, err)
	}
	if _, err := j.f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("journal write: %w", err)
	}
	j.seq++
	return nil
}

func (j *runJournal) Close() error {
	if j.f == nil {
		return nil
	}
	err := j.f.Sync()
	if closeErr := j.f.Close(); err == nil {
		err = closeErr
	}
	j.f = nil
	return err
}

// Run executes the profile and returns the graded finding. The returned error
// is INFRASTRUCTURE only (run dir, journal, finding write): a refused or
// inconclusive run is a finding with an outcome, not an error.
func Run(ctx context.Context, o Options) (*Finding, error) {
	now := o.Now
	if now == nil {
		now = time.Now
	}
	actuators := o.Actuators
	if actuators == nil {
		actuators = DefaultActuators()
	}
	probes := o.Probes
	if probes == nil {
		probes = DefaultProbes()
	}
	if o.Profile == nil {
		return nil, fmt.Errorf("run: no profile")
	}

	runID := journal.RunID(o.Raw, o.Seed)
	j, err := newRunJournal(o.RunDir, runID, journalClockBase(runID, o.Seed))
	if err != nil {
		return nil, err
	}
	defer j.Close()

	f := &Finding{
		Schema:         FindingSchemaV1,
		Profile:        o.Profile.ID(),
		ProfileClass:   o.Profile.Class,
		ProfileVersion: o.Profile.Version,
		RunID:          runID,
		RunDir:         o.RunDir,
		Seed:           o.Seed,
		Target:         TargetInfo{Kind: o.Target.Kind, Selector: o.Target.Selector, Path: o.Target.Path},
		StartedAt:      now().UTC().Format(time.RFC3339),
		Reproduce:      o.Reproduce,
	}
	steps := make([]string, 0, len(o.Profile.Steps))
	for _, s := range o.Profile.Steps {
		steps = append(steps, s.ID+"="+s.Fault)
	}
	if err := j.add(journal.RecordRunPlanned, "",
		map[string]string{"kind": "profile", "profile": f.Profile, "profile_version": f.ProfileVersion,
			"target": o.Target.Selector, "target_kind": o.Target.Kind, "seed": fmt.Sprintf("%d", o.Seed),
			"steps": strings.Join(steps, ","),
			// Determinism disclosure (PROF-031): the journal's ts is a seed-derived
			// logical clock, not the wall clock — that is what makes the
			// byte-identity of two same-seed journals (and `profile compare`)
			// possible. Wall-clock bounds live in finding.json.
			"journal_clock": "seed-derived (ts=base+seq); wall clock: finding.json"}); err != nil {
		return nil, err
	}

	// Requirements are run-level: an unmet one means NO step may be graded, so
	// every step is inconclusive with that reason (never a pass).
	f.Requires = checkRequirements(o.Profile.Requires, o.Target)
	var unmet []string
	for _, r := range f.Requires {
		if !r.Met {
			unmet = append(unmet, fmt.Sprintf("%s: %s", r.Requirement, r.Detail))
		}
	}
	reqReason := ""
	if len(unmet) > 0 {
		reqReason = "requirements not met — " + strings.Join(unmet, "; ")
	}
	f.Notes = runNotes(o, unmet)

	for _, step := range o.Profile.Steps {
		sf := runStep(ctx, o, step, actuators, probes, now, reqReason)
		f.Steps = append(f.Steps, sf)
		if err := journalStep(j, sf); err != nil {
			return nil, err
		}
	}

	f.Outcome = worstOutcome(f.Steps)
	f.FinishedAt = now().UTC().Format(time.RFC3339)
	applyRunCause(f, o.Profile)

	if err := j.add(journal.RecordVerdict, journal.Verdict(verdictFor(f.Outcome)),
		map[string]string{"outcome": string(f.Outcome), "steps": fmt.Sprintf("%d", len(f.Steps))}); err != nil {
		return nil, err
	}
	if err := j.add(journal.RecordRunClosed, journal.Verdict(verdictFor(f.Outcome)),
		map[string]string{"kind": "profile", "profile": f.Profile, "outcome": string(f.Outcome),
			"steps": fmt.Sprintf("%d", len(f.Steps))}); err != nil {
		return nil, err
	}
	if err := j.Close(); err != nil {
		return nil, err
	}

	doc, err := f.JSON()
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(o.RunDir, "finding.json"), doc, 0o600); err != nil {
		return nil, fmt.Errorf("finding.json: %w", err)
	}
	return f, nil
}

// runStep resolves, lands, probes and grades one step. The result is a named
// return so the deferred duration assignment lands on what the caller receives
// (a bare `return sf` copies to the return slot BEFORE the defer runs, which
// would silently ship duration_ms: 0 for every step).
func runStep(ctx context.Context, o Options, step Step, actuators map[string]FaultActuator,
	probes map[string]ProbeFunc, now func() time.Time, reqReason string) (sf StepFinding) {
	started := now()
	sf = StepFinding{ID: step.ID, Outcome: OutcomeInconclusive}
	defer func() { sf.DurationMS = now().Sub(started).Milliseconds() }()

	// 1. Resolve the primitive against the loaded catalog. An unknown id is
	//    inconclusive BY NAME — the acceptance shape, not a refusal to load.
	desc := o.Catalog.Get(step.Fault)
	if desc == nil {
		sf.Reason = fmt.Sprintf("primitive %q is not in the loaded catalog (%d primitives resolved from catalog/faults/)",
			step.Fault, o.Catalog.Len())
		return sf
	}
	sf.Fault = FaultRef{ID: desc.ID, Name: desc.Name, Backend: desc.Backend,
		Tier: tierString(desc), Params: map[string]string{}}
	for k, v := range step.Params {
		sf.Fault.Params[k] = v
	}

	// 2. Requirements are run-level but reported per step so a reader never
	//    sees a step with no verdict and no reason.
	if reqReason != "" {
		sf.Reason = reqReason
		return sf
	}

	// 3. Params must be declared by the primitive: a profile cannot invent a
	//    knob the primitive does not have (that would be a silently ignored
	//    line in the finding).
	if bad := paramMismatch(step, desc); bad != "" {
		sf.Reason = bad
		return sf
	}

	// 4. An actuator must exist for this primitive in this build.
	act := actuators[desc.ID]
	if act == nil {
		sf.Reason = fmt.Sprintf("no actuator for primitive %q in this build (implemented: %s) — the step was not injected",
			desc.ID, implementedActuators(actuators))
		return sf
	}

	req := LandRequest{Step: step, Desc: desc, Target: o.Target, Dir: o.RunDir, RunID: journal.RunID(o.Raw, o.Seed), Seed: o.Seed}
	sf.InjectedAt = now().UTC().Format(time.RFC3339)
	lr := act.Apply(ctx, req)
	if !lr.Landed {
		sf.Reason = "fault did not land — " + nonEmpty(lr.Reason, "the actuator reported no landing witness")
		return sf
	}
	sf.Landed = true
	sf.LandedWitness = lr.Witness

	// 5. Probe: the actuator measured, the probe grades. No probe ⇒ no grade.
	probe, ok := probes[step.Probe]
	if !ok {
		sf.Reason = fmt.Sprintf("no probe %q in this build (known: %s)", step.Probe, strings.Join(KnownProbes, ", "))
		return sf
	}
	pr := probe(lr.Obs)
	sf.Probe = &pr

	// 6. Inverse: the fault must be gone. An unproven inverse leaves the
	//    target's state unknown, so it cannot grade pass.
	dr := act.Detach(ctx, req)
	if dr.Witness == nil {
		sf.Reason = "inverse not proven — " + nonEmpty(dr.Reason, "the actuator reported no detach witness")
		return sf
	}
	sf.Detached = dr.Witness

	if pr.Passed {
		sf.Outcome = OutcomePassed
		return sf
	}
	sf.Outcome = OutcomeFailed
	sf.CauseClass = step.CauseClass
	if step.CauseClass != "" {
		sf.Hint = o.Profile.CauseClasses[step.CauseClass].Hint
	}
	return sf
}

// applyRunCause lifts the first failed step's cause class and hint to the run.
func applyRunCause(f *Finding, p *Profile) {
	switch f.Outcome {
	case OutcomeFailed:
		for _, s := range f.Steps {
			if s.Outcome == OutcomeFailed && s.CauseClass != "" {
				f.CauseClass, f.Hint = s.CauseClass, s.Hint
				return
			}
		}
	case OutcomeInconclusive:
		for _, s := range f.Steps {
			if s.Outcome == OutcomeInconclusive && s.Reason != "" {
				f.Reason = s.Reason
				return
			}
		}
	}
}

// runNotes is the honest-disclosure list: every declared rail this build does
// not enforce for a profile run, and the scenario's own limit, is named in the
// finding rather than left for a reader to discover.
func runNotes(o Options, unmet []string) []string {
	var notes []string
	switch o.Target.Kind {
	case "scratch":
		notes = append(notes, "target is an L0 scratch path: the executor spawns its own reference writer, so the probe grades mischief's reference write loop, not an external program")
	case "dbdurability":
		notes = append(notes, fmt.Sprintf(
			"target is a controlled commit-log database the executor builds and spawns as a child (commit discipline %q): the probe grades that child's commit protocol and its recovery report, not an external database server",
			o.Target.Discipline))
		notes = append(notes, "the S-010 fsync-lie actuator models the volatile write-back layer inside the injected child's memory: the commit child's exit is the simulated power loss, and the parent's post-exit read of the database file is the external loss witness")
	default:
		notes = append(notes, fmt.Sprintf("target kind %q is not attachable by this build's executor (only scratch:<path> and dbdurability:<fsync|none>:<path> are) — the run is inconclusive", o.Target.Kind))
	}
	for _, s := range o.Profile.Steps {
		if d := o.Catalog.Get(s.Fault); d != nil && d.Tier != nil && d.Tier.Min > 0 {
			notes = append(notes, fmt.Sprintf("%s declares minimum tier %s; the scope ladder and the AC-19 selftest rail are NOT enforced by profile run (the selftest harness lands the same primitives on L0 scratch by the same posture)",
				d.ID, tierString(d)))
		}
	}
	notes = append(notes, "the AC-8 load gate is not wired into profile run in this build")
	if len(unmet) > 0 {
		notes = append(notes, "requirements not met: "+strings.Join(unmet, "; "))
	}
	return notes
}

// checkRequirements resolves every declared requirement against the host.
func checkRequirements(reqs []Requirement, tgt Target) []RequirementResult {
	out := make([]RequirementResult, 0, len(reqs))
	for _, r := range reqs {
		res := RequirementResult{Requirement: r.Token, Declared: r.Detail}
		switch r.Token {
		case ReqCC:
			if p, err := exec.LookPath("cc"); err == nil {
				res.Met, res.Detail = true, "cc -> "+p
			} else {
				res.Detail = "no C compiler (cc) on PATH: the LD_PRELOAD shim cannot be built"
			}
		case ReqScratchTarget:
			switch {
			case tgt.Kind != "scratch":
				res.Detail = fmt.Sprintf("target kind %q is not a scratch path (want scratch:<path>)", tgt.Kind)
			case tgt.Path == "":
				res.Detail = "scratch selector names no path"
			case !filepath.IsAbs(tgt.Path):
				res.Detail = fmt.Sprintf("target path %q is not absolute", tgt.Path)
			case !underScratchBase(tgt.Path):
				res.Detail = fmt.Sprintf("target path %q is outside the scratch base (%s, /tmp)", tgt.Path, os.TempDir())
			default:
				res.Met, res.Detail = true, "target "+tgt.Path+" is under the scratch base"
			}
		case ReqDurabilityTarget:
			switch {
			case tgt.Kind != "dbdurability":
				res.Detail = fmt.Sprintf("target kind %q is not a controlled database target (want dbdurability:<fsync|none>:<path>)", tgt.Kind)
			case tgt.Discipline != "fsync" && tgt.Discipline != "none":
				res.Detail = fmt.Sprintf("selector discipline %q is not one of fsync|none", tgt.Discipline)
			case tgt.Path == "":
				res.Detail = "durability selector names no database path"
			case !filepath.IsAbs(tgt.Path):
				res.Detail = fmt.Sprintf("database path %q is not absolute", tgt.Path)
			case !underScratchBase(tgt.Path):
				res.Detail = fmt.Sprintf("database path %q is outside the scratch base (%s, /tmp)", tgt.Path, os.TempDir())
			default:
				res.Met, res.Detail = true, fmt.Sprintf("database %s is under the scratch base, commit discipline %q", tgt.Path, tgt.Discipline)
			}
		case ReqWritableTarget:
			if tgt.Path == "" {
				res.Detail = "scratch selector names no path"
			} else if err := unix.Access(filepath.Dir(tgt.Path), unix.W_OK); err != nil {
				res.Detail = fmt.Sprintf("parent dir %s is not writable: %v", filepath.Dir(tgt.Path), err)
			} else {
				res.Met, res.Detail = true, "parent dir "+filepath.Dir(tgt.Path)+" is writable"
			}
		default:
			res.Detail = fmt.Sprintf("unknown requirement token %q (known: %s)", r.Token, strings.Join(KnownRequirements, ", "))
		}
		out = append(out, res)
	}
	return out
}

// underScratchBase reports whether p is inside the scratch base. Both
// os.TempDir() and /tmp are accepted: TMPDIR is derived per-session on some
// hosts, and the operator-facing recipe names /tmp explicitly.
func underScratchBase(p string) bool {
	clean := filepath.Clean(p)
	for _, base := range []string{os.TempDir(), "/tmp"} {
		if base == "" {
			continue
		}
		if rel, err := filepath.Rel(filepath.Clean(base), clean); err == nil &&
			rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// paramMismatch names the first step param the primitive does not declare (or
// whose declared enumeration refuses the value). Empty means the step is
// applicable.
func paramMismatch(step Step, desc *catalog.Descriptor) string {
	names := make([]string, 0, len(desc.Params))
	for name := range desc.Params {
		names = append(names, name)
	}
	sortStringsAsc(names)
	// Iterate the step's params in sorted order too: the map itself must not
	// decide WHICH mismatch a run with several bad params reports first
	// (PROF-031 determinism — everything the run logs must be seed-stable).
	badNames := make([]string, 0, len(step.Params))
	for name := range step.Params {
		badNames = append(badNames, name)
	}
	sortStringsAsc(badNames)
	for _, name := range badNames {
		val := step.Params[name]
		spec, ok := desc.Params[name]
		if !ok {
			return fmt.Sprintf("param %q is not declared by %s (declared: %s)", name, desc.ID, strings.Join(names, ", "))
		}
		if len(spec.Enum) > 1 {
			allowed := false
			for _, e := range spec.Enum {
				if e == val {
					allowed = true
					break
				}
			}
			if !allowed {
				return fmt.Sprintf("param %q value %q is not one of the declared values (%s)", name, val, strings.Join(spec.Enum, "|"))
			}
		}
	}
	return ""
}

// journalStep appends a step's record set: armed, landed (or not), sampled,
// graded, and a finding record carrying the outcome.
func journalStep(j *runJournal, sf StepFinding) error {
	armed := map[string]string{"step": sf.ID}
	if sf.Fault.ID != "" {
		armed["primitive"] = sf.Fault.ID
	}
	if err := j.add(journal.RecordFaultArmed, "", armed); err != nil {
		return err
	}
	if sf.Landed && sf.LandedWitness != nil {
		if err := j.add(journal.RecordFaultLanded, "", map[string]string{
			"step": sf.ID, "primitive": sf.Fault.ID,
			"witness_kind": sf.LandedWitness.Kind, "witness_source": sf.LandedWitness.Source,
			"proof": sf.LandedWitness.Evidence}); err != nil {
			return err
		}
	}
	if sf.Probe != nil {
		if err := j.add(journal.RecordProbeSample, "", map[string]string{
			"step": sf.ID, "probe": sf.Probe.Name, "observed": sf.Probe.Observed}); err != nil {
			return err
		}
		if err := j.add(journal.RecordAssertionResult, "", map[string]string{
			"step": sf.ID, "probe": sf.Probe.Name, "expected": sf.Probe.Expected,
			"observed": sf.Probe.Observed, "passed": fmt.Sprintf("%t", sf.Probe.Passed)}); err != nil {
			return err
		}
	}
	if sf.Detached != nil {
		if err := j.add(journal.RecordFaultReverted, "", map[string]string{
			"step": sf.ID, "witness_kind": sf.Detached.Kind, "witness_source": sf.Detached.Source,
			"proof": sf.Detached.Evidence}); err != nil {
			return err
		}
	}
	fields := map[string]string{"step": sf.ID, "outcome": string(sf.Outcome)}
	if sf.CauseClass != "" {
		fields["cause_class"] = sf.CauseClass
		fields["hint"] = sf.Hint
	}
	if sf.Reason != "" {
		fields["reason"] = sf.Reason
	}
	return j.add(journal.RecordFinding, "", fields)
}

// journalClockBase derives the journal's LOGICAL clock base from the run id
// and the seed (PROF-031): record ts values are base+seq, so two runs of the
// same profile bytes at the same seed write byte-identical journals — a
// record's timestamp is a pure function of the seed and the record's
// position, never of the wall clock. The half-shift keeps the value positive.
func journalClockBase(runID string, seed int64) int64 {
	h := sha256.New()
	fmt.Fprintf(h, "mischief.profile.journal\n%s\n%d\n", runID, seed)
	sum := h.Sum(nil)
	return int64(binary.BigEndian.Uint64(sum[:8]) >> 1)
}

// tierString renders a descriptor's declared tier ("L1"), or "" when the
// descriptor carries none (the loader refuses that, so this is defensive).
func tierString(d *catalog.Descriptor) string {
	if d == nil || d.Tier == nil {
		return ""
	}
	return fmt.Sprintf("L%d", d.Tier.Min)
}

// implementedActuators lists the primitives this build can inject, sorted.
func implementedActuators(m map[string]FaultActuator) string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sortStringsAsc(ids)
	return strings.Join(ids, ", ")
}

// sortStringsAsc sorts in place (insertion sort: the sets here are tiny and
// the package avoids another import).
func sortStringsAsc(s []string) {
	for i := 1; i < len(s); i++ {
		for k := i; k > 0 && s[k] < s[k-1]; k-- {
			s[k], s[k-1] = s[k-1], s[k]
		}
	}
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}
