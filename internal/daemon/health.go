package daemon

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// This file owns the daemon's OWN boot identity — the health.json
// `daemon_boot` value (MISCHIEF-DF-32). It must never be derived from
// internal/reverter's fold id: that one is deliberately HOST-boot-scoped
// (journal.go bootIdentity adopts boot.json's marker for every process on
// the same host boot), so a health document written by a dead daemon and
// one written by its restart would carry byte-identical values and a
// consumer keying on daemon_boot could not tell them apart. The daemon's
// own id is minted fresh at every process start — one restart, one new
// value — which is exactly the freshness key AC 1–3 of DF-32 ask for.

// daemonBootIDFunc is the seam behind daemonBootID: a var so tests can
// inject a deterministic generator and prove health.json carries the
// DAEMON's own id rather than any fold- or host-scoped value.
var daemonBootIDFunc = randomDaemonBootID

// daemonBootID returns this daemon process's boot id: hex16, the same
// shape as the reverter's ids, but scoped to the PROCESS (a fresh value
// per daemon start), not the host boot.
func daemonBootID() string { return daemonBootIDFunc() }

// randomDaemonBootID derives a fresh random id: hex(sha256(32 random
// bytes))[:16] — the same derivation as the reverter's randomBootID, so
// both id families stay visually and structurally comparable.
func randomDaemonBootID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("daemon: boot id: %v", err))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8]) // [:16] hex chars = first 8 bytes
}
