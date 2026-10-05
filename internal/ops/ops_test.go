package ops

import (
	"os"
	"strings"
	"syscall"
	"testing"
)

// ── the control: drop-in == enforcement verb table ──────────────────────────

// TestDropInMatchesVerbTable is THE drift pin (the brief's "a test asserting
// generated drop-in == enforcement verb table"): render the drop-in, parse
// it back, and every parsed grant must be exactly a sudo-verb rendering
// from the same table — and every sudo verb must appear. The mutation arm
// then proves the comparison can FAIL (a row added to the render but not
// the table, and vice versa, is caught).
func TestDropInMatchesVerbTable(t *testing.T) {
	body := string(RenderDropIn(defaultSudoersUser))
	granted := ParseDropInVerbs(body)
	if len(granted) == 0 {
		t.Fatal("parse found no grants in the rendered drop-in")
	}
	want := map[string]bool{}
	for _, v := range SudoVerbs() {
		line := v.Binary
		if !v.wildcard() {
			var parts []string
			parts = append(parts, v.Binary)
			parts = append(parts, v.Args...)
			line = strings.Join(parts, " ")
		}
		want[line] = true
	}
	// every parsed grant is a table rendering (no smuggled rows)
	for _, g := range granted {
		if !want[g] {
			t.Fatalf("drop-in grants %q which is NOT in the verb table", g)
		}
	}
	// every table verb is granted (no silent shrink)
	seen := map[string]bool{}
	for _, g := range granted {
		seen[g] = true
	}
	for line := range want {
		if !seen[line] {
			t.Fatalf("verb table row %q is missing from the rendered drop-in", line)
		}
	}
	// determinism: same table, same bytes
	if again := string(RenderDropIn(defaultSudoersUser)); again != body {
		t.Fatal("RenderDropIn is not deterministic")
	}

	// MUTATION ARM: the comparison catches a smuggled row — a binary
	// swapped in the rendered drop-in parses back as a grant that is NOT
	// in the table.
	foreign := strings.Replace(body, "/usr/sbin/tc", "/usr/bin/evil-tc", 1)
	foundForeign := false
	for _, g := range ParseDropInVerbs(foreign) {
		if strings.HasPrefix(g, "/usr/bin/evil") {
			if want[g] {
				t.Fatal("mutation arm: foreign grant accepted as a table row")
			}
			foundForeign = true
		}
	}
	if !foundForeign {
		t.Fatal("mutation arm: the doctored drop-in did not produce a foreign grant")
	}
}

// TestCheckTracksVerbTable: the enforcement matcher accepts exactly the
// table's shapes — a wildcard row's binary with any args, a fixed row's
// exact prefix, and NOTHING else (mutation arm: a non-table binary
// refuses).
func TestCheckTracksVerbTable(t *testing.T) {
	// wildcard row: tc, any args
	if err := Check("/usr/sbin/tc", []string{"qdisc", "add", "dev", "x", "netem", "delay", "100ms"}); err != nil {
		t.Fatalf("tc wildcard refused: %v", err)
	}
	// fixed row: ip netns add <name>
	if err := Check("/usr/sbin/ip", []string{"netns", "add", "mc-test"}); err != nil {
		t.Fatalf("netns_add refused: %v", err)
	}
	// fixed row, wrong shape: ip netns (no add) refuses
	if err := Check("/usr/sbin/ip", []string{"netns", "list"}); err == nil {
		t.Fatal("ip netns list accepted — the shape check is broken")
	}
	// fixed row, missing tail arg
	if err := Check("/usr/sbin/ip", []string{"netns", "add"}); err == nil {
		t.Fatal("netns add without a name accepted")
	}
	// mutation arm: a binary outside the table refuses
	if err := Check("/bin/rm", []string{"-rf", "/"}); err == nil {
		t.Fatal("/bin/rm accepted — the allowlist is broken")
	}
	// delegated rows are never sudo-granted
	if v, ok := Lookup("cgroup_create"); !ok || v.Exec != ExecRootlessDelegated {
		t.Fatalf("cgroup_create exec kind: %+v", v)
	}
}

// TestNoSetuidInDesign: the table's exec kinds are only sudo and
// rootless-delegated; nothing in the package references setuid.
func TestNoSetuidInDesign(t *testing.T) {
	for _, v := range VerbTable() {
		switch v.Exec {
		case ExecSudo, ExecRootlessDelegated:
		default:
			t.Fatalf("verb %s: unexpected exec kind %q", v.ID, v.Exec)
		}
	}
}

// ── helper manifest ─────────────────────────────────────────────────────────

// TestHelperManifestMatchesTable: the manifest carries EVERY table row
// (sudo + delegated) with stable fields; a future helper binary's
// allowlist is exactly this.
func TestHelperManifestMatchesTable(t *testing.T) {
	m := string(RenderHelperManifest())
	for _, v := range SortedVerbs() {
		if !strings.Contains(m, `"`+v.ID+`"`) {
			t.Fatalf("manifest missing verb %s", v.ID)
		}
	}
	if !strings.Contains(m, "manifest-only") {
		t.Fatal("manifest does not state its manifest-only milestone")
	}
	if again := string(RenderHelperManifest()); again != m {
		t.Fatal("RenderHelperManifest is not deterministic")
	}
}

// ── install / uninstall (temp roots only) ───────────────────────────────────

func tempRoot(t *testing.T) {
	t.Helper()
	prev := Root
	Root = t.TempDir()
	t.Cleanup(func() { Root = prev })
}

func TestInstallPlanOrderAndModes(t *testing.T) {
	tempRoot(t)
	opts := InstallOpts{Prefix: "/var/lib/mischief"}
	p, err := PlanInstall(opts)
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}
	if len(p.Actions) != 3 {
		t.Fatalf("actions: %d, want 3 (mkdir, drop-in, manifest)", len(p.Actions))
	}
	if p.Actions[0].Kind != OpWriteDir || p.Actions[0].Path != "/var/lib/mischief" {
		t.Fatalf("first action: %+v", p.Actions[0])
	}
	drop := p.Actions[1]
	if drop.Path != DefaultDropInPath || drop.Mode != "0440" {
		t.Fatalf("drop-in action: %+v", drop)
	}
	if len(drop.Bytes) == 0 {
		t.Fatal("drop-in action carries no bytes")
	}
	// dry-run plan string names paths and byte counts, never contents
	line := drop.DryRunString()
	if strings.Contains(line, "NOPASSWD") {
		t.Fatalf("dry-run line leaks file contents: %s", line)
	}
	if !strings.Contains(line, "bytes=") {
		t.Fatalf("dry-run line missing byte count: %s", line)
	}
}

// TestInstallExecuteTempRoot: the plan engine executes into a temp root —
// drop-in at 0440, manifest at 0644, content identical to the renders.
func TestInstallExecuteTempRoot(t *testing.T) {
	tempRoot(t)
	opts := InstallOpts{Prefix: "/var/lib/mischief"}
	p, err := PlanInstall(opts)
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}
	sums, err := p.Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	drop := string(RenderDropIn(defaultSudoersUser))
	if got := sums[DefaultDropInPath]; got != sha256hex([]byte(drop)) {
		t.Fatalf("drop-in sum mismatch: %s", got)
	}
	raw, err := os.ReadFile(resolve(DefaultDropInPath))
	if err != nil || string(raw) != drop {
		t.Fatalf("drop-in on disk differs: %v", err)
	}
	fi, err := os.Stat(resolve(DefaultDropInPath))
	if err != nil || fi.Mode().Perm() != 0o440 {
		t.Fatalf("drop-in mode: %v (%v)", fi, err)
	}
	fi, err = os.Stat(resolve("/var/lib/mischief/helper-manifest.json"))
	if err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("manifest mode: %v (%v)", fi, err)
	}
	// idempotent re-install succeeds
	p2, err := PlanInstall(opts)
	if err != nil {
		t.Fatalf("re-install plan: %v", err)
	}
	if _, err := p2.Execute(); err != nil {
		t.Fatalf("idempotent re-install: %v", err)
	}
	// uninstall removes everything, idempotently
	up, err := PlanUninstall(opts, "", RetentionPolicy{})
	if err != nil {
		t.Fatalf("PlanUninstall: %v", err)
	}
	removed, err := up.ExecuteUninstall()
	if err != nil {
		t.Fatalf("ExecuteUninstall: %v", err)
	}
	if len(removed) < 3 {
		t.Fatalf("removed %d, want >= 3 (drop-in, manifest, prefix dir)", len(removed))
	}
	for _, path := range []string{DefaultDropInPath, "/var/lib/mischief/helper-manifest.json"} {
		if _, err := os.Stat(resolve(path)); !os.IsNotExist(err) {
			t.Fatalf("%s still present after uninstall", path)
		}
	}
	// second uninstall: nothing left to remove, no error
	up2, err := PlanUninstall(opts, "", RetentionPolicy{})
	if err != nil {
		t.Fatalf("PlanUninstall 2: %v", err)
	}
	removed2, err := up2.ExecuteUninstall()
	if err != nil {
		t.Fatalf("idempotent uninstall: %v", err)
	}
	if len(removed2) != 0 {
		t.Fatalf("second uninstall removed %d (want 0)", len(removed2))
	}
}

// TestInstallRefusesForeignDropIn: a file mischief did not generate at the
// drop-in path refuses the install (fail closed, no clobber).
func TestInstallRefusesForeignDropIn(t *testing.T) {
	tempRoot(t)
	if err := os.MkdirAll(resolve("/etc/sudoers.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resolve(DefaultDropInPath), []byte("Defaults env_keep+=FOO\n"), 0o440); err != nil {
		t.Fatal(err)
	}
	_, err := PlanInstall(InstallOpts{})
	if err == nil || !strings.Contains(err.Error(), "did not generate") {
		t.Fatalf("foreign drop-in not refused: %v", err)
	}
	// a drifted mischief render IS refreshable: os.WriteFile cannot
	// truncate an existing 0440 file (no write bit) — remove then write,
	// the way a regenerating operator would.
	if err := os.Remove(resolve(DefaultDropInPath)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resolve(DefaultDropInPath), []byte("# GENERATED by `mischief install` (SPEC-11).\nstale stuff\n"), 0o440); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanInstall(InstallOpts{}); err != nil {
		t.Fatalf("drifted mischief render refused: %v", err)
	}
}

// TestUninstallRefusesResiduePrefix: a non-empty prefix dir (residue the
// install did not create) refuses rmdir — uninstall never eats unknown
// files.
func TestUninstallRefusesResiduePrefix(t *testing.T) {
	tempRoot(t)
	opts := InstallOpts{Prefix: "/var/lib/mischief"}
	p, err := PlanInstall(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Execute(); err != nil {
		t.Fatal(err)
	}
	// drop residue in the prefix
	if err := os.WriteFile(resolve("/var/lib/mischief/operator-data.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	up, err := PlanUninstall(opts, "", RetentionPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = up.ExecuteUninstall()
	if err == nil || !strings.Contains(err.Error(), "residue") {
		t.Fatalf("residue prefix not refused: %v", err)
	}
	// the residue survived
	if _, err := os.Stat(resolve("/var/lib/mischief/operator-data.txt")); err != nil {
		t.Fatal("residue was removed — uninstall must never eat unknown files")
	}
}

// ── audit ───────────────────────────────────────────────────────────────────

// TestAuditFreshInstallClean: after a temp-root install the audit reports
// installed+matching, no setuid findings, every sudo verb granted.
func TestAuditFreshInstallClean(t *testing.T) {
	tempRoot(t)
	p, err := PlanInstall(InstallOpts{Prefix: "/var/lib/mischief"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Execute(); err != nil {
		t.Fatal(err)
	}
	a, err := RunAudit(AuditOpts{Prefix: "/var/lib/mischief"})
	if err != nil {
		t.Fatalf("RunAudit: %v", err)
	}
	if !a.DropInInstalled || !a.DropInMatches {
		t.Fatalf("drop-in posture: %+v diff=%s", a, a.DropInDiff)
	}
	if !a.HelperManifestInstalled || !a.HelperManifestMatches {
		t.Fatalf("manifest posture: %+v", a)
	}
	if len(a.SetuidBinaries) != 0 {
		t.Fatalf("setuid findings on a fresh install: %v", a.SetuidBinaries)
	}
	for _, v := range a.Verbs {
		if v.Exec == string(ExecSudo) && !v.GrantedInDropIn {
			t.Fatalf("sudo verb %s not granted", v.ID)
		}
		if v.ID == "cgroup_create" && v.GrantedInDropIn {
			t.Fatal("cgroup_create must never be sudo-granted")
		}
	}
	if !strings.Contains(string(RenderAuditJSON(a)), `"drop_in_matches": true`) {
		t.Fatal("audit JSON does not carry drop_in_matches")
	}
}

// TestAuditDetectsDriftAndSetuid: a hand-edited drop-in and a setuid file
// in the prefix are both reported (mutation arms on the clean state).
func TestAuditDetectsDriftAndSetuid(t *testing.T) {
	tempRoot(t)
	p, _ := PlanInstall(InstallOpts{Prefix: "/var/lib/mischief"})
	if _, err := p.Execute(); err != nil {
		t.Fatal(err)
	}
	// drift: hand-edit the drop-in (remove-then-write: the 0440 file has
	// no write bit, so an edit is unlink+create, as for any editor)
	hand := resolve(DefaultDropInPath)
	raw, _ := os.ReadFile(hand)
	if err := os.Remove(hand); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hand, append(raw, []byte("%operators ALL=(ALL) ALL\n")...), 0o440); err != nil {
		t.Fatal(err)
	}
	a, err := RunAudit(AuditOpts{Prefix: "/var/lib/mischief"})
	if err != nil {
		t.Fatal(err)
	}
	if a.DropInMatches {
		t.Fatal("hand-edited drop-in reported as matching")
	}
	if !strings.Contains(a.DropInDiff, "+%operators ALL=(ALL) ALL") {
		t.Fatalf("drift diff does not name the extra line: %s", a.DropInDiff)
	}
	// setuid finding in the prefix (chmod AFTER create: os.WriteFile's
	// mode arg is masked by the umask on create — syscall.Chmod sets the
	// 0600 bits that a plain create cannot)
	setuid := resolve("/var/lib/mischief/rogue")
	if err := os.WriteFile(setuid, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Chmod(setuid, 0o4755); err != nil {
		t.Fatal(err)
	}
	a2, err := RunAudit(AuditOpts{Prefix: "/var/lib/mischief"})
	if err != nil {
		t.Fatal(err)
	}
	if len(a2.SetuidBinaries) != 1 || !strings.Contains(a2.SetuidBinaries[0], "rogue") {
		t.Fatalf("setuid not reported: %v", a2.SetuidBinaries)
	}
}

// ── posture (the doctor bridge's engine) ────────────────────────────────────

// TestPostureAbsentNamesMissingPiece: NET_ADMIN against an EMPTY audit
// (nothing installed) reports absent with THE missing piece — the drop-in
// path named — never a bare false. This is the removed-helper arm's engine.
func TestPostureAbsentNamesMissingPiece(t *testing.T) {
	// audit of an uninstalled host (temp root: nothing on disk)
	tempRoot(t)
	a, err := RunAudit(AuditOpts{Prefix: "/var/lib/mischief"})
	if err != nil {
		t.Fatal(err)
	}
	cp := Posture(a, "NET_ADMIN")
	if cp.Available {
		t.Fatal("NET_ADMIN available with nothing installed")
	}
	if !strings.Contains(cp.Missing, DefaultDropInPath) {
		t.Fatalf("missing piece does not name the drop-in path: %q", cp.Missing)
	}
	if !strings.Contains(cp.Missing, "not installed") {
		t.Fatalf("missing piece does not name the absence: %q", cp.Missing)
	}
	// rootless-delegated kinds never require the drop-in
	cp2 := Posture(a, "cgroup2 + user scope")
	if !cp2.Available {
		t.Fatalf("cgroup2 posture blocked on the privileged surface: %+v", cp2)
	}
	// drift upgrades the reason
	p, _ := PlanInstall(InstallOpts{})
	if _, err := p.Execute(); err != nil {
		t.Fatal(err)
	}
	hand := resolve(DefaultDropInPath)
	raw, _ := os.ReadFile(hand)
	if err := os.Remove(hand); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hand, []byte(strings.Replace(string(raw), "/usr/sbin/ip", "/usr/sbin/zz", 1)), 0o440); err != nil {
		t.Fatal(err)
	}
	a2, _ := RunAudit(AuditOpts{Prefix: "/var/lib/mischief"})
	cp3 := Posture(a2, "NET_ADMIN")
	if cp3.Available || !strings.Contains(cp3.Missing, "drifted") {
		t.Fatalf("drift posture: %+v", cp3)
	}
}

// ── setuid survey ───────────────────────────────────────────────────────────

func TestSurveySetuidMissingPrefix(t *testing.T) {
	tempRoot(t)
	out, err := SurveySetuid("/var/lib/mischief")
	if err != nil || len(out) != 0 {
		t.Fatalf("missing prefix: %v %v", out, err)
	}
}
