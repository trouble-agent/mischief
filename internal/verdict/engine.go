// Package verdict is SPEC-05: the mischief verdict engine — the vocabulary,
// the probe/assertion model, the integrity oracle gate, the control run, and
// the timing model (docs/SPEC-PLAN.md SPEC-05; PRD §4 closure rules, §6.4).
//
// The engine is a total, synchronous function over collected evidence:
// Grade(Plan, Evidence) → Result. It never observes a live system, never
// runs a process, and holds no state; the runner (SPEC-02's journal writer,
// future SPEC-04 actuator) collects Evidence and calls Grade once. This
// makes the grading rules table-testable and byte-reproducible, which is
// what AC-9's round trip and the anti-false-green rules (AC-13/15/16)
// actually need.
//
// Anti-false-green closure rules (PRD §4), enforced in Grade in this order:
//
//  1. rails refusal        → aborted            (the run refused before arming)
//  2. control red          → void               (AC-16: the harness, not the target, is the finding)
//  3. baseline red         → void               (same harness-defect family)
//  4. timeline insane      → void               (a backwards timeline is a harness defect)
//  5. fault never landed   → no_op              (AC-3: armed but no proof = the run FAILS)
//  6. oracle sees loss     → corrupted          (data loss is graded before recovery)
//  7. fault never reverted → not_recovered      (revert proof is part of closure, §4 rule 1)
//  8. probes never green   → not_recovered; all probes dead → hung
//  9. budget exceeded      → degraded / not_recovered (measured cost, not a pass)
//  10. assertions failed    → not_recovered      (a green probe set is not closure alone)
//  11. AC-15: recovered requires an integrity oracle — an absent oracle
//     downgrades recovered to unverified (named refusal), never silently
//     recovered.
//
// AC-13 holds by construction: the fault's exit code is not an input to
// Grade at all. Evidence carries what was measured out-of-band (landed
// proof, probe samples, oracle checks); a fault process that exits 0
// without a landed proof grades no_op, never recovered.
//
// Vocabulary ownership: the closed Verdict type lives in
// internal/journal (PRD §6.4; "no vocabulary of its own", SPEC-PLAN rule 5).
// This package consumes it and adds the AC-15 coercion result Unverified
// — not a ninth vocabulary value, but a named refusal the runner journals
// in place of recovered.
package verdict

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/trouble-agent/mischief/internal/journal"
)

// Unverified is the named AC-15 refusal: probes were green, budget held,
// assertions passed — but no integrity oracle was declared, so the engine
// refuses to grade recovered. It is not a member of the closed journal
// vocabulary; the runner journals the coercion note (String) with
// verdict not_recovered.
type Unverified string

// String renders the refusal naming the missing oracle (AC-15's "naming
// the missing oracle").
func (u Unverified) String() string {
	return fmt.Sprintf("unverified: integrity oracle %q was not declared; recovered refused (AC-15)", u.oracle())
}

func (u Unverified) oracle() string {
	if u == "" {
		return "(unnamed)"
	}
	return string(u)
}

// Probe is one entry of the pre/post probe set: a named out-of-band check
// with its expected result. The engine never executes probes; the runner
// does and records the samples.
type Probe struct {
	// Name identifies the probe ("redis-ping").
	Name string
	// Expect is the expected sample value ("ok").
	Expect string
}

// Sample is one recorded probe observation.
type Sample struct {
	// Probe is the probe the sample belongs to.
	Probe Probe
	// OK reports whether the observed value equals the expected one.
	OK bool
	// Observed is what the probe actually returned (evidence, not grading).
	Observed string
	// At is when the sample was taken (used for probe freshness; zero time
	// is legal and treated as "taken, time unrecorded").
	At time.Time
}

// Assertion is a named post-recovery check beyond liveness (PRD: "liveness
// alone never closes it"). A probe set that is green while an assertion
// fails does not grade recovered.
type Assertion struct {
	// Name identifies the assertion ("queue-drained", "counter-monotone").
	Name string
	// OK is the assertion result.
	OK bool
	// Detail carries the failure detail for the journal.
	Detail string
}

// Oracle is the pluggable integrity check (checksums / counters / ledger
// sequence — PRD §4 closure rule 2). The runner implements it against the
// declared integrity: block (journal.IntegrityDecl); the engine only asks
// it "was anything lost?".
type Oracle interface {
	// Name identifies the oracle ("ledger-seq", "sha256-files") for the
	// AC-15 refusal message and the journal.
	Name() string
	// Check reports whether integrity held (nil = no loss detected) or
	// what was lost.
	Check() error
}

// OracleReport is one integrity oracle's recorded verdict: which oracle
// spoke and what it said (nil error = integrity held).
type OracleReport struct {
	// Name is the oracle's declared name ("ledger-seq", "sha256-files").
	Name string
	// Err is the loss detail when the oracle reported loss (nil = clean).
	Err error
}

// Plan is the graded experiment's declared plan: what the engine needs to
// know beyond the evidence.
type Plan struct {
	// Probes is the declared probe set (order preserved).
	Probes []Probe
	// Budget is the declared recovery budget (0 = no budget declared).
	Budget time.Duration
	// IntegrityNames lists the declared integrity oracles (AC-15 gate:
	// empty = recovered is forbidden).
	IntegrityNames []string
	// ControlGreen is the control run's outcome (AC-16: red control voids
	// the experiment). The zero value is false — an un-run control is not
	// a green control.
	ControlGreen bool
}

// Evidence is everything measured during the run, out-of-band of the fault
// process (AC-13: the fault's own exit code is deliberately not a field).
type Evidence struct {
	// Landed reports whether the fault's landed_proof fired (measured
	// out-of-band, never inferred from the actuator's exit status).
	Landed bool
	// Reverted reports whether the inverse's revert proof fired.
	Reverted bool
	// RailsRefused is the named rail refusal, when the run was refused
	// before arming (empty = no refusal).
	RailsRefused string
	// Timeline is the four measured durations.
	Timeline Timeline
	// Baseline is the pre-fault probe samples (the pre-fault tree must be
	// green before anything is armed).
	Baseline []Sample
	// Post is the probe samples taken after revert, within the budget.
	Post []Sample
	// Assertions are the post-recovery checks beyond liveness.
	Assertions []Assertion
	// Oracles carries one report per integrity oracle: the oracle's
	// declared name and its verdict (nil error = clean, non-nil = loss).
	// The engine consults these, never the Oracle interface directly —
	// the runner runs the declared oracles and records what they said.
	Oracles []OracleReport
}

// Result is the graded outcome.
type Result struct {
	// Verdict is the graded value from the closed journal vocabulary.
	Verdict journal.Verdict
	// Timeline is the timing model attached to every verdict (SPEC-05).
	Timeline Timeline
	// Reason names the deciding rule, for the journal's verdict record.
	Reason string
	// Unverified carries the AC-15 coercion when Verdict was refused
	// recovered for a missing oracle ("" otherwise).
	Unverified Unverified
}

// Grade is the total grading function: (plan, evidence) → result, no
// hidden inputs, deterministic. It applies the closure rules in the order
// documented on the package; the first matching rule decides.
func Grade(p Plan, ev Evidence) Result {
	res := Result{Timeline: ev.Timeline}
	if err := ev.Timeline.Validate(); err != nil {
		return voidf(res, "timeline insane: %v", err)
	}
	if ev.RailsRefused != "" {
		return finish(res, journal.VerdictAborted, "rails refusal: %s", ev.RailsRefused)
	}
	if !p.ControlGreen {
		return voidf(res, "control run not green (AC-16): the harness, not the target, is the finding")
	}
	if reason := baselineRed(p, ev.Baseline); reason != "" {
		return voidf(res, "%s", reason)
	}
	if !ev.Landed {
		return finish(res, journal.VerdictNoOp, "fault armed but landed proof never fired (AC-3)")
	}
	if reason := oracleLoss(ev); reason != "" {
		return finish(res, journal.VerdictCorrupted, "%s", reason)
	}
	if !ev.Reverted {
		return finish(res, journal.VerdictNotRecovered, "revert proof never fired (closure rule 1)")
	}

	// Probe track: grade the post-revert samples against the declared set.
	track, reason := gradeProbes(p, ev.Post)
	if reason != "" {
		return finish(res, track, "%s", reason)
	}

	// Probe set green: budget and assertions decide recovered vs degraded.
	over := ev.Timeline.Recover > p.Budget && p.Budget > 0
	if reason := assertionsFailed(ev.Assertions); reason != "" {
		if over {
			return finish(res, journal.VerdictNotRecovered,
				"assertion failed and budget exceeded (%s > %s): %s",
				ev.Timeline.Recover, p.Budget, reason)
		}
		return finish(res, journal.VerdictNotRecovered, "%s", reason)
	}
	if over {
		return finish(res, journal.VerdictDegraded,
			"recovered within probes but over budget (%s > %s): passes with a measured cost",
			ev.Timeline.Recover, p.Budget)
	}

	// AC-15: recovered requires an integrity oracle. Absent = refused,
	// naming the missing oracle. The coercion downgrades to
	// not_recovered and carries the named refusal.
	if len(p.IntegrityNames) == 0 {
		u := Unverified("integrity")
		out := finish(res, journal.VerdictNotRecovered, "%s", u.String())
		out.Unverified = u
		return out
	}
	for _, name := range p.IntegrityNames {
		if name == "" {
			u := Unverified("(unnamed)")
			out := finish(res, journal.VerdictNotRecovered, "%s", u.String())
			out.Unverified = u
			return out
		}
	}
	return finish(res, journal.VerdictRecovered,
		"probe set green within budget (%s ≤ %s), assertions passed, %d oracle(s) declared and clean",
		ev.Timeline.Recover, p.Budget, len(p.IntegrityNames))
}

// Aggregate grades N results of the same experiment (the battery/replay
// shape): unanimous agreement returns the common verdict; disagreement
// grades flaky (PRD §6.4: "flaky (N runs disagree)"). A single result is
// itself; zero runs grade void (nothing to grade is not a pass). The
// returned Result carries the first result's timeline and — on
// disagreement — a reason naming the disagreeing values in run order.
func Aggregate(rs []Result) Result {
	if len(rs) == 0 {
		return Result{Verdict: journal.VerdictVoid, Reason: "aggregate of zero runs"}
	}
	first := rs[0]
	for _, r := range rs[1:] {
		if r.Verdict != first.Verdict {
			vals := make([]string, 0, len(rs))
			for _, x := range rs {
				vals = append(vals, x.Verdict.String())
			}
			return Result{
				Verdict:  journal.VerdictFlaky,
				Timeline: first.Timeline,
				Reason:   fmt.Sprintf("%d runs disagree: %s", len(rs), strings.Join(vals, ", ")),
			}
		}
	}
	out := first
	if len(rs) > 1 && out.Reason != "" {
		out.Reason = fmt.Sprintf("%d runs agree on %s: %s", len(rs), first.Verdict, first.Reason)
	}
	return out
}

// Record renders the result as a journal verdict record (SPEC-02's wire
// shape): the verdict value in the closed-vocabulary field, everything
// else as canonical fields — reason, the four timings, and the AC-15
// refusal note when present. The coercion note rides Fields, never the
// Verdict field: Unverified is not a vocabulary member, so the journal's
// write-path validation must be able to accept every record the engine
// produces.
func (res Result) Record(runID string, seq int, ts int64) journal.Record {
	f := map[string]string{
		"reason":    res.Reason,
		"t_landed":  res.Timeline.Landed.String(),
		"t_symptom": res.Timeline.Symptom.String(),
		"t_detect":  res.Timeline.Detect.String(),
		"t_recover": res.Timeline.Recover.String(),
	}
	if res.Unverified != "" {
		f["unverified"] = res.Unverified.String()
	}
	return journal.Record{
		Type:    journal.RecordVerdict,
		RunID:   runID,
		Seq:     seq,
		TS:      ts,
		Verdict: res.Verdict,
		Fields:  f,
	}
}

// voidf grades void with a named reason (AC-16 family).
func voidf(res Result, format string, args ...any) Result {
	res.Verdict = journal.VerdictVoid
	res.Reason = fmt.Sprintf(format, args...)
	return res
}

// finish sets the verdict and reason on the result.
func finish(res Result, v journal.Verdict, format string, args ...any) Result {
	res.Verdict = v
	res.Reason = fmt.Sprintf(format, args...)
	return res
}

// baselineRed returns the reason the pre-fault baseline is not green, or ""
// when the baseline is green (AC-16 family: a red baseline means the
// harness was broken before the fault armed).
func baselineRed(p Plan, baseline []Sample) string {
	for _, pr := range p.Probes {
		found := false
		for _, s := range baseline {
			if s.Probe.Name == pr.Name {
				found = true
				if !s.OK {
					return fmt.Sprintf("pre-fault baseline probe %q red (AC-16): expected %q, observed %q",
						pr.Name, pr.Expect, s.Observed)
				}
			}
		}
		if !found {
			return fmt.Sprintf("pre-fault baseline missing probe %q (AC-16)", pr.Name)
		}
	}
	return ""
}

// oracleLoss returns the reason an integrity oracle reported loss, or ""
// when none did (rule 6: data loss is graded before recovery — a run that
// recovers liveness on top of lost data is corrupted, not recovered). The
// reason names the oracle that spoke.
func oracleLoss(ev Evidence) string {
	for _, o := range ev.Oracles {
		if o.Err != nil {
			return fmt.Sprintf("integrity oracle %q reported loss: %v", o.Name, o.Err)
		}
	}
	return ""
}

// gradeProbes grades the post-revert samples against the declared probe
// set. It returns the track verdict (recovered/degraded/not_recovered/hung
// — budget and assertions applied by the caller) and a reason when the
// track is not green.
func gradeProbes(p Plan, post []Sample) (journal.Verdict, string) {
	dead, missing := 0, 0
	for _, pr := range p.Probes {
		found := false
		for _, s := range post {
			if s.Probe.Name == pr.Name {
				found = true
				if !s.OK {
					dead++
					break
				}
			}
		}
		if !found {
			missing++
		}
	}
	switch {
	case missing == len(p.Probes) && len(p.Probes) > 0:
		return journal.VerdictHung, "no probe answered after revert: target stopped answering (hung)"
	case missing+dead > 0:
		return journal.VerdictNotRecovered,
			fmt.Sprintf("probe set not green after revert: %d failed, %d missing of %d declared",
				dead, missing, len(p.Probes))
	}
	return journal.VerdictRecovered, ""
}

// assertionsFailed returns the reason an assertion failed, or "" when all
// passed (a green probe set is not closure alone — PRD closure rule 2).
func assertionsFailed(as []Assertion) string {
	for _, a := range as {
		if !a.OK {
			return fmt.Sprintf("assertion %q failed: %s", a.Name, a.Detail)
		}
	}
	return ""
}

// SortSamples orders samples by probe name then observation time — the
// stable form tests and journal fields compare (map/slice order must never
// decide a verdict).
func SortSamples(s []Sample) {
	sort.Slice(s, func(i, j int) bool {
		if s[i].Probe.Name != s[j].Probe.Name {
			return s[i].Probe.Name < s[j].Probe.Name
		}
		return s[i].At.Before(s[j].At)
	})
}
