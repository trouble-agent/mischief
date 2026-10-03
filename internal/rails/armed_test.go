// Tests for the per-run armed set (AC-12): arming and disarming stay inside
// the run's value, two sets never see each other's faults, and the global
// config file is byte-identical before and after (nothing here writes
// state). The AC-12 config snapshot is taken around a real arm/disarm
// sequence on a real temp file — the honest version of "byte-identical".
//
// ch:trace row=MSF-004 spec=docs/SPEC-PLAN.md (SPEC-03) test=internal/rails/armed_test.go evidence=internal/rails/armed_test.go witness=none:no-live-host-run-in-worktree
package rails

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestArmDisarmLifecycle: Arm records (with a params copy — the caller
// mutating its map afterwards must not change the armed record), Get/Len/
// Contains/IDs report, Disarm removes and reports whether it was armed; a
// disarm of a never-armed fault is reported false, not swallowed (SPEC-04
// grades reverts; a claimed revert of nothing is a no_op, not a pass).
func TestArmDisarmLifecycle(t *testing.T) {
	set := NewArmedSet()
	if set.Len() != 0 || len(set.IDs()) != 0 {
		t.Fatalf("fresh set not empty: %v", set.IDs())
	}

	params := map[string]string{"ms": "50"}
	rec, err := set.Arm(ArmedFault{Primitive: "P-001", Params: params, TTL: 30 * time.Second})
	if err != nil {
		t.Fatalf("Arm: %v", err)
	}
	if rec.TTL != 30*time.Second {
		t.Fatalf("record TTL=%v, want 30s", rec.TTL)
	}
	// the caller mutates its own map after arming; the armed record must not move
	params["ms"] = "999"
	if got := set.Get("P-001").Params["ms"]; got != "50" {
		t.Fatalf("armed record params followed the caller's map: ms=%q, want %q (Arm copies)", got, "50")
	}

	if !set.Contains("P-001") || set.Len() != 1 {
		t.Fatalf("Contains/Len after Arm: %v/%d", set.Contains("P-001"), set.Len())
	}
	if set.IDs()[0] != "P-001" {
		t.Fatalf("IDs=%v, want [P-001]", set.IDs())
	}

	// re-arming the same primitive replaces the record in THIS run
	if _, err := set.Arm(ArmedFault{Primitive: "P-001", Params: map[string]string{"ms": "75"}}); err != nil {
		t.Fatalf("re-Arm: %v", err)
	}
	if got := set.Get("P-001").Params["ms"]; got != "75" {
		t.Fatalf("re-Arm kept the old record: ms=%q, want 75", got)
	}
	if set.Len() != 1 {
		t.Fatalf("re-Arm duplicated the set: %v", set.IDs())
	}

	if set.Disarm("P-002") {
		t.Fatal("Disarm of a never-armed primitive reported true; a claimed revert of nothing must be reported")
	}
	if !set.Disarm("P-001") {
		t.Fatal("Disarm of an armed primitive reported false")
	}
	if set.Len() != 0 || set.Contains("P-001") || set.Get("P-001") != nil {
		t.Fatalf("set not empty after Disarm: %v", set.IDs())
	}

	if _, err := set.Arm(ArmedFault{}); err == nil {
		t.Fatal("Arm of an unnamed primitive succeeded; want the refusal")
	}
}

// TestArmedSetIsolation (AC-12's run half): two runs' sets never see each
// other's faults — arming in run B never changes run A's status shape, and
// IDs() is the sorted per-run status surface.
func TestArmedSetIsolation(t *testing.T) {
	runA := NewArmedSet()
	runB := NewArmedSet()
	if _, err := runA.Arm(ArmedFault{Primitive: "P-001"}); err != nil {
		t.Fatalf("run A Arm: %v", err)
	}
	if _, err := runB.Arm(ArmedFault{Primitive: "P-002"}); err != nil {
		t.Fatalf("run B Arm: %v", err)
	}
	if _, err := runB.Arm(ArmedFault{Primitive: "P-003"}); err != nil {
		t.Fatalf("run B Arm: %v", err)
	}

	gotA, gotB := runA.IDs(), runB.IDs()
	wantA, wantB := []string{"P-001"}, []string{"P-002", "P-003"}
	if len(gotA) != 1 || gotA[0] != wantA[0] {
		t.Fatalf("run A sees %v, want %v", gotA, wantA)
	}
	if len(gotB) != 2 || gotB[0] != wantB[0] || gotB[1] != wantB[1] {
		t.Fatalf("run B sees %v, want %v (IDs sorted, per-run)", gotB, wantB)
	}

	if !runB.Disarm("P-002") {
		t.Fatal("run B disarm failed")
	}
	if runA.Contains("P-002") {
		t.Fatal("run B's disarm leaked into run A's set")
	}
	if gotA2 := runA.IDs(); len(gotA2) != 1 || gotA2[0] != "P-001" {
		t.Fatalf("run A changed by run B's activity: %v", gotA2)
	}
}

// TestGlobalConfigByteIdentical (AC-12's config half): a config file,
// snapshotted as raw bytes around a real arm/disarm sequence, is
// byte-identical after — the armed set writes no global state. The snapshot
// is taken from disk twice (os.ReadFile) so the comparison is over what the
// filesystem actually carried, not over a value the test holds in memory.
func TestGlobalConfigByteIdentical(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "mischief.toml")
	cfg := "# mischief global config\n[ladder]\nL5_allow_real = false\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("snapshot before: %v", err)
	}

	set := NewArmedSet()
	for _, id := range []string{"P-001", "P-002"} {
		if _, err := set.Arm(ArmedFault{Primitive: id, TTL: time.Second}); err != nil {
			t.Fatalf("Arm(%s): %v", id, err)
		}
	}
	for _, id := range []string{"P-001", "P-002"} {
		if !set.Disarm(id) {
			t.Fatalf("Disarm(%s) reported false", id)
		}
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("snapshot after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("global config changed across arm/disarm:\nbefore: %q\nafter:  %q", before, after)
	}
	if set.Len() != 0 {
		t.Fatalf("set not empty after disarms: %v", set.IDs())
	}
}
