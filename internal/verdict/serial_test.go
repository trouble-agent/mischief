package verdict

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/trouble-agent/mischief/internal/journal"
)

// journalRoundTrip drives one Record through the journal's own wire hop —
// MarshalJSON (the exact bytes a journal line receives, validation
// included, fail-closed on write) → UnmarshalJSON (read-side validation
// included). Returns the read-back record.
func journalRoundTrip(t *testing.T, r journal.Record) journal.Record {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("journal write-side marshal: %v", err)
	}
	var back journal.Record
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("journal read-side unmarshal: %v", err)
	}
	return back
}

// TestAC9EveryVerdictJSONRoundTripByteStable: for every value of the
// closed vocabulary, the graded result's journal envelope survives
// write→read with the verdict byte-identical, the wire line carrying the
// value verbatim, and a re-serialise of the read-back record producing
// identical bytes (stability, not just equality).
func TestAC9EveryVerdictJSONRoundTripByteStable(t *testing.T) {
	all := []journal.Verdict{
		journal.VerdictRecovered, journal.VerdictDegraded, journal.VerdictNotRecovered,
		journal.VerdictHung, journal.VerdictCorrupted, journal.VerdictNoOp,
		journal.VerdictAborted, journal.VerdictFlaky, journal.VerdictVoid,
	}
	if len(all) != 9 {
		t.Fatalf("vocabulary size = %d, want the closed nine (§6.4)", len(all))
	}
	for _, v := range all {
		t.Run(v.String(), func(t *testing.T) {
			res := Result{Verdict: v, Reason: "test: " + v.String()}
			rec := res.Record("0123456789abcdef", 3, 1700000000)
			back := journalRoundTrip(t, rec)
			if back.Verdict != v {
				t.Fatalf("verdict mutated through journal: %q -> %q", v, back.Verdict)
			}
			// byte-stability: the read-back record re-serialises to the
			// same bytes (a verdict value that changes representation on
			// the second hop would break the AC-9 chain downstream)
			b1, err := json.Marshal(rec)
			if err != nil {
				t.Fatal(err)
			}
			b2, err := json.Marshal(back)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(b1, b2) {
				t.Fatalf("re-serialise differs:\n first: %s\n second: %s", b1, b2)
			}
			// the value appears verbatim on the wire (no encoding drift)
			if !strings.Contains(string(b1), `"verdict":"`+v.String()+`"`) {
				t.Fatalf("wire line %s does not carry %q verbatim", b1, v)
			}
		})
	}
}

// TestAC9UnverifiedNoteSurvivesEnvelope: the AC-15 coercion note rides the
// record's fields, not the closed verdict field — and survives the hop.
func TestAC9UnverifiedNoteSurvivesEnvelope(t *testing.T) {
	p := greenPlan()
	p.IntegrityNames = nil
	res := Grade(p, greenEvidence())
	if res.Unverified == "" {
		t.Fatal("expected an AC-15 refusal")
	}
	rec := res.Record("0123456789abcdef", 1, 1000)
	back := journalRoundTrip(t, rec)
	if back.Verdict != journal.VerdictNotRecovered {
		t.Fatalf("coerced verdict mutated: %q", back.Verdict)
	}
	if got := back.Fields["unverified"]; got != res.Unverified.String() {
		t.Fatalf("unverified note mutated: %q -> %q", res.Unverified.String(), got)
	}
}

// TestAC9GobRoundTripEveryVerdict: the gob half of AC-9 (CLI pipe / IPC
// transports are gob-shaped in this fleet). Every verdict value survives a
// gob encode→decode of the Result byte-identically, timings included.
func TestAC9GobRoundTripEveryVerdict(t *testing.T) {
	all := []journal.Verdict{
		journal.VerdictRecovered, journal.VerdictDegraded, journal.VerdictNotRecovered,
		journal.VerdictHung, journal.VerdictCorrupted, journal.VerdictNoOp,
		journal.VerdictAborted, journal.VerdictFlaky, journal.VerdictVoid,
	}
	for _, v := range all {
		in := Result{
			Verdict: v,
			Timeline: Timeline{
				Landed:  1500 * time.Millisecond,
				Symptom: 5 * time.Millisecond,
				Detect:  250 * time.Millisecond,
				Recover: 3 * time.Second,
			},
			Reason:     "gob: " + v.String(),
			Unverified: Unverified("integrity"),
		}
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(in); err != nil {
			t.Fatalf("%s: gob encode: %v", v, err)
		}
		var out Result
		if err := gob.NewDecoder(&buf).Decode(&out); err != nil {
			t.Fatalf("%s: gob decode: %v", v, err)
		}
		if out.Verdict != in.Verdict {
			t.Fatalf("%s: verdict mutated through gob: %q -> %q", v, in.Verdict, out.Verdict)
		}
		if out.Timeline != in.Timeline {
			t.Fatalf("%s: timeline mutated through gob: %+v -> %+v", v, in.Timeline, out.Timeline)
		}
		if out.Reason != in.Reason || out.Unverified != in.Unverified {
			t.Fatalf("%s: reason/unverified mutated through gob", v)
		}
	}
}

// TestTimelineJSONRoundTrip: the timing model rides the envelope — the
// four durations survive marshal→unmarshal as the same durations, and
// Timeline.String stays stable.
func TestTimelineJSONRoundTrip(t *testing.T) {
	in := Timeline{
		Landed:  time.Second,
		Symptom: 10 * time.Millisecond,
		Detect:  250 * time.Millisecond,
		Recover: 2 * time.Second,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Timeline
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("timeline mutated: %+v -> %+v", in, out)
	}
	s := in.String()
	want := "t_landed=1s t_symptom=10ms t_detect=250ms t_recover=2s"
	if s != want {
		t.Fatalf("String() = %q, want %q", s, want)
	}
	// the canonical form is parseable back into the same timeline
	tl, err := ParseTimeline(in.Landed.String(), in.Symptom.String(), in.Detect.String(), in.Recover.String())
	if err != nil {
		t.Fatal(err)
	}
	if tl != in {
		t.Fatalf("ParseTimeline(String()) != in: %+v vs %+v", tl, in)
	}
}

// TestVerdictStringIsTheJournaledForm: String() renders exactly the
// journaled spelling — the CLI prints what the journal carries (AC-9's
// last hop).
func TestVerdictStringIsTheJournaledForm(t *testing.T) {
	for _, v := range []journal.Verdict{
		journal.VerdictRecovered, journal.VerdictDegraded, journal.VerdictNotRecovered,
		journal.VerdictHung, journal.VerdictCorrupted, journal.VerdictNoOp,
		journal.VerdictAborted, journal.VerdictFlaky, journal.VerdictVoid,
	} {
		if v.String() != string(v) {
			t.Fatalf("%q: String() drifted from the journaled form", v)
		}
	}
	// an out-of-vocabulary value is still carried as its literal text —
	// never rewritten — but the journal's validation refuses it on write
	rogue := journal.Verdict("IMMATERIAL")
	if rogue.String() != "IMMATERIAL" {
		t.Fatal("out-of-vocabulary value rewritten on render")
	}
	rec := (Result{Verdict: rogue, Reason: "x"}).Record("0123456789abcdef", 0, 1)
	if _, err := rec.MarshalJSON(); err == nil {
		t.Fatal("out-of-vocabulary verdict accepted by the journal write path")
	}
}

// TestRecordCarriesTimingsAndReason: the envelope's fields carry the four
// timings and the reason so a findings row can quote the timing model
// without re-deriving it.
func TestRecordCarriesTimingsAndReason(t *testing.T) {
	res := Result{
		Verdict: journal.VerdictRecovered,
		Timeline: Timeline{
			Landed:  2 * time.Second,
			Symptom: 10 * time.Millisecond,
			Recover: 3 * time.Second,
		},
		Reason: "the deciding rule",
	}
	back := journalRoundTrip(t, res.Record("0123456789abcdef", 5, 42))
	want := map[string]string{
		"t_landed":  "2s",
		"t_symptom": "10ms",
		"t_detect":  "0s",
		"t_recover": "3s",
		"reason":    "the deciding rule",
	}
	for k, v := range want {
		if got := back.Fields[k]; got != v {
			t.Fatalf("field %s = %q, want %q", k, got, v)
		}
	}
	if fmt.Sprint(back.Seq) != "5" || back.TS != 42 {
		t.Fatalf("seq/ts mutated: seq=%d ts=%d", back.Seq, back.TS)
	}
}
