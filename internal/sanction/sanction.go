// Package sanction is SPEC-13: the host-level isolation rail — the FIRST
// rail a run meets, before target resolution, the scope ladder or the load
// gate. A host admits mischief only when it carries an explicit SANCTION
// MARKER: a reason-bearing file (default /etc/mischief-sanction, override
// with MISCHIEF_SANCTION_FILE) and/or the environment variable
// MISCHIEF_SANCTION=1. An absent, unreadable or reasonless marker is a
// REFUSAL with the rails exit code (2), never a warning — the check fails
// closed, because mischief's own work *is* the fault and the fleet's main
// host carries the scheduler, the gateway and the memory daemon (PRD §7,
// "Where mischief ITSELF is allowed to run").
//
// Design authority: docs/SPEC-PLAN.md (SPEC-13 isolation contract) and
// docs/prd/mischief-v0.1.md §7. The refusal shape and exit mapping are the
// rails' own (github.com/trouble-agent/mischief/internal/rails): a sanction
// refusal is a rails.Refusal carrying rails.ReasonSanction and grading
// rails.VerdictAborted, so the CLI maps it to exit 2 through the one
// MapExit contract every rail shares. The taxonomy member lives in the
// rails package so the refusal vocabulary stays closed in one place.
//
// Separation of concerns (SPEC-13 states it so neither rail can be read as
// a substitute for the other):
//
//   - HOST ADMISSION (this package) answers "may mischief run here at
//     all?" — one boolean over the marker, checked before anything else.
//   - TARGET PROTECTION (internal/rails Resolve) answers "may this fault
//     land on that target?" — the protected list, structural exclusions and
//     the operator-override journal contract, evaluated at target
//     resolution.
//
// A sanctioned host does NOT weaken target protection: even on a bunker
// with a fresh marker, the scheduler/gateway/memory-daemon list still
// refuses at resolution (SPEC-13's own table defers to SPEC-03 there).
//
// Writes nothing: the check performs exactly one read (the marker probe)
// and no writes at all — a refused host leaves no journal entry, no state
// file and no marker of its own. TestSanctionCheckWritesNothing pins the
// read-only surface; a future writer in this package fails that test first.
//
// NOT-LIST (what SPEC-13 does not promise here):
//
//   - No host identity: the package does not detect "this is the main host"
//     (no cloud metadata, no fingerprinting). The marker is the only input;
//     a main host carrying a hand-made marker is operator error, the same
//     territory as the protected-list override — recorded acts, not
//     prevented ones.
//   - No expiry: the marker carries no TTL. The ephemeral host's lifetime
//     is the sanction's lifetime; revocation is destroying the host (or
//     deleting the marker), not a protocol this package runs.
//   - No per-tier environment logic: which tier runs where (L0 scratch,
//     L1 rootless bunker, L2/L3 sanctioned non-fleet box, L4 simulator,
//     L5 real cloud opt-in) is doctrine, owned by docs/SPEC-PLAN.md
//     (SPEC-13) — this package owns only the admission boolean.
//
// ch:trace row=MSF-020 spec=docs/SPEC-PLAN.md (SPEC-13) evidence=internal/sanction/ witness=none:no-sanctioned-host-run-possible-in-worktree
package sanction

import (
	"fmt"
	"os"
	"strings"

	"github.com/trouble-agent/mischief/internal/rails"
)

// EnvSanction is the environment-variable half of the marker: set to
// EnvSanctionValue ("1") to sanction a host at the deployment layer (the
// provisioning of an ephemeral bunker flips this on, the fleet main host
// never does).
const EnvSanction = "MISCHIEF_SANCTION"

// EnvSanctionValue is the only value of MISCHIEF_SANCTION that sanctions:
// exactly "1". Any other value — "true", "yes", "0", empty — does not
// admit, and a check on such a host names the value it found (no
// truthiness zoo: the doctrine names one explicit act).
const EnvSanctionValue = "1"

// EnvSanctionFile is the env override for the marker file path. When set,
// its value replaces DefaultMarkerPath as the file the check probes.
const EnvSanctionFile = "MISCHIEF_SANCTION_FILE"

// DefaultMarkerPath is the marker file a sanctioned host provisions
// (production shape: /etc/mischief-sanction).
const DefaultMarkerPath = "/etc/mischief-sanction"

// Options carries the check's inputs; the zero value checks the real host
// (os.LookupEnv, os.Hostname, one marker read). Tests inject the seams —
// the package never mutates the process environment to stage a scenario.
type Options struct {
	// EnvLookup resolves environment variables; nil uses os.LookupEnv.
	EnvLookup func(key string) (string, bool)
	// Hostname resolves the host's name for the refusal text; nil uses
	// os.Hostname. An error here becomes the hostname "<unknown>" in the
	// refusal text (the refusal must not be blocked by a broken hostname
	// lookup — fail closed still refuses).
	Hostname func() (string, error)
	// MarkerRead probes the marker file; nil uses os.ReadFile. An error is
	// "the marker did not probe" — unreadable counts as absent (fail
	// closed), with the probe failure named in the refusal text.
	MarkerRead func(path string) (string, error)
}

// Inspect is the check's evidence: what the host carries, in the order the
// rails speak (env first, then the file). Check refuses exactly when
// Sanctioned() is false.
type Inspect struct {
	// EnvSanctioned: MISCHIEF_SANCTION is set to "1".
	EnvSanctioned bool
	// MarkerPath is the file path the check probed (resolved from the env
	// override or the default).
	MarkerPath string
	// MarkerFound: the marker file probed successfully (readable).
	MarkerFound bool
	// Reason is the marker's first non-empty, non-comment line — the
	// reason the host carries the marker. Empty when MarkerFound is false
	// or the file carries no reason line.
	Reason string
}

// Sanctioned reports whether the evidence admits the host: the env half
// set, OR the marker file present WITH a reason line. An empty or
// comment-only marker file is not a marker (SPEC-13: "absent/empty marker
// = REFUSAL, not a warning").
func (in Inspect) Sanctioned() bool {
	if in.EnvSanctioned {
		return true
	}
	return in.MarkerFound && strings.TrimSpace(in.Reason) != ""
}

// InspectEnv gathers the evidence for the real host (os.LookupEnv +
// os.Hostname are still injected by Check; InspectEnv is the Options{0}
// convenience for callers that only want to look).
func InspectEnv() Inspect {
	return InspectWith(Options{})
}

// InspectWith gathers the evidence under the given seams.
func InspectWith(opts Options) Inspect {
	env := opts.envLookup()
	envSanctioned := false
	if v, ok := env(EnvSanction); ok && v == EnvSanctionValue {
		envSanctioned = true
	}
	path := markerPath(env)
	var found bool
	var reason string
	if body, err := opts.markerRead()(path); err == nil {
		found = true
		reason = parseReasonLine(body)
	}
	return Inspect{EnvSanctioned: envSanctioned, MarkerPath: path, MarkerFound: found, Reason: reason}
}

// Check is the first rail: refuse an unsanctioned host before anything
// else runs. It returns nil when the host is sanctioned and a
// *rails.Refusal (Reason rails.ReasonSanction, exit 2) otherwise. The
// refusal text names the hostname and BOTH marker routes — the missing
// file path (with its env override name) and the env variable — so the
// operator can see exactly which act is missing and on which host.
func Check(opts ...Options) error {
	o := Options{}
	if len(opts) > 0 {
		o = opts[0]
	}
	in := InspectWith(o)
	if in.Sanctioned() {
		return nil
	}
	return sanctionRefusal(o.hostname(), in)
}

// sanctionRefusal builds the typed refusal: the rails taxonomy member, the
// rails verdict, the rails exit — SPEC-13 adds a reason, not a new shape.
func sanctionRefusal(host string, in Inspect) *rails.Refusal {
	detail := fmt.Sprintf(
		"host %q is not sanctioned for mischief (SPEC-13): no readable marker file at %s (override with %s) carrying a reason line, and %s is not set to %q — mischief runs only on an ephemeral sanctioned host; nothing was written",
		host, in.MarkerPath, EnvSanctionFile, EnvSanction, EnvSanctionValue)
	if in.MarkerFound && strings.TrimSpace(in.Reason) == "" {
		detail = fmt.Sprintf(
			"host %q is not sanctioned for mischief (SPEC-13): marker file %s carries no reason line (empty and comment-only markers are refusals, not warnings) and %s is not set to %q — write the reason the host is sanctioned into the marker",
			host, in.MarkerPath, EnvSanction, EnvSanctionValue)
	}
	return &rails.Refusal{
		Reason:  rails.ReasonSanction,
		Detail:  detail,
		Verdict: rails.VerdictAborted,
	}
}

// markerPath resolves the file half of the marker: the env override when
// set (even empty — an explicit empty override means "no file", and the
// empty path then simply never probes), the default otherwise.
func markerPath(env func(string) (string, bool)) string {
	if v, ok := env(EnvSanctionFile); ok {
		return v
	}
	return DefaultMarkerPath
}

// parseReasonLine returns the marker's reason: the first non-empty,
// non-comment ("#") line, trimmed. Empty when the file is empty or
// comment-only — which Sanctioned() refuses (SPEC-13: the marker must
// carry a reason).
func parseReasonLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		return s
	}
	return ""
}

func (o Options) envLookup() func(string) (string, bool) {
	if o.EnvLookup != nil {
		return o.EnvLookup
	}
	return os.LookupEnv
}

func (o Options) hostname() string {
	if o.Hostname == nil {
		name, err := os.Hostname()
		if err != nil {
			return "<unknown>"
		}
		return name
	}
	name, err := o.Hostname()
	if err != nil {
		return "<unknown>"
	}
	return name
}

func (o Options) markerRead() func(string) (string, error) {
	if o.MarkerRead != nil {
		return o.MarkerRead
	}
	return func(path string) (string, error) {
		b, err := os.ReadFile(path)
		return string(b), err
	}
}

// ch:trace row=MSF-020 spec=docs/SPEC-PLAN.md (SPEC-13) evidence=internal/sanction/sanction.go witness=none:no-sanctioned-host-run-possible-in-worktree
