// Package doctor is the doctor verb's engine: load the catalog, derive the
// capability/tier ladder per primitive, report. It refuses NOTHING extra
// (the brief: "reports capability/tier ladder, refuses nothing extra") —
// doctor is a read-only surface: it never writes, never arms, never probes
// beyond what the capability registry itself reads.
package doctor

import (
	"fmt"
	"sort"

	"github.com/trouble-agent/mischief/internal/catalog"
)

// Row is one primitive's doctor line.
type Row struct {
	ID         string
	Name       string
	Backend    string
	Tier       string
	Capability string
	Status     catalog.Status
	// UnavailableNames names the absent capability when Status is
	// capability_unavailable (AC-11: naming what is absent).
	UnavailableNames string
	Selftest         catalog.SelftestState
	Maturity         string
}

// Report is the doctor verb's output.
type Report struct {
	// CatalogVersion is the loaded catalog's content version ("" when the
	// load failed; LoadErr then names the failure).
	CatalogVersion string
	// LoadErr names the catalog load failure, when one occurred.
	LoadErr string
	// Rows is one row per primitive, sorted by id.
	Rows []Row
	// RegistryNotes records what the capability registry measured (one
	// line per capability kind actually consulted, sorted).
	RegistryNotes []string
}

// Run loads the catalog at dir ("" = LoadDefault) and derives every
// primitive's row. The registry seam decides availability exactly as the
// catalog's Status does (AC-11); notes name each capability kind the
// registry was asked about, so the report shows its own evidence base.
func Run(dir string, reg catalog.CapabilityRegistry) (*Report, error) {
	var cat *catalog.Catalog
	var err error
	if dir == "" {
		cat, err = catalog.LoadDefault()
	} else {
		cat, err = catalog.LoadDir(dir)
	}
	if err != nil {
		return &Report{LoadErr: err.Error()}, nil
	}
	rep := &Report{CatalogVersion: cat.Version}
	asked := map[string]bool{}
	for _, id := range cat.IDs {
		d := cat.Primitives[id]
		st := d.Status(reg)
		if d.Capability.Kind != "none" && !asked[d.Capability.Kind] {
			asked[d.Capability.Kind] = true
			rep.RegistryNotes = append(rep.RegistryNotes,
				fmt.Sprintf("capability %q: registry reports %v", d.Capability.Kind, st.Status))
		}
		rep.Rows = append(rep.Rows, Row{
			ID:               d.ID,
			Name:             d.Name,
			Backend:          d.Backend,
			Tier:             d.Tier.Raw,
			Capability:       d.Capability.Kind,
			Status:           st.Status,
			UnavailableNames: st.UnavailableNames,
			Selftest:         d.Selftest,
			Maturity:         d.Maturity,
		})
	}
	sort.Slice(rep.RegistryNotes, func(i, j int) bool { return rep.RegistryNotes[i] < rep.RegistryNotes[j] })
	return rep, nil
}

// Render produces the report's deterministic text form (sorted rows,
// sorted notes — no clock, no host state beyond the registry's answers).
func Render(r *Report) []byte {
	var b []byte
	appendf := func(format string, args ...any) {
		b = append(b, fmt.Sprintf(format, args...)...)
	}
	if r.LoadErr != "" {
		appendf("doctor: catalog load FAILED: %s\n", r.LoadErr)
		return b
	}
	appendf("doctor: catalog %s loaded (%d primitives, version %s)\n", "ok", len(r.Rows), r.CatalogVersion)
	appendf("%-8s %-22s %-8s %-6s %-22s %-24s %-8s %s\n", "ID", "NAME", "BACKEND", "TIER", "CAPABILITY", "STATUS", "SELFTEST", "MATURITY")
	for _, row := range r.Rows {
		cap := row.Capability
		status := string(row.Status)
		if row.UnavailableNames != "" {
			status += " (" + row.UnavailableNames + ")"
		}
		appendf("%-8s %-22s %-8s %-6s %-22s %-24s %-8s %s\n",
			row.ID, truncate(row.Name, 22), row.Backend, truncate(row.Tier, 6), truncate(cap, 22), truncate(status, 24), row.Selftest, row.Maturity)
	}
	for _, n := range r.RegistryNotes {
		appendf("note: %s\n", n)
	}
	return b
}

// truncate caps s at n runes for table alignment (no ellipsis — the row is
// data, not prose).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
