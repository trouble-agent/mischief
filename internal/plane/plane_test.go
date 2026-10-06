package plane

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

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
			want:  "7bb5dcc2",
		},
		{
			name: "older daemon shape (keys/ token)",
			out:  "agent created\n  id:      keys/cefd6920\n  ttl:     4h\nssh key written: keys/cefd6920\ntarget: bunker-cefd6920@bunker-mvp",
			want:  "cefd6920",
		},
		{
			name: "id-only line",
			out:  "keys/00112233ff",
			want:  "00112233ff",
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
		NewStep("preflight", StatusPass, "server reachable", "0"),
		NewStep("ship", StatusPass, "SYNC-OK", "0"),
		NewStep("selftest", StatusFail, "exit 1: P-001 FAIL", "0"),
	}
	status, reason, stage := Fold(steps)
	if status != StatusFail || stage != "selftest" || !strings.Contains(reason, "P-001") {
		t.Fatalf("Fold = (%q,%q,%q), want fail/selftest/P-001 in reason", status, reason, stage)
	}

	steps = append(steps, NewStep("collect", StatusPass, "journal pulled 42 lines", "0"))
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

// TestParseEvidenceDropsAndCountsCorruptLines: a truncated/corrupt JSONL
// evidence file must not silently shrink — dropped lines are counted and
// named.
func TestParseEvidenceDropsAndCountsCorruptLines(t *testing.T) {
	good := NewStep("ship", StatusPass, "SYNC-OK", "1000")
	body := good.DetailB64 // not used directly; build lines by marshalling
	raw, err := json.Marshal(good)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw) + "\n{not json}\n\n  \n{\"step\":\"half\" nope\n"
	steps, dropped, err := ParseEvidence(text)
	if err != nil {
		t.Fatalf("ParseEvidence: %v", err)
	}
	if len(steps) != 1 || steps[0].Step != "ship" {
		t.Fatalf("parsed %d steps (%+v), want 1 ship step", len(steps), steps)
	}
	if dropped != 2 {
		t.Fatalf("dropped = %d, want 2", dropped)
	}
	_ = body
}

// TestEvidenceRoundTripThroughJSONL: the driver writes rows as one JSON
// line each; the package must decode exactly what it encoded, detail
// included — the contract that lets the bash script append rows with
// printf while Go owns the meaning.
func TestEvidenceRoundTripThroughJSONL(t *testing.T) {
	step := NewStep("collect", StatusSkip, "agent unreachable: timeout after 180s", "1700000000")
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
	step := NewStep("selftest", StatusFail, "quotes '\" backslash \\ newline \n tail -1 output", "0")
	if len(step.DetailB64) == 0 {
		t.Fatal("empty encoding")
	}
	if _, err := base64.StdEncoding.DecodeString(step.DetailB64); err != nil {
		t.Fatalf("encoding not valid base64: %v", err)
	}
}
