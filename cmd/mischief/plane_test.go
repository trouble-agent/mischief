package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/plane"
)

// plane_test.go — the `mischief plane` verb contract (MSF-021): the
// evidence CLI the bunker-plane driver delegates to.

// capturePlaneOutput runs fn capturing both streams (the plane verb
// writes records' human text to stdout and refusals to stderr).
func capturePlaneOutput(t *testing.T, fn func() int) (code int, stdout, stderr string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	defer func() {
		os.Stdout, os.Stderr = oldOut, oldErr
	}()
	code = fn()
	wOut.Close()
	wErr.Close()
	outBody := make(chan string, 1)
	errBody := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(rOut)
		outBody <- string(b)
	}()
	go func() {
		b, _ := io.ReadAll(rErr)
		errBody <- string(b)
	}()
	return code, <-outBody, <-errBody
}

func TestPlaneVerbDispatchAndRefusals(t *testing.T) {
	if code := run([]string{"plane"}); code != 2 {
		t.Fatalf("bare plane exit %d, want 2", code)
	}
	if code := run([]string{"plane", "teleport"}); code != 2 {
		t.Fatalf("unknown subcommand exit %d, want 2", code)
	}
	// record without required flags
	if code := run([]string{"plane", "record"}); code != 2 {
		t.Fatalf("flagless record exit %d, want 2", code)
	}
	// record with a bogus status is a usage refusal, not a bad row
	code, _, stderr := capturePlaneOutput(t, func() int {
		return run([]string{"plane", "record", "--file", filepath.Join(t.TempDir(), "ev.jsonl"), "--step", "spawn", "--status", "maybe"})
	})
	if code != 2 {
		t.Fatalf("bogus status exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "unknown --status") {
		t.Fatalf("stderr %q does not name the status refusal", stderr)
	}
	// selftest-verdict without --exit-code
	if code := run([]string{"plane", "selftest-verdict"}); code != 2 {
		t.Fatalf("flagless selftest-verdict exit %d, want 2", code)
	}
}

func TestPlaneParseAgentIDSubcommand(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "spawn.txt")
	if err := os.WriteFile(in, []byte("agent created\n  id: keys/cefd6920\nttl: 4h\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := capturePlaneOutput(t, func() int {
		return run([]string{"plane", "parse-agent-id", "--in", in})
	})
	if code != 0 || strings.TrimSpace(out) != "cefd6920" {
		t.Fatalf("parse-agent-id = (%d, %q), want (0, cefd6920)", code, out)
	}

	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, []byte("capacity exhausted (8/8)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "parse-agent-id", "--in", empty})
	})
	// Absence is DATA: exit 0 with an empty id line.
	if code != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("absent id = (%d, %q), want (0, \"\")", code, out)
	}

	// Missing input file is a real error (1), not silence.
	code, _, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "parse-agent-id", "--in", filepath.Join(dir, "nope.txt")})
	})
	if code != 1 {
		t.Fatalf("missing input exit %d, want 1", code)
	}
}

func TestPlaneRecordAppendsJSONLRows(t *testing.T) {
	ev := filepath.Join(t.TempDir(), "ev.jsonl")
	code, _, _ := capturePlaneOutput(t, func() int {
		return run([]string{"plane", "record", "--file", ev, "--step", "spawn", "--status", "skip", "--detail", "capacity exhausted (8/8)"})
	})
	if code != 0 {
		t.Fatalf("record exit %d", code)
	}
	// A second row must APPEND, not truncate.
	code, _, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "record", "--file", ev, "--step", "preflight", "--status", "pass", "--detail", "server reachable"})
	})
	if code != 0 {
		t.Fatalf("second record exit %d", code)
	}
	body, err := os.ReadFile(ev)
	if err != nil {
		t.Fatal(err)
	}
	steps, dropped, err := plane.ParseEvidence(string(body))
	if err != nil || dropped != 0 || len(steps) != 2 {
		t.Fatalf("evidence parse: %v steps=%d dropped=%d", err, len(steps), dropped)
	}
	if steps[0].Step != "spawn" || steps[0].Status != plane.StatusSkip || steps[0].Detail() != "capacity exhausted (8/8)" {
		t.Fatalf("row 0: %+v detail=%q", steps[0], steps[0].Detail())
	}
	if steps[1].Step != "preflight" || steps[1].TS == "" {
		t.Fatalf("row 1 missing fields: %+v", steps[1])
	}
}

func TestPlaneSelftestVerdictSubcommand(t *testing.T) {
	code, out, _ := capturePlaneOutput(t, func() int {
		return run([]string{"plane", "selftest-verdict", "--exit-code", "0"})
	})
	if code != 0 || strings.TrimSpace(out) != plane.StatusPass {
		t.Fatalf("exit 0 → (%d,%q), want pass", code, out)
	}
	code, out, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "selftest-verdict", "--exit-code", "137"})
	})
	if code != 0 || strings.TrimSpace(out) != plane.StatusFail {
		t.Fatalf("exit 137 → (%d,%q), want fail", code, out)
	}
}

func TestPlaneFoldSubcommand(t *testing.T) {
	dir := t.TempDir()

	// Empty file: named FAIL, exit 1.
	ev := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(ev, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := capturePlaneOutput(t, func() int {
		return run([]string{"plane", "fold", "--file", ev})
	})
	if code != 1 || !strings.Contains(out, "PLANE fail") || !strings.Contains(out, "empty evidence") {
		t.Fatalf("empty fold = (%d, %q)", code, out)
	}

	// Unreadable file: named FAIL, exit 1 — not a usage error.
	code, out, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "fold", "--file", filepath.Join(dir, "absent.jsonl")})
	})
	if code != 1 || !strings.Contains(out, "unreadable") {
		t.Fatalf("absent fold = (%d, %q)", code, out)
	}

	// A reasonless skip still grades skip (exit 3) but names the defect.
	evSkip := filepath.Join(dir, "skip.jsonl")
	code, _, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "record", "--file", evSkip, "--step", "spawn", "--status", "skip"})
	})
	if code != 0 {
		t.Fatalf("skip record exit %d", code)
	}
	code, out, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "fold", "--file", evSkip})
	})
	if code != 3 || !strings.Contains(out, "PLANE skip") || !strings.Contains(out, "plane defect") {
		t.Fatalf("reasonless-skip fold = (%d, %q)", code, out)
	}

	// The green pipeline: every stage pass → PLANE pass, exit 0.
	evGreen := filepath.Join(dir, "green.jsonl")
	for _, row := range [][2]string{
		{"preflight", "server reachable"},
		{"ship", "SYNC-OK"},
		{"selftest", "exit 0"},
		{"collect", "journal pulled"},
		{"destroy", "TEARDOWN-GONE"},
	} {
		code, _, _ = capturePlaneOutput(t, func() int {
			return run([]string{"plane", "record", "--file", evGreen, "--step", row[0], "--status", "pass", "--detail", row[1]})
		})
		if code != 0 {
			t.Fatalf("green row %v exit %d", row, code)
		}
	}
	code, out, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "fold", "--file", evGreen})
	})
	if code != 0 || !strings.Contains(out, "PLANE pass at destroy") {
		t.Fatalf("green fold = (%d, %q)", code, out)
	}

	// stdin path (--file -): the driver's pipe-shaped grading arm.
	stdinFile, err := os.Open(evGreen)
	if err != nil {
		t.Fatal(err)
	}
	defer stdinFile.Close()
	oldStdin := os.Stdin
	os.Stdin = stdinFile
	code, out, _ = capturePlaneOutput(t, func() int {
		return run([]string{"plane", "fold", "--file", "-"})
	})
	os.Stdin = oldStdin
	if code != 0 || !strings.Contains(out, "PLANE pass at destroy") {
		t.Fatalf("stdin fold = (%d, %q)", code, out)
	}
}
