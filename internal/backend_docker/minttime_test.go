package backend_docker

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// scratchNameMintTime parses the UnixNano stamp MintScratchName() embeds in
// a scratch container name (mischief-l0-<pid>-<unixnano>). Returns zero for
// names that do not carry the shape (foreign/legacy) — callers must treat
// zero as "old" so a malformed name can never hide a real leak.
func scratchNameMintTime(name string) time.Time {
	i := strings.LastIndex(name, "-")
	if i < 0 {
		return time.Time{}
	}
	ns, err := strconv.ParseInt(name[i+1:], 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// TestLiveNoScratchLeakMintTimeGuard pins the parse: a mangled or foreign
// suffix reads as "old" (a real leak must never be excused), and a real
// minted name reads as now.
func TestLiveNoScratchLeakMintTimeGuard(t *testing.T) {
	now := time.Now()
	if ts := scratchNameMintTime(MintScratchName()); now.Sub(ts) > time.Minute || ts.After(now.Add(time.Minute)) {
		t.Fatalf("minted name did not parse to ~now: %v", ts)
	}
	if !scratchNameMintTime("mischief-l0-notanumber").IsZero() {
		t.Fatal("non-numeric suffix must parse as zero (old)")
	}
	if !scratchNameMintTime("no-suffix").IsZero() {
		t.Fatal("missing suffix must parse as zero (old)")
	}
	old := time.Unix(0, now.Add(-time.Hour).UnixNano())
	if ts := scratchNameMintTime("mischief-l0-123-" + strconv.FormatInt(old.UnixNano(), 10)); !ts.Equal(old) {
		t.Fatalf("stale stamp misparsed: %v want %v", ts, old)
	}
}
