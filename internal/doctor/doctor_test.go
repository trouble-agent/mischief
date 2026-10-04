package doctor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/catalog"
)

const desc = `id: D-001
name: doctor-test-primitive
what: a descriptor for the doctor test
breaks: nothing
params: {mode: str}
landed_proof: {kind: test, check: "always"}
inverse: {action: noop}
capability: none
tier: L1
backend: sig
maturity: planned
`

const descNet = `id: D-002
name: doctor-net-primitive
what: a NET_ADMIN descriptor for the doctor test
breaks: nothing
params: {mode: str}
landed_proof: {kind: test, check: "always"}
inverse: {action: noop}
capability: NET_ADMIN
tier: L2
backend: helper
maturity: planned
`

func testCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"d-001.yaml": desc, "d-002.yaml": descNet} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cat, err := catalog.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

// TestDoctorReportsLadder: the report carries one row per primitive sorted
// by id, with tier and capability, and the absent capability is NAMED
// (AC-11: "naming what is absent") — doctor refuses nothing extra: a
// capability_unavailable row is a report, not an error.
func TestDoctorReportsLadder(t *testing.T) {
	cat := testCatalog(t)
	rep, err := Run(cat.SourceDir, catalog.RegistryFunc(func(kind string) bool { return false }))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.LoadErr != "" {
		t.Fatalf("load error: %s", rep.LoadErr)
	}
	if len(rep.Rows) != 2 {
		t.Fatalf("rows: %d, want 2", len(rep.Rows))
	}
	if rep.Rows[0].ID != "D-001" || rep.Rows[1].ID != "D-002" {
		t.Fatalf("rows not sorted by id: %s, %s", rep.Rows[0].ID, rep.Rows[1].ID)
	}
	if rep.Rows[1].Status != catalog.StatusCapabilityUnavailable || rep.Rows[1].UnavailableNames != "NET_ADMIN" {
		t.Fatalf("NET_ADMIN row not named absent: %+v", rep.Rows[1])
	}
	if rep.Rows[0].Status != catalog.StatusReady {
		t.Fatalf("none-capability row not ready: %+v", rep.Rows[0])
	}
	// render: deterministic, and carries the absent name
	r1 := Render(rep)
	for i := 0; i < 10; i++ {
		if r2 := Render(rep); !bytes.Equal(r1, r2) {
			t.Fatalf("render %d differs (map-order leak)", i)
		}
	}
	if !strings.Contains(string(r1), "NET_ADMIN") {
		t.Fatal("render does not name the absent capability")
	}
}

// TestDoctorLoadFailureReported: an unloadable catalog is a REPORTED load
// failure (exit 1 in cmd) with the reason — the doctor's one job failing
// is named, never swallowed.
func TestDoctorLoadFailureReported(t *testing.T) {
	rep, err := Run("/nonexistent/catalog/dir", nil)
	if err != nil {
		t.Fatalf("Run returns the report, not an error: %v", err)
	}
	if rep.LoadErr == "" {
		t.Fatal("missing dir did not surface a load error")
	}
	if !strings.Contains(string(Render(rep)), "FAILED") {
		t.Fatal("render does not carry the load failure")
	}
}

// TestDoctorMissingDirExplicitRejected: an explicit dir that exists but
// holds no yaml refuses with the loader's named reason.
func TestDoctorMissingDirExplicitRejected(t *testing.T) {
	dir := t.TempDir()
	rep, err := Run(dir, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.LoadErr == "" || !strings.Contains(rep.LoadErr, "no .yaml files") {
		t.Fatalf("empty dir load error: %q", rep.LoadErr)
	}
}
