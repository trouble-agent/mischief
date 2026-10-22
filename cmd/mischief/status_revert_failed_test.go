package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/trouble-agent/mischief/internal/reverter"
)

// MISCHIEF-DF-54-R2: the dogfood residual DF-54 left behind. A hold whose
// measured revert FAILED (fault still landed) went dark on `status`: the
// default listing read `holds: 0` and the verb exited 0, while the daemon's
// health.json — same run dir, same journal — read ok:false / revert_failed:1.
// A script gating on the exit code graded a host with a possibly-live fault
// as clean. The residuals were two:
//
//	(1) the text/exit surface carried no revert_failed signal at all;
//	(2) status --json published a POSITIVE expiry for a terminal row (so a
//	    consumer deriving remaining TTL read "still live" for a finished
//	    hold) and rendered "holds": null for the empty active set.
//
// The fix is ONE liveness rule in the fold: Status.RevertFailedHolds (the
// terminal revert_failed rows), collected on every fold mode, surfaced in
// text (`revert_failed: N hold_ids: …`), JSON (revert_failed_holds), and
// the exit code (non-zero) — the same rows the daemon grades ok:false
// from. StatusAll/StatusSnapshotAll carry the same set, so no listing mode
// can disagree with the daemon about a possibly-live fault.

// failCLIHold arms a hold, breaks its undo after the arm, and reverts it
// through the verb so the fold grades the hold terminal revert_failed with
// the fault still landed. Returns the run dir, hold id and the live/backup
// paths (the backup is GONE — the sabotage is the failure's cause, and the
// recovery leg recreates it by path). Mirrors failOneHold in
// revert_retry_test.go.
func failCLIHold(t *testing.T, faultID string) (dir, id, file, backup string) {
	t.Helper()
	h, live, bak := scratchCLIHold(t, faultID)
	dir = t.TempDir()
	id = armCLIHolds(t, dir, h)[0]
	if err := os.Remove(bak); err != nil {
		t.Fatalf("sabotage backup after arm: %v", err)
	}
	code, out := captureStdout(t, func() int {
		return run([]string{"revert", "--hold", id, "--dir", dir})
	})
	if code != 1 || !strings.Contains(out, "outcome=revert_failed") {
		t.Fatalf("first revert of a sabotaged hold: exit %d out=%q, want exit 1 with outcome=revert_failed", code, out)
	}
	return dir, id, live, bak
}

// TestStatusReportsRevertFailedAndExitsNonzero (AC-1): the default status
// surface cannot read a fault-may-be-live run dir as clean. The failed
// hold stays OFF the active listing (its row is --all's surface — the
// DF-54 contract is unchanged), but the failure is carried as a count with
// the hold ids, the proof history still names the failed attempt, a stderr
// line points at the recovery verb, and the verb EXITS 1 — in both
// rendering modes — the same grade the revert verb returns when a proof
// grades revert_failed. Text and JSON grades ride the same rule
// (Status.RevertFailedHolds), so they cannot disagree with each other —
// and the daemon grades the SAME rows ok:false from the SAME journal, so
// neither can disagree with health.json.
func TestStatusReportsRevertFailedAndExitsNonzero(t *testing.T) {
	writeSanctionMarker(t)
	dir, id, _, backup := failCLIHold(t, "DF54R2-LIVE")

	t.Run("text_exit_nonzero_and_counter", func(t *testing.T) {
		code, out, errOut := runCaptureBoth(t, "status", "--dir", dir)
		if code != 1 {
			t.Fatalf("status over a revert_failed hold: exit %d, want 1 (out=%q)", code, out)
		}
		if !strings.Contains(out, "holds: 0") {
			t.Errorf("the active listing changed: %q (want holds: 0 — the DF-54 contract)", out)
		}
		if !strings.Contains(out, "revert_failed: 1") {
			t.Errorf("the default surface carries no revert_failed count: %q", out)
		}
		if !strings.Contains(out, "hold_ids: "+id) {
			t.Errorf("the counter does not name the failed hold's id: %q", out)
		}
		if !strings.Contains(out, " "+string(reverter.OutcomeRevertFailed)+" ") {
			t.Errorf("the failed proof is not in the history: %q", out)
		}
		if !strings.Contains(errOut, "--retry") {
			t.Errorf("the stderr pointer does not name the recovery verb: %q", errOut)
		}
		// --all carries the SAME count and exit grade (one rule, both
		// listing modes) plus the terminal row itself.
		code, out, _ = runCaptureBoth(t, "status", "--all", "--dir", dir)
		if code != 1 {
			t.Errorf("status --all over a revert_failed hold: exit %d, want 1 (out=%q)", code, out)
		}
		if !strings.Contains(out, "revert_failed: 1") || !strings.Contains(out, string(reverter.StatusRevertFailed)) {
			t.Errorf("status --all lost the count or the row: %q", out)
		}
	})

	t.Run("json_exit_nonzero_and_document", func(t *testing.T) {
		code, out := captureStdout(t, func() int { return run([]string{"status", "--json", "--dir", dir}) })
		if code != 1 {
			t.Fatalf("status --json over a revert_failed hold: exit %d, want 1\n%s", code, out)
		}
		doc := decodeJSONObject(t, out)
		rf, ok := doc["revert_failed_holds"].([]any)
		if !ok {
			t.Fatalf("revert_failed_holds is not an array: %#v", doc["revert_failed_holds"])
		}
		if len(rf) != 1 {
			t.Fatalf("revert_failed_holds: %d rows, want 1: %s", len(rf), out)
		}
		row, ok := rf[0].(map[string]any)
		if !ok {
			t.Fatalf("revert_failed_holds[0] is not an object: %#v", rf[0])
		}
		if row["hold_id"] != id || row["status"] != string(reverter.StatusRevertFailed) {
			t.Errorf("revert_failed_holds row: got hold_id=%#v status=%#v, want %s/revert_failed", row["hold_id"], row["status"], id)
		}
		if p, present := row["expiry"]; !present || p != nil {
			t.Errorf("a revert_failed row's expiry: got %#v (present=%v), want null — a consumer deriving ttl from it reads still-live for a finished hold", row["expiry"], present)
		}
	})

	// The recovery path empties the signal: after healing the cause and a
	// successful --retry, status exits 0 with no counter — the liveness
	// signal tracks the fold (the LATEST measured outcome), not the
	// history.
	t.Run("retry_recovers_to_clean", func(t *testing.T) {
		if err := os.WriteFile(backup, []byte("GOOD-CONTENT\n"), 0o644); err != nil {
			t.Fatalf("heal backup: %v", err)
		}
		code, out := captureStdout(t, func() int {
			return run([]string{"revert", "--hold", id, "--retry", "--dir", dir})
		})
		if code != 0 || !strings.Contains(out, "outcome=reverted") {
			t.Fatalf("retry of a healed hold: exit %d out=%q, want 0 with outcome=reverted", code, out)
		}
		code, out = captureStdout(t, func() int { return run([]string{"status", "--dir", dir}) })
		if code != 0 {
			t.Fatalf("status after a successful retry: exit %d, want 0\n%s", code, out)
		}
		if strings.Contains(out, "revert_failed:") {
			t.Errorf("the counter outlived the fold: %q (a successful retry re-folds the hold to reverted)", out)
		}
		if !strings.Contains(out, "holds: 0") || !strings.Contains(out, "proofs: 2") {
			t.Errorf("the clean status lost its shape after the retry: %q", out)
		}
		code, out = captureStdout(t, func() int { return run([]string{"status", "--json", "--dir", dir}) })
		if code != 0 {
			t.Fatalf("status --json after a successful retry: exit %d, want 0\n%s", code, out)
		}
		doc := decodeJSONObject(t, out)
		if rf, ok := doc["revert_failed_holds"].([]any); !ok || len(rf) != 0 {
			t.Errorf("revert_failed_holds after recovery: %#v, want []", doc["revert_failed_holds"])
		}
	})
}

// TestStatusJSONEmptyActiveSetIsNotNull (AC-2): the healthy state renders
// "holds": [] — a null would break `jq '.holds[]'` and every consumer that
// iterates the active set without a null branch. The same rule covers the
// other top-level collections.
func TestStatusJSONEmptyActiveSetIsNotNull(t *testing.T) {
	dir := t.TempDir()
	code, out := captureStdout(t, func() int { return run([]string{"status", "--json", "--dir", dir}) })
	if code != 0 {
		t.Fatalf("status --json on an empty run dir: exit %d, want 0\n%s", code, out)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("output is not a JSON object: %v", err)
	}
	for _, key := range []string{"holds", "revert_failed_holds", "history"} {
		raw, ok := doc[key]
		if !ok {
			t.Errorf("key %q absent", key)
			continue
		}
		if s := string(raw); s != "[]" {
			t.Errorf("healthy state renders %q as %s, want [] (DF-54-R2: not null)", key, s)
		}
	}
}

// TestStatusJSONTerminalRowHasNoPositiveExpiry (AC-3): a terminal hold must
// not publish a POSITIVE expiry countdown. The key stays (the key surface
// is fixed — the DF-53 contract), the VALUE is null for a non-active row,
// so `expiry - now` can never read "still live" for a finished hold. The
// control rows prove the null rule is status-gated, not absolute.
func TestStatusJSONTerminalRowHasNoPositiveExpiry(t *testing.T) {
	utc := time.Now().UTC().Truncate(time.Second)
	b, err := json.Marshal(reverter.Status{
		BootID: "boot-df54r2",
		Holds: []reverter.HoldStatusEntry{
			// The terminal row the residual showed: reverted, WITH the
			// arm-time expiry still recorded in the fold — pre-fix this
			// marshalled the future timestamp (expiry - now = +minutes).
			{HoldID: "h-reverted", FaultID: "S-001", Target: "scratch:/x", Status: reverter.StatusReverted, Expiry: utc},
			// A terminal revert_failed row: same terminal gate.
			{HoldID: "h-failed", FaultID: "S-003", Target: "scratch:/z", Status: reverter.StatusRevertFailed, Expiry: utc},
			// The control: a live row keeps its expiry.
			{HoldID: "h-armed", FaultID: "S-002", Target: "scratch:/y", Status: reverter.StatusArmed, Expiry: utc},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc struct {
		Holds []struct {
			HoldID string          `json:"hold_id"`
			Status string          `json:"status"`
			Expiry json.RawMessage `json:"expiry"`
		} `json:"holds"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(doc.Holds) != 3 {
		t.Fatalf("holds: %d rows, want 3", len(doc.Holds))
	}
	byID := map[string]struct {
		Status string
		Expiry json.RawMessage
	}{}
	for _, r := range doc.Holds {
		byID[r.HoldID] = struct {
			Status string
			Expiry json.RawMessage
		}{r.Status, r.Expiry}
	}
	for _, tc := range []struct {
		id, status string
		wantNull   bool
	}{
		{"h-reverted", "reverted", true},
		{"h-failed", "revert_failed", true},
		{"h-armed", "armed", false},
	} {
		got := byID[tc.id]
		if got.Status != tc.status {
			t.Errorf("%s status: got %q, want %q", tc.id, got.Status, tc.status)
		}
		if tc.wantNull && string(got.Expiry) != "null" {
			t.Errorf("%s (%s) publishes expiry %s (DF-54-R2: the countdown must not survive the revert)", tc.id, tc.status, string(got.Expiry))
		}
		if !tc.wantNull && got.Expiry == nil || !tc.wantNull && string(got.Expiry) == "null" {
			t.Errorf("%s (armed) lost its expiry — the null rule must be status-gated, not absolute", tc.id)
		}
	}
	if !strings.Contains(string(b), utc.Format(time.RFC3339)) {
		t.Errorf("the armed control row's expiry vanished from the document entirely: %s", string(b))
	}
}

// TestEndToEndTerminalRowExpiryNullWithAll (AC-3's live path): a hold
// reverted BEFORE its TTL — the residual's exact repro — reads back on
// `status --json --all` with expiry null, not the future timestamp.
func TestEndToEndTerminalRowExpiryNullWithAll(t *testing.T) {
	writeSanctionMarker(t)
	dir := t.TempDir()
	id := statusArm(t, dir, "S-004") // a 10s TTL; the revert lands well before it
	code, out := captureStdout(t, func() int { return run([]string{"revert", "--hold", id, "--dir", dir}) })
	if code != 0 {
		t.Fatalf("revert --hold exit: %d, want 0 (out=%q)", code, out)
	}
	code, out = captureStdout(t, func() int { return run([]string{"status", "--json", "--all", "--dir", dir}) })
	if code != 0 {
		t.Fatalf("status --json --all exit: %d, want 0\n%s", code, out)
	}
	var doc struct {
		Holds []struct {
			HoldID string          `json:"hold_id"`
			Status string          `json:"status"`
			Expiry json.RawMessage `json:"expiry"`
		} `json:"holds"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, out)
	}
	if len(doc.Holds) != 1 || doc.Holds[0].HoldID != id {
		t.Fatalf("holds: %+v, want exactly the reverted %s", doc.Holds, id)
	}
	if string(doc.Holds[0].Expiry) != "null" {
		t.Errorf("a reverted-before-TTL hold publishes expiry %s (the residual: a positive countdown for a finished hold)", string(doc.Holds[0].Expiry))
	}
}
