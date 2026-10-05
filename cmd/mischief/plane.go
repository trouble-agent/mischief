package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/trouble-agent/mischief/internal/plane"
)

// plane.go — the `mischief plane` verb (MSF-021): the evidence CLI the
// bunker test-plane driver (scripts/bunker-plane.sh) calls instead of
// re-implementing evidence logic in shell. Every record the plane writes
// is a Go-serialized JSONL row; every verdict is the internal/plane fold.
// The verb is deliberately dumb: it parses spawn output, appends evidence
// rows, and grades evidence files — it never spawns, ssh-es or destroys
// anything (the driver owns the acts; this owns the bookkeeping).
//
// Subcommands:
//
//	mischief plane parse-agent-id [--in <file|->]   — extract the agent id
//	    from `bunker spawn` output ("" when absent), exit 0.
//	mischief plane record --file E --step S --status pass|fail|skip
//	    [--detail TEXT]                                  — append one JSONL row.
//	mischief plane selftest-verdict --exit-code N      — print pass|fail.
//	mischief plane fold --file E                       — grade the evidence
//	    file: prints "PLANE <status> <stage>: <reason>", exit 0 pass / 1 fail
//	    / 3 skip (2 = usage).
//
// ch:trace row=MSF-021 spec=docs/TEST-PLANE.md evidence=cmd/mischief/plane.go + internal/plane/ witness=none:bookkeeping-only-verb
func cmdPlane(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "plane: a subcommand is required (parse-agent-id | record | selftest-verdict | fold)")
		return 2
	}
	switch args[0] {
	case "parse-agent-id":
		return cmdPlaneParseAgentID(args[1:])
	case "record":
		return cmdPlaneRecord(args[1:])
	case "selftest-verdict":
		return cmdPlaneSelftestVerdict(args[1:])
	case "fold":
		return cmdPlaneFold(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "plane: unknown subcommand %q (parse-agent-id | record | selftest-verdict | fold)\n", args[0])
		return 2
	}
}

func cmdPlaneParseAgentID(args []string) int {
	fs := flag.NewFlagSet("plane parse-agent-id", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	in := fs.String("in", "-", "read spawn output from this file, - for stdin")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var r io.Reader = os.Stdin
	if *in != "-" {
		f, err := os.Open(*in)
		if err != nil {
			fmt.Fprintln(os.Stderr, "plane parse-agent-id:", err)
			return 1
		}
		defer f.Close()
		r = f
	}
	body, err := io.ReadAll(r)
	if err != nil {
		fmt.Fprintln(os.Stderr, "plane parse-agent-id:", err)
		return 1
	}
	// The id prints verbatim — including the empty string (no id in the
	// output). Exit 0 either way: absence is DATA here (the driver records
	// the named skip), not an error of this verb.
	fmt.Println(plane.ParseAgentID(string(body)))
	return 0
}

func cmdPlaneRecord(args []string) int {
	fs := flag.NewFlagSet("plane record", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	file := fs.String("file", "", "evidence JSONL file (required)")
	step := fs.String("step", "", "pipeline stage: preflight | ship | selftest | collect | destroy (required)")
	status := fs.String("status", "", "pass | fail | skip (required)")
	detail := fs.String("detail", "", "reason/proof text (encoded by this verb)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *file == "" || *step == "" || *status == "" {
		fmt.Fprintln(os.Stderr, "plane record: --file, --step and --status are required")
		return 2
	}
	switch *status {
	case plane.StatusPass, plane.StatusFail, plane.StatusSkip:
	default:
		fmt.Fprintf(os.Stderr, "plane record: unknown --status %q (pass | fail | skip)\n", *status)
		return 2
	}
	// One write syscall per row (O_APPEND): a driver killed mid-run leaves
	// only WHOLE rows behind — a torn line would shrink ParseEvidence's
	// census and name itself there.
	f, err := os.OpenFile(*file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "plane record:", err)
		return 1
	}
	defer f.Close()
	s := plane.NewStep(*step, *status, *detail, strconv.FormatInt(time.Now().UTC().Unix(), 10))
	if _, err := fmt.Fprintln(f, mustJSON(s)); err != nil {
		fmt.Fprintln(os.Stderr, "plane record:", err)
		return 1
	}
	return 0
}

func cmdPlaneSelftestVerdict(args []string) int {
	fs := flag.NewFlagSet("plane selftest-verdict", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	code := fs.Int("exit-code", -1, "the remote selftest's exit code (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *code < 0 {
		fmt.Fprintln(os.Stderr, "plane selftest-verdict: --exit-code is required")
		return 2
	}
	fmt.Println(plane.SelftestVerdict(*code))
	return 0
}

func cmdPlaneFold(args []string) int {
	fs := flag.NewFlagSet("plane fold", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	file := fs.String("file", "", "evidence JSONL file (required; - reads stdin)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var body string
	if *file == "-" {
		b, err := io.ReadAll(bufio.NewReader(os.Stdin))
		if err != nil {
			fmt.Fprintln(os.Stderr, "plane fold:", err)
			return 1
		}
		body = string(b)
	} else {
		if *file == "" {
			fmt.Fprintln(os.Stderr, "plane fold: --file is required")
			return 2
		}
		b, err := os.ReadFile(*file)
		if err != nil {
			// An unreadable evidence file is the same honesty class as an
			// empty one: grade it a named FAIL, not a usage error.
			fmt.Printf("PLANE %s: evidence file unreadable: %v\n", plane.StatusFail, err)
			return 1
		}
		body = string(b)
	}
	steps, dropped, err := plane.ParseEvidence(body)
	if err != nil {
		fmt.Fprintln(os.Stderr, "plane fold:", err)
		return 1
	}
	if dropped > 0 {
		// Corrupt lines never silently shrink the census — the fold names
		// them in its own record before grading.
		if _, rerr := fmt.Fprintf(os.Stderr, "plane fold: NOTE %d corrupt evidence line(s) dropped\n", dropped); rerr != nil {
			_ = rerr
		}
	}
	status, reason, stage := plane.Fold(steps)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "all stages green (agent obtained, selftest exit 0, evidence pulled, agent destroyed)"
	}
	if stage == "" {
		fmt.Printf("PLANE %s: %s\n", status, reason)
	} else {
		fmt.Printf("PLANE %s at %s: %s\n", status, stage, reason)
	}
	switch status {
	case plane.StatusPass:
		return 0
	case plane.StatusSkip:
		return 3
	default:
		return 1
	}
}

// mustJSON marshals a plane.Step; the type carries no exotic values, so an
// error here is a programming defect and we crash the verb loudly rather
// than append a corrupt row.
func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("plane: evidence row marshal failed: %v", err))
	}
	return string(b)
}
