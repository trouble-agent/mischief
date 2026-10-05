package doctor

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/trouble-agent/mischief/internal/ops"
)

// This file renders the doctor report as JSON (`mischief doctor --json`,
// MSF-012): the ladder rows (with per-primitive missing pieces), the rails
// self-checks, and the SPEC-11 privileged-surface audit — one document, so
// an operator (or a lane) reads posture from one place.

// jsonRow is one ladder row's JSON shape (row + missing piece).
type jsonRow struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Backend          string `json:"backend"`
	Tier             string `json:"tier"`
	Capability       string `json:"capability"`
	Status           string `json:"status"`
	UnavailableNames string `json:"unavailable_names,omitempty"`
	MissingPiece     string `json:"missing_piece,omitempty"`
	Selftest         string `json:"selftest"`
	Maturity         string `json:"maturity"`
}

// jsonRails is one rails self-check row.
type jsonRails struct {
	Check  string `json:"check"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// jsonReport is the full doctor document.
type jsonReport struct {
	CatalogVersion string      `json:"catalog_version"`
	LoadErr        string      `json:"load_error,omitempty"`
	Rows           []jsonRow   `json:"rows"`
	RegistryNotes  []string    `json:"registry_notes,omitempty"`
	Rails          []jsonRails `json:"rails,omitempty"`
	Ops            *jsonOps    `json:"ops,omitempty"`
	OpsErr         string      `json:"ops_error,omitempty"`
}

// jsonOps is the ops section's document shape: the posture rows plus the
// audit VERBATIM (ops.RenderAuditJSON owns the audit's wire shape — the
// operator diffs the same document the audit verb prints).
type jsonOps struct {
	Privileged  []opsPosture    `json:"privileged,omitempty"`
	SetuidFlags []string        `json:"setuid_flags,omitempty"`
	Notes       []string        `json:"notes,omitempty"`
	Audit       json.RawMessage `json:"audit,omitempty"`
}

// opsPosture is one capability posture row.
type opsPosture struct {
	Kind         string   `json:"kind"`
	Available    bool     `json:"available"`
	Missing      string   `json:"missing,omitempty"`
	ServingVerbs []string `json:"serving_verbs,omitempty"`
	Findings     []string `json:"findings,omitempty"`
}

// MarshalJSON renders the audit through ops' own renderer (single source).
func (o *jsonOps) auditJSON(a *ops.Audit) {
	if a == nil {
		return
	}
	o.Audit = json.RawMessage(ops.RenderAuditJSON(a))
}

// RenderJSON renders the report as the JSON document. Load-failed reports
// render their LoadErr and nothing else (the one honest line).
func RenderJSON(r *Report) []byte {
	doc := jsonReport{CatalogVersion: r.CatalogVersion, LoadErr: r.LoadErr}
	for _, row := range r.Rows {
		doc.Rows = append(doc.Rows, jsonRow{
			ID:               row.ID,
			Name:             row.Name,
			Backend:          row.Backend,
			Tier:             row.Tier,
			Capability:       row.Capability,
			Status:           string(row.Status),
			UnavailableNames: row.UnavailableNames,
			MissingPiece:     row.MissingPiece,
			Selftest:         string(row.Selftest),
			Maturity:         row.Maturity,
		})
	}
	doc.RegistryNotes = r.RegistryNotes
	for _, ra := range r.Rails {
		doc.Rails = append(doc.Rails, jsonRails{Check: ra.Check, OK: ra.OK, Detail: ra.Detail})
	}
	if r.Ops != nil {
		jo := &jsonOps{
			Privileged:  []opsPosture{},
			SetuidFlags: r.Ops.SetuidFlags,
			Notes:       r.Ops.Notes,
		}
		for _, cp := range r.Ops.Privileged {
			jo.Privileged = append(jo.Privileged, opsPosture{
				Kind:         cp.Kind,
				Available:    cp.Available,
				Missing:      cp.Missing,
				ServingVerbs: cp.ServingVerbs,
				Findings:     cp.Findings,
			})
		}
		jo.auditJSON(r.Ops.Audit())
		doc.Ops = jo
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		// The doc is marshal-safe by construction; never fabricate.
		return []byte(fmt.Sprintf("{\"load_error\": %q}\n", err.Error()))
	}
	return append(b, '\n')
}

// WriteOut writes the report in the requested form (text or JSON) to
// stdout — the cmd-level entry both doctor forms share.
func WriteOut(r *Report, asJSON bool) {
	if asJSON {
		os.Stdout.Write(RenderJSON(r))
		return
	}
	os.Stdout.Write(Render(r))
	if r.Ops != nil {
		os.Stdout.Write(RenderOps(r.Ops))
	}
}
