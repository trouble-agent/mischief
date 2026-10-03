package verdict

import (
	"strings"
	"testing"
	"time"

	"github.com/trouble-agent/mischief/internal/journal"
)

// TestParseTimelineOK: declared durations parse; absent fields are legal
// zeros; the field name of an unparsable value is named in the error.
func TestParseTimelineOK(t *testing.T) {
	tl, err := ParseTimeline("100ms", "10ms", "", "2s")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := Timeline{Landed: 100 * time.Millisecond, Symptom: 10 * time.Millisecond, Recover: 2 * time.Second}
	if tl != want {
		t.Fatalf("timeline = %+v, want %+v", tl, want)
	}
	if _, err := ParseTimeline("100ms", "soon", "", "2s"); err == nil {
		t.Fatal("unparsable duration accepted")
	} else if !strings.Contains(err.Error(), "t_symptom") {
		t.Fatalf("error %q must name the offending field", err)
	}
}

// TestTimelineValidateMonotonicSane: the monotonic-sanity rule per stage
// (each stage starts at a common instant, so each must be non-negative);
// zero is legal (a stage that produced no observation).
func TestTimelineValidateMonotonicSane(t *testing.T) {
	cases := []struct {
		name    string
		tl      Timeline
		wantErr bool
	}{
		{"all-zero", Timeline{}, false},
		{"positive", Timeline{Landed: time.Second, Symptom: time.Millisecond, Detect: time.Millisecond, Recover: time.Second}, false},
		{"negative-landed", Timeline{Landed: -time.Second}, true},
		{"negative-symptom", Timeline{Symptom: -time.Nanosecond}, true},
		{"negative-detect", Timeline{Detect: -time.Hour}, true},
		{"negative-recover", Timeline{Recover: -time.Millisecond}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.tl.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestTimingAttachedToEveryVerdict: every vocabulary verdict carries the
// timing model through grading (SPEC-05: durations attached to every
// verdict), and a voided run still carries the measured timeline (the
// evidence survives the harness-defect verdict).
func TestTimingAttachedToEveryVerdict(t *testing.T) {
	tl := Timeline{Landed: time.Second, Symptom: 10 * time.Millisecond, Recover: time.Second}
	mk := func(mut func(*Plan, *Evidence)) Result {
		p := greenPlan()
		ev := greenEvidence()
		ev.Timeline = tl
		if mut != nil {
			mut(&p, &ev)
		}
		return Grade(p, ev)
	}
	runs := map[string]Result{
		"recovered":     mk(nil),
		"void":          mk(func(p *Plan, e *Evidence) { p.ControlGreen = false }),
		"no_op":         mk(func(p *Plan, e *Evidence) { e.Landed = false }),
		"corrupted":     mk(func(p *Plan, e *Evidence) { e.Oracles = append(e.Oracles, OracleReport{Name: "o", Err: errLoss{}}) }),
		"not_recovered": mk(func(p *Plan, e *Evidence) { e.Post = nil }),
		"aborted":       mk(func(p *Plan, e *Evidence) { e.RailsRefused = "x" }),
		"flaky": Aggregate([]Result{
			{Verdict: journal.VerdictRecovered, Timeline: tl},
			{Verdict: journal.VerdictNotRecovered, Timeline: tl},
		}),
	}
	for name, res := range runs {
		if res.Timeline != tl {
			t.Fatalf("%s: timeline not attached: %+v, want %+v", name, res.Timeline, tl)
		}
		if err := res.Timeline.Validate(); err != nil {
			t.Fatalf("%s: attached timeline insane: %v", name, err)
		}
	}
	// degraded (measured over budget): the attached timeline is the
	// MEASURED (over-budget) one — the cost is carried, not trimmed.
	pd := greenPlan()
	ed := greenEvidence()
	over := Timeline{Landed: time.Second, Symptom: 10 * time.Millisecond, Recover: 99 * time.Second}
	ed.Timeline = over
	ed.Assertions = []Assertion{{Name: "queue-drained", OK: true}}
	if res := Grade(pd, ed); res.Verdict != journal.VerdictDegraded {
		t.Fatalf("degraded scenario graded %q", res.Verdict)
	} else if res.Timeline != over {
		t.Fatalf("degraded: timeline not attached: %+v, want %+v", res.Timeline, over)
	}
	// hung through the hung-scenario shape too (empty post set, probes
	// declared): timeline still attached
	p := greenPlan()
	ev := greenEvidence()
	ev.Timeline = tl
	ev.Post = nil
	if res := Grade(p, ev); res.Verdict != journal.VerdictHung {
		t.Fatalf("hung scenario graded %q", res.Verdict)
	} else if res.Timeline != tl {
		t.Fatalf("hung: timeline not attached: %+v", res.Timeline)
	}
}

// errLoss is a tiny error for oracle-loss scenarios.
type errLoss struct{}

func (errLoss) Error() string { return "loss" }
