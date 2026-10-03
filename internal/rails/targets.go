package rails

import (
	"fmt"
	"os"
	"strings"
)

// TargetKind is the class of thing a selector names (PRD §6.2: process |
// cgroup | container | host | cloud).
type TargetKind string

const (
	// TargetProcess is a plain process or process tree on this host.
	TargetProcess TargetKind = "process"
	// TargetCgroup is a cgroup (v2) path.
	TargetCgroup TargetKind = "cgroup"
	// TargetContainer is a container (docker API).
	TargetContainer TargetKind = "container"
	// TargetHost is the host itself.
	TargetHost TargetKind = "host"
	// TargetCloud is a cloud resource (real provider or simulator).
	TargetCloud TargetKind = "cloud"
)

// ProtectedKind names a control plane on the protected list (AC-7; PRD §7:
// "Protected targets. The scheduler, gateway, memory daemon and the fleet's
// control planes are protected=true and unreachable from the automatic
// (monkey) path").
type ProtectedKind string

const (
	// ProtectedScheduler is the coding-hermes scheduler.
	ProtectedScheduler ProtectedKind = "scheduler"
	// ProtectedGateway is the hermes gateway.
	ProtectedGateway ProtectedKind = "gateway"
	// ProtectedMemoryDaemon is the memory daemon (duckbrain).
	ProtectedMemoryDaemon ProtectedKind = "memory-daemon"
)

// String renders the protection name for refusal text.
func (p ProtectedKind) String() string { return string(p) }

// protectedReasons is the fixed table of why each control plane is on the
// list (refusal text quotes it; the wording is doctrine, PRD §7).
const protectedReasons = "fleet control plane: a fault pointed at it by accident is an outage, not an experiment"

// DeclaredTarget is what an invocation declared: either an explicit
// argument (--target) or the experiment's declared scope. Zero value means
// nothing was declared — there is no default target (AC-2).
type DeclaredTarget struct {
	// Kind is the target class; empty when nothing was declared.
	Kind TargetKind
	// Selector is the address of the target ("container:trouble-hub",
	// "pid:4242", "cgroup:/users.slice/...", "host:", ...).
	Selector string
}

// Explicit reports whether an explicit argument named the target (as opposed
// to the experiment declaring a scope). Both are explicit enough to resolve;
// neither is required to name a protected target.
func (d DeclaredTarget) Explicit() bool { return d.Kind != "" && strings.TrimSpace(d.Selector) != "" }

// Target is a resolved target: the declared address plus everything the
// resolver learned about it.
type Target struct {
	// Kind and Selector are as declared.
	Kind TargetKind
	// Selector is as declared.
	Selector string
	// PID is the resolved process id when Kind is process and the selector
	// named one; 0 when not applicable.
	PID int
	// Protected is set when the resolved target is on the protected list.
	Protected ProtectedKind
	// ProtectedAllowed records an operator explicitly allowing a protected
	// target (AC-7's "the same target runs only with the explicit operator
	// flag and the journal records it"). It is false for every automatic
	// path; the run's journal is expected to record it when true.
	ProtectedAllowed bool
}

// IsProtected reports whether the target landed on the protected list.
func (t Target) IsProtected() bool { return t.Protected != "" }

// ResolveError is a target-resolution refusal: one closed reason, the
// declared target it concerns, and — for the protected and structural
// reasons — what the refusal text names.
type ResolveError struct {
	// Reason is one of the closed refusal taxonomy values (RefusalReason).
	Reason RefusalReason
	// Declared is the declaration the refusal concerns (may be zero when
	// nothing was declared — the AC-2 shape).
	Declared DeclaredTarget
	// Protected is the control plane named, when Reason is ReasonProtected.
	Protected ProtectedKind
	// Excluded describes a structurally excluded target, when Reason is
	// ReasonExcluded (e.g. "pid 1", "kernel thread", "own process tree").
	Excluded string
	// ExitCode is the CLI exit this refusal maps to (always 2 today).
	ExitCode int
}

// Error renders the refusal, naming the measurement or the target (AC-2/7
// refusal text is quoted by the CLI, so it must carry the specifics).
func (e *ResolveError) Error() string {
	switch e.Reason {
	case ReasonNoTarget:
		return fmt.Sprintf("no target: mischief has no default target — pass --target or declare target.selector in the experiment (exit %d, nothing landed)", e.ExitCode)
	case ReasonProtected:
		return fmt.Sprintf("protected target %q refused: %s — an operator may pass the explicit override flag, and the run then records it in the journal (exit %d)", e.Protected, protectedReasons, e.ExitCode)
	case ReasonExcluded:
		return fmt.Sprintf("target %s is structurally excluded and refuses unconditionally (exit %d)", e.Excluded, e.ExitCode)
	default:
		return fmt.Sprintf("target refused (%s, exit %d)", e.Reason, e.ExitCode)
	}
}

// Exit renders the CLI exit code for this refusal (AC-2: exit 2).
func (e *ResolveError) Exit() int { return e.ExitCode }

// ResolveOptions carries the resolver's inputs beyond the declaration.
type ResolveOptions struct {
	// AllowProtected is the explicit operator override (AC-7). It must be
	// set by a named operator flag off the automatic path; the resolver
	// records the allowance on the resolved Target so the journal can say
	// so.
	AllowProtected bool
	// SelfPID is mischief's own pid (its process tree is structurally
	// excluded). Zero means "not resolvable in this context" and skips the
	// own-tree check (tests inject it).
	SelfPID int
	// SelfExe is the resolved executable path of the mischief process;
	// empty skips the own-exe check (tests inject it).
	SelfExe string
}

// Resolve turns a declared target into a resolved Target, applying the
// rails in doctrine order: no declaration → AC-2; structural exclusion
// (never, by construction) → unconditional refusal; the protected list →
// AC-7 (refused by default, operator override recorded). Resolution happens
// BEFORE any actuator is chosen, so naming a different primitive cannot
// route around it (PRD §7).
func Resolve(decl DeclaredTarget, opts ResolveOptions) (Target, error) {
	if !decl.Explicit() {
		return Target{}, &ResolveError{
			Reason:   ReasonNoTarget,
			Declared: decl,
			ExitCode: 2,
		}
	}
	t := Target{Kind: decl.Kind, Selector: decl.Selector}

	// structural exclusions — evaluated before the protected list: PID 1,
	// kernel threads, mischief's own tree and its reverter refuse with no
	// override at all.
	if exc := structuralExclusion(t, opts); exc != "" {
		return Target{}, &ResolveError{Reason: ReasonExcluded, Declared: decl, Excluded: exc, ExitCode: 2}
	}

	// protected list — evaluated at resolution (AC-7)
	if pk := protectedMatch(t); pk != "" {
		if !opts.AllowProtected {
			return Target{}, &ResolveError{Reason: ReasonProtected, Declared: decl, Protected: pk, ExitCode: 2}
		}
		t.Protected = pk
		t.ProtectedAllowed = true
	}
	return t, nil
}

// structuralExclusion names the exclusion when the target is one of the
// never-by-construction shapes, or "" when it is not.
func structuralExclusion(t Target, opts ResolveOptions) string {
	switch t.Kind {
	case TargetProcess:
		sel := t.Selector
		if pid, ok := parsePIDSelector(sel); ok {
			if pid == 1 {
				return "pid 1"
			}
			if opts.SelfPID != 0 && pid == opts.SelfPID {
				return fmt.Sprintf("pid %d (mischief's own process tree)", pid)
			}
			if opts.SelfExe != "" && sameExe(pid, opts.SelfExe) {
				return fmt.Sprintf("pid %d (mischief's own executable)", pid)
			}
		}
		if isKernelThreadSelector(sel) {
			return "kernel thread"
		}
	}
	return ""
}

// parsePIDSelector parses a "pid:<n>" or bare-integer process selector.
func parsePIDSelector(sel string) (int, bool) {
	s := strings.TrimSpace(sel)
	if rest, ok := strings.CutPrefix(s, "pid:"); ok {
		s = strings.TrimSpace(rest)
	}
	if s == "" || s[0] < '0' || s[0] > '9' {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, true
}

// sameExe reports whether pid's executable is exe (readlink /proc/<pid>/exe).
func sameExe(pid int, exe string) bool {
	link, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return false
	}
	return link == exe
}

// isKernelThreadSelector reports whether the selector names the kernel
// thread set ("kernel", "kthread", "[kworker/…]" bracket names).
func isKernelThreadSelector(sel string) bool {
	s := strings.TrimSpace(strings.TrimPrefix(sel, "pid:"))
	s = strings.TrimSpace(s)
	switch s {
	case "kernel", "kthread", "kthreads":
		return true
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		// bracketed names are kernel-internal ("[kworker/0:1]")
		return true
	}
	return false
}

// protectedMatch returns the protected kind the selector names, or "".
//
// The protected list matches on address, not on convention: the scheduler,
// gateway and memory daemon are refused wherever they resolve. Selectors
// name them as "kind:protected:<name>" (canonical), or carry the name as a
// component ("process:scheduler", "container:gateway", "host:memory-daemon").
func protectedMatch(t Target) ProtectedKind {
	sel := strings.TrimSpace(t.Selector)
	for _, pk := range []ProtectedKind{ProtectedScheduler, ProtectedGateway, ProtectedMemoryDaemon} {
		if selectorNames(sel, pk.String()) {
			return pk
		}
	}
	return ""
}

// selectorNames reports whether the selector's canonical form or any
// component equals name. Components are split on ":" and "/"; a name is
// never matched inside a longer word (so "gateway-test" is NOT the gateway).
func selectorNames(sel, name string) bool {
	if sel == "protected:"+name || sel == "protected="+name {
		return true
	}
	fields := strings.FieldsFunc(sel, func(r rune) bool { return r == ':' || r == '/' })
	for _, f := range fields {
		if f == name {
			return true
		}
	}
	return false
}

// ch:trace row=MSF-004 spec=docs/SPEC-PLAN.md (SPEC-03) evidence=internal/rails/targets.go witness=none:no-live-host-run-in-worktree
