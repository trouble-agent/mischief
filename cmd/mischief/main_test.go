package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVersionVerbPrintsStampedSha: `mischief version` prints exactly the
// stamped sha (the Makefile's -X main.gitSha value). An unstamped build
// prints "unknown" verbatim — never a fabricated hash.
func TestVersionVerbPrintsStampedSha(t *testing.T) {
	gitSha = "abc123def456"
	code, out := captureStdout(t, func() int { return run([]string{"version"}) })
	if code != 0 {
		t.Fatalf("version exit: %d", code)
	}
	if got := strings.TrimSpace(out); got != "mischief abc123def456" {
		t.Fatalf("version output: %q", got)
	}
	// --version alias
	code, out = captureStdout(t, func() int { return run([]string{"--version"}) })
	if code != 0 || strings.TrimSpace(out) != "mischief abc123def456" {
		t.Fatalf("--version: rc=%d out=%q", code, out)
	}
}

// TestUnknownVerbExit2: an unknown verb exits 2 with usage on stderr.
func TestUnknownVerbExit2(t *testing.T) {
	if code := run([]string{"frobnicate"}); code != 2 {
		t.Fatalf("unknown verb exit: %d, want 2", code)
	}
	if code := run(nil); code != 2 {
		t.Fatalf("no verb exit: %d, want 2", code)
	}
}

// TestPlanRefusesUnsanctionedHost (the cmd-level sanction gate): on this
// unsanctioned test host (no marker, no env), `mischief plan` exits 2
// fail-closed and prints the refusal naming the host — nothing landed.
func TestPlanRefusesUnsanctionedHost(t *testing.T) {
	exp := filepath.Join(t.TempDir(), "exp.yaml")
	if err := os.WriteFile(exp, []byte("id: x\ntarget: {selector: \"host:.\", scratch: true}\nfaults:\n  - primitive: P\n    params: {}\n    ttl: 1s\n    landed_proof: {kind: k, check: c}\nprobe:\n  - {name: p, cmd: \"true\", expect: \"0\"}\nbudget: {recover_within: 1s}\nseed: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// the worker's test env named MISCHIEF_SANCTION_MARKER/HOST; the judged
	// SPEC-13 package (7b29502) owns MISCHIEF_SANCTION_FILE — forced to a
	// nonexistent file, the env value is empty in the test process.
	t.Setenv("MISCHIEF_SANCTION_FILE", "/nonexistent/sanction-marker")
	code, stderr := captureStderr(t, func() int { return run([]string{"plan", "-f", exp}) })
	if code != 2 {
		t.Fatalf("plan on unsanctioned host: exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "not sanctioned") || !strings.Contains(stderr, "/nonexistent/sanction-marker") {
		t.Fatalf("refusal does not name the missing marker: %s", stderr)
	}
}

// captureStdout swaps os.Stdout for a pipe around fn.
func captureStdout(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	return capture(t, &os.Stdout, fn)
}

// captureStderr swaps os.Stderr for a pipe around fn.
func captureStderr(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	return capture(t, &os.Stderr, fn)
}

func capture(t *testing.T, target **os.File, fn func() int) (int, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := *target
	*target = w
	code := fn()
	*target = saved
	w.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 512)
	for {
		n, err := r.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	r.Close()
	return code, string(buf)
}
