package reverter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// osOpenAppend opens a file for appending (the corrupt-line tests write
// behind the store's back).
func osOpenAppend(dir, name string) (*os.File, error) {
	return os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_APPEND, 0o600)
}

// TestRecordRoundTrip: every record type serialises to one valid JSON line
// and reads back with its typed fields intact (the journal is the evidence;
// a round trip that loses fields manufactures evidence).
func TestRecordRoundTrip(t *testing.T) {
	cases := []record{
		{
			Type:   RecordArm,
			HoldID: "abc123",
			TS:     1791000000,
			BootID: "boot-x",
			Payload: &armPayload{
				FaultID: "P-1",
				Target:  "file:/tmp/x",
				Inverse: Decl{Kind: "restore-file", Params: map[string]string{"path": "/tmp/x", "backup": "/tmp/b"}},
				Check:   Decl{Kind: "file-matches-backup", Params: map[string]string{"path": "/tmp/x", "backup": "/tmp/b"}},
				TTL:     durationNanos(time.Minute),
				BootID:  "boot-x",
				ArmPID:  4242,
			},
		},
		{Type: RecordLand, HoldID: "abc123", TS: 1791000001, BootID: "boot-x", OwnPID: 4242, Fields: map[string]string{"landed": "1"}},
		{Type: RecordLandRefused, HoldID: "zzz", TS: 1791000002, BootID: "boot-x", Fields: map[string]string{"reason": "write-ahead arm line absent"}},
		{Type: RecordHoldExpired, HoldID: "abc123", TS: 1791000003, BootID: "boot-y", OwnPID: 777, Fields: map[string]string{"mode": "setsid"}},
		{
			Type:    RecordRevertProof,
			HoldID:  "abc123",
			TS:      1791000004,
			BootID:  "boot-y",
			OwnPID:  777,
			Outcome: OutcomeReverted,
			Fields: map[string]string{
				"fault_id":       "P-1",
				"detail":         "check: same",
				"role":           "owner",
				"reason":         "ttl_expiry",
				"duration_nanos": "1500000",
			},
		},
		{Type: RecordHoldStuck, HoldID: "abc123", TS: 1791000005, BootID: "boot-y", Fields: map[string]string{"fault_id": "P-1", "previous_boot": "1"}},
	}
	for _, rec := range cases {
		b, err := rec.encode()
		if err != nil {
			t.Fatalf("%s: encode: %v", rec.Type, err)
		}
		if strings.Contains(string(b), "\n") {
			t.Fatalf("%s: line contains a raw newline: %s", rec.Type, b)
		}
		var back record
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("%s: unmarshal: %v", rec.Type, err)
		}
		if back.Type != rec.Type || back.HoldID != rec.HoldID || back.BootID != rec.BootID || back.OwnPID != rec.OwnPID {
			t.Fatalf("%s: envelope drift: %+v vs %+v", rec.Type, back, rec)
		}
		if rec.Outcome != "" && back.Outcome != rec.Outcome {
			t.Fatalf("%s: outcome drift: %s vs %s", rec.Type, back.Outcome, rec.Outcome)
		}
		if rec.Payload != nil {
			if back.Payload == nil {
				t.Fatalf("%s: payload lost", rec.Type)
			}
			if back.Payload.FaultID != rec.Payload.FaultID || back.Payload.BootID != rec.Payload.BootID {
				t.Fatalf("%s: payload drift: %+v", rec.Type, back.Payload)
			}
			if back.Payload.Inverse.Kind != rec.Payload.Inverse.Kind || back.Payload.Inverse.Params["path"] != rec.Payload.Inverse.Params["path"] {
				t.Fatalf("%s: payload inverse drift: %+v", rec.Type, back.Payload.Inverse)
			}
			if back.Payload.TTL != rec.Payload.TTL {
				t.Fatalf("%s: payload ttl drift: %d vs %d", rec.Type, back.Payload.TTL, rec.Payload.TTL)
			}
		}
		if len(rec.Fields) > 0 {
			for k, v := range rec.Fields {
				if back.Fields[k] != v {
					t.Fatalf("%s: field %s drift: %q vs %q", rec.Type, k, back.Fields[k], v)
				}
			}
		}
	}
}

// TestRecordWritePathValidation: the closed vocabularies and payload rules
// are enforced at the WRITE path — an undeclared value cannot enter a
// journal through this package.
func TestRecordWritePathValidation(t *testing.T) {
	cases := []struct {
		name string
		rec  record
		want string
	}{
		{"unknown type", record{Type: "surprise", HoldID: "h", BootID: "b"}, "unknown record type"},
		{"missing hold", record{Type: RecordLand, BootID: "b"}, "hold_id: missing"},
		{"missing boot", record{Type: RecordLand, HoldID: "h"}, "boot_id: missing"},
		{"arm without payload", record{Type: RecordArm, HoldID: "h", BootID: "b"}, "requires the write-ahead payload"},
		{"payload on non-arm", record{Type: RecordLand, HoldID: "h", BootID: "b", Payload: &armPayload{FaultID: "f", TTL: 1}}, "does not carry a payload"},
		{"outcome on non-proof", record{Type: RecordLand, HoldID: "h", BootID: "b", Outcome: OutcomeReverted}, "does not carry an outcome"},
		{"undeclared outcome", record{Type: RecordRevertProof, HoldID: "h", BootID: "b", Outcome: "kind-of-reverted"}, "not in the closed vocabulary"},
	}
	for _, tc := range cases {
		_, err := tc.rec.encode()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err=%v, want %q", tc.name, err, tc.want)
		}
	}
}

// TestReadSideRejectsCorruptLines: the fold refuses (loudly) an unknown
// record type or an out-of-vocabulary outcome — a foreign or corrupted line
// must never be silently skipped (fail closed).
func TestReadSideRejectsCorruptLines(t *testing.T) {
	dir := localTempDir(t)
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = st.Close()

	// append a foreign line behind the store's back
	f, err := osOpenAppend(dir, journalFileName)
	if err != nil {
		t.Fatalf("append open: %v", err)
	}
	if _, err := f.WriteString(`{"type":"surprise","hold_id":"h","ts":1,"boot_id":"b"}` + "\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = f.Close()
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "unknown record type") {
		t.Fatalf("fold accepted an unknown record type: %v", err)
	}

	// and an out-of-vocabulary outcome
	dir2 := localTempDir(t)
	st2, err := Open(dir2)
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	_ = st2.Close()
	f2, err := osOpenAppend(dir2, journalFileName)
	if err != nil {
		t.Fatalf("append open2: %v", err)
	}
	if _, err := f2.WriteString(`{"type":"revert_proof","hold_id":"h","ts":1,"boot_id":"b","outcome":"meh"}` + "\n"); err != nil {
		t.Fatalf("write2: %v", err)
	}
	_ = f2.Close()
	if _, err := Open(dir2); err == nil || !strings.Contains(err.Error(), "closed vocabulary") {
		t.Fatalf("fold accepted an undeclared outcome: %v", err)
	}
}

// TestProofFromRecord: the proof is rebuilt from the journal line with its
// measured numbers intact (duration round-trips through the text form).
func TestProofFromRecord(t *testing.T) {
	rec := record{
		Type:    RecordRevertProof,
		HoldID:  "h1",
		TS:      1791000009,
		BootID:  "b1",
		OwnPID:  999,
		Outcome: OutcomeRevertFailed,
		Fields: map[string]string{
			"fault_id":       "P-9",
			"target":         "file:/t",
			"detail":         "inverse failed: read backup; check: differ",
			"role":           "kill_switch",
			"reason":         "kill_switch",
			"duration_nanos": "250000000",
		},
	}
	p := proofFromRecord(rec)
	if p.FaultID != "P-9" || p.Target != "file:/t" || p.Outcome != OutcomeRevertFailed {
		t.Fatalf("proof identity drift: %+v", p)
	}
	if p.Role != RoleKillSwitch || p.PID != 999 || p.Reason != "kill_switch" {
		t.Fatalf("provenance drift: %+v", p)
	}
	if p.Duration != 250*time.Millisecond {
		t.Fatalf("duration drift: %v", p.Duration)
	}
	if !strings.Contains(p.Detail, "inverse failed") {
		t.Fatalf("measured detail lost: %q", p.Detail)
	}
}
