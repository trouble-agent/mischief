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
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

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

// HomeMarkerSuggestion is the sanctioned-run recipe for an L1 container:
// /etc is not writable there, so the marker's reason-bearing file lives at
// $HOME and MISCHIEF_SANCTION_FILE points the gate at it. This is exactly
// what the test plane provisions (scripts/bunker-plane.sh) and what
// docs/TEST-PLANE.md documents; it is a SUGGESTION (rendered in doctor
// hints and prose), not a second default — DefaultMarkerPath stays the
// gate's real default so the fail-closed semantics never move.
const HomeMarkerSuggestion = "$HOME/mischief-sanction"

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

// Details wraps a sanction refusal with the placement evidence the check
// gathered. The extra fields are placement information for the doctor verb
// (and any other diagnostic consumer): they report WHERE the check probed
// and WHAT it found, without asking the consumer to re-parse the refusal
// text. Check's return type stays error, so every existing caller and test
// is untouched; consumers that want the evidence assert with
// errors.As(err, &d). The wrapped refusal stays reachable through Unwrap,
// so the rails taxonomy helpers (IsRefusal, MapExit) behave identically on
// both shapes.
type Details struct {
	// err is the underlying *rails.Refusal this evidence rode in on.
	err error
	// Inspect is the evidence the refusal was built from (env state,
	// marker path probed, found-ness, reason line).
	Inspect Inspect
	// ReadOnlyDir is true when the check PROBED a default /etc marker
	// (no MISCHIEF_SANCTION_FILE override), the probe failed, and the
	// marker's parent directory is not writable by this user — the L1
	// container shape. It is placement evidence only: it never changes
	// the refusal (SPEC-13 fails closed regardless of writability).
	ReadOnlyDir bool
}

// Error renders the underlying refusal text verbatim (Details IS the
// refusal error; the evidence fields ride alongside).
func (d *Details) Error() string { return d.err.Error() }

// Unwrap exposes the underlying refusal for errors.Is/As over the rails
// taxonomy (the *rails.Refusal stays reachable).
func (d *Details) Unwrap() error { return d.err }

// NewDetails wraps a sanction refusal with its placement evidence — the
// constructor Check uses for the read-only-etc shape and tests use to pin
// the doctor verb's hint arm.
func NewDetails(err error, in Inspect, readOnlyDir bool) *Details {
	return &Details{err: err, Inspect: in, ReadOnlyDir: readOnlyDir}
}

// Check is the first rail: refuse an unsanctioned host before anything
// else runs. It returns nil when the host is sanctioned and a
// *rails.Refusal (Reason rails.ReasonSanction, exit 2) otherwise — wrapped
// in *Details when the placement evidence is worth surfacing (see
// Details.ReadOnlyDir). The refusal text names the hostname and BOTH
// marker routes — the missing file path (with its env override name) and
// the env variable — so the operator can see exactly which act is missing
// and on which host.
func Check(opts ...Options) error {
	o := Options{}
	if len(opts) > 0 {
		o = opts[0]
	}
	in := InspectWith(o)
	if in.Sanctioned() {
		return nil
	}
	ref := sanctionRefusal(o.hostname(), in)
	if defaultEtcMarkerUnwritable(o, in) {
		return NewDetails(ref, in, true)
	}
	return ref
}

// defaultEtcMarkerUnwritable reports the L1 placement shape: no env
// override in play (the check probed the default /etc marker), the marker
// did not probe, and the marker's parent directory is not writable by
// this user — the shape where the sanctioned-run recipe belongs at
// $HOME/mischief-sanction + MISCHIEF_SANCTION_FILE instead. An explicit
// override (even empty) means the operator already chose a path, so the
// hint has nothing to add there.
func defaultEtcMarkerUnwritable(o Options, in Inspect) bool {
	if _, overridden := o.envLookup()(EnvSanctionFile); overridden {
		return false
	}
	if in.MarkerFound || in.MarkerPath != DefaultMarkerPath {
		return false
	}
	// The hint fires when the marker's directory is NOT writable by this
	// user (probeDirWritable answers "writable"); an unwritable-but-present
	// directory (or an absent one) is the L1 placement shape.
	return !probeDirWritable(filepath.Dir(in.MarkerPath))
}

// probeDirWritable is the writability probe seam: production resolves the
// directory's write permission FOR THIS USER (unix.Access, not the mode
// bits — root passes where 0755 mode bits say otherwise). Package-level so
// tests pin both arms deterministically on any host; the check itself
// stays read-only (a stat + an access syscall, no write).
var probeDirWritable = func(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	return unix.Access(dir, unix.W_OK) == nil
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
