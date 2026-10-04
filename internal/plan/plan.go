// Package plan is the M1 plan verb's emission engine: it resolves an
// experiment against the catalog and the rails and renders the resolved
// plan — WITHOUT touching anything (AC-1: no side effects, the target state
// byte-identical, no writes outside a run dir).
//
// The plan is the dry-run half of the loop (PRD §4: declare → plan → …);
// it carries everything `run` would need — resolved target, per-fault
// capability verdicts, the inverse plan (write-ahead preview), the blast
// bound — and nothing it would need secrets or live host writes for.
//
// NOT-LIST (what plan does not do at M1):
//
//   - It does not land, arm or journal: zero side effects by construction
//     (the only writer the caller may pass is a run-dir file).
//   - It does not choose actuators (SPEC-06/07 backends); it reports what
//     the rails would admit and what the host is missing (AC-11).
package plan

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/trouble-agent/mischief/internal/catalog"
	"github.com/trouble-agent/mischief/internal/journal"
	"github.com/trouble-agent/mischief/internal/rails"
)

// Fault is one fault's resolved plan row.
type Fault struct {
	// Primitive is the catalog primitive id.
	Primitive string
	// Params are the declared params (sorted-key canonical text for the
	// byte-identical rendering).
	Params string
	// TTL is the declared hold bound.
	TTL time.Duration
	// TTLRaw is the declared TTL string verbatim.
	TTLRaw string
	// LandedProof is the descriptor's landing-proof contract.
	LandedProof catalog.LandedProof
	// Inverse is the descriptor's reverter contract (the write-ahead
	// preview: this is what Arm would journal before landing).
	Inverse catalog.Inverse
	// Capability is the declared capability and the derived status
	// (AC-11: available, or naming what is absent).
	Capability string
	// Status is the derived availability ("ready" / "capability_unavailable").
	Status catalog.Status
	// UnavailableNames carries the missing capability (Status only).
	UnavailableNames string
	// Tier is the declared ladder placement (raw string).
	Tier string
	// Selftest is the declared selftest state (AC-19 relevance).
	Selftest catalog.SelftestState
}

// Plan is the resolved plan for one experiment.
type Plan struct {
	// ExperimentID is the declared display id.
	ExperimentID string
	// RunID is the content-derived run id (hex(sha256(spec+seed))[:16]) —
	// the id `run` would journal under.
	RunID string
	// Seed is the declared seed.
	Seed int64
	// Target is the resolved target (kind + selector, as declared).
	Target rails.Target
	// Faults is the resolved fault set (sorted by primitive id).
	Faults []Fault
	// Probes is the declared probe set (verbatim).
	Probes []*journal.ProbeDecl
	// Budget is the declared recovery budget.
	Budget journal.Budget
	// Refusals is the rail refusals the plan surfaced (empty = clean plan).
	Refusals []rails.RefusalReason
	// Notes carries non-refusal advisory lines (capability verdicts,
	// selftest states) that an operator should read before running.
	Notes []string
	// CatalogVersion is the content-derived catalog version the plan
	// resolved against.
	CatalogVersion string
}

// Build resolves one loaded experiment against the catalog, the rails and
// the host's capability registry. It never writes, never forks, never
// touches the network: the only host reads are the capability registry's
// (and the caller may inject a fully-available or fully-absent registry —
// tests do exactly that).
func Build(exp *journal.Experiment, specBytes []byte, cat *catalog.Catalog, reg catalog.CapabilityRegistry, scope rails.Scope) (*Plan, error) {
	p := &Plan{
		ExperimentID:   exp.ID,
		RunID:          journal.RunID(specBytes, exp.Seed),
		Seed:           exp.Seed,
		Target:         rails.Target{Kind: rails.TargetHost, Selector: exp.Target.Selector},
		CatalogVersion: cat.Version,
	}

	// Target resolution (AC-2/AC-7 shape): the experiment declares the
	// selector; resolve it through the rails BEFORE anything else so a
	// protected or excluded target refuses at resolution, not at landing.
	decl := rails.DeclaredTarget{Kind: kindFor(exp.Target.Selector), Selector: exp.Target.Selector}
	t, err := rails.Resolve(decl, rails.ResolveOptions{SelfPID: 0, SelfExe: ""})
	if err != nil {
		var re *rails.ResolveError
		if asResolve(err, &re) {
			p.Refusals = append(p.Refusals, re.Reason)
		}
		return p, err
	}
	p.Target = t

	for _, f := range exp.Faults { // exp.Faults is already sorted (AC-10 order)
		row := Fault{
			Primitive: f.Primitive,
			Params:    canonicalKV(f.Params),
			TTLRaw:    f.TTL,
			TTL:       parseTTL(f.TTL),
		}
		if d := cat.Get(f.Primitive); d != nil {
			row.LandedProof = d.LandedProof
			row.Inverse = d.Inverse
			row.Capability = d.Capability.Kind
			row.Tier = d.Tier.Raw
			row.Selftest = d.Selftest
			st := d.Status(reg)
			row.Status = st.Status
			row.UnavailableNames = st.UnavailableNames
			// Rails: scope ladder + selftest (AC-19) — plan reports, run
			// enforces; a refusal here is surfaced, not fatal to the plan
			// (the operator sees the whole picture in one pass).
			if err := rails.CheckScope(d, scope); err != nil {
				if ref, ok := asRefusal(err); ok {
					p.Refusals = append(p.Refusals, ref.Reason)
					p.Notes = append(p.Notes, fmt.Sprintf("%s: %v", f.Primitive, err))
				}
			}
			if st.Status == catalog.StatusCapabilityUnavailable {
				p.Notes = append(p.Notes, fmt.Sprintf("%s: capability_unavailable: %s is absent on this host", f.Primitive, st.UnavailableNames))
			}
		} else {
			p.Refusals = append(p.Refusals, rails.ReasonScope)
			p.Notes = append(p.Notes, fmt.Sprintf("%s: not in the loaded catalog", f.Primitive))
		}
		p.Faults = append(p.Faults, row)
	}
	p.Probes = append(p.Probes, exp.Probe...)
	p.Budget = exp.Budget
	return p, nil
}

// canonicalKV renders a params map sorted-key "k=v,..." (the journal
// package's canonicalParams shape, re-declared here to keep the plan's
// dependency surface flat).
func canonicalKV(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(m[k])
	}
	return b.String()
}

// parseTTL parses a declared TTL ("20s", "90s") — 0 when unparsable (the
// declared string stays verbatim in TTLRaw; a bad TTL is run's refusal).
func parseTTL(s string) time.Duration {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return d
}

// kindFor maps a declared selector onto the rails' target kind (the PRD's
// five classes; the selector's prefix is the kind when declared as
// "kind:rest").
func kindFor(selector string) rails.TargetKind {
	s := selector
	for _, k := range []struct {
		prefix string
		kind   rails.TargetKind
	}{
		{"process:", rails.TargetProcess},
		{"pid:", rails.TargetProcess},
		{"cgroup:", rails.TargetCgroup},
		{"container:", rails.TargetContainer},
		{"host:", rails.TargetHost},
		{"cloud:", rails.TargetCloud},
	} {
		if rest, ok := strings.CutPrefix(s, k.prefix); ok {
			_ = rest
			return k.kind
		}
	}
	return rails.TargetHost
}

// asResolve is errors.As without importing errors for one call site (the
// package keeps the same style as rails' own unwrap loop).
func asResolve(err error, target **rails.ResolveError) bool {
	for err != nil {
		if e, ok := err.(*rails.ResolveError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// asRefusal extracts a *rails.Refusal from an error chain.
func asRefusal(err error) (*rails.Refusal, bool) {
	for err != nil {
		if e, ok := err.(*rails.Refusal); ok {
			return e, true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return nil, false
		}
		err = u.Unwrap()
	}
	return nil, false
}
