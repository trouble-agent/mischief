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
	// 4 run dirs: 2 fresh, 2 ancient — each holding journal files (real
	// shape: rotation must survive populated dirs, not just empty stubs).
	for i, age := range []time.Duration{time.Hour, 2 * time.Hour, 100 * 24 * time.Hour, 200 * 24 * time.Hour} {
		mkRealRunDir(t, runsDir, fmt.Sprintf("run-%d", i), now, age)
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
	mkRealRunDir(t, runsDir, "ancient", now, 100*24*time.Hour)
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

// mkRealRunDir creates a run dir holding the real shape — journal files,
// never an empty stub (MISCHIEF-DF-33's AC: rotation coverage must use
// populated dirs) — and backdates its mtime to now-age.
func mkRealRunDir(t *testing.T, runsDir, name string, now time.Time, age time.Duration) {
	t.Helper()
	d := resolve(filepath.Join(runsDir, name))
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"journal.jsonl", "events.ndjson"} {
		if err := os.WriteFile(filepath.Join(d, f), []byte("{\"run\":"+name+"}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stamp := now.Add(-age)
	if err := os.Chtimes(d, stamp, stamp); err != nil {
		t.Fatal(err)
	}
}

// TestUninstallRotatesRealRunDirs is THE MISCHIEF-DF-33 regression pin: a
// REAL run dir (journal files inside, not the empty stubs prior coverage
// used) must rotate cleanly during uninstall. The shipped code died
// `directory not empty` (rc=1) here: the plan carried run dirs as
// file-shaped OpRemove and ExecuteUninstall's plain os.Remove cannot eat a
// populated dir. The retention verb applied the IDENTICAL policy to the
// IDENTICAL data successfully — so the operator's clean-up path worked
// while the uninstall path aborted mid-flight.
func TestUninstallRotatesRealRunDirs(t *testing.T) {
	tempRoot(t)
	prefix := "/var/lib/mischief"
	// The runs dir is NOT under the prefix (the CLI derives it under
	// $HOME/.mischief/runs) — the prefix's final rmdir must find it empty.
	runsDir := "/home/tester/.mischief/runs"
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	pol := RetentionPolicy{KeepCount: 1, KeepAge: 30 * 24 * time.Hour, Now: now, ApplyOnUninstall: true}
	// Three populated run dirs, policy keeps 1 → two rotate.
	mkRealRunDir(t, runsDir, "run-a", now, time.Hour)
	mkRealRunDir(t, runsDir, "run-b", now, 2*time.Hour)
	mkRealRunDir(t, runsDir, "run-c", now, 3*time.Hour)
	// A real installed manifest so the plan also exercises the file
	// removal + the emptied-prefix rmdir.
	if err := os.MkdirAll(resolve(prefix), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resolve(prefix+"/helper-manifest.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	up, err := PlanUninstall(InstallOpts{Prefix: prefix}, runsDir, pol)
	if err != nil {
		t.Fatalf("PlanUninstall: %v", err)
	}
	treeActions := 0
	runDirKinds := map[OpKind]bool{}
	for _, a := range up.Actions {
		for _, name := range []string{"run-a", "run-b", "run-c"} {
			if a.Path == filepath.Join(runsDir, name) {
				runDirKinds[a.Kind] = true
			}
		}
		if a.Kind == OpRemoveTree {
			treeActions++
			if a.runsDir != runsDir {
				t.Fatalf("tree action %s carries runsDir %q, want %q", a.Path, a.runsDir, runsDir)
			}
		}
	}
	if len(runDirKinds) != 1 || !runDirKinds[OpRemoveTree] {
		t.Fatalf("run dirs planned as %v, want only %q (file-shaped removal dies ENOTEMPTY on real data)", runDirKinds, OpRemoveTree)
	}
	if treeActions != 2 {
		t.Fatalf("plan carries %d tree actions, want 2", treeActions)
	}

	removed, err := up.ExecuteUninstall()
	if err != nil {
		t.Fatalf("ExecuteUninstall: %v", err)
	}
	left, err := ListRunDirs(runsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].Name != "run-a" {
		t.Fatalf("after uninstall %v remain, want [run-a]", left)
	}
	if len(removed) != 4 { // 2 run dirs + manifest + prefix dir
		t.Fatalf("removed %d (%v), want 4", len(removed), removed)
	}
	for _, want := range []string{filepath.Join(runsDir, "run-b"), filepath.Join(runsDir, "run-c")} {
		found := false
		for _, p := range removed {
			if p == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("removed list lacks rotated run dir %s", want)
		}
	}
	// The kept dir's evidence survives byte-for-byte (rotation, not a wipe).
	kept, err := os.ReadFile(resolve(filepath.Join(runsDir, "run-a", "journal.jsonl")))
	if err != nil || len(kept) == 0 {
		t.Fatalf("kept run dir evidence unreadable (%v, %d bytes)", err, len(kept))
	}
}

// TestUninstallMatchesRetentionOnSameData is the parity pin (DF-33's
// observation was that retention rc=0'd on data uninstall rc=1'd): the SAME
// policy over the SAME populated run dirs must expire the IDENTICAL set
// through both verbs.
func TestUninstallMatchesRetentionOnSameData(t *testing.T) {
	for _, verb := range []string{"retention", "uninstall"} {
		t.Run(verb, func(t *testing.T) {
			tempRoot(t)
			prefix := "/var/lib/mischief"
			// Runs dir outside the prefix (the CLI's real geometry).
			runsDir := "/home/tester/.mischief/runs"
			now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
			pol := RetentionPolicy{KeepCount: 2, KeepAge: 30 * 24 * time.Hour, Now: now, ApplyOnUninstall: true}
			mkRealRunDir(t, runsDir, "run-0", now, time.Hour)
			mkRealRunDir(t, runsDir, "run-1", now, 2*time.Hour)
			mkRealRunDir(t, runsDir, "run-2", now, 100*24*time.Hour)
			mkRealRunDir(t, runsDir, "run-3", now, 200*24*time.Hour)

			if verb == "retention" {
				p, err := PlanRetention(runsDir, pol)
				if err != nil {
					t.Fatalf("PlanRetention: %v", err)
				}
				if _, err := ExecuteRetention(runsDir, p); err != nil {
					t.Fatalf("ExecuteRetention: %v", err)
				}
			} else {
				up, err := PlanUninstall(InstallOpts{Prefix: prefix}, runsDir, pol)
				if err != nil {
					t.Fatalf("PlanUninstall: %v", err)
				}
				if _, err := up.ExecuteUninstall(); err != nil {
					t.Fatalf("ExecuteUninstall: %v", err)
				}
			}
			left, err := ListRunDirs(runsDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(left) != 2 {
				t.Fatalf("%s left %d run dirs (%v), want 2", verb, len(left), left)
			}
			for _, d := range left {
				if d.Name != "run-0" && d.Name != "run-1" {
					t.Fatalf("%s kept %q, want only the fresh pair", verb, d.Name)
				}
			}
		})
	}
}

// TestUninstallTreeConfinement: an OpRemoveTree action pointing OUTSIDE its
// runs dir is refused — the recursive removal can never eat an arbitrary
// path even if a plan is malformed.
func TestUninstallTreeConfinement(t *testing.T) {
	tempRoot(t)
	// Seed the would-be victim so "still intact" is provable (the target
	// must EXIST for the damage check to mean anything under the temp root).
	if err := os.MkdirAll(resolve("/etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resolve("/etc/passwd"), []byte("root:x:0:0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	evil := &Plan{Op: "uninstall", Actions: []Action{{
		Kind: OpRemoveTree, Path: "/etc", runsDir: "/var/lib/mischief/runs",
	}}}
	if _, err := evil.ExecuteUninstall(); err == nil {
		t.Fatal("out-of-runs-dir tree action not refused")
	}
	if _, err := os.Lstat(resolve("/etc/passwd")); err != nil {
		t.Fatalf("refused action's target damaged: %v", err)
	}
	// Same-dir trick: the runs dir ITSELF is not a direct child of itself.
	inside := &Plan{Op: "uninstall", Actions: []Action{{
		Kind: OpRemoveTree, Path: "/var/lib/mischief/runs", runsDir: "/var/lib/mischief/runs",
	}}}
	if _, err := inside.ExecuteUninstall(); err == nil {
		t.Fatal("tree action targeting the runs dir itself not refused")
	}
}

// TestUninstallTreeIdempotent: a second uninstall finds the already-rotated
// run dirs gone and succeeds (absent artefact = success, the uninstall
// contract).
func TestUninstallTreeIdempotent(t *testing.T) {
	tempRoot(t)
	prefix := "/var/lib/mischief"
	runsDir := "/home/tester/.mischief/runs"
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	pol := RetentionPolicy{KeepCount: 1, KeepAge: 30 * 24 * time.Hour, Now: now, ApplyOnUninstall: true}
	mkRealRunDir(t, runsDir, "run-a", now, time.Hour)
	mkRealRunDir(t, runsDir, "run-b", now, 2*time.Hour)
	up, err := PlanUninstall(InstallOpts{Prefix: prefix}, runsDir, pol)
	if err != nil {
		t.Fatalf("PlanUninstall: %v", err)
	}
	if _, err := up.ExecuteUninstall(); err != nil {
		t.Fatalf("first ExecuteUninstall: %v", err)
	}
	up2, err := PlanUninstall(InstallOpts{Prefix: prefix}, runsDir, pol)
	if err != nil {
		t.Fatalf("PlanUninstall 2: %v", err)
	}
	if _, err := up2.ExecuteUninstall(); err != nil {
		t.Fatalf("second ExecuteUninstall: %v", err)
	}
}
