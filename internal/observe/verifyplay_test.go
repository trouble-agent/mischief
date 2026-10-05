package observe

import (
	"io"
	"reflect"
	"strings"
	"testing"
)

// verifyplay_test.go — the v0.2 refusal contract: verify-play fails
// closed under every option combination and names what is absent. The
// --allow-real arm is pinned specifically: arming the L5 real-provider
// plane must NOT be readable as "verify-play is allowed".

// TestVerifyPlayFailsClosedAlways: the refusal fires with and without a
// complete request, with and without --allow-real. Zero combinations pass.
func TestVerifyPlayFailsClosedAlways(t *testing.T) {
	complete := VerifyPlayRequest{PlayID: "TRBL-031-play-2", Fault: "I-002", Target: "container:trouble-hub"}
	for _, tc := range []struct {
		name string
		req  VerifyPlayRequest
	}{
		{"empty request", VerifyPlayRequest{}},
		{"complete request", complete},
		{"complete + allow-real", func() VerifyPlayRequest { r := complete; r.AllowReal = true; return r }()},
		{"allow-real alone", VerifyPlayRequest{AllowReal: true}},
	} {
		err := VerifyPlay(tc.req)
		if err == nil {
			t.Fatalf("%s: VerifyPlay returned nil — verify-play must fail closed in v0.1", tc.name)
		}
		for _, want := range []string{"refused", "v0.2", "re-injection engine does not exist"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: refusal %q does not name %q", tc.name, err, want)
			}
		}
		if !strings.Contains(err.Error(), ErrVerifyPlayV02.Error()) {
			t.Errorf("%s: refusal %q does not carry the sentinel", tc.name, err)
		}
	}
}

// TestVerifyPlayAllowRealNamed: the refusal explicitly says --allow-real
// does not provide the missing pieces — the flag must never be readable
// as an unlock for the missing engine.
func TestVerifyPlayAllowRealNamed(t *testing.T) {
	err := VerifyPlay(VerifyPlayRequest{PlayID: "p", Fault: "I-002", Target: "host:x", AllowReal: true})
	if err == nil {
		t.Fatal("nil error with --allow-real")
	}
	if !strings.Contains(err.Error(), "--allow-real") || !strings.Contains(err.Error(), "does not provide") {
		t.Errorf("refusal %q must name what --allow-real does and does not do", err)
	}
}

// TestVerifyPlayMalformedRequestNamesFields: a request missing fields is
// refused for ITS reason first (the v0.2 contract's input shape is
// exercised even by the refusal).
func TestVerifyPlayMalformedRequestNamesFields(t *testing.T) {
	err := VerifyPlay(VerifyPlayRequest{AllowReal: true})
	if err == nil {
		t.Fatal("nil error for an empty request")
	}
	for _, field := range []string{"play_id", "fault", "target"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("refusal %q does not name missing field %q", err, field)
		}
	}
}

// TestLedgerSurfaceIsReadOnlyStructurally: the compile-time surface check
// lives with the read-only doctrine — the Ledger type must expose no
// method whose name suggests a write (belt to the census's braces; a
// future mutator added by accident fails here AND in the census test).
func TestLedgerSurfaceIsReadOnlyStructurally(t *testing.T) {
	lv := reflect.TypeOf(Ledger{})
	for i := 0; i < lv.NumMethod(); i++ {
		name := strings.ToLower(lv.Method(i).Name)
		for _, bad := range []string{"post", "put", "patch", "delete", "write", "append", "create", "file", "push", "update", "store"} {
			if strings.Contains(name, bad) {
				t.Errorf("Ledger.%s exists — the read-only client must expose no mutator", lv.Method(i).Name)
			}
		}
	}
}

// readAll exists so the census handler can drain the request body without
// importing io/ioutil shapes inline.
func readAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}
