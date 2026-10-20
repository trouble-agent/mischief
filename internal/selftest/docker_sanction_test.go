package selftest

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestDockerCapabilityRefusesUnsanctionedHost is SPEC-13 applied to the one
// selftest path that is reachable without the CLI: `go test ./internal/selftest`
// calls the docker landables directly. Before this gate, a laneless `go test`
// on an unsanctioned host could mint a real scratch container — the docker:oom
// scratch runs `dd if=/dev/zero of=/dev/null bs=32M`, which starved the fleet's
// main host for 104 minutes when the OOM never landed.
//
// The refusal must happen at the capability probe, before any container is
// minted, and must name the sanction rail so a skip is never mistaken for a
// missing docker daemon.
func TestDockerCapabilityRefusesUnsanctionedHost(t *testing.T) {
	t.Setenv("MISCHIEF_SANCTION", "") // no environment admission
	t.Setenv("MISCHIEF_SANCTION_FILE",
		filepath.Join(t.TempDir(), "absent-marker")) // no marker file either

	for _, id := range []string{"docker:kill", "docker:oom", "docker:pause"} {
		d := &dockerLandable{id: id}
		reason, unavailable := d.capability()
		if !unavailable {
			t.Fatalf("%s: capability reported available on an unsanctioned host "+
				"(no container may be minted here)", id)
		}
		if !strings.Contains(reason, "capability_unavailable") {
			t.Errorf("%s: reason %q must start with the capability_unavailable prefix", id, reason)
		}
		if !strings.Contains(strings.ToLower(reason), "sanction") {
			t.Errorf("%s: reason %q must name the sanction rail", id, reason)
		}
	}
}

// TestDockerCapabilityAdmitsSanctionedHost is the other half of fail-closed:
// the gate must not refuse a host that carries the marker, or the isolation
// rail would be a blanket ban and every real bunker run would skip. With the
// environment admission set, the capability probe proceeds to its docker
// checks instead of returning the sanction refusal.
func TestDockerCapabilityAdmitsSanctionedHost(t *testing.T) {
	t.Setenv("MISCHIEF_SANCTION", "1")
	t.Setenv("MISCHIEF_SANCTION_FILE", filepath.Join(t.TempDir(), "absent-marker"))

	d := &dockerLandable{id: "docker:kill"}
	reason, _ := d.capability()
	if strings.Contains(strings.ToLower(reason), "sanction") {
		t.Fatalf("sanctioned host refused with %q — the rail must admit a marked host", reason)
	}
}
