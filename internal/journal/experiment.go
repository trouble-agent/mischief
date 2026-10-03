// Package journal is SPEC-02: the experiment schema, the content-derived run
// id, the journal record types, and scrub-on-write.
//
// The journal is the evidence artefact (docs/prd/mischief-v0.1.md §6.3,
// "append-only JSONL — the evidence, same doctrine as trouble's ledger") and
// the only thing that makes a verdict auditable. The experiment schema loads
// the replay corpus in catalog/experiments/ (ex-001..ex-004); the run id is
// derived from content, not from a clock.
//
// Glossary (names owned here, reused by later specs):
//
//   - Experiment: one declared fault case (catalog/experiments/*.yaml), loaded
//     into typed structs by Load.
//   - RunID: hex(sha256(spec_bytes + seed))[:16] — content-derived, stable
//     across identical replays (AC-10).
//   - Journal: the append-only JSONL file a run writes; group-commit + fsync.
//   - Record: one journal line — a typed lifecycle event (plan..verdict).
//   - Scrub: the write-path pass that replaces credential-shaped material
//     with [REDACTED:<rule>] before any bytes leave the process (AC-20).
//
// The verdict vocabulary is the closed eight-value set owned by the PRD §6.4;
// SPEC-05 consumes it. A Verdict value round-trips journal → read → summary
// unchanged (AC-9) — that property is tested here, not asserted in prose.
//
// NOT-LIST (what SPEC-02 does not promise — see docs/SPEC-PLAN.md):
//
//   - The schema does not resolve targets, pick actuators or grade verdicts:
//     it loads and validates the declared case only (SPEC-03/SPEC-05 own the
//     rest).
//   - The journal does not promise the fault landed or the target recovered;
//     it records what was measured, including "nothing landed" (no_op is a
//     verdict value, not a schema failure).
//   - Scrub-on-write is a pattern pass over record bytes, not a semantic
//     analysis: it redacts credential SHAPES (token/key/bearer/DSN/query-key
//     material). It cannot recognise a secret that carries no credential
//     shape at all.
//   - The run id proves nothing beyond the bytes it hashes: it is
//     reproducibility, not correctness.
//   - Retention/rotation is SPEC-11; this package appends and reads, it does
//     not prune.
package journal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	yamlpkg "gopkg.in/yaml.v3"
)

// yamlNode aliases the yaml package node type used by the custom
// unmarshalers (catalog precedent).
type yamlNode = yamlpkg.Node

// LoadError is a named refusal: it carries the file, the experiment id when
// known, and a reason naming the offending field or parse failure
// (catalog's "field: problem" convention).
type LoadError struct {
	// File is the experiment file the refusal names.
	File string
	// ID is the experiment id, when the failure is attributable to one.
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

// Verdict is the closed verdict vocabulary of PRD §6.4. Every value is
// representable end to end (journal → CLI output → findings row, AC-9); the
// green-but-meaningless shapes no_op/aborted/corrupted/flaky exist precisely
// so a run that did nothing cannot grade as a pass.
type Verdict string

const (
	// VerdictRecovered: the probe set is green within budget and the
	// integrity oracle shows no loss.
	VerdictRecovered Verdict = "recovered"
	// VerdictDegraded: passes with a measured cost.
	VerdictDegraded Verdict = "degraded"
	// VerdictNotRecovered: budget expired without a green probe set.
	VerdictNotRecovered Verdict = "not_recovered"
	// VerdictHung: the target stopped answering probes.
	VerdictHung Verdict = "hung"
	// VerdictCorrupted: data loss detected by the integrity oracle.
	VerdictCorrupted Verdict = "corrupted"
	// VerdictNoOp: the fault never landed (AC-3; the run FAILS).
	VerdictNoOp Verdict = "no_op"
	// VerdictAborted: refused by rails/TTL/blast.
	VerdictAborted Verdict = "aborted"
	// VerdictFlaky: N runs disagree.
	VerdictFlaky Verdict = "flaky"
	// VerdictVoid: the control run was not green (AC-16).
	VerdictVoid Verdict = "void"
)

// valid reports whether v is one of the eight declared verdict values plus
// the void control value (nine total — the vocabulary is closed; see §6.4:
// recovered, degraded, not_recovered, hung, corrupted, no_op, aborted, flaky,
// void).
func (v Verdict) valid() bool {
	switch v {
	case VerdictRecovered, VerdictDegraded, VerdictNotRecovered, VerdictHung,
		VerdictCorrupted, VerdictNoOp, VerdictAborted, VerdictFlaky, VerdictVoid:
		return true
	}
	return false
}

// String renders the verdict value exactly as it is journaled.
func (v Verdict) String() string { return string(v) }

// target is the declared target of an experiment (§6.2).
type target struct {
	Selector string `yaml:"selector"`
	Scratch  bool   `yaml:"scratch"`
}

// expect records the declared expectation(s). Every experiment corpus file
// declares at least one; the field is free-form, so the exact YAML shape is
// preserved verbatim.
type expect struct {
	// Keys are the declared expectation names in file order ("replay_pre_fix",
	// "verdict_on_missing_target", ...).
	Keys []string
	// Values carries the declared expectation value per key (parallel to
	// Keys; both may be verdict-shaped, but the schema does not enforce
	// that — the expectation is prose-grade data until SPEC-05 consumes it).
	Values []string
}

// UnmarshalYAML keeps declaration order stable so a re-load byte-compares
// (the AC-10 fault-set stability argument extends to expect).
func (e *expect) UnmarshalYAML(value *yamlNode) error {
	var m yamlMapSlice
	if err := value.Decode(&m); err != nil {
		return fmt.Errorf("expect: must be a mapping")
	}
	for _, it := range m {
		var v string
		if err := it.Value.Decode(&v); err != nil {
			return fmt.Errorf("expect.%s: value must be a string", it.Key.Value)
		}
		e.Keys = append(e.Keys, it.Key.Value)
		e.Values = append(e.Values, v)
	}
	return nil
}

// yamlMapItem is one key/value pair of a YAML mapping in document order;
// yamlMapSlice decodes a mapping without losing that order (yaml.v3 unmarshals
// map[string]X in Go map order, which is random — the schema needs file order).
type yamlMapItem struct {
	Key   *yamlNode
	Value *yamlNode
}

type yamlMapSlice []yamlMapItem

// UnmarshalYAML collects mapping items in document order.
func (m *yamlMapSlice) UnmarshalYAML(value *yamlNode) error {
	if value.Kind != yamlpkg.MappingNode {
		return fmt.Errorf("not a mapping")
	}
	for i := 0; i+1 < len(value.Content); i += 2 {
		*m = append(*m, yamlMapItem{Key: value.Content[i], Value: value.Content[i+1]})
	}
	return nil
}

// Experiment is one declared fault case: the schema of
// catalog/experiments/*.yaml (§6.2). Load validates it; plan/run consume it.
type Experiment struct {
	// ID is the display name ("ex-002-redis-loss-recovery"). The RUN id is
	// content-derived and never equal to this field.
	ID string `yaml:"id"`
	// Target is the declared target selector + scratch posture.
	Target target `yaml:"target"`
	// Faults is the declared fault set, sorted by primitive id at load (the
	// stable order AC-10 compares).
	Faults []*FaultDecl `yaml:"faults"`
	// Probe is the pre/post probe set.
	Probe []*ProbeDecl `yaml:"probe"`
	// Budget is the recovery budget.
	Budget Budget `yaml:"budget"`
	// Observe is the optional observe.trouble declaration (absent = nil).
	Observe *ObserveDecl `yaml:"observe"`
	// Integrity is the optional integrity oracle declaration (absent = nil).
	Integrity *IntegrityDecl `yaml:"integrity"`
	// Expect records the declared expectations verbatim.
	Expect expect `yaml:"expect"`
	// Seed is the declared seed (the run id mixes it with the spec bytes).
	Seed int64 `yaml:"seed"`
	// SourceFile names the file the experiment was loaded from ("" when
	// built in memory).
	SourceFile string `yaml:"-"`
	// Repairs records every mechanical pre-pass repair applied to the source
	// bytes (rule names, in application order); the source file is never
	// mutated (catalog precedent).
	Repairs []string `yaml:"-"`
}

// FaultDecl is one declared fault inside an experiment.
type FaultDecl struct {
	// Primitive is the catalog primitive id ("I-002").
	Primitive string `yaml:"primitive"`
	// Params are the primitive's declared parameters.
	Params map[string]string `yaml:"params"`
	// TTL is how long the fault is held.
	TTL string `yaml:"ttl"`
	// LandedProof is the fault's landing proof.
	LandedProof LandedProofDecl `yaml:"landed_proof"`
	// Inverse is the recorded inverse (bare action or mapping).
	Inverse InverseDecl `yaml:"inverse"`
}

// LandedProofDecl is the experiment-level landing proof declaration.
type LandedProofDecl struct {
	Kind  string `yaml:"kind"`
	Check string `yaml:"check"`
	// Probe carries the external-probe form's probe expression ("tcp:...").
	Probe string `yaml:"probe"`
	// Expect carries the external-probe form's expectation.
	Expect string `yaml:"expect"`
}

// InverseDecl is the experiment-level inverse declaration: a bare action
// string ("restore-from-copy") or a mapping ({primitive: I-002, params:
// {mode: restore}}).
type InverseDecl struct {
	Action    string            `yaml:"action"`
	Primitive string            `yaml:"primitive"`
	Params    map[string]string `yaml:"params"`
}

// UnmarshalYAML normalises the two declared inverse forms.
func (iv *InverseDecl) UnmarshalYAML(value *yamlNode) error {
	var s string
	if err := value.Decode(&s); err == nil {
		iv.Action = s
		return nil
	}
	var m struct {
		Action    *string           `yaml:"action"`
		Primitive *string           `yaml:"primitive"`
		Params    map[string]string `yaml:"params"`
	}
	if err := value.Decode(&m); err != nil {
		return fmt.Errorf("inverse: must be an action string or a mapping with action/primitive")
	}
	if m.Action != nil {
		iv.Action = *m.Action
	}
	if m.Primitive != nil {
		iv.Primitive = *m.Primitive
	}
	iv.Params = m.Params
	return nil
}

// s_inv removed: the receiver is already a pointer.

// ProbeDecl is one declared probe.
type ProbeDecl struct {
	Name   string `yaml:"name"`
	Cmd    string `yaml:"cmd"`
	Expect string `yaml:"expect"`
}

// Budget is the declared recovery budget.
type Budget struct {
	RecoverWithin string  `yaml:"recover_within"`
	MaxErrorRate  float64 `yaml:"max_error_rate"`
}

// ObserveDecl is the observe.trouble declaration.
type ObserveDecl struct {
	Trouble *TroubleObserve `yaml:"trouble"`
}

// TroubleObserve is the trouble-specific observe shape.
type TroubleObserve struct {
	Namespace    string `yaml:"namespace"`
	ExpectRecord string `yaml:"expect_record"`
	Within       string `yaml:"within"`
}

// IntegrityDecl is the integrity oracle declaration.
type IntegrityDecl struct {
	Files []string `yaml:"files"`
	Mode  string   `yaml:"mode"`
}

// validate enforces the field rules the corpus already obeys, naming the
// offending field on refusal (catalog precedent: "field: problem").
func (e *Experiment) validate() error {
	if e.ID == "" {
		return fmt.Errorf("id: missing")
	}
	if e.Target.Selector == "" {
		return fmt.Errorf("%s: target.selector: missing", e.ID)
	}
	if len(e.Faults) == 0 {
		return fmt.Errorf("%s: faults: missing", e.ID)
	}
	for i, f := range e.Faults {
		if f.Primitive == "" {
			return fmt.Errorf("%s: faults[%d].primitive: missing", e.ID, i)
		}
		if f.LandedProof.Kind == "" && f.LandedProof.Probe == "" {
			return fmt.Errorf("%s: faults[%d].landed_proof: missing (kind or probe required)", e.ID, i)
		}
	}
	if len(e.Probe) == 0 {
		return fmt.Errorf("%s: probe: missing", e.ID)
	}
	for i, p := range e.Probe {
		if p.Name == "" || p.Cmd == "" || p.Expect == "" {
			return fmt.Errorf("%s: probe[%d]: name, cmd and expect are required", e.ID, i)
		}
	}
	if e.Budget.RecoverWithin == "" {
		return fmt.Errorf("%s: budget.recover_within: missing", e.ID)
	}
	if e.Seed == 0 {
		return fmt.Errorf("%s: seed: missing", e.ID)
	}
	return nil
}

// FaultSetKey is the stable text form of the resolved fault set: one line per
// fault, sorted by primitive id, fields joined in declared order. Two loads
// of the same experiment produce byte-identical keys (AC-10's fault-set half
// compares these).
func (e *Experiment) FaultSetKey() string {
	fs := make([]*FaultDecl, len(e.Faults))
	copy(fs, e.Faults)
	sortFaults(fs)
	var b []byte
	for _, f := range fs {
		b = append(b, f.Primitive...)
		b = append(b, 0x1f) // unit separator
		b = append(b, canonicalParams(f.Params)...)
		b = append(b, 0x1f)
		b = append(b, f.TTL...)
		b = append(b, 0x1f)
		b = append(b, f.LandedProof.Kind...)
		b = append(b, 0x1f)
		b = append(b, f.LandedProof.Probe...)
		b = append(b, 0x1f)
		b = append(b, f.LandedProof.Expect...)
		b = append(b, 0x1f)
		b = append(b, f.Inverse.Action...)
		b = append(b, 0x1f)
		b = append(b, f.Inverse.Primitive...)
		b = append(b, 0x1f)
		b = append(b, canonicalParams(f.Inverse.Params)...)
		b = append(b, '\n')
	}
	return string(b)
}

// canonicalParams renders a params map in sorted key order (map iteration is
// random; AC-10 needs byte-stable output).
func canonicalParams(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b []byte
	for _, k := range keys {
		if len(b) > 0 {
			b = append(b, ',')
		}
		b = append(b, k...)
		b = append(b, '=')
		b = append(b, m[k]...)
	}
	return string(b)
}

// sortFaults orders faults by primitive id (ties broken by the full key so
// two same-primitive faults keep a deterministic order too).
func sortFaults(fs []*FaultDecl) {
	sort.Slice(fs, func(i, j int) bool {
		if fs[i].Primitive != fs[j].Primitive {
			return fs[i].Primitive < fs[j].Primitive
		}
		return faultKey(fs[i]) < faultKey(fs[j])
	})
}

func faultKey(f *FaultDecl) string {
	return f.TTL + "\x1f" + canonicalParams(f.Params)
}

// RunID derives the content-derived run id: hex(sha256(spec_bytes +
// seed_decimal))[:16], exactly as docs/SPEC-PLAN.md SPEC-02 states. The seed
// is rendered in decimal before hashing so the derivation is a pure byte
// function of the on-disk spec plus the declared seed.
//
// NOTE: the brief writes hex(sha256(spec+seed))[:16]; the seed separator
// byte matters for injectivity — the format is "seed\n" + spec so two
// (spec, seed) pairs that concatenate identically still hash differently.
func RunID(specBytes []byte, seed int64) string {
	h := sha256.New()
	fmt.Fprintf(h, "seed\n%d\n", seed)
	h.Write(specBytes)
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:8]) // [:16] hex chars = first 8 bytes
}

// Parse parses and validates experiment bytes (the corpus shape:
// ex-001..ex-004). Same bytes in, same Experiment out — no clock, no map
// order leaks (AC-10's precondition).
func Parse(raw []byte) (*Experiment, error) {
	e := &Experiment{}
	if err := yamlpkg.Unmarshal(raw, e); err != nil {
		return nil, &LoadError{Reason: fmt.Sprintf("yaml: %v", err)}
	}
	if err := e.validate(); err != nil {
		return nil, err
	}
	// stable fault order: sort by primitive id (AC-10)
	sortFaults(e.Faults)
	return e, nil
}

// Load parses and validates one experiment YAML file (the corpus shape:
// ex-001..ex-004). The source file is never mutated; mechanical pre-pass
// repairs are named in Experiment.Repairs (catalog precedent).
func Load(path string) (*Experiment, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &LoadError{File: path, Reason: fmt.Sprintf("read: %v", err)}
	}
	e, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	e.SourceFile = path
	return e, nil
}

// LoadDir loads every .yaml experiment in dir (sorted by filename), refusing
// a malformed one by name. Used by `mischief battery` and the selftest.
func LoadDir(dir string) ([]*Experiment, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, &LoadError{Reason: fmt.Sprintf("experiments dir: %v", err)}
	}
	var files []string
	for _, ent := range ents {
		if ent.IsDir() || filepath.Ext(ent.Name()) != ".yaml" {
			continue
		}
		files = append(files, ent.Name())
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, &LoadError{Reason: fmt.Sprintf("no .yaml files in %s", dir)}
	}
	var out []*Experiment
	for _, name := range files {
		e, err := Load(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}
