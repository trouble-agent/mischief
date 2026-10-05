package backend_sim

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sim_test.go — the layer-I shape tests drive a REAL simulator over a REAL
// socket with a REAL client (httptest wiring of the sim's own handler is
// not used: Start() owns the listener, so the tests dial its URL). Every
// landed-proof assertion reads the REQUEST LOG BACK FROM DISK (the proof
// channel), never the in-memory twin.

func startSim(t *testing.T) *Simulator {
	t.Helper()
	s := NewSimulator(filepath.Join(t.TempDir(), "requests.log"))
	if err := s.Start(); err != nil {
		t.Fatalf("sim start: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// get issues one real request and returns (status, body).
func get(url string) (int, string, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return resp.StatusCode, b.String(), nil
}

// assertLogged reads the durable log from disk and requires one faulted
// entry matching path+shape (the landed-proof, AC-3).
func assertLogged(t *testing.T, s *Simulator, path string, kind ShapeKind, minCount int) {
	t.Helper()
	entries, err := ReadRequestLog(s.LogPath())
	if err != nil {
		t.Fatalf("request log read: %v", err)
	}
	n := 0
	for _, e := range entries {
		if e.Path == path && e.Faulted && e.Shape == kind {
			n++
		}
	}
	if n < minCount {
		var dump strings.Builder
		for _, e := range entries {
			dump.WriteString(e.LogLine() + "\n")
		}
		t.Fatalf("request log carries %d faulted %s entries for %s, want >= %d\nlog:\n%s",
			n, kind, path, minCount, dump.String())
	}
}

// TestRateLimitShapeWithAndWithoutRetryAfter: I-003's pair, proven in the
// log and on the wire (header present / absent).
func TestRateLimitShapeWithAndWithoutRetryAfter(t *testing.T) {
	s := startSim(t)
	if err := s.Arm(Shape{Kind: ShapeRateLimit, RetryAfter: 30 * time.Second, Burst: 1}); err != nil {
		t.Fatal(err)
	}
	st, _, err := get(s.URL() + "/v1/things")
	if err != nil || st != 429 {
		t.Fatalf("faulted request: status=%d err=%v", st, err)
	}
	// the WITH leg is already spent; arm the WITHOUT leg
	if err := s.Arm(Shape{Kind: ShapeRateLimit, Burst: 1}); err != nil {
		t.Fatal(err)
	}
	st, _, err = get(s.URL() + "/v1/things")
	if err != nil || st != 429 {
		t.Fatalf("second burst: status=%d err=%v", st, err)
	}
	assertLogged(t, s, "/v1/things", ShapeRateLimit, 2)
	entries, _ := ReadRequestLog(s.LogPath())
	withHeader, without := 0, 0
	for _, e := range entries {
		if !e.Faulted || e.Shape != ShapeRateLimit {
			continue
		}
		if strings.Contains(e.Detail, "Retry-After=30") {
			withHeader++
		} else if strings.Contains(e.Detail, "without Retry-After") {
			without++
		}
	}
	if withHeader != 1 || without != 1 {
		t.Fatalf("Retry-After pair not proven in the log: with=%d without=%d", withHeader, without)
	}
}

// TestRateLimitQuotaExhaustionVariant: the 403 quota-error leg of I-003.
func TestRateLimitQuotaExhaustionVariant(t *testing.T) {
	s := startSim(t)
	if err := s.Arm(Shape{Kind: ShapeQuota, QuotaResource: "buckets"}); err != nil {
		t.Fatal(err)
	}
	st, body, err := get(s.URL() + "/v1/buckets")
	if err != nil || st != http.StatusForbidden {
		t.Fatalf("quota shape: status=%d err=%v", st, err)
	}
	if !strings.Contains(body, "QuotaExceeded") || !strings.Contains(body, "buckets") {
		t.Fatalf("quota body must name the resource: %s", body)
	}
	assertLogged(t, s, "/v1/buckets", ShapeQuota, 1)
}

// TestAPI5xxAndPagination: I-004's legs.
func TestAPI5xxAndPagination(t *testing.T) {
	s := startSim(t)
	if err := s.Arm(Shape{Kind: ShapeAPI5xx, Status: 503, Burst: 1}); err != nil {
		t.Fatal(err)
	}
	st, _, err := get(s.URL() + "/v1/list")
	if err != nil || st != 503 {
		t.Fatalf("5xx leg: status=%d err=%v", st, err)
	}
	if err := s.Arm(Shape{Kind: ShapeAPI5xx, PaginationBug: true}); err != nil {
		t.Fatal(err)
	}
	st, body, err := get(s.URL() + "/v1/list")
	if err != nil || st != 200 {
		t.Fatalf("pagination leg: status=%d err=%v", st, err)
	}
	if !strings.Contains(body, `"next_page":1`) {
		t.Fatalf("endless-page marker missing: %s", body)
	}
	if err := s.Arm(Shape{Kind: ShapeAPI5xx, Malformed: true, Burst: 1}); err != nil {
		t.Fatal(err)
	}
	st, body, err = get(s.URL() + "/v1/list")
	if err != nil || st != 500 {
		t.Fatalf("malformed leg: status=%d err=%v", st, err)
	}
	if !strings.HasPrefix(body, `{"error": "internal`) && !strings.Contains(body, "internal") {
		t.Fatalf("malformed body unexpected: %q", body)
	}
	assertLogged(t, s, "/v1/list", ShapeAPI5xx, 3)
}

// TestEventualLag: I-005 — the write is ACKed, the read returns the old
// value inside the window, then the fresh value after it.
func TestEventualLag(t *testing.T) {
	s := startSim(t)
	if err := s.Arm(Shape{Kind: ShapeEventualLag, LagWindow: 1500 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	// seed the OLD value first (a pre-existing key)
	client := &http.Client{Timeout: 20 * time.Second}
	req, _ := http.NewRequest(http.MethodPut, s.URL()+"/kv/color", strings.NewReader("blue"))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// the faulted write: acknowledged…
	req2, _ := http.NewRequest(http.MethodPut, s.URL()+"/kv/color", strings.NewReader("red"))
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()

	// …but the read inside the window returns the OLD value
	st, body, err := get(s.URL() + "/kv/color")
	if err != nil || st != 200 {
		t.Fatalf("stale read: status=%d err=%v", st, err)
	}
	if body != "stale-value" && body != "blue" {
		t.Fatalf("read inside the lag window must serve a stale value, got %q", body)
	}
	assertLogged(t, s, "/kv/color", ShapeEventualLag, 1)

	// after the window the read is fresh
	time.Sleep(1600 * time.Millisecond)
	st, body, err = get(s.URL() + "/kv/color")
	if err != nil || st != 200 || body != "red" {
		t.Fatalf("read after the window must be fresh (red): status=%d body=%q err=%v", st, body, err)
	}
}

// TestObjectStoreShapes: I-006's three legs — 404 on existing, truncated
// multipart, expired presign 403.
func TestObjectStoreShapes(t *testing.T) {
	s := startSim(t)
	s.Put("data/report.csv", []byte("a,b,c\n1,2,3\n4,5,6\n"))

	if err := s.Arm(Shape{Kind: ShapeObjectStore, MissingKey: true}); err != nil {
		t.Fatal(err)
	}
	st, body, err := get(s.URL() + "/obj/data/report.csv")
	if err != nil || st != http.StatusNotFound {
		t.Fatalf("404-on-existing leg: status=%d err=%v", st, err)
	}
	if !strings.Contains(body, "NoSuchKey") {
		t.Fatalf("404 body: %s", body)
	}

	if err := s.Arm(Shape{Kind: ShapeObjectStore, TruncateMultipart: true}); err != nil {
		t.Fatal(err)
	}
	st, body, err = get(s.URL() + "/obj/data/report.csv")
	if err != nil || st != 200 {
		t.Fatalf("truncate leg: status=%d err=%v", st, err)
	}
	if len(body) >= len("a,b,c\n1,2,3\n4,5,6\n") {
		t.Fatalf("truncated body must be shorter than the object: %q", body)
	}

	if err := s.Arm(Shape{Kind: ShapeObjectStore, ExpiredPresign: true}); err != nil {
		t.Fatal(err)
	}
	st, body, err = get(s.URL() + "/obj/data/report.csv?X-Amz-Expires=1")
	if err != nil || st != http.StatusForbidden {
		t.Fatalf("presign leg: status=%d err=%v", st, err)
	}
	if !strings.Contains(body, "expired") {
		t.Fatalf("presign body must name expiry: %s", body)
	}
	assertLogged(t, s, "/obj/data/report.csv", ShapeObjectStore, 3)
}

// TestAuthExpiry: I-010 — the next call 401s.
func TestAuthExpiry(t *testing.T) {
	s := startSim(t)
	if err := s.Arm(Shape{Kind: ShapeAuthExpiry, Burst: 1}); err != nil {
		t.Fatal(err)
	}
	st, body, err := get(s.URL() + "/v1/whoami")
	if err != nil || st != http.StatusUnauthorized {
		t.Fatalf("auth-expiry: status=%d err=%v", st, err)
	}
	if !strings.Contains(body, "token_expired") {
		t.Fatalf("401 body: %s", body)
	}
	// burst spent: the sim returns to healthy — the self-exhaust inverse leg
	st2, _, err := get(s.URL() + "/v1/whoami")
	if err != nil || st2 != 200 {
		t.Fatalf("post-burst must be healthy: status=%d err=%v", st2, err)
	}
	assertLogged(t, s, "/v1/whoami", ShapeAuthExpiry, 1)
}

// TestMetadataFaults: I-011's three modes.
func TestMetadataFaults(t *testing.T) {
	s := startSim(t)
	if err := s.Arm(Shape{Kind: ShapeMetadata, MetadataMode: "stale-token"}); err != nil {
		t.Fatal(err)
	}
	st, body, err := get(s.URL() + "/meta/credentials")
	if err != nil || st != 200 {
		t.Fatalf("stale-token leg: status=%d err=%v", st, err)
	}
	if !strings.Contains(body, "stale-token") {
		t.Fatalf("stale token body: %s", body)
	}
	if err := s.Arm(Shape{Kind: ShapeMetadata, MetadataMode: "status500"}); err != nil {
		t.Fatal(err)
	}
	st, _, err = get(s.URL() + "/meta/credentials")
	if err != nil || st != 500 {
		t.Fatalf("500 leg: status=%d err=%v", st, err)
	}
	assertLogged(t, s, "/meta/credentials", ShapeMetadata, 2)
	// (the timeout mode is exercised by the short-window shape below via
	// burst=0? No: mode default; covered in the AC-17 test's rate shape
	// instead to keep this suite bounded)
}

// TestSnapshotStale: I-013 — restore reports a point BEFORE the fault.
func TestSnapshotStale(t *testing.T) {
	s := startSim(t)
	if err := s.Arm(Shape{Kind: ShapeSnapshotStale, StaleSeconds: 900}); err != nil {
		t.Fatal(err)
	}
	st, body, err := get(s.URL() + "/v1/restore/snap-1")
	if err != nil || st != 200 {
		t.Fatalf("restore: status=%d err=%v", st, err)
	}
	if !strings.Contains(body, `"lost_seconds":900`) {
		t.Fatalf("restore body must carry the staleness: %s", body)
	}
	assertLogged(t, s, "/v1/restore/snap-1", ShapeSnapshotStale, 1)
}

// TestDNSPropagation: I-014 — the two resolvers disagree inside the window.
func TestDNSPropagation(t *testing.T) {
	s := startSim(t)
	if err := s.Arm(Shape{Kind: ShapeDNSPropagation, StaleSeconds: 60}); err != nil {
		t.Fatal(err)
	}
	st, body, err := get(s.URL() + "/dns/resolve?name=api.internal&resolver=resolver-a")
	if err != nil || st != 200 {
		t.Fatalf("resolver-a: status=%d err=%v", st, err)
	}
	if !strings.Contains(body, "198.51.100.77") {
		t.Fatalf("resolver-a must serve the NEW record: %s", body)
	}
	st, body, err = get(s.URL() + "/dns/resolve?name=api.internal&resolver=resolver-b")
	if err != nil || st != 200 {
		t.Fatalf("resolver-b: status=%d err=%v", st, err)
	}
	if !strings.Contains(body, "203.0.113.1") || !strings.Contains(body, "stale_for_seconds\":60") {
		t.Fatalf("resolver-b must serve the OLD record for the window: %s", body)
	}
	assertLogged(t, s, "/dns/resolve", ShapeDNSPropagation, 1)
}

// TestHealthyControlSurface: no shape armed — every request is healthy and
// the log records it as NOT faulted (the control run's negative arm).
func TestHealthyControlSurface(t *testing.T) {
	s := startSim(t)
	st, body, err := get(s.URL() + "/v1/anything")
	if err != nil || st != 200 {
		t.Fatalf("control: status=%d err=%v", st, err)
	}
	if !strings.Contains(body, "healthy") {
		t.Fatalf("control body: %s", body)
	}
	entries, err := ReadRequestLog(s.LogPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Faulted {
			t.Fatalf("control run logged a faulted entry: %+v", e)
		}
	}
}

// TestArmRefusesUnknownShape: the closed vocabulary refuses at Arm.
func TestArmRefusesUnknownShape(t *testing.T) {
	s := startSim(t)
	if err := s.Arm(Shape{Kind: ShapeKind("delete-everything")}); err == nil {
		t.Fatal("unknown shape must refuse at Arm")
	}
}

// TestDisarmReturnsHealthy: the inverse verb — the log proves the healthy
// hits resumed (faulted hits stop).
func TestDisarmReturnsHealthy(t *testing.T) {
	s := startSim(t)
	if err := s.Arm(Shape{Kind: ShapeRateLimit}); err != nil {
		t.Fatal(err)
	}
	if st, _, err := get(s.URL() + "/x"); err != nil || st != 429 {
		t.Fatalf("armed: status=%d err=%v", st, err)
	}
	s.Disarm()
	if st, _, err := get(s.URL() + "/x"); err != nil || st != 200 {
		t.Fatalf("disarmed: status=%d err=%v", st, err)
	}
	c := s.Counters()
	if c[ShapeRateLimit] != 1 {
		t.Fatalf("counters: %v", c)
	}
}

// TestRequestLogIsDurableTwin: entries land on disk fsynced per line —
// a reader that never touched the process sees the same log.
func TestRequestLogIsDurableTwin(t *testing.T) {
	s := startSim(t)
	if err := s.Arm(Shape{Kind: ShapeRateLimit, Burst: 1}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := get(s.URL() + "/durable"); err != nil {
		t.Fatal(err)
	}
	// read the FILE directly (not via the package's reader helpers only)
	b, err := os.ReadFile(s.LogPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "simlog:") || !strings.Contains(string(b), "faulted shape=api-rate-limit") {
		t.Fatalf("durable log lacks the expected line:\n%s", b)
	}
}

// TestSortedShapeKindsPinned: the declared vocabulary is exactly the nine
// layer-I shapes the catalog names (docs and tests iterate deterministically).
func TestSortedShapeKindsPinned(t *testing.T) {
	got := SortedShapeKinds()
	want := []string{
		"api-5xx", "api-rate-limit", "auth-expiry", "dns-propagation",
		"eventual-lag", "metadata-fault", "object-store", "quota-exhausted",
		"snapshot-stale",
	}
	if len(got) != len(want) {
		t.Fatalf("shape vocabulary: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("shape vocabulary[%d]: got %s want %s", i, got[i], want[i])
		}
	}
}

// TestParseLogLineRoundTrip: the log's text form parses back to the fields
// the proof assertions read.
func TestParseLogLineRoundTrip(t *testing.T) {
	e := RequestLogEntry{
		At: time.Now(), Method: "GET", Path: "/v1/x", Status: 429,
		Shape: ShapeRateLimit, Faulted: true, Detail: "429 burst",
	}
	line := e.LogLine()
	got := parseLogLine(line)
	if got.Method != "GET" || got.Path != "/v1/x" || got.Status != 429 ||
		got.Shape != ShapeRateLimit || !got.Faulted {
		t.Fatalf("round trip drifted: %+v from %q", got, line)
	}
}

// TestHttptestCompatibility: the sim handler is an http.Handler — a
// second server can wrap it (the shape backend_net's Proxy publishes).
func TestHttptestCompatibility(t *testing.T) {
	s := NewSimulator(filepath.Join(t.TempDir(), "wrap.log"))
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	srv := httptest.NewServer(http.HandlerFunc(s.handle))
	defer srv.Close()
	if err := s.Arm(Shape{Kind: ShapeRateLimit, Burst: 1}); err != nil {
		t.Fatal(err)
	}
	st, _, err := get(srv.URL + "/wrapped")
	if err != nil || st != 429 {
		t.Fatalf("wrapped handler: status=%d err=%v", st, err)
	}
}

// TestStubErrorIsSentinel: the v0.2 stub refusal is matchable.
func TestStubErrorIsSentinel(t *testing.T) {
	stub := &RealStub{Allowlist: Allowlist{Tags: []string{"aws:sandbox"}}, MaxSpendUSD: 5.0}
	err := stub.Apply("I-003", Shape{Kind: ShapeRateLimit},
		RealGate{AllowReal: true, ResourceTag: "aws:sandbox", SpendCapUSD: 5, Provider: "aws"})
	if err == nil {
		t.Fatal("stub must refuse even with a full gate")
	}
	if !errors.Is(err, ErrStub) {
		t.Fatalf("refusal must wrap ErrStub: %v", err)
	}
	var se *StubError
	if !errors.As(err, &se) || se.Decision != DecideReal {
		t.Fatalf("refusal must report the gate decision real: %+v", se)
	}
	if stub.SpentUSD() != 0 {
		t.Fatalf("stub spent must stay zero: %f", stub.SpentUSD())
	}
}
