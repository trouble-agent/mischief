package selftest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/trouble-agent/mischief/internal/backend_signal"
)

// ── S-001: LD_PRELOAD shim write-EIO ────────────────────────────────────────
//
// Land: compile the generated anchored shim, arm the rule on a scratch
// target, run the control child (write lands, counter stays zero), run
// the faulted child under the shim env, and prove the landing from the
// shim's OWN counter file (backend_signal.ArmShim/Prove — the descriptor's
// landed_proof.kind shim-counter). Inverse: DetachShim — the fault env is
// removed and the NEXT write to the path is proven to succeed (the
// descriptor's verify). Pre/post: the scratch pair {target, unaffected}
// byte-compared — a failed detach leaves the target empty (the EIO
// emptied it via the writer's own truncate) and the compare drifts.

type shimLandable struct {
	dir    string
	arm    *backend_signal.ShimArm
	env    []string
	victim string
	target string
	other  string
}

func (s *shimLandable) capability() (string, bool) {
	if _, err := exec.LookPath("cc"); err != nil {
		return "no C compiler (cc) on PATH: the LD_PRELOAD shim cannot be built", true
	}
	return "", false
}

// snapshotFiles serialises the named files under dir into one deterministic
// blob: sorted "name=<hexbytes>" lines. Present-but-empty and absent are
// distinct (absent renders as "name=MISSING"), so a restore that deletes
// instead of restoring cannot pass the compare.
func snapshotFiles(dir string, names ...string) ([]byte, error) {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	var out []byte
	for i, n := range sorted {
		if i > 0 {
			out = append(out, '\n')
		}
		out = append(out, n...)
		out = append(out, '=')
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			if os.IsNotExist(err) {
				out = append(out, "MISSING"...)
				continue
			}
			return nil, fmt.Errorf("snapshot %s: %v", n, err)
		}
		out = append(out, fmt.Sprintf("%x", b)...)
	}
	return out, nil
}

func (s *shimLandable) prepare() ([]byte, error) {
	s.target = filepath.Join(s.dir, "selftest-target.tmp")
	s.other = filepath.Join(s.dir, "unaffected.tmp")
	r, err := backend_signal.ParseRule(fmt.Sprintf("write:%s:EIO", s.target))
	if err != nil {
		return nil, fmt.Errorf("rule: %v", err)
	}
	arm, env, err := backend_signal.ArmShim("S-001", r, s.dir)
	if err != nil {
		return nil, fmt.Errorf("arm: %v", err)
	}
	s.arm, s.env = arm, env

	victimSrc := filepath.Join(s.dir, "victim.c")
	if err := os.WriteFile(victimSrc, backend_signal.BuildVictim(s.target, s.other), 0o600); err != nil {
		return nil, fmt.Errorf("victim source: %v", err)
	}
	s.victim = filepath.Join(s.dir, "victim")
	if b, err := exec.Command("cc", "-O2", "-o", s.victim, victimSrc).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("victim build: %v: %s", err, trimOut(b))
	}

	// control run (no fault env): the target must hold the written bytes
	// and the counter must NOT exist — the control pins the discrimination
	// the landed-proof leans on.
	if b, err := exec.Command(s.victim, s.target).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("control run: %v: %s", err, trimOut(b))
	}
	cb, err := os.ReadFile(s.target)
	if err != nil || len(cb) == 0 {
		return nil, fmt.Errorf("control wrote no bytes (got %v, err %v)", cb, err)
	}
	if pl, err := backend_signal.ReadProofFile(arm.ProofPath()); err != nil || pl.Hits != 0 {
		return nil, fmt.Errorf("control produced a proof counter (hits=%d, err %v) — the control must be counter-less", pl.Hits, err)
	}
	return snapshotFiles(s.dir, filepath.Base(s.target), filepath.Base(s.other))
}

func (s *shimLandable) land() landOutcome {
	if s.arm == nil {
		return bad("land: shim never armed (prepare never ran)")
	}
	faulted := exec.Command(s.victim, s.target)
	faulted.Env = append(os.Environ(), s.env...)
	if b, err := faulted.CombinedOutput(); err != nil {
		return bad("faulted run failed: %v: %s", err, trimOut(b))
	}
	res := s.arm.Prove()
	if !res.Landed() {
		return bad("%s", res.String())
	}
	// out-of-band confirmation, filesystem side: the anchored write was
	// really refused — the faulted child truncated the target on open and
	// its write never landed, so the file is empty right now.
	if cb, err := os.ReadFile(s.target); err != nil || len(cb) != 0 {
		return bad("counter fired but the target holds %d bytes — the EIO did not actually block the write", len(cb))
	}
	// the unaffected path must survive the fault (the anchored rule's
	// whole point)
	if ob, err := os.ReadFile(s.other); err != nil || len(ob) == 0 {
		return bad("unaffected path did not survive the fault (got %v, err %v)", ob, err)
	}
	return ok(res.String())
}

func (s *shimLandable) inverse() landOutcome {
	if s.arm == nil || s.victim == "" {
		return bad("inverse: shim never armed (land never proved)")
	}
	return fromSignalOutcome(backend_signal.DetachShim("S-001", s.victim, s.target))
}

func (s *shimLandable) post() ([]byte, error) {
	return snapshotFiles(s.dir, filepath.Base(s.target), filepath.Base(s.other))
}

func (s *shimLandable) cleanup() {}

func trimOut(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
