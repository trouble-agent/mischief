package doctor

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/ops"
)

// ── SPEC-11 posture (MSF-012) — the audit seam decides everything ───────────

// TestDoctorOpsAbsentNamesReason (the brief's removed-helper arm): with the
// audit seam reporting NOTHING installed, RunOps derives per-primitive
// posture rows where NET_ADMIN is absent WITH THE REASON (the drop-in path,
// the missing artefact) — not a bare "unavailable". The test creates a
// temp-state posture through the seam (never touches /etc).
func TestDoctorOpsAbsentNamesReason(t *testing.T) {
	// seam: the audit of a host with nothing installed
	saved := OpsAuditFn
	OpsAuditFn = func(ops.AuditOpts) (*ops.Audit, error) { return uninstalledAudit(), nil }
	t.Cleanup(func() { OpsAuditFn = saved })

	cat := testCatalog(t)
	rep, err := Run(cat.SourceDir, catalogRegistryAllFalse())
	if err != nil {
		t.Fatal(err)
	}
	RunOps(rep, OpsOptions{})
	if rep.Ops == nil {
		t.Fatal("RunOps did not attach the ops section")
	}
	// the posture row for NET_ADMIN exists and is absent-with-reason
	var found *ops.CapabilityPosture
	for i := range rep.Ops.Privileged {
		if rep.Ops.Privileged[i].Kind == "NET_ADMIN" {
			found = &rep.Ops.Privileged[i]
		}
	}
	if found == nil {
		t.Fatalf("no NET_ADMIN posture row: %+v", rep.Ops.Privileged)
	}
	if found.Available {
		t.Fatal("NET_ADMIN posture available with nothing installed")
	}
	for _, want := range []string{"not installed", ops.DefaultDropInPath, "mischief install"} {
		if !strings.Contains(found.Missing, want) {
			t.Fatalf("missing piece %q does not name %q", found.Missing, want)
		}
	}
	// AppendOpsRows upgrades the per-primitive reason
	AppendOpsRows(rep)
	upgraded := false
	for _, row := range rep.Rows {
		if row.Capability == "NET_ADMIN" && strings.Contains(row.UnavailableNames, "sudoers drop-in not installed") {
			upgraded = true
		}
	}
	if !upgraded {
		t.Fatal("per-primitive reason not upgraded to name the artefact")
	}
	// text render carries the posture section
	out := string(RenderOps(rep.Ops))
	if !strings.Contains(out, "privileged surface") || !strings.Contains(out, "absent") {
		t.Fatalf("ops render lacks the posture: %s", out)
	}
}

// TestDoctorJSONCarriesOpsAndMissingPieces: doctor --json emits rows with
// missing_piece, the rails rows, and the ops section with the audit embedded.
func TestDoctorJSONCarriesOpsAndMissingPieces(t *testing.T) {
	saved := OpsAuditFn
	OpsAuditFn = func(ops.AuditOpts) (*ops.Audit, error) { return uninstalledAudit(), nil }
	t.Cleanup(func() { OpsAuditFn = saved })

	cat := testCatalog(t)
	rep, err := Run(cat.SourceDir, catalogRegistryAllFalse())
	if err != nil {
		t.Fatal(err)
	}
	RunOps(rep, OpsOptions{})
	AppendOpsRows(rep)
	doc := RenderJSON(rep)
	var parsed struct {
		Rows []struct {
			ID           string `json:"id"`
			Status       string `json:"status"`
			MissingPiece string `json:"missing_piece"`
		} `json:"rows"`
		Ops *struct {
			Privileged []struct {
				Kind      string `json:"kind"`
				Available bool   `json:"available"`
				Missing   string `json:"missing"`
			} `json:"privileged"`
			Audit json.RawMessage `json:"audit"`
		} `json:"ops"`
	}
	if err := json.Unmarshal(doc, &parsed); err != nil {
		t.Fatalf("doctor --json does not parse: %v\n%s", err, doc)
	}
	var netRow bool
	for _, r := range parsed.Rows {
		if r.ID == "D-002" {
			netRow = true
			if r.Status != "capability_unavailable" {
				t.Fatalf("D-002 status %q", r.Status)
			}
			if !strings.Contains(r.MissingPiece, "not installed") {
				t.Fatalf("D-002 missing_piece %q", r.MissingPiece)
			}
		}
	}
	if !netRow {
		t.Fatal("D-002 (NET_ADMIN) row missing from the JSON")
	}
	if parsed.Ops == nil || len(parsed.Ops.Privileged) == 0 {
		t.Fatal("ops section missing from the JSON")
	}
	if len(parsed.Ops.Audit) == 0 || !strings.Contains(string(parsed.Ops.Audit), "drop_in_matches") {
		t.Fatal("the audit is not embedded in the ops JSON")
	}
}

// TestDoctorOpsAuditFailureSurfaces: an audit that cannot run lands as a
// named row in the section — never a silent skip, never a panic.
func TestDoctorOpsAuditFailureSurfaces(t *testing.T) {
	saved := OpsAuditFn
	OpsAuditFn = func(ops.AuditOpts) (*ops.Audit, error) { return nil, errors.New("sudoers unreadable") }
	t.Cleanup(func() { OpsAuditFn = saved })

	cat := testCatalog(t)
	rep, err := Run(cat.SourceDir, catalogRegistryAllFalse())
	if err != nil {
		t.Fatal(err)
	}
	RunOps(rep, OpsOptions{})
	if rep.Ops == nil || rep.Ops.AuditErr == "" {
		t.Fatalf("audit failure not surfaced: %+v", rep.Ops)
	}
	if !strings.Contains(string(RenderOps(rep.Ops)), "audit FAILED") {
		t.Fatal("render does not carry the audit failure")
	}
}

// TestDoctorWithoutOpsUnchanged: the additive contract — a report that
// never asked for ops renders and JSONs exactly as before (no ops section,
// no missing pieces).
func TestDoctorWithoutOpsUnchanged(t *testing.T) {
	cat := testCatalog(t)
	rep, err := Run(cat.SourceDir, catalogRegistryAllFalse())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Ops != nil {
		t.Fatal("ops section attached without RunOps")
	}
	if strings.Contains(string(Render(rep)), "privileged surface") {
		t.Fatal("text render carries an ops section without RunOps")
	}
	if strings.Contains(string(RenderJSON(rep)), `"ops"`) {
		t.Fatal("JSON carries an ops section without RunOps")
	}
}

// uninstalledAudit builds the audit of a host where NOTHING is installed
// (the "deliberately removed helper" state — built, not touched on disk).
func uninstalledAudit() *ops.Audit {
	return &ops.Audit{
		DropInPath:              ops.DefaultDropInPath,
		DropInInstalled:         false,
		DropInMatches:           false,
		DropInDiff:              "drop-in not installed (expected " + ops.DefaultDropInPath + ")",
		HelperManifestPath:      ops.DefaultHelperManifestPath,
		HelperManifestInstalled: false,
		SetuidBinaries:          []string{},
		Verbs:                   auditVerbsNoneGranted(),
	}
}

// auditVerbsNoneGranted: every sudo verb measured NOT granted.
func auditVerbsNoneGranted() []ops.AuditVerb {
	var out []ops.AuditVerb
	for _, v := range ops.SortedVerbs() {
		out = append(out, ops.AuditVerb{
			ID: v.ID, Exec: string(v.Exec), Binary: v.Binary,
			GrantedInDropIn: false, WildcardGrant: false, Reason: v.Why,
		})
	}
	return out
}

// catalogRegistryAllFalse: every capability absent (the D-002 arm).
func catalogRegistryAllFalse() catalogRegistryShim { return catalogRegistryShim{} }

type catalogRegistryShim struct{}

func (catalogRegistryShim) Available(string) bool { return false }
