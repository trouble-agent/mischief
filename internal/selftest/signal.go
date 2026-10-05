package selftest

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/trouble-agent/mischief/internal/backend_signal"
)

// landable is one primitive's executable selftest surface: the harness
// drives exactly these five steps, in order, on a scratch dir the run
// owns. Every step either measures (proof text) or names what it could
// not measure — nothing asserts.
type landable interface {
	// capability answers whether the primitive CAN land on this host.
	// missing=true names the absent piece (a recorded SKIP, AC-11 shape);
	// missing=false proceeds.
	capability() (msg string, missing bool)
	// prepare captures the pre-state snapshot (AC-4's byte-identical
	// compare basis). A zero-length snapshot is refused by the runner
	// (a vacuous compare).
	prepare() ([]byte, error)
	// land runs the landing path and reports the measured outcome: landed
	// with proof text, or the reason it did not land.
	land() landOutcome
	// inverse executes the recorded inverse and reports the measured
	// revert outcome.
	inverse() landOutcome
	// post re-reads the state the prepare snapshot covered.
	post() ([]byte, error)
	// cleanup releases everything the selftest armed (children, servers,
	// fds). Best-effort; runs on every path, including skips and fails.
	cleanup()
}

// landOutcome is the shared measured outcome shape for the land and
// inverse steps.
type landOutcome struct {
	// landed: the measured proof fired.
	landed bool
	// proof is the measured proof text ("" when not landed).
	proof string
	// err names why the step failed ("" when landed).
	err string
}

func ok(proof string) landOutcome { return landOutcome{landed: true, proof: proof} }

func bad(format string, a ...any) landOutcome {
	return landOutcome{landed: false, err: fmt.Sprintf(format, a...)}
}

// fromSignalOutcome adapts backend_signal's Outcome (the sig/shim/
// prlimit/seccomp backends' measured result) onto the harness outcome.
func fromSignalOutcome(out backend_signal.Outcome) landOutcome {
	if out.Landed() {
		return ok(out.LandedProof)
	}
	return bad("%s", out.String())
}

// buildLandable is the production builder: primitive id → its landable on
// the given scratch dir.
//
//   - The five M1-green primitives get live landables (P-001, S-001,
//     N-012, F-009 rootless; R-001 probe-gated at capability()).
//   - Catalog primitives whose actuator cannot run on the L0 scratch
//     harness get a SKIP with the missing piece NAMED (AC-11 shape): the
//     L2/L3 tiers (N-001 netns, C-009/I-002 container lifecycle) and the
//     shim shapes the v0.1 shim does not implement (S-004 short-write,
//     T-001 clock). A skip is recorded, never a pass.
//   - UNKNOWN ids (not in the catalog at all) get a FAIL: a skip would
//     read as "known but unavailable on this host" and inflate the M1
//     exit criterion.
func buildLandable(id, dir string) landable {
	switch id {
	case "P-001":
		return &signalLandable{}
	case "S-001":
		return &shimLandable{dir: dir}
	case "N-012":
		return &proxyLandable{}
	case "F-009":
		return &fileReplaceLandable{dir: dir}
	case "R-001":
		return &scopeLandable{}
	case "S-004":
		return &skipLandable{id: id, missing: "shim short-write mode: the v0.1 shim backend implements full-failure errno rules on the write syscall only (no partial-write injection yet)"}
	case "T-001":
		return &skipLandable{id: id, missing: "shim clock interception: the v0.1 shim backend implements the write syscall only (no clock_gettime/gettimeofday rules yet)"}
	case "N-001":
		return &skipLandable{id: id, missing: netHelperMissing()}
	case "C-009", "I-002":
		return &skipLandable{id: id, missing: dockerActuatorMissing()}
	default:
		return &refusalLandable{id: id}
	}
}

// netHelperMissing names what N-001 (netns+netem, tier L2) lacks on this
// host, measured: the privileged helper when sudo is absent; the L2-scoped
// target when it is present (the L0 scratch harness is unprivileged by
// construction and never gains netns scope).
func netHelperMissing() string {
	if _, err := exec.LookPath("sudo"); err != nil {
		return "sudo (privileged helper): netns/netem requires `sudo ip netns ... tc qdisc ...` (probe/RESULTS.md); unprivileged user+net namespaces cannot create a veth on this host class (EPERM, AppArmor userns restriction)"
	}
	return "an L2-scoped namespace target: the privileged helper is present, but the primitive is tier L2 and the L0 scratch harness is unprivileged by construction — netns+netem faults have no scratch actuator at M1"
}

// dockerActuatorMissing names what C-009/I-002 (tier L3, docker API)
// lack, measured against the docker socket.
func dockerActuatorMissing() string {
	if fi, err := os.Stat("/var/run/docker.sock"); err == nil && fi.Mode()&os.ModeSocket != 0 {
		return "an L3 container actuator: the docker socket is present, but the primitive is tier L3 (container lifecycle) and the L0 scratch harness owns no container at M1 — container faults have no scratch actuator"
	}
	return "docker API (socket /var/run/docker.sock not present or not a socket): container-lifecycle faults are tier L3 and have no scratch actuator at M1 in any case"
}

// skipLandable is the recorded-skip landable: the primitive's actuator
// cannot run on the L0 scratch harness, and the missing piece is NAMED
// (a skip is recorded, never a pass — the AC-19 set stays honest).
type skipLandable struct {
	id      string
	missing string
}

func (s *skipLandable) capability() (string, bool) { return s.missing, true }
func (s *skipLandable) prepare() ([]byte, error) {
	return nil, fmt.Errorf("unreachable: %s skipped at capability", s.id)
}
func (s *skipLandable) land() landOutcome    { return bad("unreachable: skipped at capability") }
func (s *skipLandable) inverse() landOutcome { return bad("unreachable: skipped at capability") }
func (s *skipLandable) post() ([]byte, error) {
	return nil, fmt.Errorf("unreachable: skipped at capability")
}
func (s *skipLandable) cleanup() {}

// refusalLandable FAILS ids with no M1-tier selftest yet (S-004, T-001)
// and unknown ids alike: their honest state at this milestone is "no
// executable selftest exists", which is a named failure of the harness
// request, not a host capability skip.
type refusalLandable struct{ id string }

func (r *refusalLandable) capability() (string, bool) { return "", false }
func (r *refusalLandable) prepare() ([]byte, error) {
	return nil, fmt.Errorf("no M1-tier selftest exists for %s (its actuator lands in a later milestone; the primitive refuses outside L0 until then)", r.id)
}
func (r *refusalLandable) land() landOutcome    { return bad("unreachable: prepare refused") }
func (r *refusalLandable) inverse() landOutcome { return bad("unreachable: prepare refused") }
func (r *refusalLandable) post() ([]byte, error) {
	return nil, fmt.Errorf("unreachable: prepare refused")
}
func (r *refusalLandable) cleanup() {}

// ── P-001: SIGSTOP / SIGCONT ────────────────────────────────────────────────
//
// Land: backend_signal.ApplySignal SIGSTOPs a scratch child and proves
// the /proc state 'T' proof (2 consecutive samples — the descriptor's
// landed_proof.check, measured). Inverse: the fault's own Resume
// (SIGCONT) with the resume proof (state leaves 'T' — the descriptor's
// verify, measured). Pre/post: the child's STABLE scheduling identity
// (comm, ppid, pgrp, session, tty, tpgid — statIdentity), byte-compared:
// insensitive to the state byte the fault owns and to the child's own
// volatile counters, sensitive to everything the fault must restore.

type signalLandable struct {
	cmd   *exec.Cmd
	fault backend_signal.Inverter
}

func (s *signalLandable) capability() (string, bool) {
	return "", false // rootless, no capability: kill+stat of an own child
}

// statIdentity reads /proc/<pid>/stat and extracts the STABLE scheduling
// identity: comm, ppid, pgrp, session, tty_nr, tpgid (stat fields 1-2 and
// 4-8). The volatile fields are excluded on purpose: the state byte is
// the fault's own battleground (proven separately by the land and
// inverse proofs), and the counters from minflt onward move with the
// process's own life — comparing them would measure the child's page
// faults, not the fault's revert.
func statIdentity(pid int) ([]byte, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return nil, err
	}
	open := bytes.IndexByte(b, '(')
	close_ := bytes.LastIndexByte(b, ')')
	if open < 0 || close_ < 0 || close_ < open {
		return nil, fmt.Errorf("/proc/%d/stat: unexpected shape", pid)
	}
	comm := b[open+1 : close_]
	tail := strings.Fields(string(b[close_+1:]))
	if len(tail) < 6 {
		return nil, fmt.Errorf("/proc/%d/stat: tail too short (%d fields)", pid, len(tail))
	}
	// tail[0]=state tail[1]=ppid tail[2]=pgrp tail[3]=session
	// tail[4]=tty_nr tail[5]=tpgid — the identity the SIGSTOP/SIGCONT
	// pair must restore.
	return []byte(fmt.Sprintf("comm=%s ppid=%s pgrp=%s session=%s tty=%s tpgid=%s",
		comm, tail[1], tail[2], tail[3], tail[4], tail[5])), nil
}

func (s *signalLandable) prepare() ([]byte, error) {
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		return nil, fmt.Errorf("spawn scratch child: %v", err)
	}
	s.cmd = child
	pid := child.Process.Pid
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, err := statIdentity(pid)
		if err == nil {
			return b, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("spawned pid %d never became observable in /proc", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (s *signalLandable) land() landOutcome {
	if s.cmd == nil {
		return bad("land: no scratch child (prepare never ran)")
	}
	out, f := backend_signal.ApplySignal("P-001", s.cmd.Process.Pid, backend_signal.SigStop)
	s.fault = f
	return fromSignalOutcome(out)
}

func (s *signalLandable) inverse() landOutcome {
	if s.fault == nil {
		return bad("inverse: no armed signal fault (land never proved)")
	}
	return fromSignalOutcome(s.fault.Resume())
}

func (s *signalLandable) post() ([]byte, error) {
	return statIdentity(s.cmd.Process.Pid)
}

func (s *signalLandable) cleanup() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = s.cmd.Process.Kill()
	_, _ = s.cmd.Process.Wait()
}
