package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// planJSONExperiment is the clean-plan experiment for the --json key-naming
// regression (MISCHIEF-DF-26): the same corpus shape the --scratch-dir tests
// use — one L0 primitive (capability: none) that plans clean on this host.
const planJSONExperiment = `id: plan-json-keys
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
seed: 7
`

// planJSONDescriptor is the minimal N-012 descriptor the experiment resolves
// against (self-contained: the test builds its own catalog dir in a TempDir,
// so the acceptance arm never depends on repo fixture paths that break under
// worktree nesting).
const planJSONDescriptor = `id: N-012
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

// TestPlanJSONKeysAreSnakeCase (MISCHIEF-DF-26): `mischief plan --json` is a
// machine surface — every other JSON surface in the product is snake_case
// (evidence JSONL rows, journal/verdict strings, the doctor/audit/battery
// reports), so a scripted consumer `plan --json | jq .scratch_dir` must get
// the value, not null. The proof drives the REAL verb through run() and
// asserts the exact key set of the top-level object AND of one fault row:
// no PascalCase (Go field-name) keys anywhere on the --json surface.
func TestPlanJSONKeysAreSnakeCase(t *testing.T) {
	t.Setenv("MISCHIEF_SANCTION", "1")
	catalogDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(catalogDir, "n-012.yaml"), []byte(planJSONDescriptor), 0o644); err != nil {
		t.Fatal(err)
	}
	exp := filepath.Join(t.TempDir(), "exp.yaml")
	if err := os.WriteFile(exp, []byte(planJSONExperiment), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := captureStdout(t, func() int {
		return run([]string{"plan", "--json", "-f", exp, "--catalog-dir", catalogDir})
	})
	if code != 0 {
		t.Fatalf("plan --json: exit %d, want 0\n%s", code, out)
	}

	var top map[string]any
	if err := json.Unmarshal([]byte(out), &top); err != nil {
		t.Fatalf("plan --json output is not a JSON object: %v\n%s", err, out)
	}

	// The full top-level key set (sorted) — a new field must land here as
	// snake_case or this assertion fails loud.
	wantTop := []string{
		"budget",
		"catalog_version",
		"experiment_id",
		"faults",
		"notes",
		"probes",
		"refusals",
		"run_id",
		"scratch_dir",
		"seed",
		"target",
	}
	gotTop := make([]string, 0, len(top))
	for k := range top {
		gotTop = append(gotTop, k)
	}
	sort.Strings(gotTop)
	if strings.Join(gotTop, ",") != strings.Join(wantTop, ",") {
		t.Fatalf("plan --json top-level keys:\n got %v\nwant %v", gotTop, wantTop)
	}

	// Spot-checks the acceptance names explicitly — each must carry its
	// value, not null.
	for _, k := range []string{"scratch_dir", "catalog_version", "experiment_id", "run_id"} {
		if _, ok := top[k]; !ok {
			t.Fatalf("plan --json missing key %q", k)
		}
	}
	if got, ok := top["scratch_dir"].(string); !ok || got != "" {
		t.Fatalf("scratch_dir: got %#v, want an empty string (no --scratch-dir declared)", top["scratch_dir"])
	}
	if got, ok := top["catalog_version"].(string); !ok || got == "" {
		t.Fatalf("catalog_version: got %#v, want the resolved version string", top["catalog_version"])
	}

	// The nested surfaces ride the same contract: fault rows, probes and
	// the budget are snake_case too (the brief's RecoverWithin is a
	// budget field, so a tag pass that stopped at the top level would
	// leave the consumer broken).
	if len(top["faults"].([]any)) != 1 {
		t.Fatalf("faults: got %d rows, want 1", len(top["faults"].([]any)))
	}
	fault := top["faults"].([]any)[0].(map[string]any)
	wantFault := []string{
		"capability",
		"inverse",
		"landed_proof",
		"params",
		"primitive",
		"selftest",
		"status",
		"tier",
		"ttl",
		"ttl_raw",
		"unavailable_names",
	}
	gotFault := make([]string, 0, len(fault))
	for k := range fault {
		gotFault = append(gotFault, k)
	}
	sort.Strings(gotFault)
	if strings.Join(gotFault, ",") != strings.Join(wantFault, ",") {
		t.Fatalf("plan --json fault-row keys:\n got %v\nwant %v", gotFault, wantFault)
	}

	probe := top["probes"].([]any)[0].(map[string]any)
	wantProbe := []string{"cmd", "expect", "name"}
	gotProbe := make([]string, 0, len(probe))
	for k := range probe {
		gotProbe = append(gotProbe, k)
	}
	sort.Strings(gotProbe)
	if strings.Join(gotProbe, ",") != strings.Join(wantProbe, ",") {
		t.Fatalf("plan --json probe keys:\n got %v\nwant %v", gotProbe, wantProbe)
	}

	budget := top["budget"].(map[string]any)
	wantBudget := []string{"max_error_rate", "recover_within"}
	gotBudget := make([]string, 0, len(budget))
	for k := range budget {
		gotBudget = append(gotBudget, k)
	}
	sort.Strings(gotBudget)
	if strings.Join(gotBudget, ",") != strings.Join(wantBudget, ",") {
		t.Fatalf("plan --json budget keys:\n got %v\nwant %v", gotBudget, wantBudget)
	}
	if got, ok := budget["recover_within"].(string); !ok || got != "30s" {
		t.Fatalf("recover_within: got %#v, want \"30s\"", budget["recover_within"])
	}

	// Landed proof and inverse resolve from the descriptor — snake_case,
	// with the values the fixture declared.
	lp := fault["landed_proof"].(map[string]any)
	for _, k := range []string{"kind", "check"} {
		if _, ok := lp[k]; !ok {
			t.Fatalf("landed_proof missing key %q (got %v)", k, lp)
		}
	}
	inv := fault["inverse"].(map[string]any)
	if got, ok := inv["action"].(string); !ok || got != "restore-proxy" {
		t.Fatalf("inverse.action: got %#v, want \"restore-proxy\"", inv["action"])
	}

	// Nothing on the surface may carry a PascalCase/Go-field key.
	for _, k := range []string{"ScratchDir", "CatalogVersion", "LandedProof", "ExperimentID", "RecoverWithin", "RunID"} {
		if _, ok := top[k]; ok {
			t.Fatalf("plan --json leaked PascalCase key %q", k)
		}
	}
}
