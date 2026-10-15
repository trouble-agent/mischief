package ops

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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

// ── MISCHIEF-DF-27: permission-denied sudoers.d read classification ─────────

// hardenDir turns logicalDir (under Root) into a hardened shape: a dir
// the current planner cannot traverse or read (the Debian-default
// hardened /etc/sudoers.d is exactly this for an unprivileged planner —
// 0750 root:root against uid 1000). Run as the suite's uid, the
// owner-denied mode produces the identical EACCES at traversal; the
// premise pins the errno so the arms below can never assert nothing.
// Permission bits are restored on cleanup BEFORE t.TempDir's RemoveAll
// (cleanups run LIFO) or the temp root becomes undeletable.
func hardenDir(t *testing.T, logicalDir string) {
	t.Helper()
	dir := resolve(logicalDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "foreign"), []byte("Defaults env_keep+=FOO\n"), 0o440); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o070); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o750); err != nil {
			t.Logf("hardenDir cleanup: chmod restore: %v", err)
		}
	})
	if _, err := os.ReadFile(filepath.Join(dir, "foreign")); errors.Is(err, fs.ErrPermission) {
		return // hardened: EACCES at traversal, as designed
	} else if os.Getuid() == 0 {
		t.Skip("root cannot produce EACCES in-process (CAP_DAC_OVERRIDE); the permission class is covered by the classifier table")
	} else {
		t.Fatalf("premise: hardened read did not fail with EACCES: %v", err)
	}
}

// TestInstallDropInReadClassification: the read-error arms that need no
// unprivileged identity — absence stays a clean install, and a
// non-NotExist/non-Permission failure (a directory AT the drop-in path:
// EISDIR) keeps the fail-closed refusal.
func TestInstallDropInReadClassification(t *testing.T) {
	body := RenderDropIn(defaultSudoersUser)
	t.Run("not-exist stays a clean install", func(t *testing.T) {
		tempRoot(t)
		fact, err := checkForeignFile(DefaultDropInPath, body)
		if err != nil {
			t.Fatalf("absent drop-in refused: %v", err)
		}
		if fact != nil {
			t.Fatalf("absent drop-in carried a fact: %v", fact)
		}
	})
	t.Run("other read errors still refuse", func(t *testing.T) {
		tempRoot(t)
		// A directory AT the drop-in path: os.ReadFile fails with EISDIR
		// — neither NotExist nor Permission — and must keep the
		// fail-closed refusal (the repo's standing "unreadable" arm).
		if err := os.MkdirAll(resolve(DefaultDropInPath), 0o755); err != nil {
			t.Fatal(err)
		}
		fact, err := checkForeignFile(DefaultDropInPath, body)
		if fact != nil {
			t.Fatalf("EISDIR read produced a plan-warn fact: %v", fact)
		}
		var ie *InstallError
		if err == nil || !errors.As(err, &ie) || !strings.Contains(err.Error(), "unreadable") {
			t.Fatalf("EISDIR read not refused as unreadable InstallError: %v", err)
		}
	})
}

// TestInstallPermDeniedDropInPlansNotRefuses: the DF-27 core contract on
// the hardened host — the permission-denied drop-in read is the
// plan-warn fact (never a refusal), and the unprivileged dry-run PLANS
// with the folded DF-17 advisory instead of erroring (cmd wiring: a nil
// DryRun plan renders and exits 0).
func TestInstallPermDeniedDropInPlansNotRefuses(t *testing.T) {
	tempRoot(t)
	hardenDir(t, "/etc/sudoers.d")
	// classifier arm: fact, not error
	fact, err := checkForeignFile(DefaultDropInPath, RenderDropIn(defaultSudoersUser))
	if err != nil {
		t.Fatalf("permission-denied read refused: %v", err)
	}
	if fact == nil {
		t.Fatal("permission-denied read produced no plan-warn fact (planned silently)")
	}
	if !strings.Contains(fact.Error(), DefaultDropInPath) {
		t.Fatalf("fact does not name the path: %v", fact)
	}
	// plan arm: the DF-17 advisory carries the permission fact
	pinUID(t, 1000)
	p, err := PlanInstall(InstallOpts{Prefix: "/var/lib/mischief", DryRun: true})
	if err != nil {
		t.Fatalf("hardened host must plan, not refuse: %v", err)
	}
	if len(p.Actions) != 3 {
		t.Fatalf("actions: %d, want 3 (mkdir, drop-in, manifest)", len(p.Actions))
	}
	if len(p.Advisories) != 1 {
		t.Fatalf("advisories: %v, want exactly 1 (the folded DF-17 line)", p.Advisories)
	}
	for _, want := range []string{DefaultDropInPath, "requires root", "permission denied"} {
		if !strings.Contains(p.Advisories[0], want) {
			t.Fatalf("folded advisory %q missing %q", p.Advisories[0], want)
		}
	}
	r := p.Render()
	if strings.Index(r, "ADVISORY: ") > strings.Index(r, "write "+DefaultDropInPath) {
		t.Fatalf("advisory not before the action list:\n%s", r)
	}
}

// TestInstallPlanPermDeniedManifestRefused: the hardened-sudoers.d shape
// does NOT extend to the operator-owned prefix — an unreadable manifest
// there keeps the fail-closed refusal (never plan blind over one's own
// state dir).
func TestInstallPlanPermDeniedManifestRefused(t *testing.T) {
	tempRoot(t)
	hardenDir(t, "/var/lib/mischief")
	_, err := PlanInstall(InstallOpts{})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("permission-denied manifest planned through: %v", err)
	}
}

// TestClassifyReadErrorTable: the classification switch itself, fed real
// errno values — wrapped ones included (os.IsPermission returns FALSE
// for a %w-wrapped EACCES on this toolchain; the errors.Is matcher must
// see through the wrap, which is what the io-layer read produces).
func TestClassifyReadErrorTable(t *testing.T) {
	var realEACCES error
	if os.Getuid() != 0 {
		dir := t.TempDir()
		real := filepath.Join(dir, "f")
		if err := os.WriteFile(real, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(real, 0); err != nil {
			t.Fatal(err)
		}
		_, realEACCES = os.ReadFile(real)
		if realEACCES == nil {
			t.Fatal("premise failed: chmod 000 did not block a uid!=0 read")
		}
	} else {
		// as root, mode 000 still reads; the hardened-dir errno is
		// synthesized directly
		realEACCES = syscall.EACCES
	}
	tcs := []struct {
		name string
		err  error
		want readVerdict
	}{
		{"ENOENT is clean", os.ErrNotExist, readAbsent},
		{"EACCES is plan-warn", syscall.EACCES, readPermDenied},
		{"real EACCES is plan-warn", realEACCES, readPermDenied},
		{"wrapped EACCES is plan-warn", fmt.Errorf("open /etc/sudoers.d/mischief: %w", syscall.EACCES), readPermDenied},
		{"EIO is refused", syscall.EIO, readRefused},
		{"EISDIR is refused", syscall.EISDIR, readRefused},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyReadError(tc.err); got != tc.want {
				t.Fatalf("classify(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
