package doctor

import (
	"fmt"
	"sort"

	"github.com/trouble-agent/mischief/internal/catalog"
	"github.com/trouble-agent/mischief/internal/ops"
)

// This file extends doctor for SPEC-11 (MSF-012): the privileged-surface
// posture, per primitive — available/absent PLUS the missing piece
// (AC-11's "naming what is absent", upgraded from capability-kind-only to
// the exact artefact: drop-in not installed, helper manifest missing,
// drop-in drifted, grant missing from the installed drop-in).
//
// ADDITIVE by design: Run/Render above are untouched; RunOps appends an
// OpsSection to the report (zero value renders nothing, so existing
// consumers — and the existing tests — see no change unless the caller
// asks for ops).

// OpsOptions carries the ops posture inputs; zero value = production
// paths. Tests override the seams (OpsRoot, OpsAuditFn) — never the real
// /etc on the dev host.
type OpsOptions struct {
	// Prefix is mischief's state prefix ("" = ops.DefaultPrefix).
	Prefix string
	// SudoersUser must match the installed drop-in's grantee.
	SudoersUser string
}

// OpsAuditFn is the audit seam: package-level so tests pin a stub audit
// instead of touching the host's /etc. Production = ops.RunAudit.
var OpsAuditFn = func(opts ops.AuditOpts) (*ops.Audit, error) { return ops.RunAudit(opts) }

// OpsSection is the privileged-surface section of the doctor report.
type OpsSection struct {
	// Privileged: per capability kind that maps to privileged verbs, one
	// posture row (available/absent + THE missing piece).
	Privileged []ops.CapabilityPosture
	// AuditErr names the audit's own failure ("" = the audit ran).
	AuditErr string
	// SetuidFlags carries prefix setuid findings (design: none).
	SetuidFlags []string
	// Notes carries the audit's posture notes verbatim.
	Notes []string
	// audit is the raw audit (JSON rendering embeds it verbatim).
	audit *ops.Audit
}

// Audit exposes the underlying audit (JSON rendering hands it to
// ops.RenderAuditJSON so the operator diff surface is THE audit's shape).
func (s *OpsSection) Audit() *ops.Audit { return s.audit }

// audit is the raw audit; the JSON shape embeds it via MarshalJSON below.

// RunOps surveys the privileged surface and derives per-primitive posture
// rows, appended to rep (additive).
func RunOps(rep *Report, opts OpsOptions) {
	if rep == nil {
		return
	}
	sec := &OpsSection{}
	audit, err := OpsAuditFn(ops.AuditOpts{Prefix: opts.Prefix, SudoersUser: opts.SudoersUser})
	if err != nil {
		sec.AuditErr = err.Error()
		rep.Ops = sec
		return
	}
	sec.audit = audit
	sec.SetuidFlags = audit.SetuidBinaries
	sec.Notes = audit.Notes

	// One posture row per DISTINCT capability kind in the report's rows
	// (a kind maps to the same table verbs every time — the map in ops is
	// kind-keyed), sorted by kind for a deterministic report.
	kinds := map[string]bool{}
	for _, row := range rep.Rows {
		if row.Capability == "" || row.Capability == "none" {
			continue
		}
		kinds[row.Capability] = true
	}
	for _, k := range sortedKeys(kinds) {
		sec.Privileged = append(sec.Privileged, ops.Posture(audit, k))
	}
	rep.Ops = sec
}

// sortedKeys returns map keys sorted (deterministic rendering).
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// AppendOpsRows derives per-PRIMITIVE absent reasons: every row whose
// capability maps to privileged verbs and whose posture is absent gains
// UnavailableNames upgraded to name the missing ARTEFACT (AC-11: the verb
// stays listed; the reason names what is absent). Existing
// capability_unavailable rows keep their kind name when no ops posture
// exists (fail closed to the old wording, never to a lie).
func AppendOpsRows(rep *Report) {
	if rep == nil || rep.Ops == nil {
		return
	}
	byKind := map[string]ops.CapabilityPosture{}
	for _, cp := range rep.Ops.Privileged {
		byKind[cp.Kind] = cp
	}
	for i := range rep.Rows {
		row := &rep.Rows[i]
		cp, ok := byKind[row.Capability]
		if !ok || cp.Available {
			continue
		}
		row.MissingPiece = cp.Missing
		if row.Status == catalog.StatusCapabilityUnavailable && cp.Missing != "" {
			// AC-11 upgrade: the status stays capability_unavailable (the
			// vocabulary is closed); the reason upgrades to the artefact.
			row.UnavailableNames = cp.Missing
		}
	}
}

// RenderOps renders the ops section (text form, after the main table).
func RenderOps(sec *OpsSection) []byte {
	if sec == nil {
		return nil
	}
	var b []byte
	appendf := func(format string, args ...any) {
		b = append(b, fmt.Sprintf(format, args...)...)
	}
	appendf("privileged surface (SPEC-11):\n")
	if sec.AuditErr != "" {
		appendf("  audit FAILED: %s\n", sec.AuditErr)
		return b
	}
	for _, cp := range sec.Privileged {
		appendf("  %s\n", cp.RenderPosture())
		for _, f := range cp.Findings {
			appendf("    note: %s\n", f)
		}
	}
	if len(sec.SetuidFlags) > 0 {
		appendf("  setuid FLAGS (design ships none — investigate):\n")
		for _, s := range sec.SetuidFlags {
			appendf("    %s\n", s)
		}
	}
	for _, n := range sec.Notes {
		appendf("  note: %s\n", n)
	}
	return b
}
