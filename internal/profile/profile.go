// Package profile is the testing-profile executor (PROF-003): the engine
// behind `mischief profile run <class>/<name> --target <selector> --json`.
//
// A PROFILE is the agent-facing unit of work — an ordered set of fault steps
// for one application class (storage, linux-cli, database, ...), each step
// naming a primitive from catalog/faults/. The loop it serves is the one the
// design authority (docs/prd/mischief-testing-profiles.md §1) describes: an
// AI agent injects a domain fault into its own software, reads a
// MACHINE-READABLE finding, fixes the code, and re-runs the SAME profile to
// prove the fix (the finding flips). Everything here is judged by one
// question — does this let an agent close that loop without a human?
//
// That profiles PRD is not in THIS tree yet (it lives on the
// docs/domain-pipelines branch, and the sibling PROF-001 row cites it too);
// the concrete shapes realised here — the step/fault declaration, the journal
// record vocabulary and the verdict vocabulary — are the committed
// docs/prd/mischief-v0.1.md shapes (§6.2, §6.3, §6.4), and the profile schema
// itself is defined and committed at catalog/profiles/README.md.
//
// The finding is the product. One JSON object on stdout carries the resolved
// target, every step's injected fault, the landing WITNESS, the probe's
// expected-vs-observed pair, the graded outcome, and the exact command that
// reproduces it. The three-value doctrine is the whole point:
//
//	passed       the fault landed AND the probe held
//	failed       the fault landed AND the probe broke
//	inconclusive no signal — the fault never landed, a primitive is missing,
//	             a requirement is unmet, or no actuator exists. NEVER a pass.
//
// A false green is the one outcome that makes the tool worthless: an agent
// that reads `passed` on a run where nothing was injected stops looking. So
// every "we could not do the thing" path is inconclusive with the reason
// NAMED, and the exit code says which (0 passed, 1 failed, 3 inconclusive).
//
// Run dir (the evidence artefact): <dir>/<run_id>/profile.jsonl holds the
// journal record set in the internal/journal format (run_planned, per-step
// fault_armed/fault_landed/probe_sample/assertion_result/finding, verdict,
// run_closed), finding.json holds the exact finding document, and shim/
// holds the generated injection source, the built shim, the scenario writer
// and the shim's own hit counter — the injection log a reviewer reads.
//
// NOT-LIST (what this build does NOT promise):
//
//   - It does not ship an actuator for every primitive. S-004 (short write) is
//     the shipped one; a step naming any other catalog primitive grades
//     inconclusive naming the missing actuator. An unknown primitive id grades
//     inconclusive naming the catalog it was resolved against.
//   - It does not attach to a live process. The shipped actuator faults a
//     CHILD the executor itself spawns (the L0 scratch shape): a target
//     selector that is not `scratch:<path>` grades inconclusive rather than
//     pretending to have injected anything.
//   - It does not enforce the scope ladder or the AC-19 selftest rail. The
//     selftest harness lands these same primitives on an L0 scratch target by
//     the same posture, and the manifest's tiers (S-004 declares L1 while its
//     selftest is not green) make the two rails mutually unsatisfiable for
//     every shipped shim primitive. The finding reports the descriptor's
//     declared tier in a note instead of silently implying it was met.
//   - It does not run the AC-8 load gate. Documented in the commit body: a
//     saturated host is not refused here yet.
//   - It does not prove the fault's effect on SOMEONE ELSE'S program: at L0
//     the target is a plain file and the executor supplies the reference
//     writer. The probe grades what that writer landed, which is exactly what
//     the descriptor's landed_proof names (size delta vs bytes believed).
//   - It does not grade a budget or a recovery window: there is no budget
//     field in the profile schema and no wall-clock grading here.
//
// ch:trace row=PROF-003 spec=docs/prd/mischief-testing-profiles.md (§3 finding,§4 surface,§6.2) evidence=internal/profile/ witness=live:short-write-scratch-run
package profile

import (
	"fmt"
	"strings"

	yamlpkg "gopkg.in/yaml.v3"
)

// SchemaV1 is the profile file schema this build reads. A profile declaring
// any other schema is refused BY NAME at load — a schema token exists so a
// future shape cannot be read as if it were this one.
const SchemaV1 = "mischief.profile/v1"

// FindingSchemaV1 is the finding document schema the emitter writes.
const FindingSchemaV1 = "mischief.finding/v1"

// Outcome is the three-value finding doctrine (PRD §3). The vocabulary is
// closed: there is no "ok", no "reverted", no fourth value.
type Outcome string

const (
	// OutcomePassed: the fault landed and the probe held.
	OutcomePassed Outcome = "passed"
	// OutcomeFailed: the fault landed and the probe broke.
	OutcomeFailed Outcome = "failed"
	// OutcomeInconclusive: no signal — nothing was proven either way.
	// NEVER a pass.
	OutcomeInconclusive Outcome = "inconclusive"
)

// Valid reports whether o is one of the three declared values.
func (o Outcome) Valid() bool {
	switch o {
	case OutcomePassed, OutcomeFailed, OutcomeInconclusive:
		return true
	}
	return false
}

// ExitCode maps the outcome onto the adoption-surface contract (PRD §4):
// 0 passed, 1 failed, 3 inconclusive. An undeclared value is 3 — an
// unrecognised outcome must never read as success.
func (o Outcome) ExitCode() int {
	switch o {
	case OutcomePassed:
		return 0
	case OutcomeFailed:
		return 1
	default:
		return 3
	}
}

// Probe vocabulary. A profile names ONE probe per step from this closed set;
// an unknown name is refused at load (a typo must not silently grade
// "nothing ran" as a pass).
const (
	// ProbePayloadComplete: the target's write loop landed every byte of the
	// payload it was asked to write (the S-004 short-write probe).
	ProbePayloadComplete = "payload-complete"
)

// KnownProbes is the closed probe vocabulary, sorted.
var KnownProbes = []string{ProbePayloadComplete}

// Requirement tokens. A profile's `requires:` entries name one of these; any
// other token is an UNMET requirement at run time (inconclusive, naming it),
// never a silently ignored line.
const (
	// ReqCC: a C compiler on PATH (an LD_PRELOAD shim is built per run).
	ReqCC = "cc"
	// ReqScratchTarget: the target must be an absolute path under the scratch
	// base (os.TempDir()). The executor refuses to fault a path outside it.
	ReqScratchTarget = "scratch-target"
	// ReqWritableTarget: the target's parent directory must be writable.
	ReqWritableTarget = "writable-target"
)

// KnownRequirements is the closed requirement vocabulary, sorted.
var KnownRequirements = []string{ReqCC, ReqScratchTarget, ReqWritableTarget}

// Requirement is one declared requirement: a vocabulary token plus the
// profile's own prose ("cc: a C compiler on PATH").
type Requirement struct {
	// Token is the vocabulary token (matched case-sensitively).
	Token string
	// Detail is the declared prose after the colon; may be empty.
	Detail string
}

// UnmarshalYAML accepts both spellings a YAML author naturally writes:
//
//   - cc: a C compiler on PATH        (a one-key mapping)
//   - "cc: a C compiler on PATH"      (a scalar)
//   - cc                             (a bare token)
func (r *Requirement) UnmarshalYAML(value *yamlpkg.Node) error {
	if value.Kind == yamlpkg.MappingNode {
		if len(value.Content) != 2 {
			return fmt.Errorf("requires: each mapping entry must have exactly one token key")
		}
		token := strings.TrimSpace(value.Content[0].Value)
		detail := value.Content[1]
		if detail.Kind != yamlpkg.ScalarNode {
			return fmt.Errorf("requires.%s: detail must be a string", token)
		}
		if token == "" || strings.ContainsAny(token, " 	") {
			return fmt.Errorf("requires: %q must be a single-word token (\"token: detail\")", token)
		}
		r.Token, r.Detail = token, strings.TrimSpace(detail.Value)
		return nil
	}
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("requires: each entry must be a string (\"token: detail\") or a one-key mapping")
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("requires: empty entry")
	}
	tok, detail, ok := strings.Cut(s, ":")
	if !ok {
		r.Token, r.Detail = s, ""
	} else {
		r.Token = strings.TrimSpace(tok)
		r.Detail = strings.TrimSpace(detail)
	}
	if r.Token == "" || strings.ContainsAny(r.Token, " 	") {
		return fmt.Errorf("requires: %q must start with a single-word token (\"token: detail\")", s)
	}
	return nil
}

// paramMap decodes a YAML mapping of scalars into string params. Numbers and
// booleans decode to their literal text ("0.5" -> "0.5", "true" -> "true"),
// so a profile may write `fraction: 0.5` unquoted; a nested collection is a
// named refusal (profile params are flat scalars, catalog precedent).
type paramMap map[string]string

// UnmarshalYAML implements yaml.Unmarshaler.
func (m *paramMap) UnmarshalYAML(value *yamlpkg.Node) error {
	if value.Kind == 0 {
		return nil
	}
	if value.Kind != yamlpkg.MappingNode {
		return fmt.Errorf("params: must be a mapping of name->scalar")
	}
	out := paramMap{}
	for i := 0; i+1 < len(value.Content); i += 2 {
		name := strings.TrimSpace(value.Content[i].Value)
		val := value.Content[i+1]
		if name == "" {
			return fmt.Errorf("params: empty param name")
		}
		if val.Kind != yamlpkg.ScalarNode {
			return fmt.Errorf("params.%s: value must be a scalar (got a %s)", name, nodeKindName(val.Kind))
		}
		out[name] = val.Value
	}
	*m = out
	return nil
}

func nodeKindName(k yamlpkg.Kind) string {
	switch k {
	case yamlpkg.DocumentNode:
		return "document"
	case yamlpkg.SequenceNode:
		return "sequence"
	case yamlpkg.MappingNode:
		return "mapping"
	case yamlpkg.AliasNode:
		return "alias"
	}
	return "scalar"
}

// Step is one ordered action of a profile: it names the fault to inject and
// the probe that grades what happened.
type Step struct {
	// ID is the step id, unique inside the profile ("crash-after-ack").
	ID string `yaml:"id"`
	// Fault is the primitive id the step injects (S-004); resolved against
	// the loaded catalog at RUN time, so an unknown id grades inconclusive
	// instead of refusing the profile at load.
	Fault string `yaml:"fault"`
	// Params are the fault's parameters; every name here must be declared by
	// the primitive's own params block (checked at run time, inconclusive
	// naming the offending pair when it is not).
	Params paramMap `yaml:"params"`
	// Probe names the probe that grades the step (closed vocabulary).
	Probe string `yaml:"probe"`
	// CauseClass is the closed-vocabulary failure class this step's failure
	// routes to; the profile's cause_classes block supplies the hint. Checked
	// at load (referential integrity), so an agent never receives a
	// cause_class the profile does not define.
	CauseClass string `yaml:"cause_class"`
}

// CauseClass is one declared failure class: the steering hint an agent gets
// when a step fails into it (PRD §3: "a short steering sentence, not a
// diagnosis").
type CauseClass struct {
	// Hint is the declared hint; required (an empty class is a dead route).
	Hint string `yaml:"hint"`
}

// Profile is one loaded testing profile: catalog/profiles/<class>/<name>.yaml.
type Profile struct {
	// Schema must be SchemaV1.
	Schema string `yaml:"schema"`
	// Name is the profile name (the file's base name, without .yaml).
	Name string `yaml:"name"`
	// Class is the application class (the directory segment above the file).
	Class string `yaml:"class"`
	// Version is the profile's own semantic version ("1.0.0") — profiles
	// version independently of the binary.
	Version string `yaml:"version"`
	// Summary is the agent-facing one-paragraph intent.
	Summary string `yaml:"summary"`
	// Seed is the declared default seed; --seed overrides it. A run id and
	// the generated scenario bytes derive from it, so the same profile+seed
	// replays byte-identically.
	Seed int64 `yaml:"seed"`
	// Requires is the declared requirement set; an unmet entry makes the run
	// inconclusive (never a pass).
	Requires []Requirement `yaml:"requires"`
	// Steps is the ordered action list; order is the contract.
	Steps []Step `yaml:"steps"`
	// CauseClasses maps a closed-vocabulary class ("storage:silent-short-write")
	// to its hint.
	CauseClasses map[string]CauseClass `yaml:"cause_classes"`
	// SourceFile names the file the profile was loaded from.
	SourceFile string `yaml:"-"`
}

// ID renders the profile id the CLI takes and the finding carries
// ("<class>/<name>").
func (p *Profile) ID() string { return p.Class + "/" + p.Name }

// LoadError is a named refusal: the file, the profile id when known, and a
// reason naming the offending field (catalog convention).
type LoadError struct {
	// File is the profile file the refusal names.
	File string
	// ID is the profile id, when the failure is attributable to one.
	ID string
	// Reason names the offending field or the parse failure.
	Reason string
}

// Error renders "file (id): reason", "file: reason", or the bare reason.
func (e *LoadError) Error() string {
	switch {
	case e.File != "" && e.ID != "":
		return fmt.Sprintf("%s (%s): %s", e.File, e.ID, e.Reason)
	case e.File != "":
		return fmt.Sprintf("%s: %s", e.File, e.Reason)
	default:
		return e.Reason
	}
}

// validate enforces the schema rules, naming the offending field on refusal.
// It is CATALOG-FREE by construction: fault references, param declarations and
// requirement tokens are resolved at RUN time so a profile naming a primitive
// that does not exist still LOADS and grades inconclusive — the acceptance
// shape ("a missing primitive yields inconclusive naming the reason, never a
// pass") is unreachable if the loader refuses the file instead.
func (p *Profile) validate() error {
	if p.Schema == "" {
		return fmt.Errorf("schema: missing (want %s)", SchemaV1)
	}
	if p.Schema != SchemaV1 {
		return fmt.Errorf("schema: %q is not this build's schema (%s)", p.Schema, SchemaV1)
	}
	if p.Name == "" {
		return fmt.Errorf("name: missing")
	}
	if strings.Contains(p.Name, "/") {
		return fmt.Errorf("name: %q must be a bare name (the class is a separate field)", p.Name)
	}
	if p.Class == "" {
		return fmt.Errorf("%s: class: missing", p.Name)
	}
	if strings.Contains(p.Class, "/") {
		return fmt.Errorf("%s: class: %q must be a single path segment", p.Name, p.Class)
	}
	if p.Version == "" {
		return fmt.Errorf("%s: version: missing (profiles version independently of the binary)", p.Name)
	}
	if len(p.Steps) == 0 {
		return fmt.Errorf("%s: steps: missing (a profile with no steps proves nothing)", p.Name)
	}
	seen := map[string]bool{}
	for i, s := range p.Steps {
		if s.ID == "" {
			return fmt.Errorf("%s: steps[%d].id: missing", p.Name, i)
		}
		if seen[s.ID] {
			return fmt.Errorf("%s: steps[%d].id: duplicate step id %q", p.Name, i, s.ID)
		}
		seen[s.ID] = true
		if s.Fault == "" {
			return fmt.Errorf("%s: steps[%d] (%s): fault: missing (the primitive id this step injects)", p.Name, i, s.ID)
		}
		if s.Probe == "" {
			return fmt.Errorf("%s: steps[%d] (%s): probe: missing (the probe that grades this step)", p.Name, i, s.ID)
		}
		if !knownProbe(s.Probe) {
			return fmt.Errorf("%s: steps[%d] (%s): probe: unknown probe %q (known: %s)",
				p.Name, i, s.ID, s.Probe, strings.Join(KnownProbes, ", "))
		}
		if s.CauseClass != "" {
			cc, ok := p.CauseClasses[s.CauseClass]
			if !ok {
				return fmt.Errorf("%s: steps[%d] (%s): cause_class: %q is not declared in cause_classes", p.Name, i, s.ID, s.CauseClass)
			}
			if strings.TrimSpace(cc.Hint) == "" {
				return fmt.Errorf("%s: cause_classes.%s: hint: missing", p.Name, s.CauseClass)
			}
		}
	}
	for name, cc := range p.CauseClasses {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("%s: cause_classes: empty class name", p.Name)
		}
		if strings.TrimSpace(cc.Hint) == "" {
			return fmt.Errorf("%s: cause_classes.%s: hint: missing", p.Name, name)
		}
	}
	return nil
}

func knownProbe(name string) bool {
	for _, k := range KnownProbes {
		if k == name {
			return true
		}
	}
	return false
}
