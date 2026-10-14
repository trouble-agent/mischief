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
//     every row's status must be in the closed vocabulary (the first
//     unknown status anywhere fails the fold closed) and every row's
//     STAGE must be a StageVocabulary name — NewStep refuses to build a
//     row for an unknown stage, and Fold fails closed on any alien-stage
//     row already in a file, so a nonsense stage can never fold as PASS;
//     otherwise the LAST step's status is the run verdict; a skip
//     without a named reason is a plane defect rendered as a named skip,
//     never a pass; an empty evidence file is a named FAIL (the
//     zero-byte evidence class).
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

// StageVocabulary is the closed set of pipeline stage names an evidence row
// may carry — exactly the stages scripts/bunker-plane.sh documents and
// records (see the stage table at the top of that script). Anything else is
// not a stage of this plane and is refused at every door that can admit it:
// record (NewStep), file decode (ParseEvidence) and grading (Fold). The
// vocabulary is closed so an alien stage name can never ride a row into an
// evidence file and then fold as PASS ("PLANE pass at nonsense").
var StageVocabulary = []string{
	"preflight",
	"build",
	"spawn",
	"ship",
	"selftest",
	"collect",
	"destroy",
}

// ValidateStage reports whether stage is in the closed vocabulary. The
// error names the allowed set (the refusal says what is absent), so a
// caller sees the whole vocabulary at the moment of the mistake.
func ValidateStage(stage string) error {
	for _, ok := range StageVocabulary {
		if stage == ok {
			return nil
		}
	}
	return fmt.Errorf("unknown stage %q (allowed: %s)", stage, strings.Join(StageVocabulary, " | "))
}

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
	// Step names the pipeline stage — a StageVocabulary name (preflight |
	// build | spawn | ship | selftest | collect | destroy): the closed set
	// the doc's stage table enumerates. NewStep refuses anything else, and
	// Fold grades an alien stage a plane defect.
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
// The stage name is validated against the closed vocabulary BEFORE any row
// can exist: an unknown stage returns an error naming the allowed set and
// the caller records nothing. Status validation is NOT duplicated here —
// the record verb already refuses unknown statuses, and Fold fails closed
// on any that slip through an out-of-band writer.
func NewStep(step, status, detail, ts string) (Step, error) {
	if err := ValidateStage(step); err != nil {
		return Step{}, err
	}
	return Step{Step: step, Status: status, DetailB64: base64.StdEncoding.EncodeToString([]byte(detail)), TS: ts}, nil
}

// Fold grades a whole evidence file. Rules, in order:
//
//  1. Zero recorded steps is a named FAIL — an empty evidence file means
//     the plane died before recording anything (the zero-byte evidence
//     class this pipeline exists to avoid), never a silent pass.
//  2. Every row's status must be in the closed vocabulary (pass | fail |
//     skip): the FIRST unknown status anywhere in the file fails the fold
//     closed, naming that row — an invalid row on an early stage must
//     never be overwritten by a later valid pass.
//  3. Every row's stage must be a vocabulary stage (preflight | build |
//     spawn | ship | selftest | collect | destroy): an alien stage name is
//     the same plane defect as an unknown status and fails the fold closed
//     naming that row and stage — a nonsense stage can never carry a PASS
//     verdict, no matter what a later row claims. (Rows with alien stages
//     can only appear in hand-edited or foreign-writer files: NewStep and
//     the record verb refuse them at the source.)
//  4. Otherwise the LAST step's status is the run verdict (steps append
//     in pipeline order; destroy/teardown records last).
//  5. A skip whose detail is empty is still a skip, but its reason is
//     rendered as a plane defect — a skip without a named reason is never
//     allowed to masquerade as a clean, understood skip.
//
// Fold returns (status, reason, lastStage).
func Fold(steps []Step) (string, string, string) {
	if len(steps) == 0 {
		return StatusFail, "empty evidence: no steps recorded (the plane died before its first record)", ""
	}
	for i, s := range steps {
		if err := ValidateStage(s.Step); err != nil {
			return StatusFail, fmt.Sprintf("plane defect: unknown stage %q on row %d of %d (allowed: %s)", s.Step, i+1, len(steps), strings.Join(StageVocabulary, " | ")), s.Step
		}
		if s.Status != StatusPass && s.Status != StatusFail && s.Status != StatusSkip {
			return StatusFail, fmt.Sprintf("plane defect: unknown status %q on step %q (row %d of %d)", s.Status, s.Step, i+1, len(steps)), s.Step
		}
	}
	last := steps[len(steps)-1]
	reason := last.Detail()
	if last.Status == StatusSkip && strings.TrimSpace(reason) == "" {
		reason = "plane defect: skip recorded without a named reason"
	}
	return last.Status, reason, last.Step
}

// ParseEvidence decodes a JSONL evidence file body into steps. Lines that
// do not parse are SKIPPED but counted: the returned error names how many
// lines were dropped, so a truncated/corrupt file cannot silently shrink.
func ParseEvidence(body string) ([]Step, int, error) {
	var steps []Step
	dropped := 0
	alien := 0
	var alienNames []string
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
		// An alien stage on a structurally parseable row is not silently
		// folded later as if it were plane output: count it here so the
		// caller can name the foreign row before Fold fails it closed.
		// The row still parses through — Fold owns the verdict.
		if s.Step != "" && ValidateStage(s.Step) != nil {
			alien++
			seen := false
			for _, n := range alienNames {
				if n == s.Step {
					seen = true
					break
				}
			}
			if !seen {
				alienNames = append(alienNames, s.Step)
			}
		}
		steps = append(steps, s)
	}
	if alien > 0 {
		return steps, dropped, fmt.Errorf("%d evidence row(s) carry stage(s) outside the plane vocabulary (%s; allowed: %s)",
			alien, strings.Join(alienNames, ", "), strings.Join(StageVocabulary, " | "))
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
