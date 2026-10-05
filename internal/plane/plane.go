// Package plane is the MSF-021 bunker test plane's evidence contract: the
// one-command ephemeral-agent pipeline (scripts/bunker-plane.sh) that obtains
// an L1 bunker agent, ships the repo, runs the L0/L1 selftest there, pulls
// the journal and landed-proof counters back, and destroys the agent.
//
// This package owns every piece of that pipeline a unit test can and must
// own, so the shell script stays a thin driver over tested logic:
//
//   - ParseAgentID     the ONLY spawn-output parser (the script calls it
//     through `mischief plane parse-agent-id`; it never greps a second copy).
//   - SelftestVerdict  the remote `mischief selftest --all` exit-code
//     contract (0 = every primitive green-or-skip; anything else fail).
//   - Step / Fold      the evidence JSONL row shape and the verdict fold:
//     the LAST step's status is the run verdict; a skip without a named
//     reason is a plane defect rendered as a named skip, never a pass; an
//     empty evidence file is a named FAIL (the zero-byte evidence class).
//   - SSHArgs          the exact ssh argv shape the plane uses, asserted by
//     test so the script and the tests cannot drift apart.
//   - FinalVerdict     the honesty rule AC 3/4 demand: an unobtainable agent
//     is a SKIP with a named reason — never a pass, never a bare failure.
//
// Nothing here executes ssh, spawns agents or touches the network: the
// driver (the script) does that; this package grades what came back.
//
// ch:trace row=MSF-021 spec=docs/TEST-PLANE.md evidence=internal/plane/ witness=none:live-plane-run-logged-in-task
package plane

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// Verdict vocabulary (closed): the run statuses an evidence file may carry.
const (
	StatusPass = "pass"
	StatusFail = "fail"
	StatusSkip = "skip"
)

// SelftestVerdict maps the remote `mischief selftest --all` exit code onto
// the verdict vocabulary. The CLI's contract (cmd/mischief/selftest.go) is
// exit 0 only when every primitive is green-or-skip; 1 grades a failed
// primitive; 2 a usage/flag refusal; anything else is a crash or a killed
// run. Only 0 is a pass — the mapping is never inverted for convenience.
func SelftestVerdict(exitCode int) string {
	if exitCode == 0 {
		return StatusPass
	}
	return StatusFail
}

// ParseAgentID extracts the agent id from `bunker spawn` output. Two
// output shapes are recognized, in order:
//
//   - "Agent created: <id>" — the current bunkerd spawn line (the id is
//     the first hex token of ≥8 chars after the marker);
//   - "keys/<id>" — the key-path token older/other daemon shapes carry in
//     their connection bundle.
//
// Output naming neither token — a capacity refusal, a transport error, an
// empty string — parses as "" (the caller records a named SKIP).
func ParseAgentID(spawnOutput string) string {
	if id := hexTokenAfter(spawnOutput, "Agent created:"); id != "" {
		return id
	}
	for {
		i := strings.Index(spawnOutput, "keys/")
		if i < 0 {
			return ""
		}
		rest := spawnOutput[i+len("keys/"):]
		j := 0
		for j < len(rest) && isHex(rest[j]) {
			j++
		}
		if j >= 8 {
			return rest[:j]
		}
		spawnOutput = rest
	}
}

// hexTokenAfter returns the first whitespace-delimited hex token of ≥8
// characters following the marker, or "" when the marker is absent or no
// qualifying token follows it.
func hexTokenAfter(s, marker string) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	rest := strings.TrimLeft(s[i+len(marker):], " 	")
	j := 0
	for j < len(rest) && isHex(rest[j]) {
		j++
	}
	if j >= 8 {
		return rest[:j]
	}
	return ""
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// Step is one evidence row: what the plane did, how it graded, and why.
// Detail travels base64-encoded (detail_b64) so the shell driver can
// append rows with printf and never fight shell quoting on error text —
// the payload is always printable ASCII by construction.
type Step struct {
	// Step names the pipeline stage: build | preflight | spawn | ship |
	// selftest | collect | destroy (the closed set the doc's stage table
	// enumerates).
	Step string `json:"step"`
	// Status is pass | fail | skip (the closed vocabulary above).
	Status string `json:"status"`
	// DetailB64 carries the reason/proof text, base64-encoded.
	DetailB64 string `json:"detail_b64"`
	// TS is the UTC wall time the step was recorded (unix seconds, "" when
	// the driver clock failed — never a fabricated timestamp).
	TS string `json:"ts"`
}

// Detail decodes the step's reason/proof text. A malformed payload decodes
// to a placeholder naming the corruption — never to an empty string that
// could read as "no reason".
func (s Step) Detail() string {
	if s.DetailB64 == "" {
		return ""
	}
	b, err := base64.StdEncoding.DecodeString(s.DetailB64)
	if err != nil {
		return fmt.Sprintf("<undecodable detail: %v>", err)
	}
	return string(b)
}

// NewStep builds a Step from plain-text detail (the encoder side of Detail).
func NewStep(step, status, detail, ts string) Step {
	return Step{Step: step, Status: status, DetailB64: base64.StdEncoding.EncodeToString([]byte(detail)), TS: ts}
}

// Fold grades a whole evidence file. Rules, in order:
//
//  1. Zero recorded steps is a named FAIL — an empty evidence file means
//     the plane died before recording anything (the zero-byte evidence
//     class this pipeline exists to avoid), never a silent pass.
//  2. The LAST step's status is the run verdict (steps append in pipeline
//     order; destroy/teardown records last).
//  3. A skip whose detail is empty is still a skip, but its reason is
//     rendered as a plane defect — a skip without a named reason is never
//     allowed to masquerade as a clean, understood skip.
//
// Fold returns (status, reason, lastStage).
func Fold(steps []Step) (string, string, string) {
	if len(steps) == 0 {
		return StatusFail, "empty evidence: no steps recorded (the plane died before its first record)", ""
	}
	last := steps[len(steps)-1]
	reason := last.Detail()
	if last.Status == StatusSkip && strings.TrimSpace(reason) == "" {
		reason = "plane defect: skip recorded without a named reason"
	}
	if last.Status != StatusPass && last.Status != StatusFail && last.Status != StatusSkip {
		return StatusFail, fmt.Sprintf("plane defect: unknown status %q on final step %q", last.Status, last.Step), last.Step
	}
	return last.Status, reason, last.Step
}

// ParseEvidence decodes a JSONL evidence file body into steps. Lines that
// do not parse are SKIPPED but counted: the returned error names how many
// lines were dropped, so a truncated/corrupt file cannot silently shrink.
func ParseEvidence(body string) ([]Step, int, error) {
	var steps []Step
	dropped := 0
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var s Step
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			dropped++
			continue
		}
		steps = append(steps, s)
	}
	return steps, dropped, nil
}

// FinalVerdict is the honesty rule AC 3/4 impose on the whole plane: when
// no agent was obtained, the run is a SKIP carrying the named reason —
// never a pass, and never a generic failure that hides whether the
// blocker was capacity, reachability or a missing CLI. When an agent WAS
// obtained, the selftest's verdict governs unchanged (fail stays fail).
// An empty namedReason for a skip is refused: the caller gets the
// synthesized "bunker agent not obtained" reason instead of silence.
func FinalVerdict(agentObtained bool, selftestReported string, namedReason string) (status, reason string) {
	if !agentObtained {
		if strings.TrimSpace(namedReason) == "" {
			namedReason = "bunker agent not obtained (reason not recorded — plane defect)"
		}
		return StatusSkip, namedReason
	}
	switch selftestReported {
	case StatusPass:
		return StatusPass, ""
	case StatusSkip:
		// A selftest that itself skipped everything is not a pass; grade it
		// by its exit contract instead — SelftestVerdict never emits skip, so
		// this arm only fires on a mis-wired caller. Fail closed.
		return StatusFail, "plane defect: selftest reported skip; the exit contract grades pass/fail only"
	default:
		return StatusFail, namedReason
	}
}

// SanitizeKeyPath renders an ssh identity for evidence/records: the
// ABSOLUTE PATH only. Key material never enters a record; an empty path
// renders as "<none>" so a missing key is visible rather than blank.
func SanitizeKeyPath(p string) string {
	if strings.TrimSpace(p) == "" {
		return "<none>"
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}
