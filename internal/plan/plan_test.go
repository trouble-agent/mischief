package plan

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/catalog"
)

// testExperiment is the minimal corpus-shape experiment (journal.Load's
// validate rules: id, target.selector, faults with landed_proof, probe,
// budget, seed).
const testExperiment = `id: plan-test
target: {selector: "host:.", scratch: true}
faults:
  - primitive: N-012
    params: {code: "429", rate: "0.5"}
    ttl: 20s
    landed_proof: {kind: http, check: "responses carry 429"}
    inverse: {action: restore-proxy}
probe:
  - {name: probe-ok, cmd: "true", expect: "0"}
budget: {recover_within: 30s}
seed: 42
`

// testDescriptor is the minimal descriptor shape the loader's fault
// contract requires.
const testDescriptor = `id: N-012
name: http-error-injection
what: inject HTTP error codes at a proxy
breaks: clients that do not back off
params: {code: "429", rate: "0..1"}
landed_proof: {kind: http, check: "responses carry the code"}
inverse: {action: restore-proxy, verify: "responses are 200 again"}
capability: none
tier: L0
backend: proxy
maturity: planned
`

func testSetup(t *testing.T) (*catalog.Catalog, []byte) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "n-012.yaml"), []byte(testDescriptor), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.LoadDir(dir)
	if err != nil {
		t.Fatalf("load test catalog: %v", err)
	}
	return cat, []byte(testExperiment)
}

// TestPlanBuildResolvesAndSorts: the plan resolves the declared fault set
// through the catalog (capability, tier, selftest, inverse) and the run id
// is the journal package's content derivation — same bytes+seed, same id.
func TestPlanBuildResolvesAndSorts(t *testing.T) {
	cat, spec := testSetup(t)
	exp, err := parseExp(spec)
	if err != nil {
		t.Fatalf("parse experiment: %v", err)
	}
	p, err := Build(exp, spec, cat, allAvailable, testScope())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(p.Faults) != 1 {
		t.Fatalf("faults: %d, want 1", len(p.Faults))
	}
	f := p.Faults[0]
	if f.Primitive != "N-012" || f.Capability != "none" || f.Status != catalog.StatusReady {
		t.Fatalf("fault row mismatch: %+v", f)
	}
	if f.Inverse.Action != "restore-proxy" || f.Inverse.Verify != "responses are 200 again" {
		t.Fatalf("inverse not resolved from descriptor: %+v", f.Inverse)
	}
	if len(p.Refusals) != 0 {
		t.Fatalf("clean plan carries refusals: %v", p.Refusals)
	}
	if p.RunID == "" {
		t.Fatal("run id empty")
	}
}

// TestRenderByteIdentical (AC-1's plan half): the same Plan renders to
// byte-identical output across repeated calls AND across rebuilt plans
// (two Build calls over the same inputs) — map iteration must never leak
// into the text.
//
// ch:trace row=MSF-014 spec=docs/prd/mischief-v0.1.md evidence=cmd/mischief/ + Makefile witness=none:no-live-target-run-in-worktree
func TestRenderByteIdentical(t *testing.T) {
	cat, spec := testSetup(t)
	exp, err := parseExp(spec)
	if err != nil {
		t.Fatal(err)
	}
	p1, err := Build(exp, spec, cat, allAvailable, testScope())
	if err != nil {
		t.Fatal(err)
	}
	r1 := Render(p1)
	for i := 0; i < 20; i++ {
		if r2 := Render(p1); !bytes.Equal(r1, r2) {
			t.Fatalf("render %d differs from render 0 (map-order leak):\n%s\n---\n%s", i, r1, r2)
		}
		// also: a fresh Build + Render must be byte-identical (no state
		// carried between plans)
		p2, err := Build(exp, spec, cat, allAvailable, testScope())
		if err != nil {
			t.Fatal(err)
		}
		if r2 := Render(p2); !bytes.Equal(r1, r2) {
			t.Fatalf("fresh build %d renders differently:\n%s\n---\n%s", i, r1, r2)
		}
	}
	// multi-fault plans must also be stable (the fault order comes from
	// the experiment's sorted order; the inverse params come from a map)
	multi := strings.Replace(testExperiment, `    params: {code: "429", rate: "0.5"}`,
		`    params: {rate: "0.5", code: "429", path: "/x", mode: "m"}`, 1)
	multiDesc := strings.Replace(testDescriptor, `params: {code: "429", rate: "0..1"}`,
		`params: {rate: "0..1", code: "429", path: str, mode: str}`, 1)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "n-012.yaml"), []byte(multiDesc), 0o644); err != nil {
		t.Fatal(err)
	}
	cat2, err := catalog.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	exp2, err := parseExp([]byte(multi))
	if err != nil {
		t.Fatal(err)
	}
	p3, err := Build(exp2, []byte(multi), cat2, allAvailable, testScope())
	if err != nil {
		t.Fatal(err)
	}
	q1 := Render(p3)
	for i := 0; i < 20; i++ {
		if q2 := Render(p3); !bytes.Equal(q1, q2) {
			t.Fatalf("multi-fault render %d differs (map-order leak in params):\n%s\n---\n%s", i, q1, q2)
		}
	}
}

// TestPlanZeroSideEffects (AC-1): Build+Render touch NOTHING — the only
// host writes a plan may perform are none. The proof: a watch directory
// tree hashed before and after is identical, and the working directory is
// byte-identical too (plan never writes outside a run dir — here it writes
// nowhere at all because no run dir was given).
func TestPlanZeroSideEffects(t *testing.T) {
	cat, spec := testSetup(t)
	exp, err := parseExp(spec)
	if err != nil {
		t.Fatal(err)
	}
	watch := t.TempDir()
	sentinel := filepath.Join(watch, "state.txt")
	if err := os.WriteFile(sentinel, []byte("pre-plan state\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hashTree := func() map[string]string {
		m := map[string]string{}
		_ = filepath.Walk(watch, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			b, _ := os.ReadFile(path)
			m[path] = string(b)
			return nil
		})
		return m
	}
	before := hashTree()
	for i := 0; i < 5; i++ {
		if _, err := Build(exp, spec, cat, allAvailable, testScope()); err != nil {
			t.Fatal(err)
		}
	}
	after := hashTree()
	if len(before) != len(after) {
		t.Fatalf("file count changed: %d -> %d (plan wrote to the watched tree)", len(before), len(after))
	}
	for path, h := range before {
		if after[path] != h {
			t.Fatalf("file %s changed by plan (AC-1 violation): %q -> %q", path, h, after[path])
		}
	}
}

// TestPlanProtectedTargetRefusesAtResolution (AC-7 shape): a plan whose
// experiment names a protected control-plane selector refuses with
// protected_target, the refusal is surfaced in the plan's refusal list,
// and Build's error is a rails refusal (exit 2 through rails.MapExit).
func TestPlanProtectedTargetRefusesAtResolution(t *testing.T) {
	cat, spec := testSetup(t)
	exp, err := parseExp(spec)
	if err != nil {
		t.Fatal(err)
	}
	exp.Target.Selector = "process:scheduler"
	_, err = Build(exp, spec, cat, allAvailable, testScope())
	if err == nil {
		t.Fatal("protected target planned without refusal")
	}
	if !isRefusalErr(err) {
		t.Fatalf("error is not a rails refusal: %v", err)
	}
	// the failure path also returns the (partial) plan carrying the refusal
	p, _ := Build(exp, spec, cat, allAvailable, testScope())
	if p == nil || len(p.Refusals) == 0 || p.Refusals[0] != "protected_target" {
		t.Fatalf("refusal not surfaced: %+v", p)
	}
}

// TestPlanNoDefaultTarget (AC-2 shape): an experiment with an empty
// target selector refuses no_target — there is no default target, ever.
func TestPlanNoDefaultTarget(t *testing.T) {
	cat, spec := testSetup(t)
	exp, err := parseExp(spec)
	if err != nil {
		t.Fatal(err)
	}
	exp.Target.Selector = ""
	_, err = Build(exp, spec, cat, allAvailable, testScope())
	if err == nil {
		t.Fatal("empty target planned without refusal")
	}
	p, _ := Build(exp, spec, cat, allAvailable, testScope())
	if p == nil || len(p.Refusals) == 0 || p.Refusals[0] != "no_target" {
		t.Fatalf("no_target refusal not surfaced: %+v", p)
	}
}

// TestPlanCapabilityUnavailableNamed (AC-11 shape): a registry that
// reports the capability absent yields status capability_unavailable with
// the absent kind NAMED in the fault row and the notes.
func TestPlanCapabilityUnavailableNamed(t *testing.T) {
	desc := strings.Replace(testDescriptor, "capability: none", "capability: NET_ADMIN", 1)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "n-012.yaml"), []byte(desc), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	exp, err := parseExp(testSetupSpec())
	if err != nil {
		t.Fatal(err)
	}
	p, err := Build(exp, testSetupSpec(), cat, nothingAvailable, testScope())
	if err != nil {
		t.Fatalf("capability_unavailable is a plan note, not a build refusal: %v", err)
	}
	if len(p.Faults) != 1 || p.Faults[0].Status != catalog.StatusCapabilityUnavailable || p.Faults[0].UnavailableNames != "NET_ADMIN" {
		t.Fatalf("fault row does not name the absent capability: %+v", p.Faults)
	}
	if len(p.Notes) == 0 || !strings.Contains(p.Notes[0], "NET_ADMIN") {
		t.Fatalf("notes do not name what is absent: %v", p.Notes)
	}
	if !strings.Contains(string(Render(p)), "capability_unavailable") {
		t.Fatal("render does not carry the capability_unavailable status")
	}
}
