package selftest

import (
	"strings"
	"testing"
)

// Each primitive gets its own live test so the external witness cannot be
// accidentally hidden by an all-corpus aggregate. A capability boundary is an
// honest named skip; a pass must carry the primitive's measured witness.
func TestS020ExternalWitness(t *testing.T) {
	assertErrnoWitness(t, "S-020", "delivered=6")
}

func TestS021ExternalWitness(t *testing.T) {
	assertErrnoWitness(t, "S-021", "EPIPE")
}

func TestS022ExternalWitness(t *testing.T) {
	assertErrnoWitness(t, "S-022", "ENOSPC")
}

func TestS023ExternalWitness(t *testing.T) {
	assertErrnoWitness(t, "S-023", "EACCES")
}

func TestS024ExternalWitness(t *testing.T) {
	assertErrnoWitness(t, "S-024", "delivered 8 bytes")
}

func assertErrnoWitness(t *testing.T, id, witness string) {
	t.Helper()
	pinHealthyLoad(t)
	result := RunOnePublic(id)
	switch result.State {
	case "pass":
		if result.LandProof == "" || !strings.Contains(result.LandProof, witness) {
			t.Fatalf("%s pass lacks external witness %q: land=%q", id, witness, result.LandProof)
		}
		if result.RevertProof == "" {
			t.Fatalf("%s pass lacks inverse witness: %q", id, result.RevertProof)
		}
	case "skip":
		if !strings.HasPrefix(result.Reason, "capability_unavailable:") || result.Reason == "capability_unavailable: " {
			t.Fatalf("%s skip is not an honest named capability skip: %q", id, result.Reason)
		}
	default:
		t.Fatalf("%s result=%s reason=%q land=%q revert=%q", id, result.State, result.Reason, result.LandProof, result.RevertProof)
	}
}
