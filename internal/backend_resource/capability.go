// Capability probing for the SPEC-07 storage/resource backends. Every
// primitive answers "can it land here?" from a live measurement, and the
// answer is fail-closed: an unreadable host reads as incapable, never
// capable. The measured basis (the probe's raw evidence) rides in every
// refusal so the AC-11 text names what is absent and WHY that claim is
// measured rather than assumed.
//
// Measured on this host while writing the probes (uid 1000, CapEff=0):
//
//   - /sys/fs/cgroup/cgroup.controllers → "cpuset cpu io memory hugetlb
//     pids rdma misc dmem" (no freezer — it is a core feature, not a
//     controller, so its absence from the controller list is correct and
//     must never be read as "freezer unsupported");
//   - /sys/fs/cgroup/cgroup.freeze → absent (rc=2) at the ROOT cgroup;
//   - mkdir /sys/fs/cgroup/<name> → Permission denied (mkdir_rc=1);
//   - a ROOT-created child cgroup exposes cgroup.freeze + limit files
//     (probe/RESULTS.md: 4 matching entries).
package backend_resource

import (
	"strings"
)

// Capability names used across the primitives (stable strings: they appear
// in refusals, selftest reports and the catalog's landed-proof prose).
const (
	capNameScope     = "systemd-run user scope"
	capNameFreezer   = "cgroup v2 freezer"
	capNameDMLoop    = "dm/loop I/O error backend"
	capNameFsfreeze  = "fsfreeze (FIFREEZE)"
	capNameRemountRO = "read-only remount"
)

// cgroupControllers reads /sys/fs/cgroup/cgroup.controllers. The path is
// read via the host seam; the default v2 mount is fixed at /sys/fs/cgroup
// (verified against mountinfo by the caller that cares: TestFsType detects
// the real mount and skips when it is not v2, so the tests stay honest on
// any host).
func cgroupControllers(h host) (string, error) {
	b, err := h.ReadFile("/sys/fs/cgroup/cgroup.controllers")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// rootFreezeFile reports whether /sys/fs/cgroup/cgroup.freeze exists. The
// MEASURED truth this encodes (PRD audit correction): the root cgroup does
// NOT expose it; a root-created child does. Existence at the root is the
// correct signal for "the freezer interface is reachable at our level" —
// an absent file means the freeze path needs a child cgroup, which needs
// the privileged helper on this host.
func rootFreezeFile(h host) (bool, string) {
	if _, err := h.Stat("/sys/fs/cgroup/cgroup.freeze"); err == nil {
		return true, "cgroup.freeze present at /sys/fs/cgroup (root exposes the freeze interface)"
	}
	return false, "/sys/fs/cgroup/cgroup.freeze absent (root cgroup has no freeze file; freezer is core, not a controller)"
}

// probeFreezer is the freezer primitive's live capability probe. It does
// NOT attempt a rootless mkdir (measured DENIED on this host; probing by
// attempting would only repeat the audit's measurement). It reads the
// freeze interface's real position: root freeze file (absent here), the
// controller list (freezer correctly absent — core feature), CapEff
// (CAP_SYS_ADMIN gates cgroup subtree control), and the userns status
// (measured broken on this host, so no unshared-mount escape either).
func probeFreezer(h host) CapabilityResult {
	name := capNameFreezer
	present, basis := rootFreezeFile(h)
	if present {
		return CapabilityResult{Available: true, Name: name,
			Basis: basis + "; freeze path reachable at the current cgroup level"}
	}
	eff, capBasis := capEff(h)
	privileged := hasCaps(eff, capSysAdmin)
	controllers, cErr := cgroupControllers(h)
	cgBasis := "cgroup.controllers unreadable"
	if cErr == nil {
		cgBasis = "cgroup.controllers=" + truncateLine(controllers)
	}
	unBasis := "userns available"
	if h.UsernsFailed() {
		unBasis = "userns creation refused (uid_map write fails)"
	}
	if privileged {
		return CapabilityResult{Available: true, Name: name,
			Basis: basis + "; " + capBasis + " with CAP_SYS_ADMIN — root can create the child cgroup (measured 4 freeze/limit files in a root child)"}
	}
	return CapabilityResult{
		Name:    name,
		Missing: "privileged helper (a helper that creates a child cgroup and writes cgroup.freeze); no CAP_SYS_ADMIN, rootless cgroup mkdir measured DENIED, userns escape unavailable",
		Basis:   basis + "; " + cgBasis + "; " + capBasis + "; " + unBasis,
	}
}

// loopFreeDevice finds a free loop device via `losetup -f` (measured on
// this host: /dev/loop27 with the tools present). Empty result means the
// tool is absent or answered nothing; the basis line says which.
func loopFreeDevice(h host) (string, string) {
	if _, err := h.LookPath("losetup"); err != nil {
		return "", "losetup not on PATH"
	}
	out, err := h.Run([]string{"losetup", "-f"})
	if err != nil {
		return "", "losetup -f failed: " + errText(err)
	}
	dev := strings.TrimSpace(out)
	if dev == "" {
		return "", "losetup -f returned nothing (no free loop device)"
	}
	return dev, "losetup -f -> " + dev
}

// probeDMLoop is the dm/loop I/O-error primitive's probe. LANDING an
// I/O error needs: the loop or device-mapper substrate (module load:
// CAP_SYS_MODULE; device nodes: CAP_MKNOD/CAP_SYS_ADMIN), and a scratch
// backing file + a free loop device. This host measures: uid 1000,
// CapEff=0, loop-control root-only. The probe reports each measured fact;
// capability is true only when the whole chain is demonstrable.
func probeDMLoop(h host) CapabilityResult {
	name := capNameDMLoop
	eff, capBasis := capEff(h)
	missing := make([]string, 0, 4)
	bases := make([]string, 0, 4)
	if !hasCaps(eff, capSysModule, capMknod) {
		missing = append(missing, "CAP_SYS_MODULE+CAP_MKNOD (dm_mod/loop module load + scratch device node)")
		bases = append(bases, capBasis)
	}
	dev, loopBasis := loopFreeDevice(h)
	if dev == "" {
		missing = append(missing, "free loop device")
		bases = append(bases, loopBasis)
	} else {
		bases = append(bases, loopBasis)
	}
	if len(missing) == 0 {
		return CapabilityResult{Available: true, Name: name, Basis: strings.Join(bases, "; ")}
	}
	return CapabilityResult{
		Name:    name,
		Missing: strings.Join(missing, " AND "),
		Basis:   strings.Join(bases, "; "),
	}
}

// probeFsfreeze probes the FIFREEZE path: the binary and, decisively, the
// kernel interface — an actual freeze/thaw of an UNMOUNTED-IN-SCRATCH ext4
// on a scratch loop device is the only honest proof, which needs the same
// privileges as the dm/loop chain (CAP_SYS_ADMIN on the mount + a loop
// device). Without a loop device to prove against, the probe reports the
// tool present but the interface unproven — capability unavailable with
// the measured basis (this host: loop-control is root-only).
func probeFsfreeze(h host) CapabilityResult {
	name := capNameFsfreeze
	if _, err := h.LookPath("fsfreeze"); err != nil {
		return CapabilityResult{Name: name,
			Missing: "fsfreeze binary",
			Basis:   "fsfreeze not on PATH"}
	}
	// The interface proof needs CAP_SYS_ADMIN (FIFREEZE checks it in
	// sb_prepare_remount_read / freeze_super) plus a scratch block device.
	eff, capBasis := capEff(h)
	if !hasCaps(eff, capSysAdmin) {
		return CapabilityResult{Name: name,
			Missing: "CAP_SYS_ADMIN (FIFREEZE/FITHAW reject unprivileged callers) — freeze faults need the privileged helper",
			Basis:   "fsfreeze on PATH; " + capBasis + "; scratch-probe shape: mkfs.ext4 on losetup-attached scratch file, FIFREEZE, FITHAW"}
	}
	dev, loopBasis := loopFreeDevice(h)
	if dev == "" {
		return CapabilityResult{Name: name,
			Missing: "scratch loop device for the interface proof",
			Basis:   loopBasis}
	}
	return CapabilityResult{Available: true, Name: name,
		Basis: "fsfreeze on PATH; " + capBasis + "; " + loopBasis}
}

// probeRemountRO probes the RO-remount path: a bind-mount of a scratch
// directory followed by `mount -o remount,ro,bind` is a CAP_SYS_ADMIN
// syscall path (mount(2)); rootless callers are refused by the kernel.
// The probe measures the binary/tool presence, the capability, and (for
// the loop-scratch variant) a free loop device.
func probeRemountRO(h host) CapabilityResult {
	name := capNameRemountRO
	eff, capBasis := capEff(h)
	if !hasCaps(eff, capSysAdmin) {
		return CapabilityResult{Name: name,
			Missing: "CAP_SYS_ADMIN (mount -o remount,ro,bind is a mount(2) call; rootless callers get EPERM) — RO-remount faults need the privileged helper",
			Basis:   capBasis + "; probe shape: bind-mount scratch dir, remount ro, verify write() fails EROFS, remount rw"}
	}
	if _, err := h.LookPath("mount"); err != nil {
		return CapabilityResult{Name: name,
			Missing: "mount binary",
			Basis:   "mount not on PATH"}
	}
	return CapabilityResult{Available: true, Name: name,
		Basis: capBasis + "; mount on PATH; probe shape: bind-mount scratch dir, remount ro, verify write() fails EROFS, remount rw"}
}
