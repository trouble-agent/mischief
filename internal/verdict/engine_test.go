package verdict

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/trouble-agent/mischief/internal/journal"
)

// greenPlan is the fully-green baseline plan: control green, one probe,
// budget 5s, one integrity oracle declared.
func greenPlan() Plan {
	return Plan{
		Probes:         []Probe{{Name: "ping", Expect: "ok"}},
		Budget:         5 * time.Second,
		IntegrityNames: []string{"ledger-seq"},
		ControlGreen:   true,
	}
}

// greenEvidence is the fully-green baseline evidence: landed, reverted,
// sane timeline, green baseline, green post set, clean oracles.
func greenEvidence() Evidence {
	return Evidence{
		Landed:   true,
		Reverted: true,
		Timeline: Timeline{
			Landed:  100 * time.Millisecond,
			Symptom: 10 * time.Millisecond,
			Recover: 250 * time.Millisecond,
		},
		Baseline: []Sample{{Probe: Probe{Name: "ping", Expect: "ok"}, OK: true}},
		Post:     []Sample{{Probe: Probe{Name: "ping", Expect: "ok"}, OK: true}},
	}
}

// TestVocabularyReachableAndSerializable walks one scenario per vocabulary
// verdict: each value is reachable by grading real evidence shapes, and
// each graded result survives the journal envelope (Record → JSON → read)
// with the verdict value byte-identical (AC-9 half; the full round-trip
// matrix lives in serial_test.go). Timeline sanity is asserted on every
// scenario: a verdict never rides an insane timeline.
func TestVocabularyReachableAndSerializable(t *testing.T) {
	sickPost := func() []Sample {
		return []Sample{{Probe: Probe{Name: "ping", Expect: "ok"}, OK: false, Observed: "conn refused"}}
	}
	scenarios := []struct {
		name string
		plan func(*Plan)
		ev   func(*Evidence)
		agg  []Result // when non-nil, graded via Aggregate instead of Grade
		want journal.Verdict
	}{
		{"recovered", nil, nil, nil, journal.VerdictRecovered},
		{"degraded-over-budget", nil, func(e *Evidence) {
			e.Timeline.Recover = 6 * time.Second
		}, nil, journal.VerdictDegraded},
		{"not_recovered-probe-red", nil, func(e *Evidence) {
			e.Post = sickPost()
		}, nil, journal.VerdictNotRecovered},
		{"hung-no-probe-answered", nil, func(e *Evidence) {
			e.Post = nil
		}, nil, journal.VerdictHung},
		{"corrupted-oracle-loss", nil, func(e *Evidence) {
			e.Oracles = []OracleReport{{Name: "ledger-seq", Err: errors.New("sequence regressed 42 -> 41")}}
		}, nil, journal.VerdictCorrupted},
		{"no_op-never-landed", nil, func(e *Evidence) {
			e.Landed = false
		}, nil, journal.VerdictNoOp},
		{"aborted-rails-refusal", nil, func(e *Evidence) {
			e.RailsRefused = "protected-target: scheduler"
		}, nil, journal.VerdictAborted},
		{"void-control-red", func(p *Plan) {
			p.ControlGreen = false
		}, nil, nil, journal.VerdictVoid},
		{"flaky-runs-disagree", nil, nil, []Result{
			{Verdict: journal.VerdictRecovered, Timeline: Timeline{Recover: time.Second}},
			{Verdict: journal.VerdictNotRecovered, Timeline: Timeline{Recover: time.Second}},
			{Verdict: journal.VerdictRecovered, Timeline: Timeline{Recover: time.Second}},
		}, journal.VerdictFlaky},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			var res Result
			switch {
			case sc.agg != nil:
				res = Aggregate(sc.agg)
			default:
				p := greenPlan()
				if sc.plan != nil {
					sc.plan(&p)
				}
				ev := greenEvidence()
				if sc.ev != nil {
					sc.ev(&ev)
				}
				res = Grade(p, ev)
			}
			if res.Verdict != sc.want {
				t.Fatalf("verdict = %q, want %q (reason: %s)", res.Verdict, sc.want, res.Reason)
			}
			// vocabulary membership is proven by the journal hop below:
			// the verdict value survives the journal envelope byte-identically
			// (Record.MarshalJSON validates against the closed vocabulary on
			// write — an out-of-vocabulary value would fail this hop).
			rec := res.Record("0123456789abcdef", 1, 1000)
			back := journalRoundTrip(t, rec)
			if back.Verdict != res.Verdict || back.Verdict.String() != sc.want.String() {
				t.Fatalf("verdict mutated through journal: %q -> %q (want %q)",
					res.Verdict, back.Verdict, sc.want)
			}
			// every verdict carries a sane timeline
			if err := res.Timeline.Validate(); err != nil {
				t.Fatalf("verdict %q carries insane timeline: %v", res.Verdict, err)
			}
			if res.Reason == "" {
				t.Fatalf("verdict %q carries no deciding reason", res.Verdict)
			}
		})
	}
}

// TestAC13FaultExitCodeNeverGradesRecovered: the fault process's exit code
// is not an input to Grade — Evidence has no field to carry it, and the two
// exit-0 shapes grade no_op / not_recovered from out-of-band probes. The
// constructive half: when the out-of-band probes ARE green, the engine
// grades recovered without ever having seen an exit status.
func TestAC13FaultExitCodeNeverGradesRecovered(t *testing.T) {
	t.Run("fault exits 0 without landing -> no_op, never recovered", func(t *testing.T) {
		ev := greenEvidence()
		ev.Landed = false
		// the fault backend "exited 0" — there is nowhere to say it, and
		// nowhere Grade could read it from; the graded value comes from the
		// absent landed proof alone.
		res := Grade(greenPlan(), ev)
		if res.Verdict != journal.VerdictNoOp {
			t.Fatalf("verdict = %q, want no_op (AC-13: exit 0 without landing is no_op)", res.Verdict)
		}
	})
	t.Run("target alive but not serving -> probe set red -> not recovered", func(t *testing.T) {
		ev := greenEvidence()
		ev.Post = []Sample{{
			Probe:    Probe{Name: "ping", Expect: "ok"},
			OK:       false,
			Observed: "process alive, port closed",
		}}
		res := Grade(greenPlan(), ev)
		if res.Verdict == journal.VerdictRecovered {
			t.Fatalf("alive-but-not-serving graded recovered (AC-13 violation)")
		}
		if res.Verdict != journal.VerdictNotRecovered {
			t.Fatalf("verdict = %q, want not_recovered", res.Verdict)
		}
	})
	t.Run("green probes grade recovered with no exit-status input", func(t *testing.T) {
		res := Grade(greenPlan(), greenEvidence())
		if res.Verdict != journal.VerdictRecovered {
			t.Fatalf("verdict = %q, want recovered", res.Verdict)
		}
	})
}

// TestAC15MissingOracleForbidsRecovered: without a declared integrity
// oracle the engine refuses recovered, downgrades to not_recovered, and
// names the missing oracle in the refusal.
func TestAC15MissingOracleForbidsRecovered(t *testing.T) {
	t.Run("no oracle declared -> recovered refused", func(t *testing.T) {
		p := greenPlan()
		p.IntegrityNames = nil // the AC-15 shape: no integrity: block
		res := Grade(p, greenEvidence())
		if res.Verdict == journal.VerdictRecovered {
			t.Fatal("recovered graded without an integrity oracle (AC-15 violation)")
		}
		if res.Verdict != journal.VerdictNotRecovered {
			t.Fatalf("verdict = %q, want not_recovered (the AC-15 coercion)", res.Verdict)
		}
		if res.Unverified == "" {
			t.Fatal("coersion carried no named refusal")
		}
		msg := res.Unverified.String()
		if !strings.Contains(msg, "oracle") || !strings.Contains(msg, "AC-15") {
			t.Fatalf("refusal %q must name the missing oracle and AC-15", msg)
		}
		// the refusal rides the journal envelope as a field
		rec := res.Record("0123456789abcdef", 1, 1000)
		back := journalRoundTrip(t, rec)
		if back.Verdict != journal.VerdictNotRecovered {
			t.Fatalf("coerced verdict mutated through journal: %q", back.Verdict)
		}
		if back.Fields["unverified"] == "" {
			t.Fatal("unverified note lost through the journal envelope")
		}
	})
	t.Run("blank oracle name is not a declared oracle", func(t *testing.T) {
		p := greenPlan()
		p.IntegrityNames = []string{""}
		res := Grade(p, greenEvidence())
		if res.Verdict == journal.VerdictRecovered {
			t.Fatal("a blank oracle name graded recovered (AC-15 violation)")
		}
		if res.Unverified == "" {
			t.Fatal("blank oracle name carried no refusal")
		}
	})
	t.Run("declared clean oracle -> recovered stands", func(t *testing.T) {
		res := Grade(greenPlan(), greenEvidence())
		if res.Verdict != journal.VerdictRecovered || res.Unverified != "" {
			t.Fatalf("verdict = %q unverified=%q; a declared clean oracle closes recovered",
				res.Verdict, res.Unverified)
		}
	})
}

// oracleStub is the pluggable-oracle proof: the engine consumes the Oracle
// interface's Check() result, collected by the runner into Evidence.
type oracleStub struct {
	name string
	err  error
}

func (o oracleStub) Name() string { return o.name }
func (o oracleStub) Check() error { return o.err }

// TestOraclePluggableInterface: an oracle implementation behind the
// interface drives corrupted (loss) or leaves recovered intact (clean),
// without the engine knowing anything about the implementation.
func TestOraclePluggableInterface(t *testing.T) {
	clean := oracleStub{name: "sha256-files", err: nil}
	loss := oracleStub{name: "ledger-seq", err: errors.New("3 files truncated")}

	p := greenPlan()
	ev := greenEvidence()
	if err := clean.Check(); err != nil {
		ev.Oracles = append(ev.Oracles, OracleReport{Name: clean.Name(), Err: err})
	}
	if res := Grade(p, ev); res.Verdict != journal.VerdictRecovered {
		t.Fatalf("clean oracle graded %q, want recovered", res.Verdict)
	}

	ev = greenEvidence()
	if err := loss.Check(); err != nil {
		ev.Oracles = append(ev.Oracles, OracleReport{Name: loss.Name(), Err: err})
	}
	res := Grade(p, ev)
	if res.Verdict != journal.VerdictCorrupted {
		t.Fatalf("loss oracle graded %q, want corrupted", res.Verdict)
	}
	if !strings.Contains(res.Reason, loss.name) {
		t.Fatalf("reason %q must name the reporting oracle %q", res.Reason, loss.name)
	}
}

// TestAC16ControlRedVoids: a red control run voids the experiment no
// matter what the faulted run looked like (the harness, not the target,
// is the finding). Same for a red pre-fault baseline, a missing baseline
// sample, and an insane timeline.
func TestAC16ControlRedVoids(t *testing.T) {
	t.Run("control red voids even a green faulted run", func(t *testing.T) {
		p := greenPlan()
		p.ControlGreen = false
		res := Grade(p, greenEvidence()) // everything else green
		if res.Verdict != journal.VerdictVoid {
			t.Fatalf("verdict = %q, want void (AC-16)", res.Verdict)
		}
		if !strings.Contains(res.Reason, "control") {
			t.Fatalf("reason %q must name the control run", res.Reason)
		}
	})
	t.Run("red baseline probe voids", func(t *testing.T) {
		ev := greenEvidence()
		ev.Baseline = []Sample{{
			Probe: Probe{Name: "ping", Expect: "ok"}, OK: false, Observed: "already broken",
		}}
		res := Grade(greenPlan(), ev)
		if res.Verdict != journal.VerdictVoid {
			t.Fatalf("verdict = %q, want void", res.Verdict)
		}
	})
	t.Run("missing baseline sample voids", func(t *testing.T) {
		p := greenPlan()
		p.Probes = append(p.Probes, Probe{Name: "queue-depth", Expect: "0"})
		res := Grade(p, greenEvidence()) // baseline has only "ping"
		if res.Verdict != journal.VerdictVoid {
			t.Fatalf("verdict = %q, want void", res.Verdict)
		}
	})
	t.Run("negative timeline duration voids", func(t *testing.T) {
		ev := greenEvidence()
		ev.Timeline.Symptom = -time.Second
		res := Grade(greenPlan(), ev)
		if res.Verdict != journal.VerdictVoid {
			t.Fatalf("verdict = %q, want void", res.Verdict)
		}
	})
}

// TestRulePrecedence pins the documented decision order: rails refusal
// beats control, control beats no_op, no_op beats oracle corruption is NOT
// the order — corruption beats recovery tracks, and the probe track comes
// after revert closure.
func TestRulePrecedence(t *testing.T) {
	t.Run("rails refusal beats control red (aborted)", func(t *testing.T) {
		p := greenPlan()
		p.ControlGreen = false
		ev := greenEvidence()
		ev.RailsRefused = "blast-bound"
		if res := Grade(p, ev); res.Verdict != journal.VerdictAborted {
			t.Fatalf("verdict = %q, want aborted first", res.Verdict)
		}
	})
	t.Run("control red beats no_op (void)", func(t *testing.T) {
		p := greenPlan()
		p.ControlGreen = false
		ev := greenEvidence()
		ev.Landed = false
		if res := Grade(p, ev); res.Verdict != journal.VerdictVoid {
			t.Fatalf("verdict = %q, want void before no_op", res.Verdict)
		}
	})
	t.Run("oracle loss beats probe red (corrupted)", func(t *testing.T) {
		ev := greenEvidence()
		ev.Post = []Sample{{Probe: Probe{Name: "ping", Expect: "ok"}, OK: false}}
		ev.Oracles = []OracleReport{{Name: "ledger-seq", Err: errors.New("loss")}}
		if res := Grade(greenPlan(), ev); res.Verdict != journal.VerdictCorrupted {
			t.Fatalf("verdict = %q, want corrupted before the probe track", res.Verdict)
		}
	})
	t.Run("not reverted beats probe red (not_recovered)", func(t *testing.T) {
		ev := greenEvidence()
		ev.Reverted = false
		ev.Post = []Sample{{Probe: Probe{Name: "ping", Expect: "ok"}, OK: false}}
		if res := Grade(greenPlan(), ev); res.Verdict != journal.VerdictNotRecovered {
			t.Fatalf("verdict = %q, want not_recovered from the revert closure", res.Verdict)
		}
	})
}

// TestAssertionsAndBudget: liveness alone never closes the recovery loop —
// a green probe set with a failed assertion is not_recovered; a green set
// over budget is degraded (passes with a measured cost).
func TestAssertionsAndBudget(t *testing.T) {
	t.Run("green probes + failed assertion -> not_recovered", func(t *testing.T) {
		ev := greenEvidence()
		ev.Assertions = []Assertion{{Name: "queue-drained", OK: false, Detail: "3 items left"}}
		res := Grade(greenPlan(), ev)
		if res.Verdict != journal.VerdictNotRecovered {
			t.Fatalf("verdict = %q, want not_recovered", res.Verdict)
		}
		if !strings.Contains(res.Reason, "queue-drained") {
			t.Fatalf("reason %q must name the failed assertion", res.Reason)
		}
	})
	t.Run("green probes + green assertions + over budget -> degraded", func(t *testing.T) {
		ev := greenEvidence()
		ev.Timeline.Recover = 7 * time.Second
		ev.Assertions = []Assertion{{Name: "queue-drained", OK: true}}
		res := Grade(greenPlan(), ev)
		if res.Verdict != journal.VerdictDegraded {
			t.Fatalf("verdict = %q, want degraded", res.Verdict)
		}
	})
	t.Run("failed assertion over budget -> not_recovered", func(t *testing.T) {
		ev := greenEvidence()
		ev.Timeline.Recover = 7 * time.Second
		ev.Assertions = []Assertion{{Name: "queue-drained", OK: false}}
		res := Grade(greenPlan(), ev)
		if res.Verdict != journal.VerdictNotRecovered {
			t.Fatalf("verdict = %q, want not_recovered", res.Verdict)
		}
	})
	t.Run("zero budget never triggers degraded", func(t *testing.T) {
		p := greenPlan()
		p.Budget = 0 // undeclared budget: no cost measure to fail
		ev := greenEvidence()
		ev.Timeline.Recover = time.Hour
		if res := Grade(p, ev); res.Verdict != journal.VerdictRecovered {
			t.Fatalf("verdict = %q, want recovered (no budget declared)", res.Verdict)
		}
	})
}

// TestAggregateFlakyAndSingletons: N runs disagree → flaky; unanimous →
// the common verdict; zero runs → void.
func TestAggregateFlakyAndSingletons(t *testing.T) {
	rec := Result{Verdict: journal.VerdictRecovered, Timeline: Timeline{Recover: time.Second}}
	nrec := Result{Verdict: journal.VerdictNotRecovered, Timeline: Timeline{Recover: 2 * time.Second}}

	if got := Aggregate(nil); got.Verdict != journal.VerdictVoid {
		t.Fatalf("zero runs graded %q, want void", got.Verdict)
	}
	single := Aggregate([]Result{rec})
	if single.Verdict != journal.VerdictRecovered || single.Reason != rec.Reason {
		t.Fatalf("single result mutated: %+v", single)
	}
	un := Aggregate([]Result{rec, rec, rec})
	if un.Verdict != journal.VerdictRecovered {
		t.Fatalf("unanimous graded %q, want recovered", un.Verdict)
	}
	fl := Aggregate([]Result{rec, nrec, rec})
	if fl.Verdict != journal.VerdictFlaky {
		t.Fatalf("disagreement graded %q, want flaky", fl.Verdict)
	}
	if !strings.Contains(fl.Reason, "recovered") || !strings.Contains(fl.Reason, "not_recovered") {
		t.Fatalf("flaky reason %q must name the disagreeing values", fl.Reason)
	}
	if fl.Timeline != rec.Timeline {
		t.Fatalf("flaky must carry the first run's timeline, got %+v", fl.Timeline)
	}
	// flaky survives the journal envelope like every vocabulary value
	back := journalRoundTrip(t, fl.Record("0123456789abcdef", 9, 2000))
	if back.Verdict != journal.VerdictFlaky {
		t.Fatalf("flaky mutated through journal: %q", back.Verdict)
	}
}

// TestSortSamplesCanonicalOrder: samples sort by probe name then time —
// nothing derived from a verdict may depend on slice order.
func TestSortSamplesCanonicalOrder(t *testing.T) {
	base := time.Unix(0, 0)
	in := []Sample{
		{Probe: Probe{Name: "zeta"}, At: base},
		{Probe: Probe{Name: "alpha"}, At: base.Add(2 * time.Second)},
		{Probe: Probe{Name: "alpha"}, At: base.Add(1 * time.Second)},
	}
	SortSamples(in)
	want := []string{"alpha", "alpha", "zeta"}
	for i, s := range in {
		if s.Probe.Name != want[i] {
			t.Fatalf("sample %d = %q, want %q", i, s.Probe.Name, want[i])
		}
	}
	if !in[0].At.Before(in[1].At) {
		t.Fatal("same-probe samples not ordered by time")
	}
}
