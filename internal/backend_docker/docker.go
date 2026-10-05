// Package backend_docker is SPEC-09's docker node-fault backend: container
// pause, kill and memory-squeeze/OOM as NODE-level faults, delivered through
// the docker CLI against an L0 scratch container the run created itself.
//
// Design authority: docs/SPEC-PLAN.md (SPEC-09 backends: nodes & cloud) and
// docs/FAULT-CATALOG.md — the docker-plane expressions of the freeze (P-013),
// container-kill (P-008: `docker inspect` State.Running=false, exit 137) and
// memory-squeeze (R-001) shapes. The catalog corpus itself is NOT extended
// (the corpus is the strictly-invalid-by-design read-only 10-file set); these
// primitives are documented here and carry backend-scoped ids
// ("docker:pause", "docker:kill", "docker:oom") that the selftest harness
// covers like any catalog id.
//
// The landed-proof contract (AC-3, the backend_signal precedent): the docker
// command's exit code is necessary but never sufficient — landing is proven
// by an INDEPENDENT read-back of `docker inspect` state (Paused=true for
// pause; Running=false AND ExitCode=137 for kill; State.OOMKilled=true AND
// Running=false for oom; HostConfig.Memory read-back for the squeeze legs).
// A receipt without the observed state is a no_op that names what did not
// land.
//
// The write-ahead inverse (the internal/reverter pattern, MSF-005): before a
// fault may land, the hold — fault, DECLARATIVE inverse, DECLARATIVE check,
// restore parameters — is appended to a JSONL journal and fsynced; Land
// refuses when the write-ahead line is not durable, so the inverse always
// predates the fault. The inverse kinds (docker-unpause, docker-start,
// docker-memory-restore) are executable from JOURNAL BYTES ALONE by a
// detached process (ExecInverseFromJournal) — no closures, no back-references
// into the arming process. Reverts append a measured revert_proof record
// (reverted | revert_failed); a revert whose check fails never records
// "reverted".
//
// Safety floor (the L0 scratch contract): every mutating verb refuses a
// container name that does not carry the mischief-l0- prefix — this package
// cannot pause, kill, update, start or remove anything it did not name. The
// default image policy never pulls: the selftest probes LOCAL images only and
// skips (naming the missing piece) when none is present.
//
// NOT-LIST (what SPEC-09's docker backend does not promise here):
//
//   - No docker daemon library: the CLI is the ABI (docker 29.x, cgroup v2
//     on this host); everything goes through one exec seam tests inject.
//   - No restart policies, crash loops (P-014) or rolling replace (C-009):
//     v0.1 carries the three node faults the SPEC-09 row names.
//   - No journal daemon: the write-ahead journal is a file the caller owns
//     (a run dir); TTL ownership and boot reconcile stay in
//     internal/reverter, which this package does not duplicate.
//
// ch:trace row=MSF-010 spec=docs/SPEC-PLAN.md#SPEC-09 evidence=internal/backend_docker/ witness=none:l0-scratch-containers-only
package backend_docker

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ScratchNamePrefix is the ONLY container name prefix this package will
// fault: the L0 scratch contract. A mutating verb on any other name refuses.
const ScratchNamePrefix = "mischief-l0-"

// Runner is the exec seam: one docker CLI invocation. The production runner
// shells out to the docker binary; tests inject fakes. It returns the
// command's stdout, stderr and error — the caller never sees the process.
type Runner func(args ...string) (string, string, error)

// CLI is the docker facade: one runner plus the probed daemon facts.
type CLI struct {
	run Runner
	// Binary is the docker binary path the production runner resolved ("" for
	// injected runners).
	Binary string
}

// NewCLI resolves the docker binary on PATH. An absent binary is a named
// capability refusal (AC-11 shape), not a panic.
func NewCLI() (*CLI, error) {
	bin, err := exec.LookPath("docker")
	if err != nil {
		return nil, &CapabilityError{Missing: "docker (binary not on PATH)",
			Detail: "docker node faults drive the docker CLI; install docker or add it to PATH"}
	}
	c := &CLI{Binary: bin}
	c.run = func(args ...string) (string, string, error) {
		cmd := exec.Command(bin, args...)
		var out, errBuf strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &errBuf
		if err := cmd.Run(); err != nil {
			return out.String(), errBuf.String(), err
		}
		return out.String(), errBuf.String(), nil
	}
	return c, nil
}

// NewCLIWithRunner builds a CLI over an injected runner (tests).
func NewCLIWithRunner(r Runner) *CLI { return &CLI{run: r} }

// CapabilityError is the AC-11 refusal: what is missing, named.
type CapabilityError struct {
	Missing string
	Detail  string
}

func (e *CapabilityError) Error() string {
	return fmt.Sprintf("backend_docker: capability_unavailable: missing %s: %s", e.Missing, e.Detail)
}

// ErrNotScratch is the safety-floor refusal: the named container does not
// carry the L0 scratch prefix, so this package will not touch it.
var ErrNotScratch = errors.New("backend_docker: refusing non-scratch container (L0 contract: only " + ScratchNamePrefix + "* names may be faulted)")

// CheckScratch enforces the name prefix on every mutating path.
func CheckScratch(name string) error {
	if !strings.HasPrefix(name, ScratchNamePrefix) || name == ScratchNamePrefix {
		return fmt.Errorf("%w: got %q", ErrNotScratch, name)
	}
	return nil
}

// MintScratchName derives a unique L0 scratch container name (pid + nanos —
// the selftest harness's token shape).
func MintScratchName() string {
	return fmt.Sprintf("%s%d-%d", ScratchNamePrefix, os.Getpid(), time.Now().UnixNano())
}

// Probe measures the daemon's reachability and version (the capability
// read the selftest's capability() gate consumes). An unreachable daemon
// names itself; fail closed — an unreadable daemon never reads as capable.
func (c *CLI) Probe() (string, error) {
	out, _, err := c.run("version", "--format", "{{.Server.Version}}")
	if err != nil {
		return "", &CapabilityError{Missing: "docker daemon (unreachable)",
			Detail: "docker version failed; the daemon may be down or the socket inaccessible"}
	}
	return strings.TrimSpace(out), nil
}

// ImagePresent reports whether an image exists LOCALLY (docker image
// inspect). It never pulls: the selftest's no-network contract.
func (c *CLI) ImagePresent(image string) bool {
	_, _, err := c.run("image", "inspect", image)
	return err == nil
}

// InspectState is the slice of `docker inspect` the landed-proof contract
// reads. Field tags match the daemon's JSON exactly.
type InspectState struct {
	Name            string `json:"Name"`
	Running         bool   `json:"-"`
	Restarting      bool   `json:"-"`
	Paused          bool   `json:"-"`
	OOMKilled       bool   `json:"-"`
	Dead            bool   `json:"-"`
	ExitCode        int    `json:"-"`
	RestartCount    int    `json:"-"`
	StartedAt       string `json:"-"`
	MemoryBytes     int64  `json:"-"`
	MemorySwapBytes int64  `json:"-"`
	Image           string `json:"-"`
	raw             json.RawMessage
}

// dockerInspect is the on-wire shape subset (State nested, HostConfig flat).
type dockerInspect struct {
	Name  string `json:"Name"`
	Image string `json:"Image"`
	State struct {
		Running      bool   `json:"Running"`
		Restarting   bool   `json:"Restarting"`
		Paused       bool   `json:"Paused"`
		OOMKilled    bool   `json:"OOMKilled"`
		Dead         bool   `json:"Dead"`
		ExitCode     int    `json:"ExitCode"`
		RestartCount int    `json:"RestartCount"`
		StartedAt    string `json:"StartedAt"`
	} `json:"State"`
	HostConfig struct {
		Memory     int64 `json:"Memory"`
		MemorySwap int64 `json:"MemorySwap"`
	} `json:"HostConfig"`
}

// Snapshot reads one container's state and renders the byte-comparable
// pre/post snapshot line (the volatile StartedAt is excluded on purpose —
// the fault's own battleground; comparing it would measure the restart, not
// the revert).
func (c *CLI) Snapshot(name string) ([]byte, error) {
	st, err := c.Inspect(name)
	if err != nil {
		return nil, err
	}
	return st.snapshotLine(), nil
}

func (s *InspectState) snapshotLine() []byte {
	// The snapshot covers what the INVERSE must restore (image, running,
	// paused, oom, memory) and excludes what the daemon owns: RestartCount,
	// ExitCode of a dead phase and StartedAt (the fault's own battleground —
	// comparing those would measure the restart, not the revert).
	return []byte(fmt.Sprintf("container=%s image=%s running=%t paused=%t memory=%d",
		s.Name, s.Image, s.Running, s.Paused, s.MemoryBytes))
}

// Inspect reads and parses one container's state. A missing container is a
// named error (the caller decides whether that is the expected state).
func (c *CLI) Inspect(name string) (*InspectState, error) {
	if err := CheckScratch(name); err != nil {
		return nil, err
	}
	out, errOut, err := c.run("inspect", name)
	if err != nil {
		return nil, fmt.Errorf("docker inspect %s: %v: %s", name, err, oneLine(errOut))
	}
	var arr []dockerInspect
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &arr); err != nil {
		return nil, fmt.Errorf("docker inspect %s: parse: %v (raw: %s)", name, err, oneLine(out))
	}
	if len(arr) != 1 {
		return nil, fmt.Errorf("docker inspect %s: %d objects returned, want 1", name, len(arr))
	}
	d := arr[0]
	return &InspectState{
		Name: strings.TrimPrefix(d.Name, "/"), Image: d.Image,
		Running: d.State.Running, Restarting: d.State.Restarting, Paused: d.State.Paused,
		OOMKilled: d.State.OOMKilled, Dead: d.State.Dead,
		ExitCode: d.State.ExitCode, RestartCount: d.State.RestartCount,
		StartedAt:   d.State.StartedAt,
		MemoryBytes: d.HostConfig.Memory, MemorySwapBytes: d.HostConfig.MemorySwap,
	}, nil
}

// RunScratch creates AND starts an L0 scratch container running cmd under
// the given memory cap (bytes; 0 = docker default). The caller owns the
// container; RemoveScratch is its inverse.
func (c *CLI) RunScratch(name, image string, memoryBytes int64, cmd []string) error {
	if err := CheckScratch(name); err != nil {
		return err
	}
	args := []string{"run", "-d", "--name", name}
	if memoryBytes > 0 {
		args = append(args, "--memory", strconv.FormatInt(memoryBytes, 10),
			"--memory-swap", strconv.FormatInt(memoryBytes, 10))
	}
	args = append(args, image)
	args = append(args, cmd...)
	out, errOut, err := c.run(args...)
	if err != nil {
		return fmt.Errorf("docker run (scratch %s): %v: %s", name, err, oneLine(errOut))
	}
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf("docker run (scratch %s): no container id returned", name)
	}
	return nil
}

// RemoveScratch force-removes an L0 scratch container (cleanup path; the
// prefix check applies).
func (c *CLI) RemoveScratch(name string) error {
	if err := CheckScratch(name); err != nil {
		return err
	}
	_, errOut, err := c.run("rm", "-f", name)
	if err != nil {
		return fmt.Errorf("docker rm -f %s: %v: %s", name, err, oneLine(errOut))
	}
	return nil
}

// Outcome is this package's measured-landing result (the backend_signal
// Outcome shape, local to the package: the backend families do not import
// each other). Landed means the independent inspect read-back fired.
type Outcome struct {
	Primitive   string
	Backend     string
	Landed      bool
	LandedProof string
	NoOpReason  string
	Err         error
}

func (o Outcome) OK() bool { return o.Landed && o.Err == nil }

func landed(primitive, proof string) Outcome {
	return Outcome{Primitive: primitive, Backend: "docker", Landed: true, LandedProof: proof}
}

func noOp(primitive, reason string) Outcome {
	if reason == "" {
		reason = "no_op graded without a stated reason — contract violation (AC-3)"
	}
	return Outcome{Primitive: primitive, Backend: "docker", NoOpReason: reason}
}

func failed(primitive string, err error) Outcome {
	return Outcome{Primitive: primitive, Backend: "docker", Err: err,
		NoOpReason: "refused before landing: " + err.Error()}
}

// poll windows: generous under host load, bounded so a wedged daemon cannot
// hang a run. Vars, not consts: the unit tests shorten them (a fake host
// proves its point in milliseconds; waiting a real 90s window to grade a
// no_op would make the suite load-sensitive).
var (
	proofWindow    = 60 * time.Second
	proofInterval  = 250 * time.Millisecond
	oomKillWindow  = 90 * time.Second
	startupWindow  = 30 * time.Second
	minOOMCapBytes = int64(8 << 20) // never arm an OOM cap below 8 MiB (kernel floor)
)

// waitFor polls cond until it holds or the window expires.
func waitFor(window, interval time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(window)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(interval)
	}
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// snapshotLine renders the inspect state as the proof tail (the backend's
// snapshot shape, surfaced for the inverse proof line).
func (s *InspectState) SnapshotText() string {
	return string(s.snapshotLine())
}

// snapshotLine is the byte-comparable pre/post snapshot (see Snapshot).

// MintScratchName's name shape (unit-tested).
var scratchNameRe = regexp.MustCompile(`^mischief-l0-\d+-\d+$`)

// UpdateScratchMemory applies a memory limit to an L0 scratch container
// via `docker update`. The cgroup-v2 update path is environment-hostile:
// on some runners/drivers (notably GitHub-hosted ubuntu with cgroup
// namespaces) runc refuses with an openat2 error on the container scope.
// The selftests grade that honest refusal as a capability skip, not a
// failure (the primitive is unavailable on that host, not broken).
func (c *CLI) UpdateScratchMemory(name string, memoryBytes int64) error {
	if err := CheckScratch(name); err != nil {
		return err
	}
	_, errOut, err := c.run("update", "--memory", strconv.FormatInt(memoryBytes, 10),
		"--memory-swap", strconv.FormatInt(memoryBytes, 10), name)
	if err != nil {
		return fmt.Errorf("docker update --memory %d %s: %v: %s", memoryBytes, name, err, oneLine(errOut))
	}
	return nil
}

// CgroupUpdateUnavailable reports whether an error from UpdateScratchMemory
// is the known runc/cgroup-v2 driver refusal (an environment capability
// boundary, not a code defect). Callers convert true into an
// AC-11-shaped capability skip naming the missing piece.
func CgroupUpdateUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "cgroup.controllers") ||
		strings.Contains(msg, "runc did not terminate successfully")
}
