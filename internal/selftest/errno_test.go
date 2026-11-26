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
	assertErrnoWitness(t, "S-024", "delivered 7 bytes")
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

// The S-024 land predicate must demand the catalog contract exactly: a
// 7-byte short read followed by a MEASURED EOF. The old 8-byte injection, a
// complete read, or a missing EOF read must all be rejected.
func TestS024LandPredicateRejectsWrongReadOrMissingEOF(t *testing.T) {
	for _, out := range []string{
		"partial_read=8 eof=1",  // old 8-byte injection
		"partial_read=7 eof=0",  // second read returned data, not EOF
		"partial_read=7",        // old output format: EOF never measured
		"partial_read=14 eof=1", // control-shaped complete read on fault path
		"",                      // no witness at all
	} {
		if res := checkS024Land(out); res.landed {
			t.Fatalf("S-024 land predicate accepted %q", out)
		}
	}
	if res := checkS024Land("partial_read=7 eof=1"); !res.landed || res.proof == "" {
		t.Fatalf("S-024 land predicate rejected 7-byte short read + measured EOF: %+v", res)
	}
}

// The S-020 land predicate must demand that EINTR actually fired: the
// victim's attempts counter must be >= 2. A no-shim control run delivers the
// 6 bytes in a single attempt and must fail.
func TestS020LandPredicateRejectsSingleAttempt(t *testing.T) {
	for _, out := range []string{
		"delivered=6 attempts=1", // no-shim control: EINTR never fired
		"delivered=6",            // old output format: no attempts count
		"attempts=3",             // no delivered count
		"",                       // no witness at all
	} {
		if res := checkS020Land(out); res.landed {
			t.Fatalf("S-020 land predicate accepted %q", out)
		}
	}
	if res := checkS020Land("delivered=6 attempts=2"); !res.landed || res.proof == "" {
		t.Fatalf("S-020 land predicate rejected EINTR-interrupted run: %+v", res)
	}
}
