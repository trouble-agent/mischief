package main

// MISCHIEF-DF-72: the version verb owns its argument surface. Before the
// fix the dispatcher printed "mischief <sha>" and exited 0 for EVERYTHING
// after the verb token — `version --json` was not JSON and `version
// --nope` was a silent success. These arms pin the three contracts: the
// --json machine document, the named exit-2 refusal for unknown flags,
// and the unchanged plain-text default.

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestVersionVerbJSONAndRefusal (MISCHIEF-DF-72): `version --json` emits
// one parseable JSON document with a non-empty .version (rc 0); an
// unknown flag is a named refusal on stderr with exit 2 and nothing on
// stdout; bare `version` keeps the plain "mischief <sha>" line with
// rc 0; `version --help` keeps the conventional help contract (exit 0 —
// tools/check_cli_table.py drives the built binary with `version
// --help` and fails closed on any nonzero exit).
func TestVersionVerbJSONAndRefusal(t *testing.T) {
	gitSha = "df72sha0001"

	t.Run("--json parses with non-empty version", func(t *testing.T) {
		code, out := captureStdout(t, func() int { return run([]string{"version", "--json"}) })
		if code != 0 {
			t.Fatalf("version --json: exit %d, want 0\nstdout=%q", code, out)
		}
		var doc struct {
			Version string `json:"version"`
			Tool    string `json:"tool"`
		}
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("version --json stdout is not JSON: %v\nstdout=%q", err, out)
		}
		if doc.Version == "" {
			t.Fatal("version --json: .version is empty")
		}
		if doc.Version != gitSha {
			t.Fatalf("version --json: .version = %q, want the stamped sha %q", doc.Version, gitSha)
		}
		if doc.Tool != "mischief" {
			t.Fatalf("version --json: .tool = %q, want \"mischief\"", doc.Tool)
		}
	})

	t.Run("unknown flag refuses with exit 2", func(t *testing.T) {
		code, stdout, stderr := captureBoth(t, func() int { return run([]string{"version", "--nope"}) })
		if code != 2 {
			t.Fatalf("version --nope: exit %d, want 2\nstdout=%q\nstderr=%q", code, stdout, stderr)
		}
		if strings.TrimSpace(stdout) != "" {
			t.Fatalf("version --nope: refusal must keep stdout empty, got %q", stdout)
		}
		if !strings.Contains(stderr, "--nope") {
			t.Fatalf("version --nope: refusal does not name the flag: %q", stderr)
		}
		if !strings.Contains(stderr, "--json") || !strings.Contains(stderr, "--version") {
			t.Fatalf("version --nope: refusal does not name the supported flags: %q", stderr)
		}
	})

	t.Run("bare version stays plain exit 0", func(t *testing.T) {
		code, out := captureStdout(t, func() int { return run([]string{"version"}) })
		if code != 0 {
			t.Fatalf("version: exit %d, want 0", code)
		}
		if got := strings.TrimSpace(out); got != "mischief "+gitSha {
			t.Fatalf("version output changed: %q, want %q", got, "mischief "+gitSha)
		}
	})

	t.Run("--help stays exit 0", func(t *testing.T) {
		code, out := captureStdout(t, func() int { return run([]string{"version", "--help"}) })
		if code != 0 {
			t.Fatalf("version --help: exit %d, want 0", code)
		}
		if got := strings.TrimSpace(out); got != "mischief "+gitSha {
			t.Fatalf("version --help output: %q, want %q", got, "mischief "+gitSha)
		}
	})
}

// ch:trace row=MISCHIEF-DF-72 spec=none:dogfood-row wave=mischief-foreman-2026-10-28-19-31-37.json#MISCHIEF-DF-72 test=cmd/mischief/df72_version_test.go::TestVersionVerbJSONAndRefusal doc=none:cli-help-output prompt=/tmp/mf-20261028-1931-MISCHIEF-DF-72-brief.txt evidence=commit witness=none:unit-test verdict=none:pending-foreman-judge commit=self memory=none:board-row
// STATUS: complete
// TIER: T2 (verb change + targeted test + full package suite + binary probes invoked and observed)
// EVIDENCE: commit
