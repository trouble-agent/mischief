package plane

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// fx builds a vocabulary-valid Step for fixtures, panicking if NewStep's
// closed-stage gate ever rejects one (a fixture drift would otherwise
// surface as a confusing Fold failure downstream; canonical fixture args
// make the panic unreachable, mirroring mustJSON's contract in the CLI).
func fx(step, status, detail, ts string) Step {
	s, err := NewStep(step, status, detail, ts)
	if err != nil {
		panic("plane test fixture rejected by NewStep: " + err.Error())
	}
	return s
}

// TestParseAgentIDFromSpawnOutput: the real `bunker spawn` output shape
// (the id rides after "keys/" in the key-path line) yields the agent id;
// every non-spawn output — a capacity refusal, a transport error, empty
// output — parses to "" so the caller can only ever record a named SKIP,
// never a fabricated agent.
func TestParseAgentIDFromSpawnOutput(t *testing.T) {
	cases := []struct {
		name, out, want string
	}{
		{
			name: "real spawn output shape (current daemon: Agent created line)",
			out:  "Creating agent...\nAgent created: 7bb5dcc2\n\n══════════ Connection Bundle ══════════\n\n  SSH Key:      /home/demo/.config/bunker/keys/7bb5dcc2\n═ Use `bunker exec` to run commands in this agent ═",
			want: "7bb5dcc2",
		},
		{
			name: "older daemon shape (keys/ token)",
			out:  "agent created\n  id:      keys/cefd6920\n  ttl:     4h\nssh key written: keys/cefd6920\ntarget: bunker-cefd6920@bunker-mvp",
			want: "cefd6920",
		},
		{
			name: "id-only line",
			out:  "keys/00112233ff",
			want: "00112233ff",
		},
		{
			name: "capacity refusal has no id",
			out:  "spawn: capacity exhausted (8/8)",
			want: "",
		},
		{
			name: "transport error has no id",
			out:  "server info: unauthenticated: invalid token",
			want: "",
		},
		{name: "empty output", out: "", want: ""},
		{
			name: "short token under 8 hex chars is not an id",
			out:  "keys/abc123",
			want: "",
		},
		{
			name: "marker at end of output",
			out:  "creating agent keys/",
			want: "",
		},
	}
	for _, tc := range cases {
		if got := ParseAgentID(tc.out); got != tc.want {
			t.Errorf("%s: ParseAgentID(%q) = %q, want %q", tc.name, tc.out, got, tc.want)
		}
	}
}

// TestSelftestVerdictExitContract: only exit 0 grades pass — the
// selftest CLI's own contract (green-or-skip only). Every non-zero code,
// including usage-refusal 2 and kill 137, grades fail.
func TestSelftestVerdictExitContract(t *testing.T) {
	for code, want := range map[int]string{0: StatusPass, 1: StatusFail, 2: StatusFail, 137: StatusFail, 255: StatusFail} {
		if got := SelftestVerdict(code); got != want {
			t.Errorf("SelftestVerdict(%d) = %q, want %q", code, got, want)
		}
	}
}

// TestFoldEmptyEvidenceIsNamedFail: an evidence file with zero steps is
// the zero-byte evidence class — it grades FAIL with a reason naming the
// emptiness, never a silent pass.
func TestFoldEmptyEvidenceIsNamedFail(t *testing.T) {
	status, reason, stage := Fold(nil)
	if status != StatusFail {
		t.Fatalf("empty evidence graded %q, want fail", status)
	}
	if !strings.Contains(reason, "empty evidence") {
		t.Fatalf("empty-evidence reason %q does not name the emptiness", reason)
	}
	if stage != "" {
		t.Fatalf("empty evidence has no stage, got %q", stage)
	}
}

// TestFoldLastStepIsVerdict: steps append in pipeline order; the last
// step's status is the run verdict.
func TestFoldLastStepIsVerdict(t *testing.T) {
	steps := []Step{
		fx("preflight", StatusPass, "server reachable", "0"),
		fx("ship", StatusPass, "SYNC-OK", "0"),
		fx("selftest", StatusFail, "exit 1: P-001 FAIL", "0"),
	}
	status, reason, stage := Fold(steps)
	if status != StatusFail || stage != "selftest" || !strings.Contains(reason, "P-001") {
		t.Fatalf("Fold = (%q,%q,%q), want fail/selftest/P-001 in reason", status, reason, stage)
	}

	steps = append(steps, fx("collect", StatusPass, "journal pulled 42 lines", "0"))
	if status, _, stage = Fold(steps); status != StatusPass || stage != "collect" {
		t.Fatalf("after green collect: (%q,%q), want pass/collect", status, stage)
	}
}

// TestFoldSkipWithoutReasonIsPlaneDefect: a skip row with an empty detail
// still grades skip (never pass) but its rendered reason names the plane
// defect — the "skip without a named reason" honesty rule.
func TestFoldSkipWithoutReasonIsPlaneDefect(t *testing.T) {
	steps := []Step{{Step: "spawn", Status: StatusSkip, DetailB64: "", TS: "0"}}
	status, reason, _ := Fold(steps)
	if status != StatusSkip {
		t.Fatalf("skip graded %q, want skip", status)
	}
	if !strings.Contains(reason, "plane defect") {
		t.Fatalf("reasonless skip rendered %q, want a plane-defect note", reason)
	}
}

// TestFoldUnknownStatusFailsClosed.
func TestFoldUnknownStatusFailsClosed(t *testing.T) {
	steps := []Step{{Step: "spawn", Status: "maybe", DetailB64: "", TS: "0"}}
	status, reason, _ := Fold(steps)
	if status != StatusFail || !strings.Contains(reason, "unknown status") {
		t.Fatalf("unknown status graded (%q,%q), want fail naming it", status, reason)
	}
}

// TestFoldUnknownStatusOnAnyRowFailsClosed: an invalid status on an
// EARLIER evidence row must fail the fold even when a later valid row
// grades pass — the pipeline's last row (destroy) records last, but a
// bogus preflight status is a plane defect the verdict may never
// overwrite. This is the MISCHIEF-PLANE-003 reproduction.
func TestFoldUnknownStatusOnAnyRowFailsClosed(t *testing.T) {
	bogusFirst := []Step{
		fx("preflight", "bogus", "", "0"),
		fx("destroy", StatusPass, "agent destroyed", "0"),
	}
	status, reason, stage := Fold(bogusFirst)
	if status != StatusFail {
		t.Fatalf("earlier bogus status graded %q, want fail", status)
	}
	if !strings.Contains(reason, "unknown status") || !strings.Contains(reason, "bogus") || !strings.Contains(reason, "preflight") {
		t.Fatalf("reason %q must name the unknown status and its row", reason)
	}
	if stage != "preflight" {
		t.Fatalf("stage = %q, want the offending preflight row", stage)
	}

	// Same rows, invalid status on the final row — already failed closed
	// before this fix; must keep failing.
	bogusLast := []Step{
		{Step: "preflight", Status: StatusPass, DetailB64: "", TS: "0"},
		{Step: "destroy", Status: "bogus", DetailB64: "", TS: "0"},
	}
	if status, reason, _ = Fold(bogusLast); status != StatusFail || !strings.Contains(reason, "unknown status") {
		t.Fatalf("final-row bogus status graded (%q,%q), want fail naming it", status, reason)
	}
}

// TestFoldFullyValidMultiRowFileStaysPass: the position-independent
// unknown-status gate must not touch a legitimate evidence file — rows
// graded fail or skip on EARLIER stages with a green final row still fold
// to that last verdict (steps append in pipeline order).
func TestFoldFullyValidMultiRowFileStaysPass(t *testing.T) {
	valid := []Step{
		fx("build", StatusPass, "binaries built", "0"),
		fx("preflight", StatusFail, "first preflight attempt failed, retried", "1"),
		fx("spawn", StatusPass, "Agent created: abc123", "2"),
		fx("destroy", StatusPass, "agent destroyed", "3"),
	}
	status, _, stage := Fold(valid)
	if status != StatusPass || stage != "destroy" {
		t.Fatalf("fully valid multi-row file graded (%q,%q), want pass/destroy", status, stage)
	}

	skippedEarly := []Step{
		fx("preflight", StatusSkip, "no fault target in scope", "0"),
		fx("destroy", StatusPass, "nothing to destroy", "1"),
	}
	if status, _, _ = Fold(skippedEarly); status != StatusPass {
		t.Fatalf("valid skip-then-pass file graded %q, want pass", status)
	}
}

// TestParseEvidenceDropsAndCountsCorruptLines: a truncated/corrupt JSONL
// evidence file must not silently shrink — dropped lines are counted, named,
// and the decode FAILS CLOSED (a corrupt line followed by a valid pass row
// is an incomplete run, never a gradeable file).
func TestParseEvidenceDropsAndCountsCorruptLines(t *testing.T) {
	good := fx("ship", StatusPass, "SYNC-OK", "1000")
	raw, err := json.Marshal(good)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw) + "\n{not json}\n\n  \n{\"step\":\"half\" nope\n"
	steps, dropped, err := ParseEvidence(text)
	if err == nil {
		t.Fatal("corrupt evidence decoded with a nil error — an incomplete file would grade as a plane run")
	}
	if !strings.Contains(err.Error(), "2 undecodable") || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("error %q must name the corrupt count and first offending line", err)
	}
	if len(steps) != 1 || steps[0].Step != "ship" {
		t.Fatalf("parsed %d steps (%+v), want 1 ship step", len(steps), steps)
	}
	if dropped != 2 {
		t.Fatalf("dropped = %d, want 2", dropped)
	}
}

// TestEvidenceRoundTripThroughJSONL: the driver writes rows as one JSON
// line each; the package must decode exactly what it encoded, detail
// included — the contract that lets the bash script append rows with
// printf while Go owns the meaning.
func TestEvidenceRoundTripThroughJSONL(t *testing.T) {
	step := fx("collect", StatusSkip, "agent unreachable: timeout after 180s", "1700000000")
	raw, err := json.Marshal(step)
	if err != nil {
		t.Fatal(err)
	}
	steps, dropped, err := ParseEvidence(string(raw) + "\n")
	if err != nil || dropped != 0 || len(steps) != 1 {
		t.Fatalf("round trip: %v steps=%d dropped=%d", err, len(steps), dropped)
	}
	got := steps[0]
	if got.Step != "collect" || got.Status != StatusSkip || got.TS != "1700000000" {
		t.Fatalf("round trip fields: %+v", got)
	}
	if want := "agent unreachable: timeout after 180s"; got.Detail() != want {
		t.Fatalf("detail round trip: %q, want %q", got.Detail(), want)
	}
}

// TestStepDetailUndecodableNamesItself: corrupt base64 decodes to a
// placeholder naming the corruption, never to "".
func TestStepDetailUndecodableNamesItself(t *testing.T) {
	s := Step{DetailB64: "!!!not-base64!!!"}
	if d := s.Detail(); !strings.Contains(d, "undecodable") {
		t.Fatalf("corrupt detail rendered %q", d)
	}
	if d := (Step{}).Detail(); d != "" {
		t.Fatalf("empty detail rendered %q, want empty", d)
	}
}

// TestFinalVerdictUnobtainedAgentIsNamedSkip: AC 3/4 — no agent means a
// SKIP carrying the named reason (capacity, reachability, missing CLI…),
// never a pass, never a reasonless skip.
func TestFinalVerdictUnobtainedAgentIsNamedSkip(t *testing.T) {
	status, reason := FinalVerdict(false, StatusPass, "bunker capacity exhausted (8/8) on bunker-mvp")
	if status != StatusSkip {
		t.Fatalf("unobtained agent graded %q, want skip", status)
	}
	if !strings.Contains(reason, "capacity exhausted") {
		t.Fatalf("skip reason %q lost the named blocker", reason)
	}

	// A plane defect (no reason recorded) still refuses both directions:
	// skip, with a synthesized reason that names the defect.
	status, reason = FinalVerdict(false, StatusPass, "")
	if status != StatusSkip || !strings.Contains(reason, "not obtained") {
		t.Fatalf("reasonless skip = (%q,%q), want skip naming the defect", status, reason)
	}

	// An obtained agent never upgrades a failed selftest.
	status, reason = FinalVerdict(true, StatusFail, "selftest exit 1")
	if status != StatusFail || !strings.Contains(reason, "exit 1") {
		t.Fatalf("obtained+fail = (%q,%q), want fail", status, reason)
	}

	// And a green selftest on a real agent is the one pass shape.
	if status, _ = FinalVerdict(true, StatusPass, ""); status != StatusPass {
		t.Fatalf("obtained+pass = %q, want pass", status)
	}

	// A mis-wired "selftest reported skip" fails closed.
	if status, reason = FinalVerdict(true, StatusSkip, ""); status != StatusFail || !strings.Contains(reason, "plane defect") {
		t.Fatalf("skip-report arm = (%q,%q), want fail naming the defect", status, reason)
	}
}

// TestSanitizeKeyPathNeverEmpty: evidence names key PATHS, never material,
// and a missing path renders visible rather than blank.
func TestSanitizeKeyPathNeverEmpty(t *testing.T) {
	if got := SanitizeKeyPath(""); got != "<none>" {
		t.Errorf("empty path rendered %q", got)
	}
	if got := SanitizeKeyPath("relative/key"); !strings.HasPrefix(got, "/") {
		t.Errorf("relative path not absolutized: %q", got)
	}
}

// TestDetailEncodingIsPrintableASCII: the driver's printf contract — the
// encoded payload must survive a shell single-quoted argument untouched.
func TestDetailEncodingIsPrintableASCII(t *testing.T) {
	step := fx("selftest", StatusFail, "quotes '\" backslash \\ newline \n tail -1 output", "0")
	if len(step.DetailB64) == 0 {
		t.Fatal("empty encoding")
	}
	if _, err := base64.StdEncoding.DecodeString(step.DetailB64); err != nil {
		t.Fatalf("encoding not valid base64: %v", err)
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// MISCHIEF-PLANE-001 regression tests: the closed STAGE vocabulary.
// The original defect: `plane record --step nonsense` appended a row and
// `plane fold` graded it "PLANE pass at nonsense" — an alien stage name
// rode straight through record and fold as a PASS. Now: NewStep refuses to
// build an alien-stage row, the record verb refuses the flag, ParseEvidence
// reports alien-stage rows in a file, and Fold fails closed on them.
// ═════════════════════════════════════════════════════════════════════════════

// TestNewStepRejectsUnknownStage: the constructor is the first door — an
// unknown stage builds nothing and the error names the allowed set
// (AC-11 style: the refusal says what is absent).
func TestNewStepRejectsUnknownStage(t *testing.T) {
	if s, err := NewStep("nonsense", StatusPass, "bad-step", "0"); err == nil {
		t.Fatalf("NewStep built an alien-stage row: %+v", s)
	} else if !strings.Contains(err.Error(), "unknown stage") || !strings.Contains(err.Error(), "nonsense") {
		t.Fatalf("error %q must name the unknown stage and the refusal", err)
	} else {
		for _, want := range StageVocabulary {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q must name the allowed set (missing %q)", err, want)
			}
		}
	}
	// Every canonical stage still builds — the vocabulary must stay open
	// to exactly the pipeline's stages, nothing more, nothing less.
	for _, stage := range StageVocabulary {
		if s, err := NewStep(stage, StatusPass, "ok", "0"); err != nil {
			t.Fatalf("canonical stage %q refused: %v", stage, err)
		} else if s.Step != stage {
			t.Fatalf("NewStep(%q) → %q", stage, s.Step)
		}
	}
}

// TestFoldAlienStageDoesNotPass: the fold-level regression — an evidence
// file carrying an alien stage row can never fold PASS, no matter how
// green the final row is. The fold fails closed naming the offending
// stage, at every position (first, middle, last).
func TestFoldAlienStageDoesNotPass(t *testing.T) {
	alienFirst := []Step{
		{Step: "nonsense", Status: StatusPass, DetailB64: "", TS: "0"},
		{Step: "destroy", Status: StatusPass, DetailB64: "", TS: "1"},
	}
	status, reason, stage := Fold(alienFirst)
	if status != StatusFail {
		t.Fatalf("alien-first-stage file graded %q, want fail", status)
	}
	if !strings.Contains(reason, "unknown stage") || !strings.Contains(reason, "nonsense") {
		t.Fatalf("reason %q must name the unknown stage", reason)
	}
	if stage != "nonsense" {
		t.Fatalf("stage = %q, want the offending row's stage", stage)
	}

	alienLast := []Step{
		{Step: "preflight", Status: StatusPass, DetailB64: "", TS: "0"},
		{Step: "selftest", Status: StatusPass, DetailB64: "", TS: "1"},
		{Step: "collect", Status: StatusPass, DetailB64: "", TS: "2"},
		{Step: "banana", Status: StatusPass, DetailB64: "", TS: "3"},
	}
	if status, reason, _ = Fold(alienLast); status != StatusFail || !strings.Contains(reason, "banana") {
		t.Fatalf("alien-last-stage file = (%q,%q), want fail naming banana", status, reason)
	}

	// The exact live shape from the defect report: pass at "nonsense" with
	// detail "bad-step" cannot grade pass — and a green pipeline with one
	// alien row in the middle fails too.
	repro := []Step{
		{Step: "preflight", Status: StatusPass, DetailB64: "", TS: "0"},
		{Step: "nonsense", Status: StatusPass, DetailB64: "", TS: "1"},
		{Step: "destroy", Status: StatusPass, DetailB64: "", TS: "2"},
	}
	if status, reason, _ = Fold(repro); status != StatusFail || !strings.Contains(reason, "nonsense") {
		t.Fatalf("repro shape = (%q,%q), want fail naming nonsense", status, reason)
	}
}

// TestParseEvidenceReportsAlienStageRows: the file decoder counts
// alien-stage rows and returns a non-nil error naming them (and the
// allowed set) while still passing the parsed rows through for Fold to
// grade — the caller gets both the verdict and the named finding.
func TestParseEvidenceReportsAlienStageRows(t *testing.T) {
	steps, dropped, err := ParseEvidence(
		`{"step":"nonsense","status":"pass","detail_b64":"bad-step","ts":"0"}` + "\n" +
			`{"step":"destroy","status":"pass","detail_b64":"","ts":"1"}` + "\n")
	if err == nil {
		t.Fatal("alien-stage file decoded with a nil error — the finding would be silent")
	}
	if !strings.Contains(err.Error(), "nonsense") || !strings.Contains(err.Error(), "destroy") {
		t.Fatalf("error %q must name the allowed set (which contains destroy)", err)
	}
	if len(steps) != 2 {
		t.Fatalf("rows must still parse through for Fold: %d steps", len(steps))
	}
	if dropped != 0 {
		t.Fatalf("dropped = %d, want 0 (structurally valid rows are not dropped)", dropped)
	}
	// A vocabulary-clean file still decodes with a nil error.
	clean, dropped, err := ParseEvidence(
		`{"step":"spawn","status":"pass","detail_b64":"","ts":"0"}` + "\n" +
			`{"step":"collect","status":"skip","detail_b64":"YQ==","ts":"1"}` + "\n")
	if err != nil || dropped != 0 || len(clean) != 2 {
		t.Fatalf("clean file: err=%v dropped=%d steps=%d", err, dropped, len(clean))
	}
}
