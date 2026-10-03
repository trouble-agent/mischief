// Package backend_resource is SPEC-07's resource & storage fault backends:
// systemd-run user scopes (R-001 memory/CPU/task limits), the cgroup v2
// freezer, dm/loop I/O errors, fsfreeze, and RO remount.
//
// # The measured host shape (PRD §9 audit, probe/RESULTS.md)
//
//   - Rootless resource limits WORK: `systemd-run --user --scope -p
//     MemoryMax=64M -p CPUQuota=10% -p TasksMax=32 true` → rc 0 (measured on
//     this host, systemd 259). The systemd-run primitive models exactly that
//     shape and proves the scope's properties by reading them BACK from
//     systemd (`show -p` values, measured renderings: MemoryMax=67108864,
//     CPUQuotaPerSecUSec=100ms, TasksMax=32).
//   - The freezer is a cgroup v2 core feature, NOT a controller: the root
//     cgroup has no cgroup.freeze (measured: freeze_rc=2) and no `freezer`
//     appears in cgroup.controllers (measured: controllers_rc=0 without
//     freezer). A CHILD cgroup created as root exposes cgroup.freeze plus
//     the limit files (measured: 4 matching entries). This host refuses
//     rootless cgroup mkdir (measured: mkdir_rc=1), so the freezer
//     primitive reports capability_unavailable naming the missing
//     privileged helper when running rootless.
//   - dm/loop I/O errors, fsfreeze and RO remount each probe their true
//     capability live: /sys/fs/cgroup, CapEff (module load needs
//     CAP_SYS_MODULE; device nodes need CAP_MKNOD), a real FIFREEZE
//     syscall, and remount against a real loop scratch only if present.
//     Scratch support probes never touch real fleet storage.
//
// # Landed-proof (AC-3, AC-13)
//
// A landing is proven by an independent measured read-back of host state —
// the fault's own exit code never decides. A primitive that cannot land
// reports the exact refusal taxonomy; a landed-proof that never fires
// grades no_op (the caller FAILS the run, never lets a green-but-did-
// nothing run pass).
//
// # Capability model
//
// Each primitive exposes two layers:
//
//   - Probe() — live host read: what is actually available right now?
//     Fail-closed on measurement failure (detail names it), so an
//     unreadable host never reads as capable.
//   - Inject() — what a caller gets when it asks to land the fault. A
//     primitive whose capability is absent returns ErrCapabilityUnavail-
//     able naming the missing privileged helper / device / permission,
//     with the measured basis in the refusal text (AC-11), and lands
//     nothing. Injecting is for a privileged caller with a scratch
//     target; on this host (uid 1000, CapEff=0, no privileged helper)
//     the honest outcome of Inject() for the L2 primitives is the
//     capability refusal.
//
// # Selftest (AC-19)
//
// Selftest() measures what this host can actually prove on an L0 scratch
// target. The systemd-run primitive runs the real measured command (skip
// is a clean SKIP, not a failure) and green requires the landed read-back;
// the L2 primitives selftest capability honesty: green requires the
// refusal to name the true missing capability with its measured basis —
// a primitive that claims availability it cannot demonstrate fails.
//
// The privileged paths (cgroup.freeze in a root-made child cgroup, dm/loop
// targets, FIFREEZE on a real fs) are exercised via dependency injection of
// a measured probe result; the test asserts the primitive's decisions follow
// the injected evidence, not this host's accident of privilege.
//
// Tests are rootless-only on this host: no network, no writes outside a
// t.TempDir() scratch and the session bus.
package backend_resource

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Verdict vocabulary: the exact strings SPEC-07 publishes for AC-3's
// anti-gaming rule.
const (
	// VerdictNoOp is the verdict of a primitive that landed nothing
	// provable: the run FAILS (AC-3).
	VerdictNoOp = "no_op"
	// VerdictLanded is the verdict when the landed-proof's read-back fired.
	VerdictLanded = "landed"
)

// ErrCapabilityUnavailable is the error a primitive reports when the host
// lacks the capability to land it (AC-11: the verb stays available, the
// refusal names what is absent). Use NewCapabilityError to build it so the
// refusal always carries the measured basis.
var ErrCapabilityUnavailable = errors.New("capability_unavailable")

// CapabilityResult is the honest capability statement one primitive reports
// for this host: available or named-absent with the measured basis. It is
// also the dependency-injection point for the privileged paths' tests.
type CapabilityResult struct {
	// Available reports whether the primitive can land on this host.
	Available bool
	// Name is the capability name ("systemd-run --user scope",
	// "privileged cgroup helper", "dm-flakey + CAP_SYS_MODULE", ...).
	Name string
	// Missing names what is absent; empty when Available.
	Missing string
	// Basis is the measured basis for the statement (the probe's raw
	// evidence), quoted in refusals and selftest output.
	Basis string
}

// refusalText renders the AC-11 refusal text: the capability name, the
// exact missing piece and the measured basis.
func (c CapabilityResult) refusalText() string {
	if c.Available {
		return ""
	}
	return fmt.Sprintf("capability %s: missing %s; measured basis: %s", c.Name, c.Missing, c.Basis)
}

// NewCapabilityError returns the wrapped ErrCapabilityUnavailable carrying
// the measured refusal text (AC-11 shape).
func NewCapabilityError(c CapabilityResult) error {
	return fmt.Errorf("%w: %s", ErrCapabilityUnavailable, c.refusalText())
}

// host is the set of process-adjacent capabilities SPEC-07's primitives
// need from the host. Production passes realHost; tests inject a fake.
// Everything the primitives do beyond these goes through os and filepath
// directly (read-only sysfs/procfs reads).
type host interface {
	// LookPath reports whether the named executable is on PATH.
	LookPath(name string) (string, error)
	// Run runs a command (argv form) and returns its combined stdout+stderr.
	Run(argv []string) (string, error)
	// ReadFile reads a file.
	ReadFile(path string) ([]byte, error)
	// Stat stats a path.
	Stat(path string) (os.FileInfo, error)
	// MkdirAll creates dir and parents (scratch-use only).
	MkdirAll(path string, perm os.FileMode) error
	// MkdirTemp creates a scratch directory (privileged inject paths only).
	MkdirTemp() (string, error)
	// WriteFile writes a file (privileged inject paths only: cgroup.freeze
	// and friends).
	WriteFile(path string, data []byte, perm os.FileMode) error
	// Remove removes a file or empty dir (scratch cleanup only).
	Remove(path string) error
	// UsernsFailed reports whether this host is known to fail userns
	// creation (measured on this host: unshare --user --map-root-user
	// true → "write failed /proc/self/uid_map: Operation not permitted").
	UsernsFailed() bool
}

// newHostErr wraps an execution failure with the argv for context.
func newHostErr(argv []string, err error) error {
	return fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
}

// realHost is the production host implementation.
type realHost struct{}

func (realHost) LookPath(name string) (string, error) { return execLookPath(name) }

func (realHost) Run(argv []string) (string, error) { return execRun(argv) }

func (realHost) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func (realHost) Stat(path string) (os.FileInfo, error) { return os.Stat(path) }

func (realHost) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }

func (realHost) MkdirTemp() (string, error) { return os.MkdirTemp("", "msf-scratch-") }

func (realHost) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (realHost) Remove(path string) error { return os.Remove(path) }

func (realHost) UsernsFailed() bool { return usernsBrokenOnHost() }

// Hosts is the production host set; tests override it (and must restore it).
var Hosts = func() host { return realHost{} }

// CAP_* bit numbers from the kernel's capability.h: the bits the capability
// probes need (stdlib only).
const (
	capMknod     = 27 // CAP_MKNOD: create the loop device node in a scratch devtmpfs.
	capSysAdmin  = 21 // CAP_SYS_ADMIN: mount/remount, freeze/thaw, cgroup subtree control.
	capSysModule = 16 // CAP_SYS_MODULE: load the dm_mod/loop modules.
)

// procSelfStatus is the seam tests point CapEff reads at a fixture.
var procSelfStatus = "/proc/self/status"

// capEff reads this process's effective capability set from
// /proc/self/status (hex, per proc(5)). Read/parse failure is fail-closed:
// capability "unknown" is capability absent, with the reason in the basis.
func capEff(h host) (uint64, string) {
	b, err := h.ReadFile(procSelfStatus)
	if err != nil {
		return 0, fmt.Sprintf("CapEff unreadable: %v (treated as no capabilities)", err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "CapEff:") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "CapEff:"))
		if len(fields) == 0 {
			return 0, "CapEff line unparseable (treated as no capabilities)"
		}
		v, err := strconv.ParseUint(fields[0], 16, 64)
		if err != nil {
			return 0, fmt.Sprintf("CapEff %q unparseable (treated as no capabilities)", fields[0])
		}
		return v, "CapEff=" + fields[0]
	}
	return 0, "CapEff line absent (treated as no capabilities)"
}

// hasCaps reports whether every named capability bit is set.
func hasCaps(eff uint64, bits ...uint64) bool {
	for _, b := range bits {
		if eff&(1<<b) == 0 {
			return false
		}
	}
	return true
}

// fsType returns the filesystem type serving path, derived from
// /proc/self/mountinfo: the longest mount-point prefix of path wins.
// Fail-closed: an unreadable/covering-less mountinfo is an error, never a
// guessed fstype.
func fsType(h host, path string) (string, error) {
	b, err := h.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", fmt.Errorf("fstype %s: mountinfo unreadable: %w", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("fstype %s: %w", path, err)
	}
	best := ""
	bestLen := -1
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" {
			continue
		}
		mp, ok := mountPointOf(line)
		if !ok {
			continue
		}
		if !pathPrefixMatch(mp, abs) {
			continue
		}
		if len(mp) > bestLen {
			bestLen = len(mp)
			best = fsTypeFromLine(line)
		}
	}
	if best == "" {
		return "", fmt.Errorf("fstype %s: no mountinfo entry covers the path", path)
	}
	return best, nil
}

// mountPointOf extracts the (unescaped) mount point (mountinfo field 4,
// 0-indexed) from one mountinfo line.
func mountPointOf(line string) (string, bool) {
	f := strings.Split(line, " ")
	if len(f) < 10 || f[4] == "" {
		return "", false
	}
	mp := f[4]
	if strings.Contains(mp, "\\") {
		mp = decodeMountOctal(mp)
	}
	return mp, true
}

// fsTypeFromLine extracts the fstype from one mountinfo line: everything
// after the " - " separator belongs to the superblock block, whose field 0
// is the fstype.
func fsTypeFromLine(line string) string {
	i := strings.Index(line, " - ")
	if i < 0 {
		return ""
	}
	right := strings.Split(line[i+3:], " ")
	if len(right) == 0 {
		return ""
	}
	return right[0]
}

// pathPrefixMatch reports whether dir is a path-prefix of p (component
// boundary respected: /sys/fs/cgroup is a prefix of /sys/fs/cgroup/a but
// not of /sys/fs/cgroupfoo). The root mount "/" covers every absolute
// path.
func pathPrefixMatch(dir, p string) bool {
	if dir == "/" {
		return strings.HasPrefix(p, "/")
	}
	if dir == p {
		return true
	}
	return strings.HasPrefix(p, dir) && len(p) > len(dir) && p[len(dir)] == '/'
}

// osMkdirTemp and filepathAbs are the local aliases for the scratch and
// path helpers (named so grep for os.MkdirTemp lands on realHost).
func osMkdirTemp() (string, error) { return os.MkdirTemp("", "msf-scratch-") }

func filepathAbs(p string) (string, error) { return filepath.Abs(p) }

// decodeMountOctal reverses mountinfo's octal escapes (\040 space, \011
// tab, \012 newline, \134 backslash) per proc(5).
func decodeMountOctal(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) &&
			s[i+1] >= '0' && s[i+1] <= '7' &&
			s[i+2] >= '0' && s[i+2] <= '7' &&
			s[i+3] >= '0' && s[i+3] <= '7' {
			v, _ := strconv.ParseUint(s[i+1:i+4], 8, 8)
			b.WriteByte(byte(v))
			i += 3
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// usernsBrokenOnHost is the cached, measured answer to "does unshare
// --user work here" (this host refuses at the uid_map write). The probe
// runs at most once per process; a fake host answers from its fixture.
var (
	usernsOnce   sync.Once
	usernsBroken bool
)

func usernsBrokenOnHost() bool {
	usernsOnce.Do(func() {
		_, err := execRun([]string{"unshare", "--user", "--map-root-user", "true"})
		usernsBroken = err != nil
	})
	return usernsBroken
}
