package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ── retention policy selection (PURE, unit-tested per the brief) ────────────

// mkDirs builds run dirs at fixed ages relative to the pinned now.
func mkDirs(now time.Time, ages ...time.Duration) []RunDir {
	out := make([]RunDir, 0, len(ages))
	for i, a := range ages {
		out = append(out, RunDir{Name: fmt.Sprintf("run-%03d", i), ModTime: now.Add(-a)})
	}
	return out
}

func TestRetentionDefaultPolicy(t *testing.T) {
	pol := DefaultRetentionPolicy()
	if pol.KeepCount != 20 || pol.KeepAge != 30*24*time.Hour {
		t.Fatalf("default policy: %+v", pol)
	}
	if err := pol.Validate(); err != nil {
		t.Fatalf("default policy invalid: %v", err)
	}
}

// TestRetentionAgeBoundExpires: 30-day window, dirs at 1, 10, 40, 50 days
// → the two old ones expire even though the count bound (20) never bites.
func TestRetentionAgeBoundExpires(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	pol := RetentionPolicy{KeepCount: 20, KeepAge: 30 * 24 * time.Hour, Now: now}
	got := pol.SelectExpired(mkDirs(now, 24*time.Hour, 10*24*time.Hour, 40*24*time.Hour, 50*24*time.Hour))
	if len(got) != 2 {
		t.Fatalf("expired %d, want 2 (the 40d and 50d dirs): %+v", len(got), got)
	}
	for _, d := range got {
		if d.ModTime.After(now.Add(-35 * 24 * time.Hour)) {
			t.Fatalf("fresh dir expired: %+v", d)
		}
	}
}

// TestRetentionCountBoundExpires: keep 2, five fresh dirs → the 3 oldest
// expire; within-window dirs stay even when old (age bound unset).
func TestRetentionCountBoundExpires(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	pol := RetentionPolicy{KeepCount: 2, Now: now} // no age bound
	dirs := mkDirs(now, time.Hour, 2*time.Hour, 3*time.Hour, 400*24*time.Hour, 500*24*time.Hour)
	got := pol.SelectExpired(dirs)
	if len(got) != 3 {
		t.Fatalf("expired %d, want 3: %+v", len(got), got)
	}
	// the two NEWEST stay (the 400d/500d dirs are the oldest → expired)
	for _, d := range got {
		if d.ModTime.Equal(now.Add(-time.Hour)) || d.ModTime.Equal(now.Add(-2*time.Hour)) {
			t.Fatalf("newest dirs must stay: %+v", d)
		}
	}
}

// TestRetentionWhicheverFirst: both bounds set — a dir expires when EITHER
// bites; the newest-N are protected from the count bound but NOT from the
// age bound (whichever first, age wins over count protection).
func TestRetentionWhicheverFirst(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	pol := RetentionPolicy{KeepCount: 3, KeepAge: 30 * 24 * time.Hour, Now: now}
	// 4 dirs: 2 fresh, 2 ancient. Count bound (keep 3) would keep the
	// 3 newest (incl. one ancient); the AGE bound expires the ancient
	// ones anyway.
	dirs := mkDirs(now, time.Hour, 2*time.Hour, 100*24*time.Hour, 200*24*time.Hour)
	got := pol.SelectExpired(dirs)
	if len(got) != 2 {
		t.Fatalf("expired %d, want 2 (the ancient pair — age beats count): %+v", len(got), got)
	}
	// and the reverse: many fresh dirs, count bites below the age window
	pol2 := RetentionPolicy{KeepCount: 2, KeepAge: 30 * 24 * time.Hour, Now: now}
	dirs2 := mkDirs(now, 1*time.Hour, 2*time.Hour, 3*time.Hour, 4*time.Hour, 5*time.Hour)
	got2 := pol2.SelectExpired(dirs2)
	if len(got2) != 3 {
		t.Fatalf("expired %d, want 3 (count beats fresh age): %+v", len(got2), got2)
	}
}

// TestRetentionInvalidPolicyRefused: both bounds zero is not a policy —
// SelectExpired expires nothing and PlanRetention refuses.
func TestRetentionInvalidPolicyRefused(t *testing.T) {
	var pol RetentionPolicy
	if err := pol.Validate(); err == nil {
		t.Fatal("zero policy validated")
	}
	if got := pol.SelectExpired(mkDirs(time.Now(), time.Hour)); len(got) != 0 {
		t.Fatalf("zero policy expired %d dirs", len(got))
	}
}

// TestRetentionPlanAndExecute: plan names exactly the expired dirs; apply
// removes them, keeps the rest; idempotent on a second apply.
func TestRetentionPlanAndExecute(t *testing.T) {
	tempRoot(t)
	runsDir := "/var/lib/mischief/runs"
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	pol := RetentionPolicy{KeepCount: 2, KeepAge: 30 * 24 * time.Hour, Now: now, ApplyOnUninstall: true}
	// 4 run dirs: 2 fresh, 2 ancient
	for i, age := range []time.Duration{time.Hour, 2 * time.Hour, 100 * 24 * time.Hour, 200 * 24 * time.Hour} {
		d := resolve(filepath.Join(runsDir, fmt.Sprintf("run-%d", i)))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		stamp := now.Add(-age)
		if err := os.Chtimes(d, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	p, err := PlanRetention(runsDir, pol)
	if err != nil {
		t.Fatalf("PlanRetention: %v", err)
	}
	if len(p.Actions) != 2 {
		t.Fatalf("plan actions %d, want 2", len(p.Actions))
	}
	removed, err := ExecuteRetention(runsDir, p)
	if err != nil {
		t.Fatalf("ExecuteRetention: %v", err)
	}
	if len(removed) != 2 {
		t.Fatalf("removed %d, want 2", len(removed))
	}
	left, err := ListRunDirs(runsDir)
	if err != nil || len(left) != 2 {
		t.Fatalf("left dirs %d (%v), want 2", len(left), err)
	}
	// second apply: nothing left to expire
	p2, err := PlanRetention(runsDir, pol)
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.Actions) != 0 {
		t.Fatalf("second plan carried %d actions, want 0", len(p2.Actions))
	}
	// confinement: an action pointing OUTSIDE the runs dir is refused
	evil := &Plan{Op: "retention", Actions: []Action{{Kind: OpRemove, Path: "/etc"}}}
	if _, err := ExecuteRetention(runsDir, evil); err == nil {
		t.Fatal("out-of-runs-dir action not refused")
	}
}

// TestUninstallAppliesRetention: uninstall's plan carries the expired run
// dirs as removal actions (the brief: "journals per retention policy").
func TestUninstallAppliesRetention(t *testing.T) {
	tempRoot(t)
	runsDir := "/var/lib/mischief/runs"
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	if err := os.MkdirAll(resolve(filepath.Join(runsDir, "ancient")), 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := now.Add(-100 * 24 * time.Hour)
	if err := os.Chtimes(resolve(filepath.Join(runsDir, "ancient")), stamp, stamp); err != nil {
		t.Fatal(err)
	}
	pol := DefaultRetentionPolicy()
	pol.Now = now
	pol.KeepCount = 5
	pol.KeepAge = 30 * 24 * time.Hour
	up, err := PlanUninstall(InstallOpts{Prefix: "/var/lib/mischief"}, runsDir, pol)
	if err != nil {
		t.Fatalf("PlanUninstall: %v", err)
	}
	found := false
	for _, a := range up.Actions {
		if a.Path == filepath.Join(runsDir, "ancient") {
			found = true
		}
		if a.Path == runsDir {
			t.Fatal("uninstall plan removes the runs DIR itself")
		}
	}
	if !found {
		t.Fatal("uninstall plan does not carry the expired run dir")
	}
}
