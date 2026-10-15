package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/ops"
)

// ── SPEC-11 verb tests (MSF-012): plan-shape + dry-run-touches-nothing ──────

// TestInstallDryRunTouchesNothing: `mischief install --dry-run` prints the
// plan and the filesystem is byte-identical after (nothing created, not
// even the prefix dir). The dry-run is the plan, verbatim.
func TestInstallDryRunTouchesNothing(t *testing.T) {
	root := t.TempDir()
	saved := ops.Root
	ops.Root = root
	t.Cleanup(func() { ops.Root = saved })

	code, out := captureStdout(t, func() int { return run([]string{"install", "--dry-run", "--prefix", "/var/lib/mischief"}) })
	if code != 0 {
		t.Fatalf("install --dry-run exit: %d", code)
	}
	if !strings.Contains(out, "DRY-RUN PLAN (nothing executed) install") {
		t.Fatalf("dry-run header missing: %s", out)
	}
	if !strings.Contains(out, ops.DefaultDropInPath) || !strings.Contains(out, "0440") {
		t.Fatalf("plan does not name the drop-in and its mode: %s", out)
	}
	// NOTHING exists: no prefix, no sudoers.d, nothing.
	ents, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		names := make([]string, 0, len(ents))
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Fatalf("dry-run created %d entries: %v", len(ents), names)
	}
}

// TestInstallPlanOnlyOnForeignFile: the plan refuses over a foreign drop-in
// (the cmd surfaces the refusal, exit 1).
func TestInstallPlanOnlyOnForeignFile(t *testing.T) {
	root := t.TempDir()
	saved := ops.Root
	ops.Root = root
	t.Cleanup(func() { ops.Root = saved })

	if err := os.MkdirAll(filepath.Join(root, "etc/sudoers.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc/sudoers.d/mischief"), []byte("Defaults insult\n"), 0o440); err != nil {
		t.Fatal(err)
	}
	code, _ := captureStdout(t, func() int { return run([]string{"install", "--dry-run"}) })
	if code != 1 {
		t.Fatalf("foreign drop-in install exit: %d, want 1", code)
	}
}

// TestAuditJSONRunsOnTempRoot: `mischief audit --json` on a temp root
// (nothing installed) parses and reports drop_in_matches=false, no setuid.
// MISCHIEF-DF-16: an uninstalled host is a HEALTHY pre-install state —
// exit 0 with the explicit not_installed condition, never graded as drift.
func TestAuditJSONRunsOnTempRoot(t *testing.T) {
	root := t.TempDir()
	saved := ops.Root
	ops.Root = root
	t.Cleanup(func() { ops.Root = saved })

	code, out := captureStdout(t, func() int { return run([]string{"audit", "--json"}) })
	if code != 0 {
		t.Fatalf("audit on an uninstalled host: exit %d, want 0 (healthy pre-install)", code)
	}
	if !strings.Contains(out, `"drop_in_matches": false`) || !strings.Contains(out, `"setuid_binaries": []`) {
		t.Fatalf("audit JSON shape wrong: %s", out)
	}
	if !strings.Contains(out, `"condition": "not_installed"`) {
		t.Fatalf("audit JSON does not carry the not_installed condition: %s", out)
	}
	// the healthy-absence note states the fact without a drift remediation
	if strings.Contains(out, "run `mischief install`") {
		t.Fatalf("not_installed state carries a drift remediation: %s", out)
	}
}

// TestAuditUninstalledTextExitsZero: the text form carries the same grade
// (MISCHIEF-DF-16) — `condition: not_installed`, exit 0 on a fresh host.
func TestAuditUninstalledTextExitsZero(t *testing.T) {
	root := t.TempDir()
	saved := ops.Root
	ops.Root = root
	t.Cleanup(func() { ops.Root = saved })

	code, out := captureStdout(t, func() int { return run([]string{"audit"}) })
	if code != 0 {
		t.Fatalf("audit on an uninstalled host: exit %d, want 0 (healthy pre-install)", code)
	}
	if !strings.Contains(out, "condition: not_installed") {
		t.Fatalf("text audit does not name the not_installed condition: %s", out)
	}
	if strings.Contains(out, "run `mischief install`") {
		t.Fatalf("not_installed text audit carries a drift remediation: %s", out)
	}
}

// TestAuditDriftExitsOneNamesCondition: install, then hand-edit the
// drop-in — present-but-divergent IS drift: exit 1, condition named.
// (MISCHIEF-DF-16 reserves exit 1 for this arm.)
func TestAuditDriftExitsOneNamesCondition(t *testing.T) {
	root := t.TempDir()
	saved := ops.Root
	ops.Root = root
	t.Cleanup(func() { ops.Root = saved })

	p, err := ops.PlanInstall(ops.InstallOpts{Prefix: ops.DefaultPrefix})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Execute(); err != nil {
		t.Fatal(err)
	}
	// drift: unlink+create with an extra line (the 0440 file has no write
	// bit — the edit path any editor takes). The real path under the temp
	// root is resolve()'s shape: filepath.Join(Root, logical).
	hand := filepath.Join(root, ops.DefaultDropInPath)
	raw, err := os.ReadFile(hand)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(hand); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hand, append(raw, []byte("%operators ALL=(ALL) ALL\n")...), 0o440); err != nil {
		t.Fatal(err)
	}

	code, out := captureStdout(t, func() int { return run([]string{"audit"}) })
	if code != 1 {
		t.Fatalf("audit on a drifted host: exit %d, want 1 (present-but-divergent)", code)
	}
	if !strings.Contains(out, "condition: drift") {
		t.Fatalf("drift audit does not name the condition: %s", out)
	}
	// same grade on the JSON surface
	code, out = captureStdout(t, func() int { return run([]string{"audit", "--json"}) })
	if code != 1 {
		t.Fatalf("drift JSON audit exit: %d, want 1", code)
	}
	if !strings.Contains(out, `"condition": "drift"`) {
		t.Fatalf("drift JSON audit does not name the condition: %s", out)
	}
}

// TestRetentionReportOnlyTouchesNothing: without --apply, the retention
// verb reports the plan and removes nothing (the dirs survive).
func TestRetentionReportOnlyTouchesNothing(t *testing.T) {
	root := t.TempDir()
	saved := ops.Root
	ops.Root = root
	t.Cleanup(func() { ops.Root = saved })

	runs := filepath.Join(root, "home", "runs")
	if err := os.MkdirAll(filepath.Join(runs, "run-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out := captureStdout(t, func() int {
		return run([]string{"retention", "--runs-dir", filepath.Join(root, "home/runs"), "--keep-count", "1"})
	})
	if code != 0 {
		t.Fatalf("retention report exit: %d", code)
	}
	if !strings.Contains(out, "nothing to do") && !strings.Contains(out, "0 action") {
		t.Fatalf("report form unexpected: %s", out)
	}
	if _, err := os.Stat(filepath.Join(runs, "run-a")); err != nil {
		t.Fatal("report-only retention removed a run dir")
	}
}

// TestUninstallDryRunNamesArtefacts: the uninstall dry-run plan names the
// drop-in, the manifest, and the prefix — and touches nothing.
func TestUninstallDryRunNamesArtefacts(t *testing.T) {
	root := t.TempDir()
	saved := ops.Root
	ops.Root = root
	t.Cleanup(func() { ops.Root = saved })

	code, out := captureStdout(t, func() int { return run([]string{"uninstall", "--dry-run", "--runs-dir", ""}) })
	if code != 0 {
		t.Fatalf("uninstall --dry-run exit: %d", code)
	}
	for _, want := range []string{ops.DefaultDropInPath, ops.DefaultPrefix + "/helper-manifest.json", ops.DefaultPrefix} {
		if !strings.Contains(out, want) {
			t.Fatalf("uninstall plan does not name %s:\n%s", want, out)
		}
	}
	// nothing existed before; nothing exists now
	ents, _ := os.ReadDir(root)
	if len(ents) != 0 {
		t.Fatalf("dry-run uninstall created %d entries", len(ents))
	}
}

// TestUsageListsOpsVerbs: the SPEC-11 verbs are in the usage text.
func TestUsageListsOpsVerbs(t *testing.T) {
	_, out := captureStderr(t, func() int { return run([]string{"frobnicate"}) })
	for _, v := range []string{"install", "uninstall", "audit", "retention"} {
		if !strings.Contains(out, v) {
			t.Fatalf("usage does not list %s:\n%s", v, out)
		}
	}
}
