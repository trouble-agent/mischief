package chaos

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/trouble-agent/mischief/internal/catalog"
)

// matrix.go — the project-scoped matrix generator (M6): the artifact is
// derived from the project's OWN capability data — the catalog's
// capability + tier fields probed against the chaos host's
// CapabilityRegistry — never a fleet-wide regurgitation (the row's scope
// guard: one lane per project, the matrix scoped to THAT project).
//
// A cell is included only when the project's chaos host can actually
// execute it:
//
//   - capability: the descriptor's capability kind must answer ready on
//     the registry (catalog.Status — the AC-11 derivation, not a
//     string map). An absent capability still APPEARS on the matrix,
//     named capability_absent with the missing kind in the reason —
//     a NULL must carry a reason (the fleet's null-with-reason law);
//   - tier: the descriptor's minimum tier must be at or below the lane's
//     tier_max ceiling; above it the cell is named tier_above_ceiling;
//   - selector: every cell targets L0 scratch (the battery's own
//     NOT-LIST discipline), so the lane never arms anything against a
//     real target by construction.
//
// The order of the checks is deliberate: tier first (a declared ceiling
// is policy, not a host fact), then capability (the host's measured
// answer). A cell above the ceiling is never even probed.

// ProjectCell is one row of the project matrix: the primitive, whether
// the lane will execute it, and the reason when it will not.
type ProjectCell struct {
	// Primitive is the catalog primitive id.
	Primitive string `json:"primitive"`
	// Name is the primitive's human name.
	Name string `json:"name"`
	// Capability is the declared capability kind ("none", "docker", ...).
	Capability string `json:"capability"`
	// TierRaw is the declared tier string verbatim.
	TierRaw string `json:"tier_raw"`
	// TierMin is the parsed minimum L-level.
	TierMin int `json:"tier_min"`
	// Status: "included" (the lane will execute this cell),
	// "tier_above_ceiling", or "capability_absent".
	Status string `json:"status"`
	// Reason carries the exclusion reason (empty when included).
	Reason string `json:"reason,omitempty"`
}

// MatrixArtifact is the generated artifact: mischief.chaos.matrix/v1.
type MatrixArtifact struct {
	// Schema stamps the artifact ("mischief.chaos.matrix/v1").
	Schema string `json:"schema"`
	// Project is the ONE project the matrix is scoped to.
	Project string `json:"project"`
	// Scope is "project" (the generator refuses to render any other).
	Scope string `json:"scope"`
	// TierMax is the lane's tier ceiling the cells were filtered by.
	TierMax int `json:"tier_max"`
	// CatalogVersion is the content-derived catalog version the cells
	// were derived from.
	CatalogVersion string `json:"catalog_version"`
	// GeneratedAt is the unix second of generation.
	GeneratedAt int64 `json:"generated_at"`
	// Cells is the primitive set, sorted by primitive id.
	Cells []ProjectCell `json:"cells"`
}

// ProjectMatrix renders the project-scoped matrix for one project from
// the project's own capability data. An empty catalog yields an empty
// matrix (the executor refuses an empty matrix; the artifact honestly
// shows zero cells rather than pretending).
func ProjectMatrix(project string, cat *catalog.Catalog, reg catalog.CapabilityRegistry, l *Lane) *MatrixArtifact {
	m := &MatrixArtifact{
		Schema:  MatrixSchema,
		Project: project,
		Scope:   ScopeProject,
		TierMax: l.Matrix.TierMax,
	}
	if cat == nil {
		return m
	}
	m.CatalogVersion = cat.Version
	m.GeneratedAt = time.Now().Unix()
	for _, id := range cat.IDs {
		d := cat.Get(id)
		if d == nil {
			continue
		}
		cell := ProjectCell{
			Primitive:  d.ID,
			Name:       d.Name,
			Capability: d.Capability.Kind,
			Status:     "included",
		}
		if d.Tier != nil {
			cell.TierRaw = d.Tier.Raw
			cell.TierMin = d.Tier.Min
			if d.Tier.Min > l.Matrix.TierMax {
				cell.Status = "tier_above_ceiling"
				cell.Reason = fmt.Sprintf("declared minimum tier L%d is above the lane's tier_max %d (%s)", d.Tier.Min, l.Matrix.TierMax, d.Tier.Raw)
				m.Cells = append(m.Cells, cell)
				continue
			}
		}
		// Capability: the AC-11 derivation via the registry seam (the
		// same Status the doctor/plan verbs show), not a string map.
		if st := d.Status(reg); st.Status == catalog.StatusCapabilityUnavailable {
			cell.Status = "capability_absent"
			cell.Reason = fmt.Sprintf("capability %q unavailable on the chaos host: %s", d.Capability.Kind, st.UnavailableNames)
		}
		m.Cells = append(m.Cells, cell)
	}
	sort.Slice(m.Cells, func(i, j int) bool { return m.Cells[i].Primitive < m.Cells[j].Primitive })
	return m
}

// RenderMatrixMarkdown renders the artifact as the markdown table the
// board finding (and the human reviewer) reads.
func RenderMatrixMarkdown(m *MatrixArtifact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# chaos matrix — %s (project-scoped)\n\n", m.Project)
	fmt.Fprintf(&b, "- schema: `%s`\n", m.Schema)
	fmt.Fprintf(&b, "- scope: %s (one lane, one project — never a fleet-wide artifact)\n", m.Scope)
	fmt.Fprintf(&b, "- tier ceiling: L%d\n", m.TierMax)
	fmt.Fprintf(&b, "- catalog: %s\n", m.CatalogVersion)
	fmt.Fprintf(&b, "\n| primitive | name | capability | tier | status | reason |\n")
	fmt.Fprintf(&b, "|---|---|---|---|---|---|\n")
	for _, c := range m.Cells {
		reason := c.Reason
		if reason == "" {
			reason = "—"
		}
		fmt.Fprintf(&b, "| `%s` | %s | `%s` | L%d | %s | %s |\n",
			c.Primitive, c.Name, c.Capability, c.TierMin, c.Status, reason)
	}
	return b.String()
}
