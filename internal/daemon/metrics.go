// Metrics is the daemon's Prometheus surface (MISCHIEF-OPS-001): the
// daemon owns TTLs and reverts faults, but until this file existed its run
// outcomes were only legible by reading journals and health.json. A hold
// that kept failing to revert, or a sweep that stopped running altogether,
// produced no signal a monitor could alert on.
//
// Shape: dependency-free Prometheus TEXT exposition (no client_golang) on a
// SEPARATELY BOUND listener — the daemon deliberately does not serve HTTP on
// its health surface (health.json is a file in the run dir), so the metric
// port is the daemon's only socket and it binds loopback by default.
//
// Bounded labels only: `result` is the journal's closed Outcome vocabulary
// (reverted/revert_failed) and `reason` is a fixed classification of the
// daemon's own error paths, so a scrape can never mint an unbounded series.
package daemon

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// MetricsAddrEnv names the environment variable that selects the metrics
// listener address. The value "off" disables the listener entirely.
const MetricsAddrEnv = "MISCHIEFD_METRICS_ADDR"

// DefaultMetricsAddr is loopback-only: the surface is for a monitor on the
// same host (or an authenticated proxy), never a public socket.
const DefaultMetricsAddr = "127.0.0.1:9464"

// revertBuckets are the histogram buckets for one revert attempt, in
// seconds. A revert is an action plus a measured check, so the useful range
// runs from milliseconds to a few seconds.
var revertBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// Metrics is a concurrency-safe counter set rendered as Prometheus text.
// Every method is nil-safe: a Daemon built without one (or a test literal)
// must not panic, and instrumentation must never be able to kill the loop.
type Metrics struct {
	mu      sync.Mutex
	start   time.Time
	cycles  uint64
	scrapes uint64
	// reverts counts finished revert attempts by the journal's Outcome
	// vocabulary (reverted / revert_failed).
	reverts map[string]uint64
	// errors counts daemon error paths by a fixed reason token
	// (refold / revert / health_write).
	errors map[string]uint64
	// durations is the revert-attempt histogram.
	durCounts []uint64
	durSum    float64
	durCount  uint64
}

// NewMetrics builds an empty counter set stamped with the process's start.
func NewMetrics() *Metrics {
	return &Metrics{
		start:     time.Now(),
		reverts:   map[string]uint64{},
		errors:    map[string]uint64{},
		durCounts: make([]uint64, len(revertBuckets)),
	}
}

// IncCycle counts one completed daemon heartbeat/sweep cycle: a daemon whose
// cycles stop advancing has stopped folding, whatever health.json says.
func (m *Metrics) IncCycle() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.cycles++
	m.mu.Unlock()
}

// IncError counts one daemon error path. reason is a closed classification
// owned by this package, never a free-form message (no unbounded labels).
func (m *Metrics) IncError(reason string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.errors[reason]++
	m.mu.Unlock()
}

// ObserveRevert records one finished revert attempt: its outcome (the
// journal's Outcome string) and how long it took end to end.
func (m *Metrics) ObserveRevert(d time.Duration, result string) {
	if m == nil {
		return
	}
	secs := d.Seconds()
	m.mu.Lock()
	m.reverts[result]++
	m.durCount++
	m.durSum += secs
	for i, b := range revertBuckets {
		if secs <= b {
			m.durCounts[i]++
		}
	}
	m.mu.Unlock()
}

// Handler returns the scrape handler: GET/HEAD /metrics renders the text
// exposition, any other path is a 404 (a monitor misconfigured to /stats
// should see a plain miss, not an empty 200 that looks healthy).
func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		m.countScrape()
		_ = m.render(w)
	})
}

func (m *Metrics) countScrape() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.scrapes++
	m.mu.Unlock()
}

// ServeListener serves the exposition over an already-bound listener until
// ctx is cancelled. Tests use it to prove the REAL socket path (a handler
// test alone would let a broken listener ship).
func (m *Metrics) ServeListener(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{Handler: m.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Serve binds addr and serves the exposition until ctx is cancelled. A bind
// failure is returned to be LOGGED by the caller, never to stop the daemon:
// losing the metric surface must not stop containment.
func (m *Metrics) Serve(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("daemon: metrics listen %s: %w", addr, err)
	}
	return m.ServeListener(ctx, ln)
}

// render writes the exposition. Series with no observations yet are emitted
// at zero for the families whose absence would otherwise read as "no data"
// (cycles, reverts by known outcome, errors by known reason), so a monitor
// can alert on a rate drop from the first scrape.
func (m *Metrics) render(w io.Writer) error {
	if m == nil {
		// A nil counter set still ANSWERS: an un-instrumented daemon must
		// expose an exposition that reads as "up, nothing counted", never
		// a panic on the scrape path.
		_, err := io.WriteString(w, "# HELP mischiefd_up 1 when the metrics surface answers a scrape.\n# TYPE mischiefd_up gauge\nmischiefd_up 1\n")
		return err
	}
	m.mu.Lock()
	cycles, scrapes := m.cycles, m.scrapes
	reverts := make(map[string]uint64, len(m.reverts))
	for k, v := range m.reverts {
		reverts[k] = v
	}
	errs := make(map[string]uint64, len(m.errors))
	for k, v := range m.errors {
		errs[k] = v
	}
	counts := append([]uint64(nil), m.durCounts...)
	sum, count := m.durSum, m.durCount
	start := m.start
	m.mu.Unlock()

	var b strings.Builder
	b.WriteString("# HELP mischiefd_up 1 when the metrics surface answers a scrape.\n")
	b.WriteString("# TYPE mischiefd_up gauge\n")
	b.WriteString("mischiefd_up 1\n")
	b.WriteString("# HELP mischiefd_start_time_seconds Daemon start time (unix seconds).\n")
	b.WriteString("# TYPE mischiefd_start_time_seconds gauge\n")
	fmt.Fprintf(&b, "mischiefd_start_time_seconds %d\n", start.Unix())
	b.WriteString("# HELP mischiefd_cycles_total Heartbeat cycles that completed a sweep.\n")
	b.WriteString("# TYPE mischiefd_cycles_total counter\n")
	fmt.Fprintf(&b, "mischiefd_cycles_total %d\n", cycles)
	b.WriteString("# HELP mischiefd_scrapes_total Exposition scrapes served.\n")
	b.WriteString("# TYPE mischiefd_scrapes_total counter\n")
	fmt.Fprintf(&b, "mischiefd_scrapes_total %d\n", scrapes)
	b.WriteString("# HELP mischiefd_revert_total Revert attempts by outcome.\n")
	b.WriteString("# TYPE mischiefd_revert_total counter\n")
	for _, outcome := range knownOutcomes(reverts) {
		fmt.Fprintf(&b, "mischiefd_revert_total{result=%q} %d\n", outcome, reverts[outcome])
	}
	b.WriteString("# HELP mischiefd_errors_total Daemon error paths by reason.\n")
	b.WriteString("# TYPE mischiefd_errors_total counter\n")
	for _, reason := range knownReasons(errs) {
		fmt.Fprintf(&b, "mischiefd_errors_total{reason=%q} %d\n", reason, errs[reason])
	}
	b.WriteString("# HELP mischiefd_revert_duration_seconds Revert attempt duration (action + measured check).\n")
	b.WriteString("# TYPE mischiefd_revert_duration_seconds histogram\n")
	for i, le := range revertBuckets {
		fmt.Fprintf(&b, "mischiefd_revert_duration_seconds_bucket{le=%q} %d\n", trimFloat(le), counts[i])
	}
	fmt.Fprintf(&b, "mischiefd_revert_duration_seconds_bucket{le=\"+Inf\"} %d\n", count)
	fmt.Fprintf(&b, "mischiefd_revert_duration_seconds_sum %s\n", trimFloat(sum))
	fmt.Fprintf(&b, "mischiefd_revert_duration_seconds_count %d\n", count)

	_, err := io.WriteString(w, b.String())
	return err
}

// knownOutcomes returns the union of the closed outcome vocabulary and any
// observed key, sorted, so the exposition is stable scrape to scrape.
func knownOutcomes(seen map[string]uint64) []string {
	keys := []string{"no_hold", "reverted", "revert_failed"}
	for k := range seen {
		if k != "no_hold" && k != "reverted" && k != "revert_failed" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// knownReasons is the errors counterpart of knownOutcomes.
func knownReasons(seen map[string]uint64) []string {
	keys := []string{"health_write", "refold", "revert"}
	for k := range seen {
		if k != "health_write" && k != "refold" && k != "revert" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// trimFloat renders a bucket bound the way Prometheus spells it (0.005, 1,
// +Inf), avoiding exponent notation Go would otherwise emit for small values.
func trimFloat(f float64) string {
	s := fmt.Sprintf("%g", f)
	return s
}
