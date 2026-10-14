package plane

import (
	"encoding/json"
	"strings"
	"testing"
)

// fxZero builds a green row for a stage (shared with the existing tests'
// fixtures where a full Step is needed inline).
func fxZero(step string) Step {
	s, err := NewStep(step, StatusPass, "", "0")
	if err != nil {
		panic(err)
	}
	return s
}

func rowLine(s Step) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestFoldCorruptLineThenValidPassFailsClosed: the live repro — a garbage
// line followed by a valid selftest pass row must not grade the fold as a
// pass. ParseEvidence fails closed (named count + first offending line) and
// Fold itself, when fed the surviving steps, is graded FAIL by the caller
// contract (cmd/mischief demotes a pass under a decode error).
func TestFoldCorruptLineThenValidPassFailsClosed(t *testing.T) {
	_, dropped, err := ParseEvidence("not-json\n" + rowLine(fxZero("selftest")) + "\n")
	if err == nil {
		t.Fatal("corrupt line + valid pass row decoded with a nil error — incomplete evidence graded")
	}
	if dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
	if !strings.Contains(err.Error(), "1 undecodable") || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("error %q must name count and first offending line", err)
	}
}

// TestParseEvidenceValidFileStillClean: fully valid evidence remains a nil
// error with zero dropped lines — the closure must not over-tighten.
func TestParseEvidenceValidFileStillClean(t *testing.T) {
	steps, dropped, err := ParseEvidence(
		rowLine(fxZero("preflight")) + "\n" +
			rowLine(fxZero("ship")) + "\n" +
			rowLine(fxZero("selftest")) + "\n")
	if err != nil || dropped != 0 || len(steps) != 3 {
		t.Fatalf("valid file: err=%v dropped=%d steps=%d", err, dropped, len(steps))
	}
	if status, _, stage := Fold(steps); status != StatusPass || stage != "selftest" {
		t.Fatalf("valid file folded to %q at %q, want pass at selftest", status, stage)
	}
}

// TestFoldBlankLinesAreNotCorruption: blank/whitespace-only lines are the
// JSONL idiom, not evidence corruption — they must not fail the fold.
func TestFoldBlankLinesAreNotCorruption(t *testing.T) {
	steps, dropped, err := ParseEvidence("\n" + rowLine(fxZero("spawn")) + "\n\n   \n" + rowLine(fxZero("destroy")) + "\n\n")
	if err != nil || dropped != 0 || len(steps) != 2 {
		t.Fatalf("blank lines treated as corruption: err=%v dropped=%d steps=%d", err, dropped, len(steps))
	}
}
