// Package sanction is the SPEC-13 / MSF-020 sanction-marker gate (M1 stub).
//
// PRD §7: "Mischief's own development, selftest and battery runs execute on
// an ephemeral sanctioned host — a bunker instance — never on the fleet's
// main host." The main host is a protected target that REFUSES, and the
// refusal is mechanical rather than documentary:
//
//	A host admits mischief only when it carries an explicit sanction
//	marker (file + env). Absence is a REFUSAL with a non-zero exit, not
//	a warning — fail closed.
//
// This is the M1 stub of that contract: it checks the two halves of the
// marker (a marker FILE on disk and a HOST ENV declaration) and refuses
// naming the host and exactly which half is missing. MSF-020 hardens it
// (marker content binding, the neuter-proof test); this stub already fails
// closed — every ambiguous state (unreadable file, empty env, hostname
// mismatch) is a refusal, never an admission.
//
// NOT-LIST (what the stub does not promise):
//
//   - It does not verify the marker was written by the fleet's provisioning
//     path (no signature, no freshness) — that is MSF-020.
//   - It does not distinguish "bunker" from any other sanctioned host; the
//     marker IS the sanction.
package sanction

import (
	"fmt"
	"os"
	"strings"
)

// MarkerFileEnv names the env var that overrides the marker file path.
const MarkerFileEnv = "MISCHIEF_SANCTION_MARKER"

// HostEnv names the env var that must carry this host's hostname (the env
// half of the file + env marker pair).
const HostEnv = "MISCHIEF_SANCTION_HOST"

// DefaultMarkerPath is the marker file path when MarkerFileEnv is unset.
const DefaultMarkerPath = "/etc/mischief/sanctioned"

// Error is the fail-closed refusal: it names the host and the missing
// marker halves (SPEC-13's message contract at stub grade).
type Error struct {
	// Host is the hostname the check ran on.
	Host string
	// MissingFile names the marker file path that was required and absent
	// (or unreadable). Empty when the file half passed.
	MissingFile string
	// MissingEnv is true when the env half failed (unset, empty, or naming
	// a different host).
	MissingEnv bool
}

// Error renders the refusal: host first, then each missing half (the
// fail-closed message contract — an operator must be able to read exactly
// what to sanction, and nothing is admitted by ambiguity).
func (e *Error) Error() string {
	var missing []string
	if e.MissingFile != "" {
		missing = append(missing, fmt.Sprintf("sanction marker file %q absent or unreadable", e.MissingFile))
	}
	if e.MissingEnv {
		missing = append(missing, fmt.Sprintf("env %s does not name this host (set it to the hostname to sanction)", HostEnv))
	}
	return fmt.Sprintf("host %q is not sanctioned for fault injection: %s — refusing fail-closed (SPEC-13/MSF-020: mischief runs only on an ephemeral sanctioned host, never the fleet's main host)",
		e.Host, strings.Join(missing, "; "))
}

// Options carries the check's inputs; production uses the zero value
// (hostname, env and the default marker path resolved live). Tests inject
// all three so the suite never depends on the host it runs on.
type Options struct {
	// Host overrides os.Hostname ("" = resolve live).
	Host string
	// MarkerPath overrides the marker file path ("" = MarkerFileEnv or the
	// default). "none" disables the file half (tests of the env arm).
	MarkerPath string
	// EnvHost overrides HostEnv's value (a one-element probe seam; "" keeps
	// the live env).
	EnvHost string
	// Stat is the file-existence seam (nil = os.Stat).
	Stat func(string) (os.FileInfo, error)
}

// Check runs the fail-closed sanction gate. nil means the host is
// sanctioned; any non-nil result is a *Error naming host + missing halves.
// Both halves are ALWAYS evaluated (the report is complete even when the
// first half already failed) so a refusal never under-names what is missing.
func Check(opts Options) error {
	host := opts.Host
	if host == "" {
		h, err := os.Hostname()
		if err != nil {
			// a host that cannot prove its name cannot be sanctioned
			// (fail closed, same doctrine as reverter's boot identity).
			return &Error{Host: "<unresolvable>", MissingEnv: true, MissingFile: markerPath(opts)}
		}
		host = h
	}
	envHost := opts.EnvHost
	if envHost == "" {
		envHost = os.Getenv(HostEnv)
	}
	stat := opts.Stat
	if stat == nil {
		stat = os.Stat
	}

	e := &Error{Host: host}
	path := markerPath(opts)
	if path != "none" {
		if fi, err := stat(path); err != nil || fi.IsDir() {
			// unreadable == absent (fail closed); a DIRECTORY at the
			// marker path is not a marker.
			e.MissingFile = path
		}
	}
	if strings.TrimSpace(envHost) != host {
		e.MissingEnv = true
	}
	if !e.MissingEnv && e.MissingFile == "" {
		return nil
	}
	return e
}

// markerPath resolves the marker file path: Options.MarkerPath, then
// MarkerFileEnv, then the default.
func markerPath(opts Options) string {
	if opts.MarkerPath != "" {
		return opts.MarkerPath
	}
	if v := strings.TrimSpace(os.Getenv(MarkerFileEnv)); v != "" {
		return v
	}
	return DefaultMarkerPath
}
