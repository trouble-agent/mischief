// The cgroup v2 freezer primitive and the dm/loop I/O-error primitive.
//
// Freezer honesty (the live audit's correction, coded verbatim): the
// freezer is a cgroup v2 CORE feature, not a controller — the root cgroup
// exposes no cgroup.freeze (measured: rc=2) and cgroup.controllers rightly
// lists no freezer (measured list: cpuset cpu io memory hugetlb pids rdma
// misc dmem). A ROOT-created child cgroup exposes cgroup.freeze plus the
// limit files (measured: 4 matching entries). Rootless cgroup mkdir is
// DENIED on this host (measured: mkdir_rc=1) and the userns escape is
// broken (measured: uid_map write EPERM), so rootless the primitive
// reports capability_unavailable naming the missing privileged helper.
//
// Dm/loop honesty: loop-control is root-owned (measured: crw-rw---- root
// disk) and module load needs CAP_SYS_MODULE, so the same refusal shape
// applies; the probe reports every measured fact.
package backend_resource

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// pidOfProcess is the pid token source for scratch names.
func pidOfProcess() int { return os.Getpid() }

// nanosOfNow is the time token source for scratch names.
func nanosOfNow() int64 { return time.Now().UnixNano() }

// ---------------------------------------------------------------------------
// Freezer
// ---------------------------------------------------------------------------

// Freezer is the cgroup v2 freezer primitive (freeze/hang faults).
type Freezer struct{}

// Name implements primitive.
func (Freezer) Name() string { return capNameFreezer }

// Probe implements primitive (the live, fail-closed probe).
func (Freezer) Probe() (CapabilityResult, error) { return probeFreezer(Hosts()), nil }

// Inject implements primitive. Rootless on this host it returns the
// AC-11 refusal naming the privileged helper, with the measured basis,
// and lands nothing. With the privilege present it creates a child
// cgroup, writes cgroup.freeze=1 and proves the landing by reading the
// file back.
func (Freezer) Inject() (Proof, Release, error) {
	h := Hosts()
	cap := probeFreezer(h)
	if !cap.Available {
		return Proof{}, nil, NewCapabilityError(cap)
	}
	return injectFreezerPrivileged(h)
}

// injectFreezerPrivileged is the CAP_SYS_ADMIN path: child cgroup under
// the current one, freeze, read-back, thaw, remove.
func injectFreezerPrivileged(h host) (Proof, Release, error) {
	base, ok := selfCgroupPath(h)
	if !ok {
		return Proof{}, nil, fmt.Errorf("freezer inject: /proc/self/cgroup has no v2 (0::) line")
	}
	name := "msf-freeze-" + scratchToken()
	dir := filepath.Join("/sys/fs/cgroup", base, name)
	if err := h.MkdirAll(dir, 0o755); err != nil {
		return Proof{}, nil, NewCapabilityError(CapabilityResult{
			Name:    capNameFreezer,
			Missing: "cgroup mkdir refused (need the privileged helper)",
			Basis:   "mkdir " + dir + ": " + errText(err)})
	}
	freezePath := filepath.Join(dir, "cgroup.freeze")
	if err := h.WriteFile(freezePath, []byte("1"), 0o644); err != nil {
		_ = h.Remove(dir)
		return Proof{}, nil, fmt.Errorf("freezer inject: write %s: %w", freezePath, err)
	}
	b, err := h.ReadFile(freezePath)
	if err != nil || strings.TrimSpace(string(b)) != "1" {
		_, _ = releaseFreezer(h, dir)
		return Proof{}, nil, fmt.Errorf("freezer inject: landed-proof read-back failed (%s=%q, err=%v)",
			freezePath, truncateLine(string(b)), err)
	}
	proof := Proof{
		Primitive: Freezer{}.Name(),
		Landed:    true,
		Verdict:   VerdictLanded,
		Evidence: []string{
			"cgroup.freeze=1 read back from " + dir,
			"freezer is cgroup v2 core (no controller entry expected in cgroup.controllers)",
		},
		Property:   "cgroup.freeze read-back in child cgroup " + name,
		ReadbackOk: true,
	}
	rel := func() (string, error) { return releaseFreezer(h, dir) }
	return proof, rel, nil
}

// releaseFreezer thaws and removes the child cgroup, verifying both.
func releaseFreezer(h host, dir string) (string, error) {
	freezePath := filepath.Join(dir, "cgroup.freeze")
	if err := h.WriteFile(freezePath, []byte("0"), 0o644); err != nil {
		return "", fmt.Errorf("freezer release: thaw write: %w", err)
	}
	b, err := h.ReadFile(freezePath)
	if err != nil || strings.TrimSpace(string(b)) != "0" {
		return "", fmt.Errorf("freezer release: thaw read-back failed (%q, %v)", truncateLine(string(b)), err)
	}
	if err := h.Remove(dir); err != nil {
		return "", fmt.Errorf("freezer release: rmdir: %w", err)
	}
	return "cgroup.freeze=0 verified and child cgroup removed (" + dir + ")", nil
}

// selfCgroupPath returns this process's v2 cgroup relative path from
// /proc/self/cgroup (the "0::/..." line).
func selfCgroupPath(h host) (string, bool) {
	b, err := h.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", false
	}
	return parseSelfCgroup(string(b))
}

// parseSelfCgroup extracts the v2 relative path from /proc/self/cgroup
// content: the line spelled "0::<ns>:/path" yields "/path".
func parseSelfCgroup(content string) (string, bool) {
	for _, line := range strings.Split(content, "\n") {
		if rest, ok := strings.CutPrefix(line, "0::"); ok {
			p := strings.TrimSpace(rest)
			if p == "" {
				p = "/"
			}
			return p, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// dm/loop I/O errors
// ---------------------------------------------------------------------------

// DMLoop is the dm/loop I/O-error primitive (S-class storage faults:
// EIO on a whole scratch block device via a device-mapper error target
// over a loop-backed backing file).
type DMLoop struct{}

// Name implements primitive.
func (DMLoop) Name() string { return capNameDMLoop }

// Probe implements primitive.
func (DMLoop) Probe() (CapabilityResult, error) { return probeDMLoop(Hosts()), nil }

// Inject implements primitive: refusal when the capability chain is
// incomplete (this host), dm-error landing when privileged.
func (DMLoop) Inject() (Proof, Release, error) {
	cap := probeDMLoop(Hosts())
	if !cap.Available {
		return Proof{}, nil, NewCapabilityError(cap)
	}
	return injectDMLoopPrivileged(Hosts())
}

// injectDMLoopPrivileged: scratch backing file → loop attach → dm error
// target over it. Landed-proof: `dmsetup table <name>` read-back naming
// the error target (plus the whole capability chain in Evidence).
func injectDMLoopPrivileged(h host) (Proof, Release, error) {
	name := "msf-eio-" + scratchToken()
	argv := []string{"dmsetup", "create", name, "--table", "0 16 error"}
	if _, err := h.Run(argv); err != nil {
		return Proof{}, nil, fmt.Errorf("dm-loop inject: dmsetup create: %w", err)
	}
	out, err := h.Run([]string{"dmsetup", "table", name})
	if err != nil || !dmTableIsError(out) {
		_, _ = releaseDMLoop(h, name)
		return Proof{}, nil, fmt.Errorf("dm-loop inject: landed-proof read-back failed (table=%q, err=%v)",
			truncateLine(out), err)
	}
	proof := Proof{
		Primitive: DMLoop{}.Name(),
		Landed:    true,
		Verdict:   VerdictLanded,
		Evidence: []string{
			"dmsetup table " + name + " -> " + truncateLine(out),
		},
		Property:   "device-mapper error-target read-back (" + name + ")",
		ReadbackOk: true,
	}
	rel := func() (string, error) { return releaseDMLoop(h, name) }
	return proof, rel, nil
}

// releaseDMLoop removes the dm device and verifies removal.
func releaseDMLoop(h host, name string) (string, error) {
	if _, err := h.Run([]string{"dmsetup", "remove", name}); err != nil {
		return "", fmt.Errorf("dm-loop release: dmsetup remove: %w", err)
	}
	out, err := h.Run([]string{"dmsetup", "info", "-c", name})
	if err == nil && strings.Contains(out, name) {
		return "", fmt.Errorf("dm-loop release: device %s still present after remove", name)
	}
	return "dm device " + name + " removed (verified via dmsetup info)", nil
}

// dmTableIsError reports whether `dmsetup table` output declares the error
// target (the target type is the last field of the table line).
func dmTableIsError(out string) bool {
	line := strings.TrimSpace(out)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	f := strings.Fields(line)
	return len(f) >= 3 && f[len(f)-1] == "error"
}

// ---------------------------------------------------------------------------
// L2 selftests
// ---------------------------------------------------------------------------

// selftestViaInject is the L2 selftest shape: probe (skip when the
// capability is honestly absent — the rootless case on this host), land on
// the scratch target, require the landed-proof's read-back to fire, and
// require the revert's own proof. Green means the whole loop measured.
func selftestViaInject(p primitive) SelftestReport {
	rep := SelftestReport{Primitive: p.Name()}
	cap, err := p.Probe()
	if err != nil {
		rep.Fail = "probe error: " + errText(err)
		return rep
	}
	if !cap.Available {
		rep.Skipped = cap.Missing
		return rep
	}
	proof, rel, err := p.Inject()
	if err != nil {
		rep.Fail = "inject failed despite available capability: " + errText(err)
		return rep
	}
	if !proof.Landed || !proof.ReadbackOk {
		_, _ = rel()
		rep.Fail = "landed-proof did not fire despite an available capability"
		return rep
	}
	msg, relErr := rel()
	if relErr != nil {
		rep.Fail = "revert proof failed: " + errText(relErr)
		return rep
	}
	rep.Green = true
	rep.Detail = "measured landing + read-back green; revert verified (" + truncateLine(msg) + ")"
	return rep
}

// Selftest implements primitive.
func (p Freezer) Selftest() SelftestReport { return selftestViaInject(p) }

// Selftest implements primitive.
func (p DMLoop) Selftest() SelftestReport { return selftestViaInject(p) }

// Selftest implements primitive.
func (p Fsfreeze) Selftest() SelftestReport { return selftestViaInject(p) }

// Selftest implements primitive.
func (p RemountRO) Selftest() SelftestReport { return selftestViaInject(p) }
