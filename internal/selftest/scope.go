package selftest

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/trouble-agent/mischief/internal/backend_resource"
)

// ── R-001: systemd-run user scope (MemoryMax squeeze) ───────────────────────
//
// Land: start the scope via backend_resource's resolved primitive
// (transient user unit, declared limits) and require its read-back proof
// (the systemd properties — MemoryMax=67108864, TasksMax=32,
// CPUQuotaPerSecUSec=100ms — read back from systemd, AC-3 measured).
// Inverse: Release — the unit stop with its own VERIFIED proof (LoadState
// not-found / ActiveState inactive), plus our own out-of-band query.
// Pre/post: THE FAULTED OBJECT's systemd state ("unit <name>
// LoadState=<x> ActiveState=<y>"), captured by NAME before the landing
// (when it does not exist) and re-read after the release — byte-identical
// when the scope is gone, drifted when it leaked.
//
// The snapshot is object-scoped on purpose: a global "msf-run-* active
// unit count" drifts when anything else on the host creates units in the
// same namespace (a parallel go-test package does exactly that), and that
// drift is not this fault's revert.

type scopeLandable struct {
	unit     string
	proof    backend_resource.Proof
	release  backend_resource.Release
	unitName string
}

func (s *scopeLandable) capability() (string, bool) {
	c, err := backend_resource.Resolve("systemd-run user scope").Probe()
	if err != nil {
		return fmt.Sprintf("scope probe error: %v (fail-closed: unreadable never reads as capable)", err), true
	}
	if !c.Available {
		return c.Missing, true
	}
	return "", false
}

// scopePrimitive resolves the typed scope primitive (the named-unit
// variant's carrier). The name is fixed; a mismatch is a wiring bug.
func scopePrimitive() (backend_resource.SystemdScope, bool) {
	return backend_resource.ResolveScope("systemd-run user scope")
}

// unitStateLine queries one unit's load/active state from the user
// manager. A not-found unit answers "not-found" (the pre-land state).
func unitStateLine(unit string) ([]byte, error) {
	out, err := exec.Command("systemctl", "--user", "show", unit,
		"-p", "LoadState", "-p", "ActiveState", "--no-pager").CombinedOutput()
	if err != nil {
		// a host without a user manager never reaches the land step (the
		// capability gate skips first); an error here is a real failure
		return nil, fmt.Errorf("unit state query: %v: %s", err, trimOut(out))
	}
	return []byte("unit " + unit + " " + strings.Join(strings.Fields(string(out)), " ")), nil
}

func (s *scopeLandable) prepare() ([]byte, error) {
	// the unit name is minted NOW so the pre-state capture names the
	// exact object the fault will create
	s.unitName = fmt.Sprintf("msf-selftest-%d-%s", scratchPID(), scratchNanos())
	pre, err := unitStateLine(s.unitName)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(string(pre), "not-found") {
		return nil, fmt.Errorf("pre-land unit %s already exists (%s) — the scratch namespace is not clean", s.unitName, pre)
	}
	return pre, nil
}

func (s *scopeLandable) land() landOutcome {
	sp, resolved := scopePrimitive()
	if !resolved {
		return bad("scope primitive unresolved (wiring bug)")
	}
	proof, release, err := sp.InjectLimitsNamed(
		backend_resource.ScopeLimits{
			MemoryMaxBytes:  64 << 20,
			CPUQuotaPercent: 10,
			TasksMax:        32,
			Cmd:             []string{"/bin/sleep", "30"},
		}, s.unitName)
	if err != nil {
		return bad("scope inject: %s", errText(err))
	}
	s.proof, s.release = proof, release
	if !proof.Landed || !proof.ReadbackOk {
		_, _ = release()
		return bad("landed-proof did not fire (landed=%t readback=%t): %s", proof.Landed, proof.ReadbackOk, proof.Property)
	}
	joined := strings.Join(proof.Evidence, "; ")
	for _, want := range []string{"MemoryMax=67108864", "TasksMax=32", "CPUQuotaPerSecUSec=100ms"} {
		if !strings.Contains(joined, want) {
			return bad("scope read-back lacks %s (evidence: %s)", want, joined)
		}
	}
	return ok("scope read-back: " + proof.Property + " [" + joined + "]")
}

func (s *scopeLandable) inverse() landOutcome {
	if s.release == nil {
		return bad("inverse: no scope release (land never proved)")
	}
	msg, err := s.release()
	if err != nil {
		return bad("revert proof failed: %s", errText(err))
	}
	// AC-4, the out-of-band half: the faulted object's state after the
	// release must equal its pre-land state (not-found) — the release's
	// own verified text plus this independent query.
	post, err := unitStateLine(s.unitName)
	if err != nil {
		return bad("post-release unit query: %s", errText(err))
	}
	if !strings.Contains(string(post), "not-found") && !strings.Contains(string(post), "inactive") {
		return bad("revert issued but not proven: unit %s still %s after release", s.unitName, post)
	}
	return ok("scope release verified: " + msg + "; post-release object state: " + string(post))
}

func (s *scopeLandable) post() ([]byte, error) {
	return unitStateLine(s.unitName)
}

func (s *scopeLandable) cleanup() {
	if s.release != nil {
		_, _ = s.release()
	}
}

// errText renders an error's message ("" for nil) without importing
// backend_resource's unexported helper.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// scratchPID/scratchNanos name the transient unit uniquely (pid + nanos —
// backend_resource's own token shape, kept local to this package).
func scratchPID() int      { return os.Getpid() }
func scratchNanos() string { return strconv.FormatInt(time.Now().UnixNano(), 10) }
