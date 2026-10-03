// Tests for the capability layer: the fail-closed rule, the live-host
// mountinfo/fstype facts, and the freezer/dm-loop/fsfreeze/remount probes
// under injected evidence (the privileged paths' dependency-injection
// point). Rootless-only on this host: no network, no writes outside
// t.TempDir() scratch and the session bus.
//
// ch:trace row=MSF-008 spec=docs/SPEC-PLAN.md (SPEC-07) test=internal/backend_resource/capability_test.go evidence=probe/RESULTS.md witness=live:host-probes-rootless
package backend_resource

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeHost is the dependency-injection host: every answer is a fixture,
// nothing on the real host is touched except the process env.
type fakeHost struct {
	paths    map[string]bool // LookPath answers (present?)
	runs     map[string]string
	runErrs  map[string]error
	runFunc  func(argv []string) (string, error, bool) // optional catch-all hook
	files    map[string]string                         // ReadFile answers
	dirs     map[string]bool                           // Stat dir answers
	missing  map[string]bool                           // Stat ENOENT answers
	statErr  map[string]error                          // other Stat errors
	unBroken bool                                      // UsernsFailed answer
}

func newFakeHost() *fakeHost {
	return &fakeHost{
		paths:   map[string]bool{},
		runs:    map[string]string{},
		runErrs: map[string]error{},
		files:   map[string]string{},
		dirs:    map[string]bool{},
		missing: map[string]bool{},
		statErr: map[string]error{},
	}
}

func (f *fakeHost) LookPath(name string) (string, error) {
	if f.paths[name] {
		return "/usr/bin/" + name, nil
	}
	return "", errors.New("not found")
}

func (f *fakeHost) Run(argv []string) (string, error) {
	if f.runFunc != nil {
		if out, err, ok := f.runFunc(argv); ok {
			return out, err
		}
	}
	key := strings.Join(argv, " ")
	if err, ok := f.runErrs[key]; ok {
		return "", err
	}
	if out, ok := f.runs[key]; ok {
		return out, nil
	}
	return "", errors.New("unexpected command: " + key)
}

func (f *fakeHost) ReadFile(path string) ([]byte, error) {
	if s, ok := f.files[path]; ok {
		return []byte(s), nil
	}
	return nil, errors.New("open " + path + ": no such file")
}

func (f *fakeHost) Stat(path string) (os.FileInfo, error) {
	if f.missing[path] {
		return nil, os.ErrNotExist
	}
	if err, ok := f.statErr[path]; ok {
		return nil, err
	}
	if f.dirs[path] {
		return fakeFileInfo{name: filepath.Base(path), dir: true}, nil
	}
	// Natural default: nothing exists on the fake unless declared —
	// an undeclared host is an empty host, never a capable one.
	return nil, os.ErrNotExist
}

func (f *fakeHost) MkdirAll(path string, _ os.FileMode) error { f.dirs[path] = true; return nil }

func (f *fakeHost) MkdirTemp() (string, error) { return os.MkdirTemp("", "msf-test-") }

func (f *fakeHost) WriteFile(path string, _ []byte, _ os.FileMode) error {
	f.files[path] = ""
	return nil
}

func (f *fakeHost) Remove(path string) error { delete(f.dirs, path); return nil }

func (f *fakeHost) UsernsFailed() bool { return f.unBroken }

// fakeFileInfo is a minimal os.FileInfo for the fake host.
type fakeFileInfo struct {
	name string
	dir  bool
}

func (fi fakeFileInfo) Name() string       { return fi.name }
func (fi fakeFileInfo) Size() int64        { return 0 }
func (fi fakeFileInfo) Mode() os.FileMode  { return os.ModeDir*os.FileMode(boolToBit(fi.dir)) | 0o755 }
func (fi fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fi fakeFileInfo) IsDir() bool        { return fi.dir }
func (fi fakeFileInfo) Sys() any           { return nil }

// boolToBit carries the fake's dir bit into Mode().
func boolToBit(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// capEff: fail-closed parsing
// ---------------------------------------------------------------------------

func TestCapEffParsesHexAndFailsClosed(t *testing.T) {
	h := newFakeHost()
	h.files[procSelfStatus] = "Name:x\nCapEff:\t000001ffffffffff\nCapBnd:\t000001ffffffffff\n"
	eff, basis := capEff(h)
	if !hasCaps(eff, capSysModule, capMknod, capSysAdmin) {
		t.Fatalf("capEff lost capability bits: eff=%d", eff)
	}
	if !strings.Contains(basis, "CapEff=000001ffffffffff") {
		t.Fatalf("basis %q lacks the measured value", basis)
	}
	// Unparseable value → zero, fail-closed, named in the basis.
	h.files[procSelfStatus] = "CapEff:\tnothex\n"
	eff, basis = capEff(h)
	if eff != 0 || !strings.Contains(basis, "unparseable") {
		t.Fatalf("CapEff=nothex must fail closed: eff=%d basis=%q", eff, basis)
	}
	// Unreadable file → zero, fail-closed, named in the basis.
	h2 := newFakeHost() // no CapEff fixture at all
	eff, basis = capEff(h2)
	if eff != 0 || !strings.Contains(basis, "unreadable") {
		t.Fatalf("missing /proc/self/status must fail closed: eff=%d basis=%q", eff, basis)
	}
}

// ---------------------------------------------------------------------------
// fsType: the live-host mountinfo read (rootless, read-only)
// ---------------------------------------------------------------------------

func TestFsTypeDetectsRealCgroupMount(t *testing.T) {
	h := Hosts()
	got, err := fsType(h, "/sys/fs/cgroup/cgroup.controllers")
	if err != nil {
		t.Skipf("live mountinfo read unavailable on this host: %v", err)
	}
	// mountinfo's fstype token for the unified v2 hierarchy is "cgroup2"
	// (the probe script's "cgroup2fs" spelling comes from stat -f, a
	// different source; mountinfo is what this package reads).
	if got != "cgroup2" {
		t.Fatalf("fsType(/sys/fs/cgroup)=%q, want cgroup2 (PRD audit: cgroup v2 unified is a measured fact)", got)
	}
	// A path under the workspace must resolve a real, non-cgroup fstype.
	wd, err := os.Getwd()
	if err != nil {
		t.Skipf("no cwd: %v", err)
	}
	got, err = fsType(h, filepath.Join(wd, "capability_test.go"))
	if err != nil || got == "" || got == "cgroup2fs" {
		t.Fatalf("fsType under the workspace = %q (err=%v); want a real fs, never cgroup2fs", got, err)
	}
}

func TestFsTypeUncoveredPathFailsClosed(t *testing.T) {
	h := newFakeHost()
	h.files["/proc/self/mountinfo"] = "36 35 0:34 / /sys/fs/cgroup rw,nosuid nodev shared:24 - cgroup2fs cgroup rw\n"
	if _, err := fsType(h, "/tmp/nowhere-under-any-mount"); err == nil {
		t.Fatal("fsType on an uncovered path must fail closed, got nil error")
	}
}

func TestMountPointOctalDecoding(t *testing.T) {
	line := `40 35 0:36 / /mnt/we\040ird rw - ext4 /dev/sda1 rw`
	mp, ok := mountPointOf(line)
	if !ok || mp != "/mnt/we ird" {
		t.Fatalf("mountPointOf octal decode = %q ok=%v, want /mnt/we ird", mp, ok)
	}
	if got := fsTypeFromLine(line); got != "ext4" {
		t.Fatalf("fsTypeFromLine = %q, want ext4", got)
	}
	if !pathPrefixMatch("/sys/fs/cgroup", "/sys/fs/cgroup/a") ||
		pathPrefixMatch("/sys/fs/cgroup", "/sys/fs/cgroupfoo") {
		t.Fatal("pathPrefixMatch component boundary broken")
	}
	if !pathPrefixMatch("/", "/home/kara/anything") {
		t.Fatal("pathPrefixMatch: the root mount must cover every absolute path")
	}
}

// ---------------------------------------------------------------------------
// probeFreezer: the audit-correction coding (DI of the measured evidence)
// ---------------------------------------------------------------------------

// freezerFixture sets the fake host to the MEASURED rootless shape of this
// fleet host (uid 1000, CapEff=0, no root freeze file, controllers without
// freezer, userns broken).
func freezerFixture() *fakeHost {
	h := newFakeHost()
	h.unBroken = true
	h.files[procSelfStatus] = "CapEff:\t0000000000000000\n"
	h.files["/proc/self/cgroup"] = "0::/user.slice/user-1000.slice/user@1000.service/app.slice\n"
	h.files["/sys/fs/cgroup/cgroup.controllers"] = "cpuset cpu io memory hugetlb pids rdma misc dmem\n"
	h.missing["/sys/fs/cgroup/cgroup.freeze"] = true
	return h
}

func TestProbeFreezerRootlessRefusesNamingHelper(t *testing.T) {
	h := freezerFixture()
	c := probeFreezer(h)
	if c.Available {
		t.Fatal("freezer capability must be unavailable in the measured rootless fixture")
	}
	if !strings.Contains(c.Missing, "privileged helper") {
		t.Fatalf("refusal must name the privileged helper, got: %q", c.Missing)
	}
	// The measured basis: no root freeze file + controller list without
	// freezer + zero caps + broken userns.
	for _, want := range []string{
		"cgroup.freeze absent",
		"cgroup.controllers=cpuset cpu io memory hugetlb pids rdma misc dmem",
		"CapEff=0000000000000000",
		"userns creation refused",
	} {
		if !strings.Contains(c.Basis, want) {
			t.Fatalf("basis lacks measured evidence %q; basis: %q", want, c.Basis)
		}
	}
	// AC-11 shape: the rendered error wraps ErrCapabilityUnavailable and
	// names both the helper and the measured basis.
	err := NewCapabilityError(c)
	if !errors.Is(err, ErrCapabilityUnavailable) {
		t.Fatalf("NewCapabilityError must wrap ErrCapabilityUnavailable, got %v", err)
	}
	if !strings.Contains(err.Error(), "privileged helper") || !strings.Contains(err.Error(), "measured basis") {
		t.Fatalf("refusal text incomplete: %q", err.Error())
	}
}

func TestProbeFreezerRootFreezeFileMeansAvailable(t *testing.T) {
	// The audit's sibling fact: a cgroup level that EXPOSES cgroup.freeze
	// (root-owned child as root, or a delegated user level) is capable
	// without anything else.
	h := freezerFixture()
	delete(h.missing, "/sys/fs/cgroup/cgroup.freeze")
	h.dirs["/sys/fs/cgroup/cgroup.freeze"] = true // declared = exists on the fake
	c := probeFreezer(h)
	if !c.Available {
		t.Fatalf("freezer with root freeze file present must be available, got refusal: %q (%s)", c.Missing, c.Basis)
	}
	if !strings.Contains(c.Basis, "cgroup.freeze present") {
		t.Fatalf("basis must cite the freeze file, got: %q", c.Basis)
	}
}

func TestProbeFreezerPrivilegedCapsMakeAvailable(t *testing.T) {
	// With CAP_SYS_ADMIN (the privileged helper's minimum), the measured
	// child-cgroup fact (4 freeze/limit files) makes the capability
	// available even without the root freeze file.
	h := freezerFixture()
	h.files[procSelfStatus] = "CapEff:\t000001ffffffffff\n"
	c := probeFreezer(h)
	if !c.Available {
		t.Fatalf("freezer with CAP_SYS_ADMIN must be available, got refusal: %q (%s)", c.Missing, c.Basis)
	}
	if !strings.Contains(c.Basis, "4 freeze/limit files") {
		t.Fatalf("basis must cite the measured child-cgroup shape, got: %q", c.Basis)
	}
}

func TestProbeFreezerUnreadableHostFailsClosed(t *testing.T) {
	// No fixture files at all: an unreadable host must NEVER read as
	// capable — and the basis must say why.
	h := newFakeHost()
	c := probeFreezer(h)
	if c.Available {
		t.Fatal("unreadable host must fail closed (capability unavailable)")
	}
	if !strings.Contains(c.Basis, "unreadable") {
		t.Fatalf("fail-closed basis must name the unreadable probe: %q", c.Basis)
	}
}

// ---------------------------------------------------------------------------
// probeDMLoop / probeFsfreeze / probeRemountRO under injected evidence
// ---------------------------------------------------------------------------

func TestProbeDMLoopRootlessRefusalNamesBothGaps(t *testing.T) {
	h := freezerFixture() // same measured host shape
	// Measured host fidelity: the tool exists, the loop chain is what's
	// denied (loop-control is root-owned on this host).
	h.paths["losetup"] = true
	h.runErrs["losetup -f"] = errors.New("losetup: cannot find a free loop: permission denied")
	c := probeDMLoop(h)
	if c.Available {
		t.Fatal("dm/loop must be unavailable rootless in the measured fixture")
	}
	if !strings.Contains(c.Missing, "CAP_SYS_MODULE+CAP_MKNOD") || !strings.Contains(c.Missing, "free loop device") {
		t.Fatalf("refusal must name both missing pieces, got: %q", c.Missing)
	}
	if !strings.Contains(c.Basis, "CapEff=0000000000000000") || !strings.Contains(c.Basis, "losetup -f failed") {
		t.Fatalf("basis must carry both measured facts, got: %q", c.Basis)
	}
}

func TestProbeDMLoopWithCapsAndLoopAvailable(t *testing.T) {
	h := freezerFixture()
	h.files[procSelfStatus] = "CapEff:	000001ffffffffff\n"
	h.paths["losetup"] = true
	h.runs["losetup -f"] = "/dev/loop27\n"
	c := probeDMLoop(h)
	if !c.Available {
		t.Fatalf("dm/loop with caps + free loop must be available, got: %q (%s)", c.Missing, c.Basis)
	}
	if !strings.Contains(c.Basis, "/dev/loop27") {
		t.Fatalf("basis must cite the measured loop device, got: %q", c.Basis)
	}
}

func TestProbeFsfreezeToolPresentCapsMissing(t *testing.T) {
	h := freezerFixture()
	h.paths["fsfreeze"] = true
	c := probeFsfreeze(h)
	if c.Available {
		t.Fatal("fsfreeze without CAP_SYS_ADMIN must be unavailable")
	}
	if !strings.Contains(c.Missing, "CAP_SYS_ADMIN") || !strings.Contains(c.Missing, "FIFREEZE") {
		t.Fatalf("fsfreeze refusal must name the kernel gate, got: %q", c.Missing)
	}
}

func TestProbeFsfreezeMissingToolNamedAlone(t *testing.T) {
	h := freezerFixture() // fsfreeze NOT on paths
	c := probeFsfreeze(h)
	if c.Available || !strings.Contains(c.Missing, "fsfreeze binary") {
		t.Fatalf("missing binary must be the named gap, got available=%v missing=%q", c.Available, c.Missing)
	}
}

func TestProbeRemountRONamesMountSyscallGate(t *testing.T) {
	h := freezerFixture()
	c := probeRemountRO(h)
	if c.Available {
		t.Fatal("RO remount without CAP_SYS_ADMIN must be unavailable")
	}
	if !strings.Contains(c.Missing, "CAP_SYS_ADMIN") || !strings.Contains(c.Missing, "mount(2)") {
		t.Fatalf("remount refusal must name the mount(2) gate, got: %q", c.Missing)
	}
	if !strings.Contains(c.Basis, "EROFS") {
		t.Fatalf("remount basis must name the probe shape's EROFS proof, got: %q", c.Basis)
	}
}

// ---------------------------------------------------------------------------
// The Inject refusals on the measured host shape (AC-11: lands nothing)
// ---------------------------------------------------------------------------

func TestInjectRefusalsOnRootlessHostLandNothing(t *testing.T) {
	saved := Hosts
	t.Cleanup(func() { Hosts = saved })
	Hosts = func() host { return freezerFixture() }

	for _, p := range Table() {
		switch p.(type) {
		case systemdScope:
			continue // live-bus primitive: covered by its own tests
		}
		proof, rel, err := p.Inject()
		if err == nil {
			t.Fatalf("%s: Inject must refuse on the measured rootless host", p.Name())
		}
		if !errors.Is(err, ErrCapabilityUnavailable) {
			t.Fatalf("%s: refusal must wrap ErrCapabilityUnavailable, got %v", p.Name(), err)
		}
		if proof.Landed || proof.ReadbackOk || rel != nil {
			t.Fatalf("%s: refusal must land nothing (proof=%+v rel=%v)", p.Name(), proof, rel)
		}
		if !strings.Contains(err.Error(), "measured basis") {
			t.Fatalf("%s: refusal text must carry the measured basis, got %q", p.Name(), err.Error())
		}
	}
}
