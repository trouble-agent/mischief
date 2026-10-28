package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// MISCHIEF-DF-56: verb failure lines must not stutter. The executors frame
// their own failures ("install: write ...", "uninstall: rmdir ...",
// "reverter: arm: ..."), so the CLI's verb wrapper that adds its "verb:"
// prefix must collapse the result when the same token would land twice in a
// row — `uninstall: uninstall: rmdir ...` reads as a typo, not as layering.
// The rule is narrow: the same token never twice adjacently; a real call
// chain keeps one token per layer ("arm: reverter: arm: check: ..." is
// legal; "uninstall: uninstall:" is not).

// TestErrorPrefixNoStutter pins the printer helper: a doubled adjacent verb
// token collapses to one; a single executor prefix and a multi-layer chain
// without an adjacent repeat keep their shape.
func TestErrorPrefixNoStutter(t *testing.T) {
	cases := []struct {
		name string
		verb string
		err  error
		want string // exact rendered line
	}{
		{
			name: "uninstall executor double collapses",
			verb: "uninstall",
			err:  errors.New("uninstall: rmdir /opt/mischief: directory not empty"),
			want: "uninstall: rmdir /opt/mischief: directory not empty",
		},
		{
			name: "install executor double collapses",
			verb: "install",
			err:  errors.New("install: write /opt/mischief/x: permission denied"),
			want: "install: write /opt/mischief/x: permission denied",
		},
		{
			name: "bare executor error gains the verb prefix once",
			verb: "status",
			err:  errors.New("journal unreadable"),
			want: "status: journal unreadable",
		},
		{
			name: "multi-layer chain without adjacent repeat keeps its shape",
			verb: "arm",
			err:  errors.New("reverter: arm: check: no such file"),
			want: "arm: reverter: arm: check: no such file",
		},
		{
			name: "non-adjacent same token stays (different layers)",
			verb: "revert",
			err:  errors.New("reverter: hold: revert refused"),
			want: "revert: reverter: hold: revert refused",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := verbErr(tc.verb, tc.err)
			if got != tc.want {
				t.Fatalf("verbErr(%q, %v) = %q, want %q", tc.verb, tc.err, got, tc.want)
			}
			if !strings.HasPrefix(got, tc.verb+":") {
				t.Fatalf("rendered line %q lacks the verb prefix", got)
			}
			if strings.Contains(got, ": "+tc.verb+": "+tc.verb+":") ||
				strings.HasPrefix(got, tc.verb+": "+tc.verb+":") {
				t.Fatalf("rendered line %q stutters the verb token %q", got, tc.verb)
			}
		})
	}
}

// TestUninstallCLINoStutter drives the real verb end to end: a prefix dir
// holding residue makes the uninstall executor's final rmdir fail (the
// error it returns already reads "uninstall: rmdir ..."), and the CLI
// wrapper must print the verb once, not twice. The control arm pins the
// non-error surface: the same invocation's dry-run plan output keeps its
// existing uninstall lines (no behavior change beyond the collapse).
func TestUninstallCLINoStutter(t *testing.T) {
	prefix := t.TempDir()
	residue := filepath.Join(prefix, "stray.txt")
	if err := os.WriteFile(residue, []byte("uninstall refuses to eat this\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stderr := captureStderr(t, func() int {
		return run([]string{"uninstall", "--prefix", prefix, "--runs-dir", filepath.Join(t.TempDir(), "absent-runs")})
	})
	if code != 1 {
		t.Fatalf("uninstall over residue: exit %d, want 1\nstderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "uninstall: ") {
		t.Fatalf("expected an uninstall-scoped executor failure on stderr, got:\n%s", stderr)
	}
	if strings.Contains(stderr, "uninstall: uninstall:") {
		t.Fatalf("uninstall failure line stutters the verb:\n%s", stderr)
	}

	// Control: the dry-run surface is unchanged (plan lines, exit 0).
	code, stdout := captureStdout(t, func() int {
		return run([]string{"uninstall", "--prefix", prefix, "--dry-run"})
	})
	if code != 0 {
		t.Fatalf("uninstall --dry-run: exit %d, want 0\nstdout=%q", code, stdout)
	}
	if !strings.Contains(stdout, "uninstall") || !strings.Contains(stdout, prefix) {
		t.Fatalf("dry-run plan lost its uninstall/prefix lines:\n%s", stdout)
	}
}

// ch:trace row=MISCHIEF-DF-56 spec=none:dogfood-row wave=mischief-foreman-2026-10-28-19-31-37.json#MISCHIEF-DF-56 test=cmd/mischief/df56_test.go::TestErrorPrefixNoStutter doc=none:cli-help-output prompt=/tmp/mf-20261028-1931-MISCHIEF-DF-56-brief.txt evidence=commit witness=none:unit-test verdict=none:pending-foreman-judge commit=self memory=none:board-row
// STATUS: complete
// TIER: T2 (helper + targeted test + full package suite invoked and observed)
// EVIDENCE: commit
