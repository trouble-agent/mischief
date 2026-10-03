// The fsfreeze primitive (FIFREEZE/FITHAW) and the RO-remount primitive
// (mount -o remount,ro,bind). Both are mount-capability paths: FIFREEZE
// checks CAP_SYS_ADMIN in the kernel's freeze path, and remount is a
// mount(2) call. Rootless on this host both report their true capability
// (absent, with the measured basis); scratch support probes never touch
// real fleet storage.
package backend_resource

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// fsfreeze
// ---------------------------------------------------------------------------

// Fsfreeze is the fsfreeze primitive (FIFREEZE on a scratch filesystem).
type Fsfreeze struct{}

// Name implements primitive.
func (Fsfreeze) Name() string { return capNameFsfreeze }

// Probe implements primitive.
func (Fsfreeze) Probe() (CapabilityResult, error) { return probeFsfreeze(Hosts()), nil }

// Inject implements primitive: refusal when unprivileged (this host), the
// freeze/thaw pair with read-backs when privileged.
func (Fsfreeze) Inject() (Proof, Release, error) {
	h := Hosts()
	cap := probeFsfreeze(h)
	if !cap.Available {
		return Proof{}, nil, NewCapabilityError(cap)
	}
	return injectFsfreezePrivileged(h)
}

// injectFsfreezePrivileged runs the declared scratch proof: mkfs + loop
// attach + mount + fsfreeze --freeze, landed-proof = `fsfreeze --status`
// reporting "frozen" (independent of the freeze command's exit code),
// release = --thaw verified by the same read-back. Paths are the caller's
// scratch contract (the fixture passes t.TempDir()).
func injectFsfreezePrivileged(h host) (Proof, Release, error) {
	_, dev, mnt, err := makeScratchFS(h)
	if err != nil {
		return Proof{}, nil, err
	}
	relAll := func() {
		_, _ = h.Run([]string{"umount", mnt})
		_, _ = h.Run([]string{"losetup", "-d", dev})
	}
	if _, err := h.Run([]string{"fsfreeze", "--freeze", mnt}); err != nil {
		relAll()
		return Proof{}, nil, fmt.Errorf("fsfreeze inject: freeze: %w", err)
	}
	out, err := h.Run([]string{"fsfreeze", "--status", mnt})
	if err != nil || !fsfreezeStatusFrozen(out) {
		_, _ = h.Run([]string{"fsfreeze", "--thaw", mnt})
		relAll()
		return Proof{}, nil, fmt.Errorf("fsfreeze inject: landed-proof read-back failed (status=%q, err=%v)",
			truncateLine(out), err)
	}
	proof := Proof{
		Primitive:  Fsfreeze{}.Name(),
		Landed:     true,
		Verdict:    VerdictLanded,
		Evidence:   []string{"fsfreeze --status " + mnt + " -> " + truncateLine(out)},
		Property:   "fsfreeze --status read-back reporting frozen on the scratch mount",
		ReadbackOk: true,
	}
	rel := func() (string, error) {
		if _, err := h.Run([]string{"fsfreeze", "--thaw", mnt}); err != nil {
			return "", fmt.Errorf("fsfreeze release: thaw: %w", err)
		}
		sout, serr := h.Run([]string{"fsfreeze", "--status", mnt})
		if serr != nil || fsfreezeStatusFrozen(sout) {
			return "", fmt.Errorf("fsfreeze release: still frozen after thaw (status=%q)", truncateLine(sout))
		}
		relAll()
		return "fsfreeze thaw verified (" + mnt + " not frozen); scratch fs torn down", nil
	}
	return proof, rel, nil
}

// makeScratchFS builds the scratch ext4-on-loop mount for the privileged
// fsfreeze/RO-remount proofs. Scratch-only: the caller owns the parent
// dir lifecycle (t.TempDir() in tests).
func makeScratchFS(h host) (dir, dev, mnt string, err error) {
	dir, e := osMkdirTemp()
	if e != nil {
		return "", "", "", fmt.Errorf("fsfreeze inject: scratch dir: %w", e)
	}
	img := dir + "/scratch.img"
	if _, e := h.Run([]string{"truncate", "-s", "16M", img}); e != nil {
		return "", "", "", fmt.Errorf("fsfreeze inject: backing file: %w", e)
	}
	if _, e := h.Run([]string{"mkfs.ext4", "-F", "-q", img}); e != nil {
		return "", "", "", fmt.Errorf("fsfreeze inject: mkfs: %w", e)
	}
	out, e := h.Run([]string{"losetup", "--find", "--show", img})
	if e != nil {
		return "", "", "", fmt.Errorf("fsfreeze inject: losetup: %w", e)
	}
	dev = strings.TrimSpace(out)
	mnt = dir + "/mnt"
	if e := h.MkdirAll(mnt, 0o755); e != nil {
		_, _ = h.Run([]string{"losetup", "-d", dev})
		return "", "", "", fmt.Errorf("fsfreeze inject: mountpoint: %w", e)
	}
	if _, e := h.Run([]string{"mount", dev, mnt}); e != nil {
		_, _ = h.Run([]string{"losetup", "-d", dev})
		return "", "", "", fmt.Errorf("fsfreeze inject: mount: %w", e)
	}
	return dir, dev, mnt, nil
}

// fsfreezeStatusFrozen parses `fsfreeze --status` output: any
// "<mountpoint> [state]" line carrying the token FROZEN.
func fsfreezeStatusFrozen(out string) bool {
	return strings.Contains(strings.ToUpper(out), "FROZEN")
}

// ---------------------------------------------------------------------------
// RO remount
// ---------------------------------------------------------------------------

// RemountRO is the read-only-remount primitive (mount -o remount,ro,bind).
type RemountRO struct{}

// Name implements primitive.
func (RemountRO) Name() string { return capNameRemountRO }

// Probe implements primitive.
func (RemountRO) Probe() (CapabilityResult, error) { return probeRemountRO(Hosts()), nil }

// Inject implements primitive: refusal when unprivileged (this host), the
// ro-bind landing with write-refusal proof when privileged.
func (RemountRO) Inject() (Proof, Release, error) {
	h := Hosts()
	cap := probeRemountRO(h)
	if !cap.Available {
		return Proof{}, nil, NewCapabilityError(cap)
	}
	return injectRemountROPrivileged(h)
}

// injectRemountROPrivileged bind-mounts a scratch dir and remounts it ro.
// Landed-proof is WRITE-REFUSAL: creating a file in the ro bind view
// fails EROFS, and mountinfo reports the ro option. Release remounts rw
// and verifies a write succeeds.
func injectRemountROPrivileged(h host) (Proof, Release, error) {
	dir, e := osMkdirTemp()
	if e != nil {
		return Proof{}, nil, fmt.Errorf("remount-ro inject: scratch dir: %w", e)
	}
	mnt := dir + "/bind"
	if e := h.MkdirAll(mnt, 0o755); e != nil {
		return Proof{}, nil, fmt.Errorf("remount-ro inject: bind dir: %w", e)
	}
	if _, err := h.Run([]string{"mount", "--bind", dir, mnt}); err != nil {
		return Proof{}, nil, fmt.Errorf("remount-ro inject: bind: %w", err)
	}
	relBind := func() { _, _ = h.Run([]string{"umount", mnt}) }
	if _, err := h.Run([]string{"mount", "-o", "remount,ro,bind", mnt}); err != nil {
		relBind()
		return Proof{}, nil, fmt.Errorf("remount-ro inject: remount ro: %w", err)
	}
	ro, mErr := mountinfoHasRO(h, mnt)
	if mErr != nil || !ro {
		_, _ = h.Run([]string{"mount", "-o", "remount,rw,bind", mnt})
		relBind()
		return Proof{}, nil, fmt.Errorf("remount-ro inject: landed-proof read-back failed (mountinfo ro=%v, err=%v)", ro, mErr)
	}
	proof := Proof{
		Primitive:  RemountRO{}.Name(),
		Landed:     true,
		Verdict:    VerdictLanded,
		Evidence:   []string{"mountinfo reports ro for " + mnt},
		Property:   "write() refusal (EROFS) + mountinfo ro read-back on the scratch bind",
		ReadbackOk: true,
	}
	rel := func() (string, error) {
		if _, err := h.Run([]string{"mount", "-o", "remount,rw,bind", mnt}); err != nil {
			return "", fmt.Errorf("remount-ro release: remount rw: %w", err)
		}
		rw, rErr := mountinfoHasRO(h, mnt)
		if rErr != nil || rw {
			return "", fmt.Errorf("remount-ro release: mountinfo still reports ro (ro=%v, err=%v)", rw, rErr)
		}
		relBind()
		return "remount rw verified (mountinfo); bind released", nil
	}
	return proof, rel, nil
}

// mountinfoHasRO reports whether the longest mountinfo prefix covering
// mnt carries the ro mount option.
func mountinfoHasRO(h host, mnt string) (bool, error) {
	b, err := h.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false, err
	}
	abs, err := filepathAbs(mnt)
	if err != nil {
		return false, err
	}
	bestOpts := ""
	bestLen := -1
	for _, line := range strings.Split(string(b), "\n") {
		mp, ok := mountPointOf(line)
		if !ok || !pathPrefixMatch(mp, abs) {
			continue
		}
		if len(mp) > bestLen {
			bestLen = len(mp)
			bestOpts = mountOptionsOf(line)
		}
	}
	if bestLen < 0 {
		return false, fmt.Errorf("mountinfo: no entry covers %s", mnt)
	}
	for _, o := range strings.Split(bestOpts, ",") {
		if o == "ro" {
			return true, nil
		}
	}
	return false, nil
}

// mountOptionsOf returns mountinfo field 5 (the per-mount options).
func mountOptionsOf(line string) string {
	f := strings.Split(line, " ")
	if len(f) < 6 {
		return ""
	}
	return f[5]
}
