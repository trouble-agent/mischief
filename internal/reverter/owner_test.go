package reverter

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestParseOwnerArgsFlagsPrimaryEnvFallback: the argv flags are primary;
// the env carries the same facts as a fallback; a request with neither is
// an error naming the missing field (the owner request is validated before
// any journal is opened).
func TestParseOwnerArgsFlagsPrimaryEnvFallback(t *testing.T) {
	dir := localTempDir(t)
	hold := "abc123def4567890"
	expiry := time.Now().Add(time.Minute).Truncate(time.Second)

	// flags primary
	f, err := ParseOwnerArgs([]string{"-dir", dir, "-hold", hold, "-expiry", itoa64(expiry.UnixNano())})
	if err != nil {
		t.Fatalf("flags: %v", err)
	}
	if f.Dir != dir || f.HoldID != hold || !f.Expiry.Equal(expiry) {
		t.Fatalf("flags parsed wrong: %+v", f)
	}

	// env fallback (no flags)
	t.Setenv(ownerDirEnv, dir)
	t.Setenv(ownerHoldIDEnv, hold)
	t.Setenv(ownerExpiryEnv, itoa64(expiry.UnixNano()))
	f2, err := ParseOwnerArgs(nil)
	if err != nil {
		t.Fatalf("env fallback: %v", err)
	}
	if f2.Dir != dir || f2.HoldID != hold || !f2.Expiry.Equal(expiry) {
		t.Fatalf("env parsed wrong: %+v", f2)
	}

	// nothing at all: a loud refusal naming the first missing field
	t.Setenv(ownerDirEnv, "")
	t.Setenv(ownerHoldIDEnv, "")
	t.Setenv(ownerExpiryEnv, "")
	if _, err := ParseOwnerArgs(nil); err == nil || !strings.Contains(err.Error(), "dir") {
		t.Fatalf("empty request: err=%v, want a dir-missing refusal", err)
	}

	// unknown flag: refused by name
	if _, err := ParseOwnerArgs([]string{"-bogus", "x"}); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("unknown flag: err=%v, want refusal naming it", err)
	}
}

// TestDetachFailsLoudWithoutBinary: when the re-exec target cannot be
// resolved, Detach fails with ErrNoOwner — the CLI must never believe a
// detached owner exists when none does (the arm stays in-process and the
// refusal names the failure).
func TestDetachFailsLoudWithoutBinary(t *testing.T) {
	st, lf := newTestStore(t)
	t.Setenv(detachModeOverrideEnv, "setsid")
	hold, _, _ := scratchHold(t, "P-NOBIN")
	id, err := lf.Arm(hold)
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	// Point os.Executable at something impossible: the seam runs through
	// the PATH-independent executable name, so simulate by breaking the
	// detach function itself — the DEFAULT is exercised in the kill -9
	// battery; this pins the ERROR CONTRACT of the seam.
	lf.detach = func(o ownerRequest) (ownerResult, error) {
		return ownerResult{}, ErrNoOwner
	}
	if _, err := lf.Detach(id, hold.TTL); err == nil || !errorIs(err, ErrNoOwner) {
		t.Fatalf("detach: err=%v, want ErrNoOwner", err)
	}
	// no hold_expired record may exist for a failed detach
	for _, l := range journalLines(t, st.Dir) {
		if recordTypeOf(t, l) == string(RecordHoldExpired) {
			t.Fatalf("failed detach recorded an owner: %s", l)
		}
	}
}

// TestHoldExpiredRecordCarriesOwner: the hold_expired record names the
// spawn mode and the absolute expiry (the durable trace of WHO owns the
// TTL and UNTIL WHEN).
func TestHoldExpiredRecordCarriesOwner(t *testing.T) {
	st, lf := newTestStore(t)
	t.Setenv(detachModeOverrideEnv, "setsid")
	hold, file, _ := scratchHold(t, "P-EXPIRY-REC")
	hold.TTL = 45 * time.Second // killed by the test before it fires
	id, err := lf.Arm(hold)
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	res, err := lf.Detach(id, hold.TTL)
	if err != nil {
		t.Fatalf("detach: %v", err)
	}
	found := false
	for _, l := range journalLines(t, st.Dir) {
		if recordTypeOf(t, l) == string(RecordHoldExpired) {
			found = true
			if !strings.Contains(l, `"mode":"`+res.Mode+`"`) {
				t.Fatalf("expiry record lacks the mode: %s", l)
			}
			if !strings.Contains(l, "expiry_unix") {
				t.Fatalf("expiry record lacks the absolute expiry: %s", l)
			}
		}
	}
	if !found {
		t.Fatal("no hold_expired record")
	}
	// cleanup: end the hold and the owner (a revert within TTL is the
	// normal path; the killed owner then exits at wake with a reported
	// no-live-hold error).
	if _, err := lf.Revert(testContext(), id, "direct", RoleAPI); err != nil {
		t.Fatalf("revert: %v", err)
	}
	_ = os.Remove(file)
}
