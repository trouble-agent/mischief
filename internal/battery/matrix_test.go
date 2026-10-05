package battery

import (
	"sort"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/catalog"
)

// matrix_test.go — matrix generation + the --quick parity contract (the
// S-5 half of MSF-011: the quick matrix covers exactly the five legacy
// bash cells, each named alongside the primitive that replaces it).

// TestQuickMatrixCoversExactlyTheFiveLegacyCells is THE parity test: the
// quick matrix carries exactly the five bunker-qa.sh chaos cells, one cell
// per legacy cell, each annotated with the replacing primitive. A
// renamed/dropped/extra cell fails here — which is the point: the
// replacement set is pinned before the legacy cells are deleted.
func TestQuickMatrixCoversExactlyTheFiveLegacyCells(t *testing.T) {
	legacy := LegacyCellNames()
	if len(legacy) != 5 {
		t.Fatalf("legacy cell count %d, want 5 (bunker-qa.sh carries exactly five chaos cells)", len(legacy))
	}
	want := map[string]bool{
		"chaos-disconnect": true,
		"chaos-shutdown":   true,
		"chaos-corruption": true,
		"chaos-resource":   true,
		"chaos-errorpath":  true,
	}
	got := map[string]bool{}
	for _, c := range legacy {
		got[c] = true
	}
	for c := range want {
		if !got[c] {
			t.Errorf("parity table missing legacy cell %q", c)
		}
	}
	for c := range got {
		if !want[c] {
			t.Errorf("parity table names unknown cell %q (not one of the five bunker-qa.sh chaos cells)", c)
		}
	}

	m := QuickMatrix()
	if !m.Quick {
		t.Fatal("QuickMatrix().Quick = false")
	}
	if len(m.Cells) != 5 {
		t.Fatalf("quick matrix has %d cells, want exactly 5", len(m.Cells))
	}
	seen := map[string]bool{}
	for _, c := range m.Cells {
		if c.LegacyCell == "" {
			t.Errorf("quick cell %s carries no legacy cell name", c.ID)
			continue
		}
		if !want[c.LegacyCell] {
			t.Errorf("quick cell %s names %q, which is not one of the five bunker-qa.sh cells", c.ID, c.LegacyCell)
		}
		if seen[c.LegacyCell] {
			t.Errorf("legacy cell %q covered twice", c.LegacyCell)
		}
		seen[c.LegacyCell] = true
		if c.Replaces == "" {
			t.Errorf("quick cell %s (%s) carries no mapping rationale", c.ID, c.LegacyCell)
		}
		if !c.Target.Scratch {
			t.Errorf("quick cell %s target is not scratch", c.ID)
		}
	}
	for c := range want {
		if !seen[c] {
			t.Errorf("quick matrix never covers legacy cell %q", c)
		}
	}
}

// TestQuickPrimitivesAreCatalogPrimitives: every primitive the parity
// table names must exist in the real in-repo catalog — a parity row that
// names a phantom primitive would silently void the replacement claim.
func TestQuickPrimitivesAreCatalogPrimitives(t *testing.T) {
	cat, err := catalog.LoadDefault()
	if err != nil {
		t.Fatalf("catalog load: %v", err)
	}
	for _, lc := range LegacyCells() {
		d := cat.Get(lc.Primitive)
		if d == nil {
			t.Errorf("parity table names %q, which is not in catalog/faults", lc.Primitive)
			continue
		}
		// the replacing primitive must state the fault contract (the
		// loader already refuses otherwise — belt and braces: the parity
		// artefact will quote these fields)
		if d.LandedProof.Kind == "" || d.Inverse.Action == "" {
			t.Errorf("%s (%s): incomplete fault contract", lc.Primitive, d.Name)
		}
	}
}

// TestQuickMatrixReachesEveryCellLive is the LIVE parity half: the real
// catalog + runner execute the quick matrix end to end. Cells whose
// actuator cannot run on the L0 scratch harness record the named skip
// (never a silent pass); every cell must grade a closed-vocabulary
// verdict and a reason-bearing detection entry. This is the run whose
// artefact a later milestone cites when deleting the bash cells.
func TestQuickMatrixReachesEveryCellLive(t *testing.T) {
	cat, err := catalog.LoadDefault()
	if err != nil {
		t.Fatalf("catalog load: %v", err)
	}
	rn := NewRunner(cat)
	res, err := rn.Run(QuickMatrix())
	if err != nil {
		t.Fatalf("quick run: %v", err)
	}
	if len(res.Cells) != 5 {
		t.Fatalf("ran %d cells, want 5", len(res.Cells))
	}
	for _, c := range res.Cells {
		switch c.SelftestState {
		case "pass":
			if c.LandProof == "" || c.RevertProof == "" {
				t.Errorf("%s (%s) PASSED without both proofs", c.Cell.ID, c.Cell.Primitive)
			}
		case "skip":
			if c.SkipReason == "" {
				t.Errorf("%s (%s) SKIPped without naming the missing piece", c.Cell.ID, c.Cell.Primitive)
			}
		default:
			t.Errorf("%s (%s) graded %q: %s — a red battery cell on the sanctioned scratch host is a real finding; investigate, never normalize it here",
				c.Cell.ID, c.Cell.Primitive, c.SelftestState, c.Reason)
		}
		if c.Detection.State == "" {
			t.Errorf("%s: detection axis empty (every cell records a detection entry)", c.Cell.ID)
		}
		if c.Detection.State == "not_wired" && c.Detection.Reason == "" {
			t.Errorf("%s: not-wired detection entry carries no reason", c.Cell.ID)
		}
		if c.Recovery == "" {
			t.Errorf("%s: recovery axis empty", c.Cell.ID)
		}
	}
}

// TestDefaultMatrixCrossProduct: the default matrix is the project ×
// primitive cross product, sorted, each cell scratch.
func TestDefaultMatrixCrossProduct(t *testing.T) {
	m := DefaultMatrix([]string{"beta", "alpha", "beta"}, []string{"P-001", "F-009", ""})
	if len(m.Cells) != 4 {
		t.Fatalf("cells %d, want 4 (2 projects × 2 primitives; dupes/empties dropped)", len(m.Cells))
	}
	// sorted by project then primitive
	type key struct{ p, pr string }
	var got []key
	for _, c := range m.Cells {
		got = append(got, key{c.Target.Project, c.Primitive})
		if !c.Target.Scratch {
			t.Errorf("cell %s not scratch", c.ID)
		}
	}
	want := []key{{"alpha", "F-009"}, {"alpha", "P-001"}, {"beta", "F-009"}, {"beta", "P-001"}}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cell %d = %v, want %v", i, got[i], want[i])
		}
	}
	ids := map[string]bool{}
	for _, c := range m.Cells {
		if ids[c.ID] {
			t.Errorf("duplicate cell id %q", c.ID)
		}
		ids[c.ID] = true
	}
}

// TestQuickPrimitivesInCellOrder: the parity order is the bunker-qa.sh
// cell order — the artefact's parity table must read in the same order the
// legacy file runs its cells.
func TestQuickPrimitivesInCellOrder(t *testing.T) {
	m := QuickMatrix()
	var got []string
	for _, c := range m.Cells {
		got = append(got, c.LegacyCell)
	}
	want := LegacyCellNames()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cell %d = %q, want %q (the parity order is bunker-qa.sh order)", i, got[i], want[i])
		}
	}
}

// TestMatrixPrimitivesNoDup: Matrix.Primitives dedupes in cell order.
func TestMatrixPrimitivesNoDup(t *testing.T) {
	m := QuickMatrix()
	prims := m.Primitives()
	if len(prims) != len(uniq(prims)) {
		t.Errorf("Primitives() returned duplicates: %v", prims)
	}
	if !sort.StringsAreSorted(prims) {
		// F-009 twice (corruption + errorpath), the rest unique and sorted
		// — actually: cell order is NOT sorted order; just assert the
		// count is right (4 distinct of 5 cells)
	}
	if len(prims) != 4 {
		t.Errorf("distinct primitives %d, want 4 (N-012, I-002, F-009, R-001)", len(prims))
	}
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// TestSelectorSafety: a target label with hostile characters is
// sanitized; the selector always keeps a known kind prefix.
func TestSelectorSafety(t *testing.T) {
	tgt := scratchTarget("my/proj:weird label", "chaos/disconnect")
	if !strings.HasPrefix(tgt.Selector, "host:") {
		t.Errorf("selector %q lost its kind prefix", tgt.Selector)
	}
	if strings.ContainsAny(tgt.Name, "/: ") {
		t.Errorf("target name %q carries selector-hostile characters", tgt.Name)
	}
}
