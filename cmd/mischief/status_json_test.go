package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/trouble-agent/mischief/internal/reverter"
)

// TestStatusJSONSurface (MISCHIEF-DF-53): `status --json` is a machine
// surface — a script reads it with `status --json | jq '.holds[0].hold_id'` —
// and it shipped the Go field names (BootID/Holds/HoldID/FaultID/Target/
// Status/Expiry/StuckAt/LandPID/OwnPID) while plan --json (DF-26) and
// doctor --json are snake_case, and an unset StuckAt rendered as
// "0001-01-01T00:00:00Z" so a consumer could not tell "no stuck time" from a
// real timestamp.
//
// This single test enumerates the whole JSON key surface of the verb — every
// level (top-level, one hold row, one proof row) — together with the
// timestamp rules, driving the REAL verbs through run() so the contract is
// proven on the surface a consumer sees, not on a struct literal alone:
//
//	(1) an empty run dir: the top-level key set is stable, nothing PascalCase;
//	(2) arm → one hold row: exact snake_case keys, stuck_at null, expiry RFC3339;
//	(3) revert --hold → one proof row on the same contract;
//	(4) the unset-timestamp rule on BOTH timestamp fields at unit level (an
//	    armed hold always carries an expiry, so the real-verb legs can only
//	    reach the stuck_at half) plus the positive control that a set expiry
//	    is still a timestamp — the nulls are not "everything is null".
func TestStatusJSONSurface(t *testing.T) {
	writeSanctionMarker(t)
	dir := t.TempDir()

	// ── (1) empty run dir ────────────────────────────────────────────────
	code, out := captureStdout(t, func() int { return run([]string{"status", "--json", "--dir", dir}) })
	if code != 0 {
		t.Fatalf("status --json on an empty run dir: exit %d, want 0\n%s", code, out)
	}
	empty := decodeJSONObject(t, out)
	// MISCHIEF-DF-54-R2: the top level gained revert_failed_holds (the
	// liveness signal) — always present, [] when empty (the empty run dir
	// proves the non-null rule right here).
	assertKeys(t, "empty status --json", empty, []string{"boot_id", "history", "holds", "revert_failed_holds"})
	assertNoPascalCase(t, "empty status --json", empty)
	if holds, ok := empty["holds"].([]any); !ok {
		t.Errorf("empty status --json holds: got %#v, want a JSON array (DF-54-R2: [] not null)", empty["holds"])
	} else if len(holds) != 0 {
		t.Errorf("empty status --json holds: got %d rows, want 0", len(holds))
	}
	if rf, ok := empty["revert_failed_holds"].([]any); !ok {
		t.Errorf("empty status --json revert_failed_holds: got %#v, want a JSON array (DF-54-R2: [] not null)", empty["revert_failed_holds"])
	} else if len(rf) != 0 {
		t.Errorf("empty status --json revert_failed_holds: got %d rows, want 0", len(rf))
	}

	// ── (2) arm a hold through the real verb ─────────────────────────────
	code, out = captureStdout(t, func() int { return run(armTestArgs(t, dir)) })
	if code != 0 {
		t.Fatalf("arm: exit %d, want 0\n%s", code, out)
	}
	fields := strings.Fields(out)
	if len(fields) < 2 || fields[0] != "armed" {
		t.Fatalf("arm output does not start with the hold id: %q", out)
	}
	holdID := fields[1]

	code, out = captureStdout(t, func() int { return run([]string{"status", "--json", "--dir", dir}) })
	if code != 0 {
		t.Fatalf("status --json after arm: exit %d, want 0\n%s", code, out)
	}
	doc := decodeJSONObject(t, out)
	// DF-54-R2: revert_failed_holds joined the top level (the liveness
	// signal); an armed hold is live but NOT revert-failed, so it stays [].
	assertKeys(t, "status --json", doc, []string{"boot_id", "history", "holds", "revert_failed_holds"})
	assertNoPascalCase(t, "status --json after arm", doc)

	holds, ok := doc["holds"].([]any)
	if !ok || len(holds) != 1 {
		t.Fatalf("holds: got %#v, want exactly one armed hold row", doc["holds"])
	}
	hold, ok := holds[0].(map[string]any)
	if !ok {
		t.Fatalf("holds[0] is not a JSON object: %#v", holds[0])
	}
	// The acceptance's key list, hold row half.
	assertKeys(t, "status --json hold row", hold, []string{
		"expiry", "fault_id", "hold_id", "land_pid", "own_pid", "status", "stuck_at", "target",
	})
	// The values are the live ones, not placeholders: keys being right is
	// worthless if they carry nothing.
	if hold["hold_id"] != holdID {
		t.Errorf("hold_id: got %#v, want %q (the id arm printed)", hold["hold_id"], holdID)
	}
	if hold["fault_id"] != "S-001" {
		t.Errorf("fault_id: got %#v, want \"S-001\"", hold["fault_id"])
	}
	if hold["status"] != "armed" {
		t.Errorf("status: got %#v, want \"armed\"", hold["status"])
	}
	// The DF's second half: an unset timestamp is null, not year 1.
	stuck, present := hold["stuck_at"]
	if !present {
		t.Errorf("stuck_at key is absent from a hold row (a fixed key surface is what consumers pin)")
	}
	if stuck != nil {
		t.Errorf("stuck_at on a live hold: got %#v, want null (DF-53: the year-1 rendering)", stuck)
	}
	exp, ok := hold["expiry"].(string)
	if !ok {
		t.Fatalf("expiry: got %#v, want an RFC3339 string (an armed hold has a TTL)", hold["expiry"])
	}
	if _, err := time.Parse(time.RFC3339, exp); err != nil {
		t.Errorf("expiry %q is not RFC3339: %v", exp, err)
	}

	// ── (3) revert the hold: the proof row rides the same contract ───────
	code, out = captureStdout(t, func() int { return run([]string{"revert", "--hold", holdID, "--dir", dir}) })
	if code != 0 {
		t.Fatalf("revert --hold %s: exit %d, want 0\n%s", holdID, code, out)
	}
	code, out = captureStdout(t, func() int { return run([]string{"status", "--json", "--dir", dir}) })
	if code != 0 {
		t.Fatalf("status --json after revert: exit %d, want 0\n%s", code, out)
	}
	doc = decodeJSONObject(t, out)
	assertNoPascalCase(t, "status --json after revert", doc)
	history, ok := doc["history"].([]any)
	if !ok || len(history) != 1 {
		t.Fatalf("history: got %#v, want exactly one proof row", doc["history"])
	}
	proof, ok := history[0].(map[string]any)
	if !ok {
		t.Fatalf("history[0] is not a JSON object: %#v", history[0])
	}
	assertKeys(t, "status --json proof row", proof, []string{
		"detail", "duration_nanos", "fault_id", "hold_id", "outcome", "pid", "reason", "role", "target",
	})
	if proof["outcome"] != "reverted" || proof["role"] != "api" || proof["reason"] != "direct" {
		t.Errorf("proof row: got outcome=%#v role=%#v reason=%#v, want reverted/api/direct",
			proof["outcome"], proof["role"], proof["reason"])
	}
	if n, ok := proof["duration_nanos"].(float64); !ok || n <= 0 {
		t.Errorf("duration_nanos: got %#v, want the measured duration in nanoseconds", proof["duration_nanos"])
	}

	// ── (4) the timestamp rule on both fields, and the positive control ──
	utc := time.Now().UTC().Truncate(time.Second)
	b, err := json.Marshal(reverter.Status{
		BootID: "boot-unit",
		Holds: []reverter.HoldStatusEntry{
			// row 0: both timestamps unset (a stuck-less, expiry-less fold).
			{HoldID: "h-unset", FaultID: "S-001", Target: "scratch:/x", Status: reverter.StatusArmed},
			// row 1: expiry SET — the control proving the null rule is not
			// "timestamps never render".
			{HoldID: "h-set", FaultID: "S-001", Target: "scratch:/x", Status: reverter.StatusArmed, Expiry: utc},
		},
		History: []reverter.Proof{{
			HoldID: "h-unset", FaultID: "S-001", Target: "scratch:/x",
			Outcome: reverter.OutcomeReverted, Detail: "check: same",
			Duration: 118920 * time.Nanosecond, Role: reverter.RoleAPI, PID: 42, Reason: "direct",
		}},
	})
	if err != nil {
		t.Fatalf("marshal a status with unset timestamps: %v", err)
	}
	unit := decodeJSONObject(t, string(b))
	assertNoPascalCase(t, "unit status --json", unit)
	uholds, ok := unit["holds"].([]any)
	if !ok || len(uholds) != 2 {
		t.Fatalf("unit holds: got %#v, want two rows", unit["holds"])
	}
	unset, _ := uholds[0].(map[string]any)
	if v, present := unset["expiry"]; !present || v != nil {
		t.Errorf("unset expiry: got %#v (present=%v), want null", v, present)
	}
	if v, present := unset["stuck_at"]; !present || v != nil {
		t.Errorf("unset stuck_at: got %#v (present=%v), want null", v, present)
	}
	set, _ := uholds[1].(map[string]any)
	if got, ok := set["expiry"].(string); !ok || got != utc.Format(time.RFC3339) {
		t.Errorf("set expiry: got %#v, want %q", set["expiry"], utc.Format(time.RFC3339))
	}
	if got, ok := set["stuck_at"]; !ok || got != nil {
		t.Errorf("set-row stuck_at: got %#v, want null (only expiry is set on this row)", got)
	}
}

// TestStatusJSONAbsentRunDir (MISCHIEF-DF-61): `status --json --dir <absent>`
// is a machine surface — a script runs `status --json | jq -e .state` and
// consumes the exit code — but the DF-60 absent guard predates the --json
// flag and prints its TEXT refusal (`run_dir:` + `state: absent`) before the
// JSON renderer ever runs, so the pipe fed jq two text lines and failed.
// The graded contract:
//
//	(1) absent emits ONE parseable JSON object naming run_dir and
//	    state="absent" — nothing is created;
//	(2) the exit code is the SAME as text mode (1) — the state does not
//	    change grade because the consumer asked for a different rendering;
//	(3) the present-dir --json surface is untouched: present-empty keeps
//	    boot_id/holds/history with NO state key, present-with-holds keeps
//	    the armed hold row;
//	(4) the early error path (a torn journal at Open) stays fail-closed in
//	    JSON mode too: exit 1, the diagnostic on stderr, stdout EMPTY — a
//	    partial/foreign document must never masquerade as a status object.
func TestStatusJSONAbsentRunDir(t *testing.T) {
	t.Run("absent_emits_one_json_object", func(t *testing.T) {
		absent := filepath.Join(t.TempDir(), "rotated-run")
		code, out, errOut := runCaptureBoth(t, "status", "--json", "--dir", absent)
		if code != 1 {
			t.Fatalf("status --json on an absent run dir: exit %d, want 1\nstdout=%s\nstderr=%s", code, out, errOut)
		}
		doc := decodeJSONObject(t, out) // ONE object — two text lines fail here (the DF)
		if got := doc["run_dir"]; got != absent {
			t.Errorf("run_dir: got %#v, want %q", got, absent)
		}
		if got := doc["state"]; got != "absent" {
			t.Errorf("state: got %#v, want \"absent\"", got)
		}
		assertNoPascalCase(t, "absent status --json", doc)
		assertStillAbsent(t, absent)
	})

	t.Run("exit_code_matches_text_mode", func(t *testing.T) {
		base := t.TempDir()
		textCode, textOut, _ := runCaptureBoth(t, "status", "--dir", filepath.Join(base, "text-run"))
		jsonCode, jsonOut, _ := runCaptureBoth(t, "status", "--json", "--dir", filepath.Join(base, "json-run"))
		if textCode != jsonCode || textCode != 1 {
			t.Fatalf("absent exit codes diverge by mode: text=%d json=%d, both want 1\ntext out=%s\njson out=%s", textCode, jsonCode, textOut, jsonOut)
		}
	})

	t.Run("present_empty_keeps_surface", func(t *testing.T) {
		dir := t.TempDir() // exists, empty
		code, out := captureStdout(t, func() int { return run([]string{"status", "--json", "--dir", dir}) })
		if code != 0 {
			t.Fatalf("status --json on a present-empty run dir: exit %d, want 0\n%s", code, out)
		}
		doc := decodeJSONObject(t, out)
		assertKeys(t, "present-empty status --json", doc, []string{"boot_id", "history", "holds", "revert_failed_holds"})
		if state, present := doc["state"]; present {
			t.Errorf("present-empty status --json grew a state key: %#v (state is the absent guard's key)", state)
		}
	})

	t.Run("present_with_holds_keeps_surface", func(t *testing.T) {
		writeSanctionMarker(t)
		dir := t.TempDir()
		code, armOut := captureStdout(t, func() int { return run(armTestArgs(t, dir)) })
		if code != 0 {
			t.Fatalf("arm: exit %d, want 0\n%s", code, armOut)
		}
		holdID := strings.Fields(armOut)[1]
		code, out := captureStdout(t, func() int { return run([]string{"status", "--json", "--dir", dir}) })
		if code != 0 {
			t.Fatalf("status --json over an armed hold: exit %d, want 0\n%s", code, out)
		}
		doc := decodeJSONObject(t, out)
		assertNoPascalCase(t, "present-with-holds status --json", doc)
		holds, ok := doc["holds"].([]any)
		if !ok || len(holds) != 1 {
			t.Fatalf("holds: got %#v, want exactly one armed hold row", doc["holds"])
		}
		if hold := holds[0].(map[string]any); hold["hold_id"] != holdID {
			t.Errorf("hold_id: got %#v, want %q", hold["hold_id"], holdID)
		}
	})

	t.Run("early_open_error_stays_fail_closed", func(t *testing.T) {
		dir := t.TempDir()
		torn := `{"seq":1,"hold_id":"torn-tail` + "\n"
		if err := os.WriteFile(filepath.Join(dir, "reverter.jsonl"), []byte(torn), 0o600); err != nil {
			t.Fatalf("write torn journal: %v", err)
		}
		code, out, errOut := runCaptureBoth(t, "status", "--json", "--dir", dir)
		if code != 1 {
			t.Fatalf("status --json over a torn journal: exit %d, want 1 (fail-closed)\nstdout=%s\nstderr=%s", code, out, errOut)
		}
		if out != "" {
			t.Fatalf("a refused open leaked stdout in JSON mode — a consumer would parse it as a status object:\n%s", out)
		}
		if !strings.Contains(errOut, "status:") || !strings.Contains(errOut, "journal") {
			t.Fatalf("stderr does not name the open failure:\n%s", errOut)
		}
	})
}

// decodeJSONObject decodes one output buffer into a JSON object, failing the
// test on a non-object document (the verb must emit an object, never a
// diagnostic string).
func decodeJSONObject(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("output is not a JSON object: %v\n%s", err, s)
	}
	return m
}

// assertKeys pins an object's EXACT key set (sorted): a new field must land
// here as a snake_case key or this fails loud, and a PascalCase leak shows up
// as both a missing and an unexpected entry.
func assertKeys(t *testing.T, label string, obj map[string]any, want []string) {
	t.Helper()
	got := make([]string, 0, len(obj))
	for k := range obj {
		got = append(got, k)
	}
	sort.Strings(got)
	sorted := append([]string(nil), want...)
	sort.Strings(sorted)
	if strings.Join(got, ",") != strings.Join(sorted, ",") {
		t.Fatalf("%s keys:\n got %v\nwant %v", label, got, sorted)
	}
}

// assertNoPascalCase walks a decoded document and fails on any key that
// starts with an uppercase letter — the DF's defect class (a Go field name
// reaching the wire), caught at every depth so a nested struct that skips the
// wire mirror cannot hide behind a clean top level.
func assertNoPascalCase(t *testing.T, label string, v any) {
	t.Helper()
	switch node := v.(type) {
	case map[string]any:
		for k, child := range node {
			if r := []rune(k); len(r) > 0 && unicode.IsUpper(r[0]) {
				t.Errorf("%s: PascalCase key %q leaked onto the wire", label, k)
			}
			assertNoPascalCase(t, label+"."+k, child)
		}
	case []any:
		for i, child := range node {
			assertNoPascalCase(t, fmt.Sprintf("%s[%d]", label, i), child)
		}
	}
}
