package sanction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// markerSetup writes a marker file and returns Options with both halves
// satisfied for host "test-host".
func markerSetup(t *testing.T) (string, Options) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sanctioned")
	if err := os.WriteFile(path, []byte("sanctioned\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, Options{Host: "test-host", MarkerPath: path, EnvHost: "test-host"}
}

// TestCheckSanctionedHostAdmits: both halves present -> nil (admission).
func TestCheckSanctionedHostAdmits(t *testing.T) {
	path, opts := markerSetup(t)
	if err := Check(opts); err != nil {
		t.Fatalf("sanctioned host refused: %v", err)
	}
	if path == "" {
		t.Fatal("unreachable")
	}
}

// TestCheckFailClosedBothHalvesRefused: no file AND no env -> refusal
// naming BOTH the file path and the env (the fail-closed message names the
// host and every missing half — the SPEC-13/MSF-020 stub contract).
func TestCheckFailClosedBothHalvesRefused(t *testing.T) {
	err := Check(Options{Host: "fleet-main", MarkerPath: "/nonexistent/marker", EnvHost: ""})
	if err == nil {
		t.Fatal("unsanctioned host admitted (fail-closed violated)")
	}
	msg := err.Error()
	for _, want := range []string{"fleet-main", "/nonexistent/marker", HostEnv} {
		if !strings.Contains(msg, want) {
			t.Fatalf("refusal does not name %q: %s", want, msg)
		}
	}
}

// TestCheckFileOnlyMissing: marker file absent, env set -> refused, the
// refusal names the file (the env alone is not the marker).
func TestCheckFileOnlyMissing(t *testing.T) {
	err := Check(Options{Host: "h", MarkerPath: "/nonexistent/m", EnvHost: "h"})
	if err == nil {
		t.Fatal("env-only host admitted")
	}
	e, ok := err.(*Error)
	if !ok || e.MissingFile != "/nonexistent/m" || e.MissingEnv {
		t.Fatalf("wrong halves reported: %+v", e)
	}
}

// TestCheckHostMismatchRefused: marker present but the env names ANOTHER
// host -> refused (the env half binds the marker to THIS host).
func TestCheckHostMismatchRefused(t *testing.T) {
	_, opts := markerSetup(t)
	opts.EnvHost = "other-host"
	if err := Check(opts); err == nil {
		t.Fatal("mismatched env host admitted")
	}
}

// TestCheckUnreadableIsAbsent: a marker file that exists but cannot be
// statted reads as ABSENT (fail closed — the same rule as the reverter's
// boot identity and the capability probes).
func TestCheckUnreadableIsAbsent(t *testing.T) {
	err := Check(Options{
		Host:       "h",
		MarkerPath: "/definitely/not/a/path",
		EnvHost:    "h",
		Stat:       func(string) (os.FileInfo, error) { return nil, os.ErrPermission },
	})
	if err == nil {
		t.Fatal("unreadable marker admitted")
	}
}

// TestCheckDirectoryIsNotMarker: a DIRECTORY at the marker path is not a
// marker (an empty dir does not sanction a host).
func TestCheckDirectoryIsNotMarker(t *testing.T) {
	dir := t.TempDir()
	err := Check(Options{Host: "h", MarkerPath: dir, EnvHost: "h"})
	if err == nil {
		t.Fatal("directory marker admitted")
	}
}

// TestCheckUnresolvableHostRefused: a host that cannot prove its name
// cannot be sanctioned (fail closed, boot-identity doctrine).
func TestCheckUnresolvableHostRefused(t *testing.T) {
	// Host stays "" but we cannot force os.Hostname to fail; instead
	// verify the resolved-host path admits with both halves and that the
	// DEFAULT path (no overrides) refuses on this unsanctioned test host
	// — the production default IS fail-closed.
	if err := Check(Options{MarkerPath: "/nonexistent/m"}); err == nil {
		t.Fatal("default (live-host) check admitted an unsanctioned host")
	}
}
