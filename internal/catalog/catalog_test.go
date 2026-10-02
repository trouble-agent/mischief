package catalog

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// corpusIDs are the 10 machine-readable examples shipped in catalog/faults.
var corpusIDs = []string{
	"C-009", "F-009", "I-002", "N-001", "N-012",
	"P-001", "R-001", "S-001", "S-004", "T-001",
}

// TestLoadDefaultCorpus: all 10 existing YAML examples must pass the loader.
func TestLoadDefaultCorpus(t *testing.T) {
	cat, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}
	if cat.Len() != len(corpusIDs) {
		t.Fatalf("loaded %d primitives, want %d", cat.Len(), len(corpusIDs))
	}
	sort.Strings(cat.IDs)
	for i, id := range corpusIDs {
		if cat.IDs[i] != id {
			t.Fatalf("IDs[%d]=%s, want %s (full: %v)", i, cat.IDs[i], id, cat.IDs)
		}
	}
	if cat.Version == "" {
		t.Fatal("content-derived version is empty")
	}
	if cat.VersionScheme != "content-v1" {
		t.Fatalf("VersionScheme=%q, want content-v1", cat.VersionScheme)
	}
	if len(cat.Warnings) == 0 {
		t.Fatal("corpus loads with no repair warnings; the pre-pass repairs went unnamed")
	}
}

// TestFaultContractREDMissingInverse: a primitive with no inverse is refused,
// with a reason naming the field (the RED half of the fault contract).
func TestFaultContractREDMissingInverse(t *testing.T) {
	dir := t.TempDir()
	noInv := `id: X-001
name: test-fault
what: a test fault used by the schema tests
params: {path: str}
landed_proof:
  kind: counter
  check: "hits >= 1"
capability: none
tier: L1
backend: fs
maturity: proven
selftest: green
`
	if err := os.WriteFile(filepath.Join(dir, "x.yaml"), []byte(noInv), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("loader accepted a primitive with no inverse — fault contract not enforced")
	}
	le, ok := err.(*LoadError)
	if !ok {
		t.Fatalf("error is %T, want *LoadError: %v", err, err)
	}
	if !strings.Contains(le.Reason, "inverse: missing") {
		t.Fatalf("refusal does not name the missing field: %v", le)
	}
	if le.ID != "X-001" || le.File != "x.yaml" {
		t.Fatalf("refusal does not attribute file+id: %+v", le)
	}

	// GREEN half: the same primitive with the inverse stated loads.
	withInv := noInv + `inverse: {action: restore-original, verify: "path serves original bytes"}
`
	if err := os.WriteFile(filepath.Join(dir, "x.yaml"), []byte(withInv), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("loader refused the completed contract: %v", err)
	}
	if cat.Get("X-001") == nil {
		t.Fatal("X-001 not in catalog after contract completed")
	}
}

// TestFaultContractFieldsNamed: each contract field, when missing, produces a
// refusal naming THAT field.
func TestFaultContractFieldsNamed(t *testing.T) {
	base := func() string {
		return `id: Y-001
name: test-fault
what: a test fault
params: {path: str}
landed_proof:
  kind: counter
  check: "hits >= 1"
inverse: restore-original
capability: none
tier: L1
backend: fs
maturity: proven
selftest: green
`
	}
	cases := map[string]struct{ drop, wantReason string }{
		"landed_proof": {drop: "landed_proof:\n  kind: counter\n  check: \"hits >= 1\"\n", wantReason: "landed_proof: missing"},
		"capability":   {drop: "capability: none\n", wantReason: "capability: missing"},
		"tier":         {drop: "tier: L1\n", wantReason: "tier: missing"},
		"params":       {drop: "params: {path: str}\n", wantReason: "params: missing"},
		"backend":      {drop: "backend: fs\n", wantReason: "backend: missing"},
		"maturity":     {drop: "maturity: proven\n", wantReason: "maturity: missing"},
	}
	for name, tc := range cases {
		body := strings.Replace(base(), tc.drop, "", 1)
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "y.yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadDir(dir)
		if err == nil {
			t.Fatalf("%s: loader accepted a primitive missing %s", name, name)
		}
		if !strings.Contains(err.Error(), tc.wantReason) {
			t.Fatalf("%s: refusal %q does not name the field (%q)", name, err.Error(), tc.wantReason)
		}
	}
}

// TestSelftestAC19Schema: the AC-19 refusal is expressible from the schema
// alone — a non-green selftest refuses outside L0; green does not; an absent
// selftest defaults to unknown (which refuses).
func TestSelftestAC19Schema(t *testing.T) {
	dir := t.TempDir()
	body := `id: Z-001
name: unselftested
what: a fault with no green selftest
params: {path: str}
landed_proof:
  kind: counter
  check: "hits >= 1"
inverse: restore-original
capability: none
tier: L1
backend: fs
maturity: proven
selftest: missing
`
	if err := os.WriteFile(filepath.Join(dir, "z.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d := cat.Get("Z-001")
	if !d.Selftest.RefusesOutsideL0() {
		t.Fatal("selftest missing must refuse outside L0 (AC-19)")
	}

	// absent selftest -> unknown -> refuses
	absent := strings.Replace(body, "selftest: missing\n", "", 1)
	if err := os.WriteFile(filepath.Join(dir, "z.yaml"), []byte(absent), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err = LoadDir(dir)
	if err != nil {
		t.Fatalf("load without selftest field: %v", err)
	}
	if got := cat.Get("Z-001").Selftest; got != SelftestUnknown {
		t.Fatalf("absent selftest = %q, want unknown", got)
	}
	if !cat.Get("Z-001").Selftest.RefusesOutsideL0() {
		t.Fatal("unknown selftest must refuse outside L0 (AC-19)")
	}

	// green -> no refusal
	green := strings.Replace(body, "selftest: missing", "selftest: green", 1)
	if err := os.WriteFile(filepath.Join(dir, "z.yaml"), []byte(green), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err = LoadDir(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cat.Get("Z-001").Selftest.RefusesOutsideL0() {
		t.Fatal("green selftest must not refuse")
	}

	// invalid value -> named refusal
	bad := strings.Replace(body, "selftest: missing", "selftest: probably-fine", 1)
	if err := os.WriteFile(filepath.Join(dir, "z.yaml"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDir(dir); err == nil || !strings.Contains(err.Error(), "selftest: invalid value") {
		t.Fatalf("invalid selftest not refused by name: %v", err)
	}
}

// TestCapabilityAC11: an unavailable capability keeps the primitive listed and
// derives capability_unavailable naming what is absent; available/none are ready.
func TestCapabilityAC11(t *testing.T) {
	cat, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}
	noneReg := RegistryFunc(func(string) bool { return false })

	// capability none: ready even with an all-denying registry
	if got := cat.Get("S-001").Status(noneReg); got.Status != StatusReady {
		t.Fatalf("S-001 (capability none): %+v, want ready", got)
	}
	// NET_ADMIN with a denying registry: capability_unavailable naming NET_ADMIN
	got := cat.Get("N-001").Status(noneReg)
	if got.Status != StatusCapabilityUnavailable {
		t.Fatalf("N-001 with no capabilities: %+v, want capability_unavailable", got)
	}
	if got.UnavailableNames != "NET_ADMIN" {
		t.Fatalf("unavailable names %q, want NET_ADMIN", got.UnavailableNames)
	}
	// the primitive stays listed
	if cat.Get("N-001") == nil {
		t.Fatal("N-001 vanished from the catalog (AC-11: the verb stays available)")
	}
	// and with an granting registry: ready
	okReg := RegistryFunc(func(kind string) bool { return kind == "NET_ADMIN" })
	if got := cat.Get("N-001").Status(okReg); got.Status != StatusReady {
		t.Fatalf("N-001 with NET_ADMIN granted: %+v, want ready", got)
	}
	// cgroup2 + docker both denied in one sweep
	for id, want := range map[string]string{"R-001": "cgroup2", "C-009": "docker", "I-002": "docker"} {
		got := cat.Get(id).Status(noneReg)
		if got.Status != StatusCapabilityUnavailable || got.UnavailableNames != want {
			t.Fatalf("%s: %+v, want capability_unavailable(%s)", id, got, want)
		}
	}
}

// TestTierParsing: Min parses the minimum level from compound tier strings.
func TestTierParsing(t *testing.T) {
	cat, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}
	want := map[string]int{
		"F-009": 0, // "L0 (copy) / L1 (declared path) / L2 (...)"
		"N-001": 2,
		"T-001": 1,
		"I-002": 3, // "L3 / L4"
		"N-012": 0,
		"S-004": 1,
	}
	for id, min := range want {
		if got := cat.Get(id).Tier.Min; got != min {
			t.Errorf("%s tier.Min=%d, want %d (raw %q)", id, got, min, cat.Get(id).Tier.Raw)
		}
	}
	if cat.Get("F-009").Tier.Raw == "" {
		t.Error("tier raw string lost")
	}
}

// TestParamsParsing: the corpus param shapes parse into typed specs.
func TestParamsParsing(t *testing.T) {
	cat, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}

	f9 := cat.Get("F-009")
	if k := f9.Params["path"].Kinds; len(k) != 1 || k[0] != KindStr {
		t.Errorf("F-009 path kinds = %v, want [str]", k)
	}
	mode := f9.Params["mode"]
	if len(mode.Enum) != 3 || mode.Enum[0] != "replace" {
		t.Errorf("F-009 mode enum = %v, want [replace unlink-then-create restore-snapshot]", mode.Enum)
	}
	if !f9.Params["backup"].Typed || f9.Params["backup"].Kinds[0] != KindBool || f9.Params["backup"].Optional {
		// the corpus declares "backup: bool" (no "?"): typed, required
		t.Errorf("F-009 backup = %+v, want required bool", f9.Params["backup"])

	}
	n1 := cat.Get("N-001")
	if !n1.Params["peer"].Optional || n1.Params["peer"].Kinds[0] != KindIPPort {
		t.Errorf("N-001 peer = %+v, want optional ip:port", n1.Params["peer"])
	}
	t1 := cat.Get("T-001")
	if got := t1.Params["syscalls"].Values; len(got) != 2 || got[0] != "clock_gettime" {
		t.Errorf("T-001 syscalls values = %v, want [clock_gettime gettimeofday]", got)
	}
	if t1.Params["pid"].Kinds[0] != KindSelector {
		t.Errorf("T-001 pid kinds = %v, want [selector]", t1.Params["pid"].Kinds)
	}
	r12 := cat.Get("N-012")
	if !r12.Params["retry_after"].Optional || r12.Params["retry_after"].Kinds[0] != KindDur {
		t.Errorf("N-012 retry_after = %+v, want optional dur", r12.Params["retry_after"])
	}
	r1 := cat.Get("R-001")
	if r1.Params["memory_max"].Typed {
		t.Errorf("R-001 memory_max decl %q should stay untyped (verbatim)", r1.Params["memory_max"].Decl)
	}
	if r1.Params["oom_group"].Kinds[0] != KindBool || !r1.Params["oom_group"].Optional {
		t.Errorf("R-001 oom_group = %+v", r1.Params["oom_group"])
	}
	// every corpus param parsed
	for id := range corpusIDSet() {
		if len(cat.Get(id).Params) == 0 {
			t.Errorf("%s has no params parsed", id)
		}
	}
}

func corpusIDSet() map[string]bool {
	m := map[string]bool{}
	for _, id := range corpusIDs {
		m[id] = true
	}
	return m
}

// TestInverseForms: the three declared inverse forms normalise.
func TestInverseForms(t *testing.T) {
	cat, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}
	// P-001: self-inverse via primitive id + params
	p1 := cat.Get("P-001")
	if p1.Inverse.Action != "P-001" {
		t.Errorf("P-001 inverse action = %q, want P-001", p1.Inverse.Action)
	}
	if p1.Inverse.Params["signal"] != "SIGCONT" {
		t.Errorf("P-001 inverse params = %v, want signal=SIGCONT", p1.Inverse.Params)
	}
	// N-001: mapping form with action + verify
	n1 := cat.Get("N-001")
	if n1.Inverse.Action != "delete-qdisc" || n1.Inverse.Verify == "" {
		t.Errorf("N-001 inverse = %+v", n1.Inverse)
	}
	// I-002: bare action string
	if cat.Get("I-002").Inverse.Action != "restore-state + restart-consumers" {
		t.Errorf("I-002 inverse action = %q", cat.Get("I-002").Inverse.Action)
	}
}

// TestCapabilitySplit: the capability string splits into token + detail.
func TestCapabilitySplit(t *testing.T) {
	cat, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}
	if got := cat.Get("R-001").Capability; got.Kind != "cgroup2" || got.Detail == "" {
		t.Errorf("R-001 capability = %+v, want cgroup2 + detail", got)
	}
	if got := cat.Get("T-001").Capability; got.Kind != "none" {
		t.Errorf("T-001 capability kind = %q, want none", got.Kind)
	}
	if got := cat.Get("C-009").Capability; got.Kind != "docker" {
		t.Errorf("C-009 capability kind = %q, want docker", got.Kind)
	}
}

// TestDuplicateIDRefused.
func TestDuplicateIDRefused(t *testing.T) {
	dir := t.TempDir()
	body := `id: D-001
name: dup
what: a fault
params: {path: str}
landed_proof: {kind: counter, check: "hits >= 1"}
inverse: restore
capability: none
tier: L1
backend: fs
maturity: proven
selftest: green
`
	for _, n := range []string{"a.yaml", "b.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, err := LoadDir(dir)
	if err == nil || !strings.Contains(err.Error(), "duplicate id") {
		t.Fatalf("duplicate id not refused: %v", err)
	}
}

// TestUnknownParamKindRefused: a param declaring no known kind and no enum
// stays untyped (any) — it must not be guessed; but an empty declaration is
// refused by name.
func TestUnknownParamKindRefused(t *testing.T) {
	dir := t.TempDir()
	body := `id: K-001
name: kinds
what: a fault
params: {path: ""}
landed_proof: {kind: counter, check: "hits >= 1"}
inverse: restore
capability: none
tier: L1
backend: fs
maturity: proven
selftest: green
`
	if err := os.WriteFile(filepath.Join(dir, "k.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadDir(dir)
	if err == nil || !strings.Contains(err.Error(), `params.path: empty type`) {
		t.Fatalf("empty param decl not refused by name: %v", err)
	}
}

// TestVersionContentDerived: same bytes -> same version; changed/added
// primitives -> different version.
func TestVersionContentDerived(t *testing.T) {
	dir := t.TempDir()
	body := `id: V-001
name: versioned
what: a fault
params: {path: str}
landed_proof: {kind: counter, check: "hits >= 1"}
inverse: restore
capability: none
tier: L1
backend: fs
maturity: proven
selftest: green
`
	if err := os.WriteFile(filepath.Join(dir, "v.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c1, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c1.Version != c2.Version {
		t.Fatalf("version unstable across identical loads: %s vs %s", c1.Version, c2.Version)
	}
	// add a primitive -> version moves
	body2 := strings.Replace(body, "id: V-001", "id: V-002", 1)
	if err := os.WriteFile(filepath.Join(dir, "v2.yaml"), []byte(body2), 0o644); err != nil {
		t.Fatal(err)
	}
	c3, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c3.Version == c1.Version {
		t.Fatal("version unchanged after adding a primitive")
	}
	// edit bytes (comment) -> version moves even though semantics equal
	if err := os.WriteFile(filepath.Join(dir, "v.yaml"), []byte(body+"\n# edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c4, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c4.Version == c1.Version {
		t.Fatal("version unchanged after editing descriptor bytes")
	}
}

// TestLoadDefaultFindsRepoRootFromSubdir: the walk-up resolution lets a
// subcommand binary load the catalog from any cwd inside the repo.
func TestLoadDefaultFindsRepoRootFromSubdir(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := os.Chdir(t.TempDir()); err == nil {
		// a temp dir outside the repo must NOT resolve
		if _, err := LoadDefault(); err == nil {
			t.Fatal("LoadDefault resolved outside the repo")
		}
	}
	if err := os.Chdir(wd); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Join(wd, "..", "..")); err != nil {
		t.Fatal(err)
	}
	// repo root itself
	cat, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault from repo root: %v", err)
	}
	if cat.Len() != len(corpusIDs) {
		t.Fatalf("got %d primitives from repo root", cat.Len())
	}
	// and from a nested dir (the package dir itself)
	if err := os.Chdir(wd); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDefault(); err != nil {
		t.Fatalf("LoadDefault from internal/catalog/testdata: %v", err)
	}
}

// TestLoadDirMissingDirNamesReason.
func TestLoadDirMissingDirNamesReason(t *testing.T) {
	_, err := LoadDir(filepath.Join(t.TempDir(), "absent"))
	if err == nil || !strings.Contains(err.Error(), "catalog dir") {
		t.Fatalf("missing dir not refused with named reason: %v", err)
	}
	empty := t.TempDir()
	_, err = LoadDir(empty)
	if err == nil || !strings.Contains(err.Error(), "no .yaml files") {
		t.Fatalf("empty dir not refused with named reason: %v", err)
	}
}
