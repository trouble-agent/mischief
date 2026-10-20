package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/ops"
)

// MISCHIEF-DF-34: when install/uninstall abort mid-plan, the operator must
// be able to answer "what exists on disk right now because of this
// command?" from the output alone: one line per artefact that ACTUALLY
// landed (install: created/wrote) or was removed (uninstall: removed)
// before the failing step, then the failing step and its error, exit 1.
// The success paths' output is untouched (pinned by the existing verb
// tests).

// TestUninstallFailureReportsRemovedSoFar: a full install into a temp
// root, residue dropped into the prefix, then a real uninstall — the
// final rmdir refuses (fail closed) AFTER the drop-in and the manifest
// are already gone. The failure output must carry one `removed` line per
// artefact actually removed before the error, then the rmdir refusal on
// stderr, exit 1. On the pre-fix code this fails: the partial removed
// slice was discarded and only the error printed.
func TestUninstallFailureReportsRemovedSoFar(t *testing.T) {
	root := t.TempDir()
	saved := ops.Root
	ops.Root = root
	t.Cleanup(func() { ops.Root = saved })

	// a real install into the temp root (the plan engine)
	p, err := ops.PlanInstall(ops.InstallOpts{Prefix: ops.DefaultPrefix})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Execute(); err != nil {
		t.Fatal(err)
	}
	// residue the install did not create: the final prefix rmdir refuses
	if err := os.WriteFile(filepath.Join(root, "var/lib/mischief/operator-data.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	var errOut string
	code, out := captureStdout(t, func() int {
		c, e := captureStderr(t, func() int {
			return run([]string{"uninstall", "--prefix", ops.DefaultPrefix, "--runs-dir", ""})
		})
		errOut = e
		return c
	})
	if code != 1 {
		t.Fatalf("failing uninstall exit: %d, want 1\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	// one line per artefact removed before the failing step, in plan order
	want := []string{
		"removed " + ops.DefaultDropInPath,
		"removed " + ops.DefaultPrefix + "/helper-manifest.json",
	}
	for _, w := range want {
		if !strings.Contains(out, w+"\n") {
			t.Fatalf("failure output missing %q:\nstdout:\n%s\nstderr:\n%s", w, out, errOut)
		}
	}
	if got := strings.Count(out, "removed "); got != len(want) {
		t.Fatalf("removed lines: %d, want %d (no phantom removals):\n%s", got, len(want), out)
	}
	// the failing step and its error, on stderr
	if !strings.Contains(errOut, "uninstall: rmdir") || !strings.Contains(errOut, "residue") {
		t.Fatalf("stderr does not name the failing rmdir and the residue refusal:\n%s", errOut)
	}
	// the refused artefact is never reported removed
	if _, err := os.Stat(filepath.Join(root, "var/lib/mischief")); err != nil {
		t.Fatal("the refused prefix dir vanished")
	}
}

// TestInstallFailureReportsLandedSoFar: a real (non-dry-run) install that
// CREATES the state prefix and is then refused at the sudoers drop-in —
// the finding's half-state. The drop-in target is fixed
// (/etc/sudoers.d/mischief), but under a temp ops.Root it resolves inside
// the temp dir, so a read-only sudoers.d there makes the drop-in write
// fail (EACCES, non-root) AFTER the prefix mkdir has landed. The failure
// output must carry the `created` line for the prefix before the error,
// then the failing write and its error, exit 1. On the pre-fix code this
// fails: only the error printed, the created dir was invisible.
func TestInstallFailureReportsLandedSoFar(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root cannot produce EACCES in-process (CAP_DAC_OVERRIDE)")
	}
	root := t.TempDir()
	saved := ops.Root
	ops.Root = root
	t.Cleanup(func() { ops.Root = saved })

	// a hardened (read-only) sudoers.d inside the temp root: planning
	// sees a traversable dir with an absent drop-in (ENOENT — a clean
	// plan); execution lands the prefix mkdir and dies at the drop-in
	// write.
	sudoers := filepath.Join(root, "etc/sudoers.d")
	if err := os.MkdirAll(sudoers, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sudoers, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(sudoers, 0o755); err != nil {
			t.Logf("cleanup chmod: %v", err)
		}
	})

	var errOut string
	code, out := captureStdout(t, func() int {
		c, e := captureStderr(t, func() int {
			return run([]string{"install", "--prefix", ops.DefaultPrefix})
		})
		errOut = e
		return c
	})
	if code != 1 {
		t.Fatalf("failing install exit: %d, want 1\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	// the landed dir, on the same stream the success path reports writes
	if !strings.Contains(out, "created "+ops.DefaultPrefix+"\n") {
		t.Fatalf("failure output missing the landed prefix dir:\nstdout:\n%s\nstderr:\n%s", out, errOut)
	}
	if got := strings.Count(out, "created "); got != 1 {
		t.Fatalf("created lines: %d, want 1:\n%s", got, out)
	}
	// the failing step and its error, on stderr
	if !strings.Contains(errOut, "install: write "+ops.DefaultDropInPath) {
		t.Fatalf("stderr does not name the failing drop-in write:\n%s", errOut)
	}
	// the report is the disk truth: the prefix exists, nothing else landed
	if fi, err := os.Stat(filepath.Join(root, "var/lib/mischief")); err != nil || !fi.IsDir() {
		t.Fatalf("landed prefix not on disk as a dir: %v (%v)", fi, err)
	}
	if _, err := os.Stat(filepath.Join(root, "etc/sudoers.d/mischief")); !os.IsNotExist(err) {
		t.Fatal("the refused drop-in exists on disk")
	}
}
