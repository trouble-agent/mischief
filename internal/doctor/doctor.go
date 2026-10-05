// Package doctor is the doctor verb's engine: load the catalog, derive the
// capability/tier ladder per primitive, report. It refuses NOTHING extra
// (the brief: "reports capability/tier ladder, refuses nothing extra") —
// doctor is a read-only surface: it never writes, never arms, never probes
// beyond what the capability registry itself reads.
package doctor

import (
	"fmt"
	"sort"
	"strings"

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
	// capability_unavailable (AC-11: naming what is absent). MSF-012
	// upgrade: when the absence is the privileged surface's, this names
	// the missing ARTEFACT ("sudoers drop-in not installed (expected
	// /etc/sudoers.d/mischief) — run `mischief install`").
	UnavailableNames string
	// MissingPiece is the SPEC-11 posture's missing piece when different
	// from the capability kind itself ("" = no privileged-surface cause).
	MissingPiece string
	Selftest     catalog.SelftestState
	Maturity     string
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
	// Rails carries the safety-floor self-checks (MSF-014 deliverable 6:
	// sanction, load gate read, scope-ladder shape), derived from the LIVE
	// host when Run was asked to (RunRails=true). Additive: the zero value
	// renders as no rails section, so existing consumers of Report are
	// unaffected.
	Rails []RailsRow
	// Catalog is the catalog the report was built from (nil on load
	// failure). RunRailsSelfChecks consumes it; tests may also hand-build
	// a synthetic catalog and pass it directly.
	Catalog *catalog.Catalog
	// Ops is the SPEC-11 privileged-surface section (MSF-012), appended
	// by RunOps when the caller asks for it. Additive: the zero value
	// (nil) renders as no ops section — existing consumers unaffected.
	Ops *OpsSection
	// JSON emits the report as JSON instead of the text table (cmd-level
	// --json; the shape is this struct with the audit embedded).
	JSON bool
}

// Row gains MissingPiece (MSF-012): the SPEC-11 posture's named missing
// artefact ("sudoers drop-in not installed", "helper manifest missing",
// "drop-in drifted", "grant missing: netns_add") when the primitive's
// capability is absent because of the privileged surface. Empty on rows
// whose absence has no privileged-surface cause.
// (Field on the existing Row struct — see doctor.go's Row.)

// RailsRow is one rails self-check's doctor line.
type RailsRow struct {
	// Check is the check name ("sanction_marker", "load_gate",
	// "scope_ladder").
	Check string
	// OK reports whether the check passed on this host right now.
	OK bool
	// Detail is the measured one-line reason. On a pass it names what was
	// measured; on a fail it names WHY (the refusal text — never a bare
	// "failed").
	Detail string
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
	rep := &Report{CatalogVersion: cat.Version, Catalog: cat}
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

// RunRailsSelfChecks derives the rails self-check rows from the LIVE host
// and appends them to rep.Rails (additive — Run's signature and the
// existing rows are untouched). The checks, in PRD §7's rail order:
//
//   - sanction_marker: the SPEC-13/MSF-020 fail-closed gate. Production
//     resolution (zero-value Options: live hostname, live env, default
//     marker path) — on the unsanctioned main host this row FAILS, and the
//     failure is the report's most important line.
//   - load_gate: the live /proc reading (rails.ReadLoad) against the same
//     thresholds mischief's own run path uses (load1 12, PSI io 50%).
//   - scope_ladder: every descriptor's declared tier parses and sits inside
//     L0..L5 (the ladder the scope check walks is well-formed).
//
// A failing check never aborts the report: each lands as a row with
// OK=false and the named reason (the refusal text, never a bare "failed");
// the CLI decides the exit code from the rows.
func RunRailsSelfChecks(rep *Report, cat *catalog.Catalog) {
	if rep == nil || cat == nil {
		return
	}
	// 1. sanction marker (SPEC-13) — production resolution by default
	// (zero-value Options: live hostname, live env, default marker path);
	// the seam var is package-level so tests can pin both arms without
	// touching the host's real sanction state.
	if err := SanctionCheck(); err != nil {
		rep.Rails = append(rep.Rails, RailsRow{Check: "sanction_marker", OK: false, Detail: err.Error()})
	} else {
		rep.Rails = append(rep.Rails, RailsRow{Check: "sanction_marker", OK: true,
			Detail: "sanction marker present; host admitted for fault injection"})
	}

	// 2. load gate — the live measurement, checked against the run path's
	// thresholds; the refusal text names the numbers (AC-8).
	if reading, err := ReadLoad(); err != nil {
		rep.Rails = append(rep.Rails, RailsRow{Check: "load_gate", OK: false,
			Detail: fmt.Sprintf("host load unreadable (%v)", err)})
	} else if err := CheckLoad(reading); err != nil {
		rep.Rails = append(rep.Rails, RailsRow{Check: "load_gate", OK: false, Detail: err.Error()})
	} else {
		rep.Rails = append(rep.Rails, RailsRow{Check: "load_gate", OK: true,
			Detail: fmt.Sprintf("loadavg 1m %.2f, 5m %.2f, psi io some avg10 %.2f%% (gate: load<%.0f, psi<%.0f%%)",
				reading.Load1, reading.Load5, reading.PSIIoSomeAvg10, LoadThreshold, PSIThreshold)})
	}

	// 3. scope ladder shape — every descriptor's tier inside L0..L5.
	var bad []string
	for _, id := range cat.IDs {
		d := cat.Primitives[id]
		if d.Tier == nil || d.Tier.Min < 0 || d.Tier.Min > 5 {
			if d.Tier == nil {
				bad = append(bad, id+": tier missing")
			} else {
				bad = append(bad, fmt.Sprintf("%s: tier min L%d outside L0..L5", id, d.Tier.Min))
			}
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		rep.Rails = append(rep.Rails, RailsRow{Check: "scope_ladder", OK: false,
			Detail: "ladder violations: " + strings.Join(bad, "; ")})
	} else {
		rep.Rails = append(rep.Rails, RailsRow{Check: "scope_ladder", OK: true,
			Detail: fmt.Sprintf("every descriptor's tier inside L0..L5 (%d primitives)", len(cat.IDs))})
	}
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
	appendRails(&b, r.Rails)
	return b
}

// appendRails renders the rails self-check section (declared order, the
// rail order of PRD §7 — no re-sort, the order is part of the contract).
func appendRails(b *[]byte, rows []RailsRow) {
	if len(rows) == 0 {
		return
	}
	appendf := func(format string, args ...any) {
		*b = append(*b, fmt.Sprintf(format, args...)...)
	}
	appendf("rails self-checks:\n")
	for _, r := range rows {
		status := "FAIL"
		if r.OK {
			status = "ok"
		}
		appendf("  %-4s %-16s %s\n", status, r.Check, r.Detail)
	}
}

// truncate caps s at n runes for table alignment (no ellipsis — the row is
// data, not prose).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
