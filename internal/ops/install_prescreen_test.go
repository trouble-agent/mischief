package ops

import (
	"strings"
	"testing"
)

// MISCHIEF-DF-17: the install plan pre-screens the FIXED privileged
// drop-in target (/etc/sudoers.d/mischief) for an unprivileged caller —
// the dry-run already knew uid!=0, the prefix was unprivileged, and the
// target was /etc: a real install was guaranteed to fail at action 2 of 3
// and half-install. The screen is an advisory line on the plan (data, not
// an error): planning behavior is otherwise unchanged.

// pinUID pins the UIDOf seam for the test (never the ambient uid — the
// dev host is uid 1000 and tests must not depend on that).
func pinUID(t *testing.T, uid int) {
	t.Helper()
	prev := UIDOf
	UIDOf = func() int { return uid }
	t.Cleanup(func() { UIDOf = prev })
}

// TestPrivilegedDropInBlockedDetector: the detector itself — root never
// screens, unprivileged + a privileged fixed target does, --no-drop-in
// plans no privileged target at all.
func TestPrivilegedDropInBlockedDetector(t *testing.T) {
	pinUID(t, 0)
	rootOpts := InstallOpts{Prefix: DefaultPrefix}
	if got := rootOpts.privilegedDropInBlocked(); got != "" {
		t.Fatalf("root screened: %q", got)
	}
	pinUID(t, 1000)
	nonRoot := InstallOpts{Prefix: "/tmp/dfx"}
	got := nonRoot.privilegedDropInBlocked()
	for _, want := range []string{DefaultDropInPath, "requires root", "/tmp/dfx"} {
		if !strings.Contains(got, want) {
			t.Fatalf("advisory %q missing %q", got, want)
		}
	}
	skip := InstallOpts{Prefix: DefaultPrefix, SkipDropIn: true}
	if got := skip.privilegedDropInBlocked(); got != "" {
		t.Fatalf("--no-drop-in screened: %q", got)
	}
}

// TestIsPrivilegedPath: only top-level system dirs count (a temp Root or
// /tmp prefix is writable unprivileged).
func TestIsPrivilegedPath(t *testing.T) {
	for _, p := range []string{"/etc/sudoers.d/mischief", "/usr/local/bin/x", "/var/lib/m", "/boot/vmlinuz"} {
		if !isPrivilegedPath(p) {
			t.Fatalf("%s should be privileged", p)
		}
	}
	for _, p := range []string{"/tmp/dfx", "/home/u/m", "/mischief/state", "/etcless/thing"} {
		if isPrivilegedPath(p) {
			t.Fatalf("%s should not be privileged", p)
		}
	}
}

// TestInstallPlanAdvisoryNonRoot: the plan carries the advisory naming the
// blocker BEFORE execution, still plans all 3 actions, and (temp root) a
// real execute still succeeds — the screen advises, it does not block.
func TestInstallPlanAdvisoryNonRoot(t *testing.T) {
	tempRoot(t)
	pinUID(t, 1000)
	opts := InstallOpts{Prefix: "/var/lib/mischief"}
	p, err := PlanInstall(opts)
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}
	if len(p.Advisories) != 1 {
		t.Fatalf("advisories: %v, want exactly 1", p.Advisories)
	}
	adv := p.Advisories[0]
	for _, want := range []string{DefaultDropInPath, "requires root"} {
		if !strings.Contains(adv, want) {
			t.Fatalf("advisory %q missing %q", adv, want)
		}
	}
	// rendered loudly, before the action list
	r := p.Render()
	if strings.Index(r, "ADVISORY: ") > strings.Index(r, "write "+DefaultDropInPath) {
		t.Fatalf("advisory not before the action list:\n%s", r)
	}
	// planning unchanged: mkdir, drop-in, manifest
	if len(p.Actions) != 3 {
		t.Fatalf("actions: %d, want 3", len(p.Actions))
	}
	// advisory does not block a writable-root execute
	if _, err := p.Execute(); err != nil {
		t.Fatalf("real-mode execute under a writable root: %v", err)
	}
}

// TestInstallPlanNoAdvisoryRoot: the root path is unchanged — no advisory,
// same plan shape.
func TestInstallPlanNoAdvisoryRoot(t *testing.T) {
	tempRoot(t)
	pinUID(t, 0)
	p, err := PlanInstall(InstallOpts{Prefix: "/var/lib/mischief"})
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}
	if len(p.Advisories) != 0 {
		t.Fatalf("root plan carries advisories: %v", p.Advisories)
	}
	if len(p.Actions) != 3 {
		t.Fatalf("actions: %d, want 3", len(p.Actions))
	}
	if strings.Contains(p.Render(), "ADVISORY") {
		t.Fatal("root render carries an ADVISORY line")
	}
}
