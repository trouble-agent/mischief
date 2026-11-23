package profile

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// finding_schema_test.go — the finding document's SCHEMA gate.
//
// The finding (internal/profile/finding.go) is the whole contract with the
// agent on the other end. This test pins that contract to a versioned JSON
// Schema (catalog/profiles/finding-schema.v1.json) so the two cannot drift:
// findings produced by the REAL emitter shape validate, malformed documents
// refuse, and the closed cause_class vocabulary stays closed.
//
// The validation here is a minimal structural validator (required fields,
// types, enums, const) covering the constraints the schema declares — no
// third-party JSON Schema dependency is added to go.mod.

// findingSchemaPath resolves catalog/profiles/finding-schema.v1.json from the
// test's cwd using the same ancestor-walk pattern internal/catalog uses: walk
// up from here until a directory holding a go.mod is found — that is the repo
// root — and join the corpus path to it.
func findingSchemaPath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "catalog", "profiles", "finding-schema.v1.json")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod at or above %s: not inside the repo", wd)
		}
		dir = parent
	}
}

// loadFindingSchema reads the schema document into generic JSON values and
// pins its draft: a rewrite that silently changes the draft would change what
// the schema is allowed to say about itself.
func loadFindingSchema(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile(findingSchemaPath(t))
	if err != nil {
		t.Fatalf("read finding schema: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse finding schema: %v", err)
	}
	if s, _ := doc["$schema"].(string); s != "http://json-schema.org/draft-07/schema#" {
		t.Fatalf("finding schema $schema = %q, want draft-07", s)
	}
	if s, _ := doc["type"].(string); s != "object" {
		t.Fatalf("finding schema root type = %v, want object", doc["type"])
	}
	return doc
}

// jsonEqual is reflect.DeepEqual over the generic-decoded values both sides
// (schema enums and instances) are already stored as; it exists only to name
// the intent at call sites.
func jsonEqual(a, b any) bool {
	return reflect.DeepEqual(a, b)
}

// validateAgainstSchema checks v against schema (a draft-07 document decoded
// into generic values) and returns every violation it can see. It covers the
// constraint vocabulary the finding schema uses: $ref (into "definitions"),
// type, const, enum, required, properties and items. Anything else in the
// schema is prose for human readers and is not enforced here.
func validateAgainstSchema(doc map[string]any, v any) []string {
	root := doc
	var walk func(sch any, v any, path string) []string
	walk = func(sch any, v any, path string) []string {
		var errs []string
		s, ok := sch.(map[string]any)
		if !ok {
			return errs
		}
		// $ref: resolve "#/definitions/<name>" against the root document.
		if ref, ok := s["$ref"].(string); ok {
			name := strings.TrimPrefix(ref, "#/definitions/")
			target, ok := root["definitions"].(map[string]any)[name]
			if !ok {
				return []string{fmt.Sprintf("%s: schema $ref %q does not resolve", path, ref)}
			}
			return walk(target, v, path)
		}
		// type: our schema only uses the string form (object, array, string,
		// integer, boolean).
		if t, ok := s["type"].(string); ok {
			var pass bool
			switch t {
			case "string":
				_, pass = v.(string)
			case "boolean":
				_, pass = v.(bool)
			case "integer":
				f, ok := v.(float64)
				pass = ok && f == math.Trunc(f)
			case "object":
				_, pass = v.(map[string]any)
			case "array":
				_, pass = v.([]any)
			default:
				pass = true // unconstrained by this validator
			}
			if !pass {
				errs = append(errs, fmt.Sprintf("%s: type %s, want %s", path, jsonTypeName(v), t))
			}
		}
		// const.
		if c, ok := s["const"]; ok && !jsonEqual(c, v) {
			errs = append(errs, fmt.Sprintf("%s: %v, want const %v", path, v, c))
		}
		// enum.
		if list, ok := s["enum"].([]any); ok {
			found := false
			for _, e := range list {
				if jsonEqual(e, v) {
					found = true
					break
				}
			}
			if !found {
				errs = append(errs, fmt.Sprintf("%s: %v not in enum %v", path, v, list))
			}
		}
		// object: required keys + per-property recursion.
		obj, isObj := v.(map[string]any)
		if isObj {
			if req, hasReq := s["required"]; hasReq {
				for _, r := range req.([]any) {
					name, ok := r.(string)
					if !ok {
						continue
					}
					if _, present := obj[name]; !present {
						errs = append(errs, fmt.Sprintf("%s: missing required field %q", path, name))
					}
				}
			}
			if props, ok := s["properties"].(map[string]any); ok {
				for name, psch := range props {
					if pv, present := obj[name]; present {
						errs = append(errs, walk(psch, pv, path+"."+name)...)
					}
				}
			}
		}
		// array: per-item recursion.
		if items, ok := s["items"].(map[string]any); ok {
			if arr, isArr := v.([]any); isArr {
				for i, iv := range arr {
					errs = append(errs, walk(items, iv, fmt.Sprintf("%s[%d]", path, i))...)
				}
			}
		}
		return errs
	}
	return walk(root, v, "finding")
}

func jsonTypeName(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	default:
		return fmt.Sprintf("%T", v)
	}
}

func mustValidate(t *testing.T, doc map[string]any, name string, instance any) {
	t.Helper()
	if errs := validateAgainstSchema(doc, instance); len(errs) > 0 {
		t.Fatalf("%s: finding refused by schema: %s", name, strings.Join(errs, "; "))
	}
}

func mustRefuse(t *testing.T, doc map[string]any, name string, instance any, want string) {
	t.Helper()
	errs := validateAgainstSchema(doc, instance)
	if len(errs) == 0 {
		t.Fatalf("%s: malformed finding accepted — want refusal naming %q", name, want)
	}
	for _, e := range errs {
		if strings.Contains(e, want) {
			return
		}
	}
	t.Fatalf("%s: refusal does not name %q: %s", name, want, strings.Join(errs, "; "))
}

// decodeInstance rounds a Finding through its real wire format: the emitter's
// JSON() (indented, trailing newline) is the only bytes the CLI writes, so
// that is what the schema must judge.
func decodeInstance(t *testing.T, f *Finding) map[string]any {
	t.Helper()
	b, err := f.JSON()
	if err != nil {
		t.Fatalf("Finding.JSON: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("unmarshal finding document: %v", err)
	}
	return doc
}

// corpusCauseVocabulary derives the run's actual cause_class vocabulary from
// the shipped corpus: load every catalog profile and collect the classes each
// one declares in its cause_classes block (referential integrity at
// internal/profile/profile.go Parse guarantees a finding can only carry a
// class declared here).
func corpusCauseVocabulary(t *testing.T, repoRoot string) map[string]bool {
	t.Helper()
	dir := filepath.Join(repoRoot, "catalog", "profiles")
	ids, err := List(dir)
	if err != nil {
		t.Fatalf("List(corpus): %v", err)
	}
	if len(ids) == 0 {
		t.Fatalf("no profiles under %s", dir)
	}
	vocab := map[string]bool{}
	for _, id := range ids {
		class, name, err := SplitID(id)
		if err != nil {
			t.Fatalf("SplitID(%q): %v", id, err)
		}
		p, _, err := Load(dir, class, name)
		if err != nil {
			t.Fatalf("Load(%s): %v", id, err)
		}
		for cls := range p.CauseClasses {
			vocab[cls] = true
		}
	}
	return vocab
}

// schemaCauseEnum reads the closed vocabulary the schema itself declares: the
// enum present at both places cause_class is referenced (root and step).
func schemaCauseEnum(t *testing.T, doc map[string]any) []any {
	t.Helper()
	defs, _ := doc["definitions"].(map[string]any)
	cc, ok := defs["cause_class"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no definitions.cause_class")
	}
	list, ok := cc["enum"].([]any)
	if !ok || len(list) == 0 {
		t.Fatalf("definitions.cause_class declares no closed enum")
	}
	return list
}

// emitShapeFixtures builds one finding per outcome using the exact document
// type the executor emits (internal/profile/finding.go Finding/StepFinding),
// mirroring the assembly rules in executor.go Run/runStep/applyRunCause:
// failed steps carry cause_class + hint from the profile's cause_classes and
// the first failure is lifted to the run; inconclusive steps name a reason.
func emitShapeFixtures() map[string]*Finding {
	witness := func() *Witness {
		return &Witness{Kind: "external", Source: "parent probe after child exit", Evidence: "size 12 of 12 bytes (ls: 12)"}
	}
	passedStep := StepFinding{
		ID:            "short-write-append",
		Fault:         FaultRef{ID: "S-004", Name: "short-write", Backend: "shim", Tier: "L1", Params: map[string]string{"fraction": "0.5", "budget": "1"}},
		InjectedAt:    "2026-11-23T10:00:00Z",
		Landed:        true,
		LandedWitness: witness(),
		Probe:         &ProbeResult{Name: "payload-complete", Expected: "every requested byte on the file", Observed: "12 of 12 bytes", Passed: true},
		Detached:      witness(),
		Outcome:       OutcomePassed,
		DurationMS:    12,
	}
	failedStep := passedStep
	failedStep.Probe = &ProbeResult{Name: "payload-complete", Expected: "every requested byte on the file", Observed: "6 of 12 bytes", Passed: false}
	failedStep.Outcome = OutcomeFailed
	failedStep.CauseClass = "storage:silent-short-write"
	failedStep.Hint = "loop on a short write: every write must advance by the returned count."
	inconclusiveStep := StepFinding{
		ID:         "short-write-append",
		Fault:      FaultRef{},
		Landed:     false,
		Outcome:    OutcomeInconclusive,
		Reason:     "requirements not met — cc: no C compiler on PATH",
		DurationMS: 3,
	}
	requires := []RequirementResult{
		{Requirement: "cc", Met: true, Detail: "cc -> /usr/bin/cc"},
	}
	common := func() *Finding {
		return &Finding{
			Schema:         FindingSchemaV1,
			Profile:        "storage/short-write",
			ProfileClass:   "storage",
			ProfileVersion: "1.0.0",
			RunID:          "run-abcdef123456",
			RunDir:         "/tmp/mischief-run/abcdef123456",
			Seed:           7,
			Target:         TargetInfo{Kind: "scratch", Selector: "scratch:/tmp/mischief/mischief-profile-target", Path: "/tmp/mischief/mischief-profile-target"},
			Requires:       requires,
			Steps:          []StepFinding{passedStep},
			Outcome:        OutcomePassed,
			StartedAt:      "2026-11-23T10:00:00Z",
			FinishedAt:     "2026-11-23T10:00:01Z",
			Reproduce:      "mischief profile storage/short-write --target scratch:/tmp/mischief/mischief-profile-target --seed 7",
		}
	}
	passed := common()
	failed := common()
	failed.Steps = []StepFinding{failedStep}
	failed.Outcome = OutcomeFailed
	failed.CauseClass = failedStep.CauseClass
	failed.Hint = failedStep.Hint
	inconclusive := common()
	inconclusive.Requires = []RequirementResult{{Requirement: "cc", Met: false, Detail: "cc: not on PATH"}}
	inconclusive.Steps = []StepFinding{inconclusiveStep}
	inconclusive.Outcome = OutcomeInconclusive
	inconclusive.Reason = inconclusiveStep.Reason
	return map[string]*Finding{
		"passed":       passed,
		"failed":       failed,
		"inconclusive": inconclusive,
	}
}

// TestFindingSchemaAcceptsEmitterShape: one fixture per outcome, marshalled
// through the emitter's own JSON(), validates clean against the schema.
func TestFindingSchemaAcceptsEmitterShape(t *testing.T) {
	doc := loadFindingSchema(t)
	for name, f := range emitShapeFixtures() {
		// Premise: the fixture's schema token, outcome vocabulary and (for a
		// failure) cause_class are the real constants the code emits — the
		// test judges the actual wire shape, not a lookalike.
		if f.Schema != FindingSchemaV1 {
			t.Fatalf("%s: fixture schema token %q, want the emitter constant", name, f.Schema)
		}
		mustValidate(t, doc, name, decodeInstance(t, f))
	}
}

// TestFindingSchemaRefusesMalformed pins the rejection arms: a wrong outcome
// value, a missing required field, and a type violation all refuse — with
// errors that name why.
func TestFindingSchemaRefusesMalformed(t *testing.T) {
	doc := loadFindingSchema(t)
	base := decodeInstance(t, emitShapeFixtures()["passed"])

	wrongOutcome := cloneDoc(base)
	wrongOutcome["outcome"] = "ok" // the fourth value must not exist
	mustRefuse(t, doc, "wrong outcome", wrongOutcome, `not in enum`)

	missingReproduce := cloneDoc(base)
	delete(missingReproduce, "reproduce")
	mustRefuse(t, doc, "missing reproduce", missingReproduce, `missing required field "reproduce"`)

	missingSteps := cloneDoc(base)
	delete(missingSteps, "steps")
	mustRefuse(t, doc, "missing steps", missingSteps, `missing required field "steps"`)

	badTargetType := cloneDoc(base)
	badTargetType["target"] = "scratch:/tmp/x"
	mustRefuse(t, doc, "target type", badTargetType, `type string, want object`)

	badStepOutcome := cloneDoc(base)
	badStepOutcome["steps"].([]any)[0].(map[string]any)["outcome"] = 5
	mustRefuse(t, doc, "step outcome type", badStepOutcome, `not in enum`)
}

// TestFindingSchemaCauseVocabularyIsClosed derives the shipped cause_class
// vocabulary from the corpus (the same referential integrity the loader enforces)
// and proves the schema's enum says exactly that: nothing missing, nothing extra.
// A finding carrying an undocumented class is refused.
func TestFindingSchemaCauseVocabularyIsClosed(t *testing.T) {
	doc := loadFindingSchema(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	vocab := corpusCauseVocabulary(t, repoRootFrom(wd))
	enum := schemaCauseEnum(t, doc)

	// Premise: the corpus does declare a vocabulary — an empty derivation
	// would make this test a no-op tautology.
	if len(vocab) == 0 {
		t.Fatal("shipped corpus declares no cause_classes; cannot prove closure")
	}
	if len(enum) != len(vocab) {
		t.Fatalf("schema cause_class enum has %d values, corpus declares %d: enum=%v corpus=%v",
			len(enum), len(vocab), enum, vocab)
	}
	for _, e := range enum {
		cls, ok := e.(string)
		if !ok {
			t.Fatalf("schema cause_class enum entry %v is not a string", e)
		}
		if !vocab[cls] {
			t.Fatalf("schema cause_class enum documents undeclared class %q", cls)
		}
	}
	for cls := range vocab {
		found := false
		for _, e := range enum {
			if e == cls {
				found = true
			}
		}
		if !found {
			t.Fatalf("corpus-declared cause class %q missing from schema enum %v", cls, enum)
		}
	}

	// A finding with an UNDOCUMENTED cause_class must refuse, at the run
	// level and on the step that carries it.
	invalid := "test:undeclared-class"
	f := emitShapeFixtures()["failed"]
	want := decodeInstance(t, f)
	want["cause_class"] = invalid
	mustRefuse(t, doc, "undocumented run cause_class", want, invalid)
	step := want["steps"].([]any)[0].(map[string]any)
	step["cause_class"] = invalid
	mustRefuse(t, doc, "undocumented step cause_class", want, invalid)
}

// TestFindingSchemaPassedRunCarriesNoCause pins the vocabulary's ABSENCE side:
// a passed run's document has cause_class and hint absent entirely, and the
// schema still accepts it — closure means the closed set, not mandatory failure.
func TestFindingSchemaPassedRunCarriesNoCause(t *testing.T) {
	doc := loadFindingSchema(t)
	passed := decodeInstance(t, emitShapeFixtures()["passed"])
	if _, ok := passed["cause_class"]; ok {
		t.Fatal("passed run document carries cause_class")
	}
	if _, ok := passed["hint"]; ok {
		t.Fatal("passed run document carries hint")
	}
	mustValidate(t, doc, "passed", passed)
}

// repoRootFrom walks up from start until a go.mod is found, mirroring
// internal/catalog's ancestor-walk repo-root resolution.
func repoRootFrom(start string) string {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}
		dir = parent
	}
}

// cloneDoc returns an independent copy of a decoded finding document so a
// mutation cannot bleed into sibling arms.
func cloneDoc(in map[string]any) map[string]any {
	b, err := json.Marshal(in)
	if err != nil {
		panic(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return out
}
