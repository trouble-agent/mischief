package daemon

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/trouble-agent/mischief/internal/reverter"
)

// scrape is the shared fetch: it returns the body and the status code of one
// GET against the exposition handler.
func scrape(t *testing.T, h http.Handler, path string) (string, int) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	b, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b), rec.Result().StatusCode
}

// TestMetrics_EndpointServesPrometheusText is the smoke test the row's AC-3
// names: 200, the Prometheus content type, and every required family name
// present. Deleting the /metrics registration (or the handler) fails here.
func TestMetrics_EndpointServesPrometheusText(t *testing.T) {
	m := NewMetrics()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") || !strings.Contains(ct, "version=0.0.4") {
		t.Fatalf("content-type: %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"mischiefd_up 1",
		"mischiefd_cycles_total",
		"mischiefd_revert_total",
		"mischiefd_errors_total",
		"mischiefd_revert_duration_seconds_bucket",
		"mischiefd_revert_duration_seconds_sum",
		"mischiefd_revert_duration_seconds_count",
		"mischiefd_start_time_seconds",
		"mischiefd_scrapes_total",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("exposition missing %q:\n%s", want, body)
		}
	}
}

// TestMetrics_UnknownPathIs404AndNonGetRefused: a monitor pointed at the
// wrong path must see a miss, not an empty 200 that looks healthy; a scrape
// with a write method is refused with Allow.
func TestMetrics_UnknownPathIs404AndNonGetRefused(t *testing.T) {
	m := NewMetrics()
	if _, code := scrape(t, m.Handler(), "/stats"); code != http.StatusNotFound {
		t.Fatalf("wrong path: got %d want 404", code)
	}
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: got %d want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); !strings.Contains(got, "GET") {
		t.Fatalf("Allow header: %q", got)
	}
}

// TestMetrics_CountersAndHistogram pins the arithmetic: counts per outcome,
// cumulative buckets, a sum, and +Inf equal to count.
func TestMetrics_CountersAndHistogram(t *testing.T) {
	m := NewMetrics()
	m.IncCycle()
	m.IncCycle()
	m.IncError("refold")
	m.IncError("revert")
	m.ObserveRevert(3*time.Millisecond, "reverted")
	m.ObserveRevert(7*time.Millisecond, "reverted")
	m.ObserveRevert(2500*time.Millisecond, "revert_failed")

	body, code := scrape(t, m.Handler(), "/metrics")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	for _, want := range []string{
		"mischiefd_cycles_total 2",
		"mischiefd_revert_total{result=\"reverted\"} 2",
		"mischiefd_revert_total{result=\"revert_failed\"} 1",
		"mischiefd_revert_total{result=\"no_hold\"} 0",
		"mischiefd_errors_total{reason=\"refold\"} 1",
		"mischiefd_errors_total{reason=\"revert\"} 1",
		"mischiefd_errors_total{reason=\"health_write\"} 0",
		"mischiefd_revert_duration_seconds_bucket{le=\"0.005\"} 1",
		"mischiefd_revert_duration_seconds_bucket{le=\"0.01\"} 2",
		"mischiefd_revert_duration_seconds_bucket{le=\"5\"} 3",
		"mischiefd_revert_duration_seconds_bucket{le=\"+Inf\"} 3",
		"mischiefd_revert_duration_seconds_count 3",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "mischiefd_revert_duration_seconds_sum 2.51") {
		t.Fatalf("sum line wrong:\n%s", body)
	}
}

// TestMetrics_ScrapeCounterCountsScrapes: mischiefd_scrapes_total advances
// per served scrape (the pre-scrape value is rendered, so the first scrape
// reads 1).
func TestMetrics_ScrapeCounterCountsScrapes(t *testing.T) {
	m := NewMetrics()
	body, _ := scrape(t, m.Handler(), "/metrics")
	if !strings.Contains(body, "mischiefd_scrapes_total 1") {
		t.Fatalf("first scrape count:\n%s", body)
	}
	body, _ = scrape(t, m.Handler(), "/metrics")
	if !strings.Contains(body, "mischiefd_scrapes_total 2") {
		t.Fatalf("second scrape count:\n%s", body)
	}
}

// TestMetrics_ServeListenerServesOverTCP proves the REAL socket path: a
// handler test alone would let a broken listener ship.
func TestMetrics_ServeListenerServesOverTCP(t *testing.T) {
	m := NewMetrics()
	m.IncCycle()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.ServeListener(ctx, ln) }()

	resp, err := http.Get("http://" + ln.Addr().String() + "/metrics")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "mischiefd_cycles_total 1") {
		t.Fatalf("tcp scrape: status=%d body=%s", resp.StatusCode, body)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve returned: %v", err)
	}
}

// TestMetrics_NilSafe: instrumentation must never be able to kill the loop,
// so every method on a nil counter set is a no-op and its handler still
// serves an exposition.
func TestMetrics_NilSafe(t *testing.T) {
	var m *Metrics
	m.IncCycle()
	m.IncError("revert")
	m.ObserveRevert(time.Second, "reverted")
	body, code := scrape(t, m.Handler(), "/metrics")
	if code != http.StatusOK {
		t.Fatalf("nil-set handler status %d", code)
	}
	if !strings.Contains(body, "mischiefd_up 1") {
		t.Fatalf("nil-set exposition:\n%s", body)
	}
}

// TestMetrics_DaemonInstrumentsRevert is the instrumentation proof: a REAL
// TTL revert through the daemon's own sweep must move the revert counter and
// the duration histogram. Removing the ObserveRevert call fails this arm
// while the endpoint tests stay green.
func TestMetrics_DaemonInstrumentsRevert(t *testing.T) {
	dir := localTempDir(t)
	live, backup := scratchPair(t, dir)

	store, err := reverter.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	id, err := reverter.New(store).Arm(testHold(live, backup, 20*time.Millisecond))
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	if err := os.WriteFile(live, []byte("FAULTED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := reverter.New(store).Land(id); err != nil {
		t.Fatalf("land: %v", err)
	}
	store.Close()

	d, err := New(dir)
	if err != nil {
		t.Fatalf("daemon new: %v", err)
	}
	defer d.store.Close()
	if d.Metrics == nil {
		t.Fatal("New must wire a counter set")
	}
	// MISCHIEF-DF-59: the journal's arm timestamp is a whole-second floor,
	// so the fold places expiry one second beyond the last possible arm
	// instant. Wait out that bounded overshoot, not just the declared TTL.
	time.Sleep(1200 * time.Millisecond)
	if err := d.revertExpired(); err != nil {
		t.Fatalf("revertExpired: %v", err)
	}

	body, code := scrape(t, d.Metrics.Handler(), "/metrics")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if !strings.Contains(body, "mischiefd_revert_total{result=\"reverted\"} 1") {
		t.Fatalf("revert counter not moved:\n%s", body)
	}
	if !strings.Contains(body, "mischiefd_revert_duration_seconds_count 1") {
		t.Fatalf("duration histogram not moved:\n%s", body)
	}
}
