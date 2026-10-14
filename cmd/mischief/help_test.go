package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// shippedSubcommands is the operator verb surface the binary ships
// (main.go's run() switch, minus the internal __owner spawn target and
// the version/help meta verbs).
var shippedSubcommands = []string{
	"plan", "status", "revert", "doctor", "install", "uninstall",
	"audit", "retention", "selftest", "plane", "battery", "chaos",
	"sim", "serve", "arm",
}

// TestEveryShippedSubcommandHelpExitsZero (MISCHIEF-CLI-001): every
// shipped subcommand answers --help and -h with exit 0 and usage text,
// matching the top-level `mischief --help` contract. At the pre-fix HEAD
// every one of these exited 2 (flag.ErrHelp fell into the generic parse
// error path) — this test pins the conventional contract shut.
func TestEveryShippedSubcommandHelpExitsZero(t *testing.T) {
	for _, verb := range shippedSubcommands {
		for _, form := range []string{"--help", "-h"} {
			t.Run(verb+" "+form, func(t *testing.T) {
				code, stdout, stderr := captureBoth(t, func() int {
					return run([]string{verb, form})
				})
				if code != 0 {
					t.Fatalf("%s %s: exit %d, want 0\nstdout=%q\nstderr=%q",
						verb, form, code, stdout, stderr)
				}
				if strings.TrimSpace(stdout) == "" && strings.TrimSpace(stderr) == "" {
					t.Fatalf("%s %s: exit 0 but no usage output on either stream", verb, form)
				}
			})
		}
	}
}

// TestTopLevelHelpStaysZero (MISCHIEF-CLI-001, control): the top-level
// help contract was already correct — it must stay exit 0 with usage text.
func TestTopLevelHelpStaysZero(t *testing.T) {
	for _, form := range []string{"--help", "-h"} {
		code, out := captureStdout(t, func() int { return run([]string{form}) })
		if code != 0 {
			t.Fatalf("mischief %s: exit %d, want 0", form, code)
		}
		if !strings.Contains(out, "Usage:") {
			t.Fatalf("mischief %s: output lacks usage text: %q", form, out)
		}
	}
}

// TestHelpDoesNotMaskUsageErrors (MISCHIEF-CLI-001, negative controls):
// ONLY flag.ErrHelp becomes exit 0. Genuine usage errors keep exit 2 —
// an undefined flag (a FlagSet parse failure that is NOT ErrHelp), an
// unknown subcommand of a dispatcher verb, and a post-parse validation
// refusal (required flag missing after a clean parse).
func TestHelpDoesNotMaskUsageErrors(t *testing.T) {
	t.Run("undefined flag stays 2", func(t *testing.T) {
		code, _, stderr := captureBoth(t, func() int {
			return run([]string{"status", "--frobnicate"})
		})
		if code != 2 {
			t.Fatalf("status --frobnicate: exit %d, want 2\nstderr=%q", code, stderr)
		}
	})
	t.Run("unknown plane subcommand stays 2", func(t *testing.T) {
		code, _, stderr := captureBoth(t, func() int {
			return run([]string{"plane", "frobnicate"})
		})
		if code != 2 {
			t.Fatalf("plane frobnicate: exit %d, want 2\nstderr=%q", code, stderr)
		}
	})
	t.Run("post-parse validation stays 2", func(t *testing.T) {
		// chaos matrix without --project: parsed clean, refused after —
		// the help contract must not blanket-flip post-parse refusals.
		code, _, stderr := captureBoth(t, func() int {
			return run([]string{"chaos", "matrix"})
		})
		if code != 2 {
			t.Fatalf("chaos matrix (no --project): exit %d, want 2\nstderr=%q", code, stderr)
		}
	})
}

// captureBoth swaps os.Stdout AND os.Stderr for pipes around fn, so one
// run's output can be asserted on both streams (leaf FlagSets print their
// usage to the stream each verb chose — stderr — while dispatcher-level
// help prints to stdout, like the top-level help).
func captureBoth(t *testing.T, fn func() int) (int, string, string) {
	t.Helper()
	or, ow, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	er, ew, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	savedOut, savedErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = ow, ew
	code := fn()
	os.Stdout, os.Stderr = savedOut, savedErr
	ow.Close()
	ew.Close()
	readAll := func(r *os.File) string {
		b, _ := io.ReadAll(r)
		return string(b)
	}
	out, errOut := readAll(or), readAll(er)
	or.Close()
	er.Close()
	return code, out, errOut
}
