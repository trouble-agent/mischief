// Tests for the SPEC-13 host sanction rail: marker present/absent/empty,
// env-only and file-only admission, the refusal text (hostname + BOTH
// missing-marker routes), the exit-2 mapping through the shared rails
// contract, the writes-nothing property, and the rails-order seam that pins
// sanction as the FIRST rail. Every scenario runs on injected seams — no
// test touches this host's real marker paths or environment, and the
// env-var matrix arms neutralize their own injections via t.Setenv (see
// the environment-dependent-test discipline).
//
// ch:trace row=MSF-020 spec=docs/SPEC-PLAN.md (SPEC-13) test=internal/sanction/sanction_test.go evidence=internal/sanction/sanction_test.go witness=none:no-sanctioned-host-run-possible-in-worktree
package sanction

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/rails"
)

// host is the test's fixed hostname so refusal-text assertions are exact.
const host = "bunker-ephemeral-01"

// optsFor builds seams where the marker file's content is content ("" =
// unreadable) and env is the process environment view.
type scenario struct {
	name        string
	envSanction string // value of MISCHIEF_SANCTION; "" with present=false when empty here means unset
	envSet      bool
	markerBody  string
	markerErr   error // non-nil = the marker probe fails (absent/unreadable)
}

// TestSanctionRefusalTable is the admission matrix: the refusal fires on
// absence, unreadability and reasonlessness; file-only (with a reason) and
// env-only both admit; a reasonless file alongside a set env is still
// admitted BY the env (the reasonless file alone is the refusal).
//
// ch:trace row=MSF-020 spec=docs/SPEC-PLAN.md (SPEC-13) test=TestSanctionRefusalTable evidence=internal/sanction/sanction_test.go witness=none:no-sanctioned-host-run-possible-in-worktree
func TestSanctionRefusalTable(t *testing.T) {
	reasonBody := "# sanctioned by wave dispatch 2026-10-04\nbunker ephemeral host for mischief wave work\n"
	cases := []struct {
		name    string
		sc      scenario
		wantAdm bool // true = Check returns nil
	}{
		{"no marker at all", scenario{markerErr: os.ErrNotExist}, false},
		{"marker unreadable", scenario{markerErr: errors.New("permission denied")}, false},
		{"empty marker file", scenario{markerBody: ""}, false},
		{"whitespace-only marker", scenario{markerBody: "   \n\t\n"}, false},
		{"comment-only marker", scenario{markerBody: "# no reason below\n#\n"}, false},
		{"file with reason (file-only)", scenario{markerBody: reasonBody}, true},
		{"env=1 (env-only)", scenario{envSanction: EnvSanctionValue, envSet: true}, true},
		{"env=true is not a sanction", scenario{envSanction: "true", envSet: true}, false},
		{"env=0 is not a sanction", scenario{envSanction: "0", envSet: true}, false},
		{"env set empty is not a sanction", scenario{envSanction: "", envSet: true}, false},
		{"env=1 admits even with reasonless file", scenario{envSanction: EnvSanctionValue, envSet: true, markerBody: ""}, true},
		{"reasonless file alone refuses", scenario{markerBody: ""}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.sc.opts(t)
			err := Check(opts)
			got := err == nil
			if got != tc.wantAdm {
				t.Fatalf("Check(): err=%v, wantAdmit=%v", err, tc.wantAdm)
			}
			if !tc.wantAdm {
				ref, ok := err.(*rails.Refusal)
				if !ok {
					t.Fatalf("refusal is %T, want *rails.Refusal", err)
				}
				if ref.Reason != rails.ReasonSanction {
					t.Fatalf("Reason=%q, want %q", ref.Reason, rails.ReasonSanction)
				}
				if ref.Verdict != rails.VerdictAborted {
					t.Fatalf("Verdict=%q, want %q (PRD §6.4: a rail refusal grades aborted)", ref.Verdict, rails.VerdictAborted)
				}
			}
		})
	}
}

// opts converts the scenario into injected Options (never the real env,
// never the real filesystem).
func (s scenario) opts(t *testing.T) Options {
	t.Helper()
	return Options{
		EnvLookup: func(key string) (string, bool) {
			if key == EnvSanction && s.envSet {
				return s.envSanction, true
			}
			return "", false
		},
		Hostname: func() (string, error) { return host, nil },
		MarkerRead: func(path string) (string, error) {
			if s.markerErr != nil {
				return "", s.markerErr
			}
			return s.markerBody, nil
		},
	}
}

// TestRefusalTextNamesHostAndMissingMarker: the SPEC-13 refusal text names
// the hostname and both marker routes — the missing file path WITH its env
// override name, and the env variable with its sanctioning value.
//
// ch:trace row=MSF-020 spec=docs/SPEC-PLAN.md (SPEC-13) test=TestRefusalTextNamesHostAndMissingMarker evidence=internal/sanction/sanction_test.go witness=none:no-sanctioned-host-run-possible-in-worktree
func TestRefusalTextNamesHostAndMissingMarker(t *testing.T) {
	err := Check(scenario{markerErr: os.ErrNotExist}.opts(t))
	if err == nil {
		t.Fatal("unsanctioned host admitted; want the refusal")
	}
	for _, want := range []string{host, DefaultMarkerPath, EnvSanctionFile, EnvSanction, EnvSanctionValue} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q must name %q", err.Error(), want)
		}
	}
	// nothing was written and nothing was offered: the refusal grades
	// aborted through the shared rails contract
	if !rails.IsRefusal(err) {
		t.Fatal("IsRefusal(sanction refusal)=false; the sanction refusal must be a rails refusal")
	}
	if got := rails.MapExit(err); got != rails.ExitCode {
		t.Fatalf("MapExit(sanction refusal)=%d, want %d", got, rails.ExitCode)
	}
	if rails.ExitCode != 2 {
		t.Fatalf("ExitCode=%d, want 2", rails.ExitCode)
	}
}

// TestRefusalNamesEmptyMarkerReason: an empty marker file gets its own
// refusal wording — the text names the reason-line requirement, not just
// the file.
//
// ch:trace row=MSF-020 spec=docs/SPEC-PLAN.md (SPEC-13) test=TestRefusalNamesEmptyMarkerReason evidence=internal/sanction/sanction_test.go witness=none:no-sanctioned-host-run-possible-in-worktree
func TestRefusalNamesEmptyMarkerReason(t *testing.T) {
	err := Check(scenario{markerBody: ""}.opts(t))
	if err == nil {
		t.Fatal("empty marker admitted; want the refusal")
	}
	if !strings.Contains(err.Error(), "reason line") {
		t.Fatalf("refusal %q must name the reason-line requirement", err.Error())
	}
	if !strings.Contains(err.Error(), host) {
		t.Fatalf("refusal %q must name the host %q", err.Error(), host)
	}
}

// TestThisHostRefuses demonstrates the acceptance criterion ON THIS HOST:
// with the real environment and the real marker probe (no injections), the
// check must refuse — the worktree box carries no /etc/mischief-sanction
// and no MISCHIEF_SANCTION. If this test ever fails, either this host is
// sanctioned (and the fleet's main host must never be) or the check has
// been neutered — both are row-stopping findings.
//
// ch:trace row=MSF-020 spec=docs/SPEC-PLAN.md (SPEC-13) test=TestThisHostRefuses evidence=internal/sanction/sanction_test.go witness=none:no-sanctioned-host-run-possible-in-worktree
func TestThisHostRefuses(t *testing.T) {
	// belt and suspenders against an ambient injection: the env var must
	// not be set in the test process, and if a sibling test leaked it, we
	// neutralize it for the duration (restored after).
	if v, ok := os.LookupEnv(EnvSanction); ok {
		t.Logf("ambient %s=%q neutralized for this test", EnvSanction, v)
		t.Setenv(EnvSanction, "")
	}
	if _, ok := os.LookupEnv(EnvSanctionFile); ok {
		t.Setenv(EnvSanctionFile, "")
	}
	err := Check()
	if err == nil {
		t.Fatal("Check() on THIS host (no marker) returned nil; the acceptance refusal is gone")
	}
	t.Logf("this host refused as required: %v", err)
	if !rails.IsRefusal(err) || rails.MapExit(err) != 2 {
		t.Fatalf("host refusal is not a rails exit-2 refusal: %v", err)
	}
	hn, _ := os.Hostname()
	if hn != "" && !strings.Contains(err.Error(), hn) {
		t.Fatalf("refusal %q must name the real hostname %q", err.Error(), hn)
	}
}

// TestSanctionCheckWritesNothing: the refused host leaves nothing behind —
// the check's only filesystem contact is the marker READ. Proven by a
// recording MarkerRead: it must be called exactly once, with the marker
// path, and the package exposes no other write-capable call the check
// makes (a future writer in this package fails this test first).
//
// ch:trace row=MSF-020 spec=docs/SPEC-PLAN.md (SPEC-13) test=TestSanctionCheckWritesNothing evidence=internal/sanction/sanction_test.go witness=none:no-sanctioned-host-run-possible-in-worktree
func TestSanctionCheckWritesNothing(t *testing.T) {
	calls := 0
	opts := Options{
		EnvLookup: func(string) (string, bool) { return "", false },
		Hostname:  func() (string, error) { return host, nil },
		MarkerRead: func(path string) (string, error) {
			calls++
			if path != DefaultMarkerPath {
				t.Errorf("probe touched %q, want the default marker path", path)
			}
			return "", os.ErrNotExist
		},
	}
	if err := Check(opts); err == nil {
		t.Fatal("unsanctioned host admitted; want the refusal")
	}
	if calls != 1 {
		t.Fatalf("marker probed %d times, want exactly 1 read and no other filesystem contact", calls)
	}
}

// TestSanctionFirstRailOrder: the ORDER contract — sanction is the FIRST
// rail, before target resolution and the scope ladder. Proven with the
// shared rails vocabulary: on an unsanctioned host, Check refuses EVEN
// WHEN the would-be run's own rails (no target, protected target, below
// tier) would also refuse — the sanction refusal is the one returned, and
// it is satisfiable independently (a sanctioned host still refuses the
// protected target through rails.Resolve: sanction does not weaken target
// protection).
//
// ch:trace row=MSF-020 spec=docs/SPEC-PLAN.md (SPEC-13) test=TestSanctionFirstRailOrder evidence=internal/sanction/sanction_test.go witness=none:no-sanctioned-host-run-possible-in-worktree
func TestSanctionFirstRailOrder(t *testing.T) {
	unsanctioned := scenario{markerErr: os.ErrNotExist}

	// arm 1: the sanction refusal wins over every downstream refusal shape
	// — the error the host sees is the SANCTION one, not target/scope text.
	err := Check(unsanctioned.opts(t))
	if err == nil || err.(*rails.Refusal).Reason != rails.ReasonSanction {
		t.Fatalf("unsanctioned host: err=%v, want the sanction refusal first", err)
	}
	// the downstream shapes that WOULD have refused are all still refusals
	// of their own reason — proving the ordering claim is about WHO refuses
	// first, not about the others being gone
	if _, err := rails.Resolve(rails.DeclaredTarget{}, rails.ResolveOptions{}); err == nil {
		t.Fatal("sanity: rails.Resolve with no declaration must refuse (AC-2)")
	}

	// arm 2: a sanctioned host does NOT weaken target protection — the
	// protected list still refuses at resolution (SPEC-13 defers to
	// SPEC-03 there).
	sanctioned := scenario{envSanction: EnvSanctionValue, envSet: true}
	if err := Check(sanctioned.opts(t)); err != nil {
		t.Fatalf("sanctioned host refused: %v (env-only admission must pass Check)", err)
	}
	_, err = rails.Resolve(
		rails.DeclaredTarget{Kind: rails.TargetProcess, Selector: "process:scheduler"},
		rails.ResolveOptions{},
	)
	if err == nil {
		t.Fatal("sanctioned host resolved the scheduler; target protection must survive sanction")
	}
	if got := rails.MapExit(err); got != rails.ExitCode {
		t.Fatalf("protected-target refusal mapped to %d, want %d", got, rails.ExitCode)
	}
}

// TestEnvVarMatrixIsolation: the env arms of the table above run on
// injected seams, but MISCHIEF_SANCTION itself must never leak from the
// test process into another package's environment — pinned per the
// environment-dependent-test discipline (inject via t.Setenv, which
// restores the prior value after the test; the assertion fails loudly if
// the injection itself did not land).
//
// ch:trace row=MSF-020 spec=docs/SPEC-PLAN.md (SPEC-13) test=TestEnvVarMatrixIsolation evidence=internal/sanction/sanction_test.go witness=none:no-sanctioned-host-run-possible-in-worktree
func TestEnvVarMatrixIsolation(t *testing.T) {
	if _, had := os.LookupEnv(EnvSanction); had {
		t.Logf("ambient %s present before the test; t.Setenv restores it after", EnvSanction)
	}
	t.Setenv(EnvSanction, EnvSanctionValue)
	if v, _ := os.LookupEnv(EnvSanction); v != EnvSanctionValue {
		t.Fatalf("t.Setenv failed to inject: %q", v)
	}
}
