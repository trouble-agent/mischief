package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	yamlpkg "gopkg.in/yaml.v3"
)

// profile_test.go — the loader/schema contract: a profile that means what it
// says loads; one that does not is refused BY FIELD. The refusal strings are
// the test: a gate that fails with "invalid profile" teaches nobody anything.

const validProfile = `schema: mischief.profile/v1
name: short-write
class: storage
version: 1.0.0
seed: 7
summary: >
  a short write with no error
requires:
  - cc: a C compiler on PATH
  - scratch-target: under the scratch base
steps:
  - id: append
    fault: S-004
    params:
      fraction: 0.5
      budget: 1
    probe: payload-complete
    cause_class: storage:silent-short-write
cause_classes:
  storage:silent-short-write:
    hint: loop on a short write.
`

func TestParseValidProfile(t *testing.T) {
	p, err := Parse([]byte(validProfile), "storage/short-write.yaml")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.ID() != "storage/short-write" {
		t.Fatalf("ID = %q", p.ID())
	}
	if p.Seed != 7 {
		t.Fatalf("seed = %d", p.Seed)
	}
	if len(p.Requires) != 2 || p.Requires[0].Token != ReqCC || p.Requires[1].Token != ReqScratchTarget {
		t.Fatalf("requires = %+v", p.Requires)
	}
	if p.Requires[0].Detail != "a C compiler on PATH" {
		t.Fatalf("require detail = %q", p.Requires[0].Detail)
	}
	if len(p.Steps) != 1 {
		t.Fatalf("steps = %d", len(p.Steps))
	}
	s := p.Steps[0]
	if s.Fault != "S-004" || s.Probe != ProbePayloadComplete || s.CauseClass != "storage:silent-short-write" {
		t.Fatalf("step = %+v", s)
	}
	// Unquoted YAML scalars must arrive as their literal text: a fraction that
	// silently disappears would arm the wrong fault.
	if s.Params["fraction"] != "0.5" || s.Params["budget"] != "1" {
		t.Fatalf("params = %v", s.Params)
	}
	if p.CauseClasses["storage:silent-short-write"].Hint != "loop on a short write." {
		t.Fatalf("hint = %q", p.CauseClasses["storage:silent-short-write"].Hint)
	}
}

// TestParseRequiresScalarForm pins the second spelling: a quoted scalar entry
// must decode the same way as the one-key mapping form.
func TestParseRequiresScalarForm(t *testing.T) {
	body := strings.Replace(validProfile, `  - cc: a C compiler on PATH`, `  - "cc: a C compiler on PATH"`, 1)
	p, err := Parse([]byte(body), "storage/short-write.yaml")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.Requires[0].Token != ReqCC || p.Requires[0].Detail != "a C compiler on PATH" {
		t.Fatalf("requires[0] = %+v", p.Requires[0])
	}
}

// TestParseRefusalsNameTheField walks the schema rules: each case must fail
// AND the message must name the offending field.
func TestParseRefusalsNameTheField(t *testing.T) {
	stepsBlock := `steps:
  - id: append
    fault: S-004
    params:
      fraction: 0.5
      budget: 1
    probe: payload-complete
    cause_class: storage:silent-short-write
`
	cases := []struct {
		name string
		body string
		want string
	}{
		{"missing schema", strings.Replace(validProfile, "schema: mischief.profile/v1\n", "", 1), "schema: missing"},
		{"wrong schema", strings.Replace(validProfile, "mischief.profile/v1", "mischief.profile/v9", 1), "is not this build's schema"},
		{"missing name", strings.Replace(validProfile, "name: short-write\n", "", 1), "name: missing"},
		{"slashed name", strings.Replace(validProfile, "name: short-write", "name: a/b", 1), "must be a bare name"},
		{"missing class", strings.Replace(validProfile, "class: storage\n", "", 1), "class: missing"},
		{"missing version", strings.Replace(validProfile, "version: 1.0.0\n", "", 1), "version: missing"},
		{"missing step id", strings.Replace(validProfile, stepsBlock,
			"steps:\n  - {fault: S-004, probe: payload-complete}\n", 1), "steps[0].id: missing"},
		{"missing fault", strings.Replace(validProfile, "    fault: S-004\n", "", 1), "fault: missing"},
		{"missing probe", strings.Replace(validProfile, "    probe: payload-complete\n", "", 1), "probe: missing"},
		{"unknown probe", strings.Replace(validProfile, "probe: payload-complete", "probe: vibes", 1), `unknown probe "vibes"`},
		{"duplicate step id", strings.Replace(validProfile, "    probe: payload-complete",
			"    probe: payload-complete\n  - id: append\n    fault: S-004\n    probe: payload-complete", 1), "duplicate step id"},
		{"undeclared cause class", strings.Replace(validProfile, "cause_class: storage:silent-short-write", "cause_class: storage:other", 1), "is not declared in cause_classes"},
		{"empty hint", strings.Replace(validProfile, "    hint: loop on a short write.", "    hint: \"\"", 1), "hint: missing"},
		{"unknown field", validProfile + "\nnot_a_field: 1\n", "field not_a_field not found"},
		{"params sequence", strings.Replace(validProfile, stepsBlock,
			"steps:\n  - id: append\n    fault: S-004\n    params:\n      - 0.5\n    probe: payload-complete\n", 1), "params: must be a mapping"},
		{"requires mapping with two keys", strings.Replace(validProfile, "  - cc: a C compiler on PATH", "  - {cc: x, gpu: y}", 1), "exactly one token key"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.body), "storage/short-write.yaml")
			if err == nil {
				t.Fatalf("Parse accepted an invalid profile")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("refusal %q does not name %q", err.Error(), c.want)
			}
		})
	}
}

// TestParseYAMLTypeErrorIsNamed pins that a raw YAML type error (a sequence
// where a scalar belongs) surfaces as a parse refusal naming the file rather
// than a panic or a silent zero value.
func TestParseYAMLTypeErrorIsNamed(t *testing.T) {
	body := strings.Replace(validProfile, "seed: 7", "seed: [1, 2]", 1)
	_, err := Parse([]byte(body), "f.yaml")
	if err == nil {
		t.Fatal("Parse accepted a sequence seed")
	}
	if !strings.Contains(err.Error(), "f.yaml") || !strings.Contains(err.Error(), "parse:") {
		t.Fatalf("refusal %q must name the file and the parse failure", err.Error())
	}
}

// TestParseUnknownTokenStillLoads is the acceptance-critical one: an unknown
// primitive or requirement token must LOAD (so the executor can grade
// inconclusive naming it). A loader that refused the file would make the
// "missing primitive ⇒ inconclusive" contract unreachable.
func TestParseUnknownTokenStillLoads(t *testing.T) {
	body := strings.Replace(validProfile, "fault: S-004", "fault: Z-999", 1)
	body = strings.Replace(body, "  - cc: a C compiler on PATH", "  - gpu: a GPU", 1)
	if _, err := Parse([]byte(body), "storage/x.yaml"); err != nil {
		t.Fatalf("unknown primitive/requirement must still load (graded at run time): %v", err)
	}
}

func TestSplitID(t *testing.T) {
	if c, n, err := SplitID("storage/short-write"); err != nil || c != "storage" || n != "short-write" {
		t.Fatalf("SplitID = %q,%q,%v", c, n, err)
	}
	for _, bad := range []string{"", "storage", "storage/", "/short-write", "a/b/c"} {
		if _, _, err := SplitID(bad); err == nil {
			t.Fatalf("SplitID(%q) accepted an invalid id", bad)
		}
	}
}

// TestLoadFromCorpus loads the shipped profile from catalog/profiles and
// asserts it is the shape the schema doc describes (so the doc and the file
// cannot drift apart silently).
func TestLoadFromCorpus(t *testing.T) {
	dir := filepath.Join("..", "..", DefaultDir)
	ids, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) == 0 {
		t.Fatalf("no profiles under %s", dir)
	}
	found := false
	for _, id := range ids {
		class, name, err := SplitID(id)
		if err != nil {
			t.Fatalf("List returned %q: %v", id, err)
		}
		p, raw, err := Load(dir, class, name)
		if err != nil {
			t.Fatalf("Load(%s): %v", id, err)
		}
		if len(raw) == 0 {
			t.Fatalf("Load(%s) returned no bytes for the run id derivation", id)
		}
		if id == "storage/short-write" {
			found = true
			if p.Steps[0].Fault != "S-004" {
				t.Fatalf("storage/short-write must reference S-004, got %q", p.Steps[0].Fault)
			}
		}
	}
	if !found {
		t.Fatalf("the runnable storage/short-write profile is not in the corpus (%v)", ids)
	}
}

// TestLoadRefusesMisplacedID pins that the path IS the id: a file whose
// class/name disagree with its location is refused.
func TestLoadRefusesMisplacedID(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "wrong"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wrong", "short-write.yaml"), []byte(validProfile), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(dir, "wrong", "short-write"); err == nil ||
		!strings.Contains(err.Error(), "the path is the id") {
		t.Fatalf("Load accepted a misplaced profile: %v", err)
	}
}

func TestLoadMissingNamesThePath(t *testing.T) {
	dir := t.TempDir()
	_, _, err := Load(dir, "storage", "nope")
	if err == nil || !strings.Contains(err.Error(), "profile not found") {
		t.Fatalf("missing profile refusal = %v", err)
	}
}

// TestKnownVocabulariesAreSorted guards the message text the refusals render:
// an unsorted list makes the refusals non-deterministic across Go versions.
func TestKnownVocabulariesAreSorted(t *testing.T) {
	for _, list := range [][]string{KnownProbes, KnownRequirements} {
		for i := 1; i < len(list); i++ {
			if list[i] < list[i-1] {
				t.Fatalf("vocabulary not sorted: %v", list)
			}
		}
	}
}

// TestParamMapRefusesNested pins the flat-scalar contract through the public
// parser (a nested map would otherwise decode to "" and silently arm nothing).
func TestParamMapRefusesNested(t *testing.T) {
	var s struct {
		Params paramMap `yaml:"params"`
	}
	err := yamlpkg.Unmarshal([]byte("params:\n  a:\n    b: c\n"), &s)
	if err == nil || !strings.Contains(err.Error(), "must be a scalar") {
		t.Fatalf("nested param accepted: %v", err)
	}
}
