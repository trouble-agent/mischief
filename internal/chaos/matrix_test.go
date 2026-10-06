package chaos

import (
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/catalog"
)

// matrix_test.go — the project-scoped matrix generator (M6): the artifact
// is derived from the project's OWN capability data (the host registry +
// the catalog's capability/tier fields), never a fleet-wide regurgitation
// (the row's scope guard). Cells are only those the project's chaos host
// can actually execute: capability ready (or explicitly absent with a
// reason), tier at or below the lane's ceiling, L0 scratch selector.

// reg is the CapabilityRegistry seam stub: docker answers, NET_ADMIN does
// not — the two-sided host the matrix tests derive against.
func reg(docker bool) catalog.CapabilityRegistry {
	return catalog.RegistryFunc(func(kind string) bool {
		switch {
		case kind == "docker":
			return docker
		default:
			return false // NET_ADMIN, cgroup2, ...: absent on this stub host
		}
	})
}

var testCat = &catalog.Catalog{
	Primitives: map[string]*catalog.Descriptor{
		"N-012": {ID: "N-012", Name: "http-error-injection", Capability: catalog.Capability{Kind: "none"}, Tier: &catalog.Tier{Raw: "L0", Min: 0}},
		"P-001": {ID: "P-001", Name: "sigstop", Capability: catalog.Capability{Kind: "none"}, Tier: &catalog.Tier{Raw: "L1", Min: 1}},
		"R-001": {ID: "R-001", Name: "memory-max", Capability: catalog.Capability{Kind: "cgroup2", Detail: "user scope"}, Tier: &catalog.Tier{Raw: "L1", Min: 1}},
		"C-009": {ID: "C-009", Name: "rolling-replace", Capability: catalog.Capability{Kind: "docker"}, Tier: &catalog.Tier{Raw: "L3", Min: 3}},
		"N-001": {ID: "N-001", Name: "partition", Capability: catalog.Capability{Kind: "NET_ADMIN"}, Tier: &catalog.Tier{Raw: "L2", Min: 2}},
	},
	IDs: []string{"C-009", "N-001", "N-012", "P-001", "R-001"},
}

// Project scope is data, not a constant: a lane derives its matrix for the
// project the lane names.
func TestProjectScopeIsLaneData(t *testing.T) {
	l := validLane(t)
	if l.Matrix.Scope != ScopeProject {
		t.Fatalf("scope %q, want %q", l.Matrix.Scope, ScopeProject)
	}
	if l.Project != "mischief" {
		t.Fatalf("project %q leaked from the lane", l.Project)
	}
}

func TestProjectMatrixFiltersByCapabilityAndTier(t *testing.T) {
	l := validLane(t)
	l.Matrix.TierMax = 1 // the rootless-bunker workhorse ceiling
	m := ProjectMatrix("mischief", testCat, reg(false), &l)

	// Included: L0/L1, capability none or cgroup2-probed... cgroup2 is
	// absent on the stub host, so R-001 must be named-absent.
	got := map[string]string{}
	for _, c := range m.Cells {
		got[c.Primitive] = c.Status
	}
	if got["N-012"] != "included" || got["P-001"] != "included" {
		t.Errorf("ready L0/L1 cells missing: %v", got)
	}
	if got["C-009"] == "included" {
		// docker answers on the stub, but C-009 is L3 — ABOVE the tier
		// ceiling: it must be excluded, never included.
		t.Errorf("C-009 above tier_max got %q", got["C-009"])
	}
}

func TestProjectMatrixExcludedCarriesReason(t *testing.T) {
	// A NULL must carry a reason (the fleet's own law): every excluded
	// cell names WHY — capability absent, or tier above the lane ceiling.
	l := validLane(t)
	l.Matrix.TierMax = 3
	m := ProjectMatrix("mischief", testCat, reg(true), &l)
	for _, c := range m.Cells {
		if c.Status == "included" && c.Reason != "" {
			t.Errorf("included cell %s carries an exclusion reason: %s", c.Primitive, c.Reason)
		}
		if c.Status != "included" && strings.TrimSpace(c.Reason) == "" {
			t.Errorf("excluded cell %s (%s) carries no reason", c.Primitive, c.Status)
		}
	}
	// Spot-check the two exclusion classes:
	byPrim := map[string]ProjectCell{}
	for _, c := range m.Cells {
		byPrim[c.Primitive] = c
	}
	if c := byPrim["R-001"]; c.Status != "capability_absent" || !strings.Contains(c.Reason, "cgroup2") {
		t.Errorf("R-001 = %+v, want capability_absent naming cgroup2", c)
	}
	if c := byPrim["N-001"]; c.Status != "capability_absent" || !strings.Contains(c.Reason, "NET_ADMIN") {
		t.Errorf("N-001 = %+v, want capability_absent naming NET_ADMIN", c)
	}
	if c := byPrim["C-009"]; c.Status != "included" {
		t.Errorf("C-009 = %+v, want included (docker present, L3 <= tier_max 3)", c)
	}
}

func TestProjectMatrixTierCeiling(t *testing.T) {
	l := validLane(t)
	l.Matrix.TierMax = 1
	m := ProjectMatrix("mischief", testCat, reg(true), &l)
	byPrim := map[string]ProjectCell{}
	for _, c := range m.Cells {
		byPrim[c.Primitive] = c
	}
	for _, above := range []string{"C-009", "N-001"} { // L3, L2 above the ceiling
		if c := byPrim[above]; c.Status != "tier_above_ceiling" {
			t.Errorf("%s = %+v, want tier_above_ceiling", above, c)
		}
	}
	for _, in := range []string{"N-012", "P-001", "R-001"} {
		// R-001: tier ok but cgroup2 absent on this host → capability_absent
		if c := byPrim[in]; c.Status != "included" && c.Status != "capability_absent" {
			t.Errorf("%s = %+v, want included or capability_absent", in, c)
		}
	}
}

// Empty catalog → empty matrix WITH the project stamped (and no panic);
// the executor refuses an empty matrix, so the artifact marks it.
func TestProjectMatrixEmptyCatalog(t *testing.T) {
	m := ProjectMatrix("other", &catalog.Catalog{Primitives: map[string]*catalog.Descriptor{}}, reg(true), mustLane(t))
	if m.Project != "other" {
		t.Errorf("project %q, want other", m.Project)
	}
	if len(m.Cells) != 0 {
		t.Errorf("cells %d, want 0", len(m.Cells))
	}
}

// The schema stamps the artifact as project-scoped.
func TestProjectMatrixSchema(t *testing.T) {
	m := ProjectMatrix("mischief", testCat, reg(false), mustLane(t))
	if m.Schema != "mischief.chaos.matrix/v1" {
		t.Errorf("schema %q", m.Schema)
	}
	if m.Scope != ScopeProject {
		t.Errorf("scope %q, want project", m.Scope)
	}
}

func TestRenderMatrixMarkdownHasProjectAndCells(t *testing.T) {
	l := validLane(t)
	l.Matrix.TierMax = 3
	m := ProjectMatrix("mischief", testCat, reg(true), &l)
	md := RenderMatrixMarkdown(m)
	if !strings.Contains(md, "mischief") {
		t.Error("markdown missing the project name")
	}
	if !strings.Contains(md, "N-012") || !strings.Contains(md, "capability_absent") {
		t.Error("markdown missing cells or exclusion states")
	}
}

// mustLane returns a pointer to a valid lane (ProjectMatrix takes *Lane).
func mustLane(t *testing.T) *Lane {
	t.Helper()
	l := validLane(t)
	return &l
}
