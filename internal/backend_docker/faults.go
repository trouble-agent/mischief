package backend_docker

import (
	"fmt"
	"strconv"
	"time"
)

// faults.go — the three SPEC-09 docker node faults. Every fault follows the
// same measured contract: arm through the CLI seam, then prove the landing
// with an independent `docker inspect` read-back (AC-3 — the CLI's exit code
// is a receipt, never the proof), and expose a declarative inverse whose
// re-execution is proven by the same read-back in the reverse direction
// (AC-4).
//
//	pause  — docker pause: State.Paused=true for the landing; docker unpause
//	         for the inverse (Paused=false, Running stays true).
//	kill   — docker kill (SIGKILL): Running=false AND ExitCode=137;
//	         docker start for the inverse (Running=true, exit back to 0).
//	oom    — docker update --memory squeeze: the cgroup v2 limit read back
//	         through HostConfig.Memory, then OOMKilled=true AND Running=false
//	         after the workload allocates past the cap; docker start +
//	         docker update --memory <original> for the inverse.

// Pause pauses an L0 scratch container and proves the landing: the container
// is observed Paused with Running still true (a paused container is not
// dead — that distinction is the fault). The returned Fault is the armed
// hold carrying the write-ahead inverse.
func (c *CLI) Pause(name string) (Outcome, *Fault) {
	if err := CheckScratch(name); err != nil {
		return failed("docker:pause", err), nil
	}
	st, err := c.Inspect(name)
	if err != nil {
		return failed("docker:pause", err), nil
	}
	if st.Paused {
		return noOp("docker:pause", fmt.Sprintf("container %s is already paused — nothing to land", name)), nil
	}
	if !st.Running {
		return noOp("docker:pause", fmt.Sprintf("container %s is not running (exit=%d) — a stopped container cannot be paused", name, st.ExitCode)), nil
	}
	_, errOut, err := c.run("pause", name)
	if err != nil {
		return failed("docker:pause", fmt.Errorf("docker pause %s: %v: %s", name, err, oneLine(errOut))), nil
	}
	// Landed-proof: the INDEPENDENT inspect read-back (Paused=true,
	// Running=true), not the pause receipt.
	if !waitFor(proofWindow, proofInterval, func() bool {
		s, err := c.Inspect(name)
		return err == nil && s.Paused && s.Running
	}) {
		s, _ := c.Inspect(name)
		return noOp("docker:pause", fmt.Sprintf("docker pause receipt ok but %s never observed Paused within %s (running=%t paused=%t) — the anti-gaming arm",
			name, proofWindow, s != nil && s.Running, s != nil && s.Paused)), nil
	}
	f := &Fault{CLI: c, Primitive: "docker:pause", Container: name, Kind: KindPause}
	return landed("docker:pause", fmt.Sprintf("inspect: %s Paused=true Running=true (receipt: docker pause=0)", name)), f
}

// Kill SIGKILLs an L0 scratch container's init process and proves the
// landing: Running=false AND ExitCode=137 (the catalog's P-008 proof: "State
// code 137/143"). OOMKilled must stay false — an OOM kill is a DIFFERENT
// fault, and a kill receipt plus an OOM flag would be a misattributed
// landing.
func (c *CLI) Kill(name string) (Outcome, *Fault) {
	if err := CheckScratch(name); err != nil {
		return failed("docker:kill", err), nil
	}
	st, err := c.Inspect(name)
	if err != nil {
		return failed("docker:kill", err), nil
	}
	if !st.Running {
		return noOp("docker:kill", fmt.Sprintf("container %s is not running (exit=%d) — nothing to kill", name, st.ExitCode)), nil
	}
	_, errOut, err := c.run("kill", name)
	if err != nil {
		return failed("docker:kill", fmt.Errorf("docker kill %s: %v: %s", name, err, oneLine(errOut))), nil
	}
	if !waitFor(proofWindow, proofInterval, func() bool {
		s, err := c.Inspect(name)
		return err == nil && !s.Running && s.ExitCode == 137 && !s.OOMKilled
	}) {
		s, _ := c.Inspect(name)
		if s != nil && s.OOMKilled {
			return noOp("docker:kill", fmt.Sprintf("container %s died by OOM (OOMKilled=true), not by docker kill — the observed exit is not this fault's landing", name)), nil
		}
		return noOp("docker:kill", fmt.Sprintf("docker kill receipt ok but %s never observed Running=false exit=137 within %s — the anti-gaming arm", name, proofWindow)), nil
	}
	f := &Fault{CLI: c, Primitive: "docker:kill", Container: name, Kind: KindKill}
	return landed("docker:kill", fmt.Sprintf("inspect: %s Running=false ExitCode=137 (receipt: docker kill=0)", name)), f
}

// OOM squeezes an L0 scratch container's memory limit to capBytes via
// `docker update` and proves the landing TWICE (both halves measured):
//
//  1. the squeeze itself — HostConfig.Memory read back == capBytes (the
//     cgroup v2 limit actually moved); and
//  2. the kill — after the container's workload allocates past the cap
//     (the scratch command must allocate on its own; the fault applies the
//     LIMIT, not the allocation), State.OOMKilled=true AND Running=false.
//
// A squeeze without the observed OOM death is a no_op that names what was
// missing — a memory LIMIT is a configuration change, not a landed fault.
func (c *CLI) OOM(name string, capBytes int64) (Outcome, *Fault) {
	primitive := "docker:oom"
	if err := CheckScratch(name); err != nil {
		return failed(primitive, err), nil
	}
	if capBytes < minOOMCapBytes {
		return failed(primitive, fmt.Errorf("memory cap %d below the %d-byte floor — sub-floor caps are kernel-unrepresentable, not faults", capBytes, minOOMCapBytes)), nil
	}
	st, err := c.Inspect(name)
	if err != nil {
		return failed(primitive, err), nil
	}
	if !st.Running {
		return noOp(primitive, fmt.Sprintf("container %s is not running (exit=%d) — squeeze a live container", name, st.ExitCode)), nil
	}
	if st.MemoryBytes > 0 && st.MemoryBytes <= capBytes {
		return noOp(primitive, fmt.Sprintf("container %s memory limit is already %d <= cap %d — the squeeze would land nothing", name, st.MemoryBytes, capBytes)), nil
	}
	if _, errOut, err := c.run("update", "--memory", strconv.FormatInt(capBytes, 10),
		"--memory-swap", strconv.FormatInt(capBytes, 10), name); err != nil {
		return failed(primitive, fmt.Errorf("docker update --memory %d %s: %v: %s", capBytes, name, err, oneLine(errOut))), nil
	}
	// Landing half 1: the limit really moved (read back, not receipt).
	if !waitFor(proofWindow, proofInterval, func() bool {
		s, err := c.Inspect(name)
		return err == nil && s.MemoryBytes == capBytes
	}) {
		s, _ := c.Inspect(name)
		mem := int64(-1)
		if s != nil {
			mem = s.MemoryBytes
		}
		return noOp(primitive, fmt.Sprintf("docker update receipt ok but %s never observed Memory=%d within %s (last %d) — the anti-gaming arm", name, capBytes, proofWindow, mem)), nil
	}
	// Landing half 2: the OOM death (the workload allocates past the cap).
	if !waitFor(oomKillWindow, proofInterval, func() bool {
		s, err := c.Inspect(name)
		return err == nil && !s.Running && s.OOMKilled
	}) {
		s, _ := c.Inspect(name)
		if s != nil && !s.Running && !s.OOMKilled {
			return noOp(primitive, fmt.Sprintf("container %s exited (code=%d) but NOT by OOM (OOMKilled=false) — the observed exit is not this fault's landing (workload must allocate past the cap)", name, s.ExitCode)), nil
		}
		return noOp(primitive, fmt.Sprintf("squeeze landed (Memory=%d read back) but %s never observed OOMKilled=true within %s — the anti-gaming arm", capBytes, name, oomKillWindow)), nil
	}
	f := &Fault{CLI: c, Primitive: primitive, Container: name, Kind: KindOOM,
		RestoreMemoryBytes: st.MemoryBytes}
	return landed(primitive, fmt.Sprintf("inspect: %s Memory=%d read back, then OOMKilled=true Running=false (receipt: docker update=0)", name, capBytes)), f
}

// Kind is the closed fault vocabulary of the docker backend (journal-
// representable: a detached process must know what to undo from bytes).
type Kind string

const (
	KindPause Kind = "pause"
	KindKill  Kind = "kill"
	KindOOM   Kind = "oom"
)

// Valid reports whether k is a declared fault kind.
func (k Kind) Valid() bool {
	switch k {
	case KindPause, KindKill, KindOOM:
		return true
	}
	return false
}

// Fault is one armed docker fault: the hold the reverter pattern owns.
// Everything a DETACHED process needs to execute the inverse is in the
// exported fields — no closures, no back-references into the arming
// process (the write-ahead contract, MSF-005).
type Fault struct {
	CLI       *CLI
	Primitive string
	Container string
	Kind      Kind
	// RestoreMemoryBytes is the pre-fault memory limit an OOM inverse
	// restores (0 = docker default when the container had none).
	RestoreMemoryBytes int64
	// JournalPath records where the write-ahead hold for this fault was
	// appended ("" when the caller did not journal it — tests).
	JournalPath string
}

// Revert executes the fault's inverse and PROVES the undo with the same
// independent read-back class the landing used (AC-4: measured, not
// asserted):
//
//	pause  → docker unpause: Paused=false AND Running=true observed.
//	kill   → docker start:   Running=true observed, exit code back to 0,
//	          OOMKilled cleared by the daemon on start.
//	oom    → docker start + docker update --memory <original>: Running=true
//	          observed AND Memory=<original> read back.
func (f *Fault) Revert() (Outcome, error) {
	if f == nil || f.CLI == nil {
		return Outcome{}, fmt.Errorf("backend_docker: revert: nil fault or CLI (wiring bug)")
	}
	if err := CheckScratch(f.Container); err != nil {
		return Outcome{}, err
	}
	switch f.Kind {
	case KindPause:
		if _, errOut, err := f.CLI.run("unpause", f.Container); err != nil {
			return Outcome{}, fmt.Errorf("docker unpause %s: %v: %s", f.Container, err, oneLine(errOut))
		}
		if !waitFor(proofWindow, proofInterval, func() bool {
			s, err := f.CLI.Inspect(f.Container)
			return err == nil && !s.Paused && s.Running
		}) {
			return Outcome{}, fmt.Errorf("revert issued but not proven: %s still Paused after unpause", f.Container)
		}
		return landed(f.Primitive, fmt.Sprintf("revert: %s Paused=false Running=true (receipt: docker unpause=0)", f.Container)), nil
	case KindKill:
		if _, errOut, err := f.CLI.run("start", f.Container); err != nil {
			return Outcome{}, fmt.Errorf("docker start %s: %v: %s", f.Container, err, oneLine(errOut))
		}
		if !waitFor(proofWindow, proofInterval, func() bool {
			s, err := f.CLI.Inspect(f.Container)
			return err == nil && s.Running && s.ExitCode == 0
		}) {
			return Outcome{}, fmt.Errorf("revert issued but not proven: %s still not running after start", f.Container)
		}
		return landed(f.Primitive, fmt.Sprintf("revert: %s Running=true ExitCode=0 (receipt: docker start=0)", f.Container)), nil
	case KindOOM:
		// Restore the limit BEFORE the start: a workload that allocates
		// would OOM again between start and restore if the cap were still
		// armed (update works on a stopped container; start is last).
		restore := f.RestoreMemoryBytes
		memArg := "0"
		if restore > 0 {
			memArg = strconv.FormatInt(restore, 10)
		}
		swapArg := memArg
		if restore == 0 {
			swapArg = "-1"
		}
		if _, errOut, err := f.CLI.run("update", "--memory", memArg, "--memory-swap", swapArg, f.Container); err != nil {
			return Outcome{}, fmt.Errorf("docker update --memory %s %s: %v: %s", memArg, f.Container, err, oneLine(errOut))
		}
		if _, errOut, err := f.CLI.run("start", f.Container); err != nil {
			return Outcome{}, fmt.Errorf("docker start %s: %v: %s", f.Container, err, oneLine(errOut))
		}
		if !waitFor(proofWindow, proofInterval, func() bool {
			s, err := f.CLI.Inspect(f.Container)
			return err == nil && s.Running && s.MemoryBytes == restore
		}) {
			return Outcome{}, fmt.Errorf("revert issued but not proven: %s not running or Memory != %d after restore", f.Container, restore)
		}
		return landed(f.Primitive, fmt.Sprintf("revert: %s Memory=%d restored then Running=true (receipt: docker update=0, docker start=0)", f.Container, restore)), nil
	default:
		return Outcome{}, fmt.Errorf("backend_docker: revert: unknown fault kind %q (failing closed)", f.Kind)
	}
}

// journalWait is the poll interval the journal-based inverse uses when it
// must observe state (kept equal to proofInterval; named separately so the
// journal file's contract is greppable).
var journalWait = proofInterval

// inverseDelay gives `docker start` a beat before the check in the
// journal-path inverse (the detached executor polls, so this is only the
// first sample's spacing).
var inverseDelay = journalWait

// ensure time is used even if future edits drop the delay (compile-time pin)
var _ = time.Second
