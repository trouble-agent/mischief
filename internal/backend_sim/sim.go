// Package backend_sim is SPEC-09's provider simulator: a rootless userspace
// HTTP server producing the fault-catalog layer-I cloud shapes (I-003/I-004/
// I-005/I-006/I-010/I-011/I-012/I-013/I-014) with NO real provider contact
// (the L4 tier: the simulator process on a sanctioned L1-shaped host stands
// in for the cloud plane).
//
// Design authority: docs/SPEC-PLAN.md (SPEC-09) and docs/FAULT-CATALOG.md
// layer I. Every simulated response is logged to a per-run request log
// file — that request log is the LANDED-PROOF CHANNEL (AC-3: the run proves
// the fault landed by reading the log back, never by trusting the client's
// own view), and it is the proof AC-17's refusal path points at (a
// real-provider verb refused for missing --allow-real/resource-tag lands in
// the simulator instead; the log must show it received the request).
//
// The shapes, exactly as the catalog rows state them:
//
//	api-rate-limit   (I-003)  429 bursts, Retry-After present or absent,
//	                          quota-exhaustion variant (403 quota error).
//	api-5xx          (I-004)  5xx bursts; malformed body; pagination bug
//	                          (same page twice / endless page markers).
//	eventual-lag     (I-005)  a write is acknowledged, reads return the OLD
//	                          value for the declared lag window.
//	object-store     (I-006)  404 on an existing key; truncated multipart;
//	                          expired-presign 403.
//	auth-expiry      (I-010)  the next call 401s after N successes.
//	metadata-fault   (I-011)  metadata endpoint times out / 500 / stale token.
//	quota-exhausted  (I-012)  account-level quota error on create verbs.
//	snapshot-stale   (I-013)  restore returns state stamped BEFORE the
//	                          fault time (the "recovered but lost N minutes"
//	                          shape).
//	dns-propagation  (I-014)  two resolvers disagree for the declared
//	                          window (the sim serves the stale A record to
//	                          one of two named resolvers).
//	permanent-destroy (companion) — REFUSED OUTRIGHT (v0.1: destroy_class=
//	                          permanent never runs outside the simulator,
//	                          and here it is a logged refusal, never a 200).
//
// Real-provider verbs (v0.2) are STUBS behind the RealAdapter interface:
// opt-in, allowlisted, spend-capped fields are present and checked, and the
// implementations are documented refusals — nothing contacts a real cloud.
//
// ch:trace row=MSF-010 spec=docs/SPEC-PLAN.md#SPEC-09 evidence=internal/backend_sim/ witness=none:loopback-sim-only
package backend_sim

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ShapeKind is the closed vocabulary of layer-I shapes the simulator
// produces. A kind outside this set refuses at Arm.
type ShapeKind string

const (
	// ShapeRateLimit: I-003 — 429 bursts with/without Retry-After.
	ShapeRateLimit ShapeKind = "api-rate-limit"
	// ShapeAPI5xx: I-004 — 5xx bursts, malformed bodies, pagination bugs.
	ShapeAPI5xx ShapeKind = "api-5xx"
	// ShapeEventualLag: I-005 — acknowledged writes read stale for a window.
	ShapeEventualLag ShapeKind = "eventual-lag"
	// ShapeObjectStore: I-006 — 404-on-existing, truncation, expired presign.
	ShapeObjectStore ShapeKind = "object-store"
	// ShapeAuthExpiry: I-010 — the next call 401s.
	ShapeAuthExpiry ShapeKind = "auth-expiry"
	// ShapeMetadata: I-011 — metadata endpoint faults.
	ShapeMetadata ShapeKind = "metadata-fault"
	// ShapeQuota: I-012 — account-level quota exhaustion.
	ShapeQuota ShapeKind = "quota-exhausted"
	// ShapeSnapshotStale: I-013 — restore to a stale point.
	ShapeSnapshotStale ShapeKind = "snapshot-stale"
	// ShapeDNSPropagation: I-014 — two resolvers disagree for a window.
	ShapeDNSPropagation ShapeKind = "dns-propagation"
)

// Valid reports whether k is a declared shape.
func (k ShapeKind) Valid() bool {
	switch k {
	case ShapeRateLimit, ShapeAPI5xx, ShapeEventualLag, ShapeObjectStore,
		ShapeAuthExpiry, ShapeMetadata, ShapeQuota, ShapeSnapshotStale,
		ShapeDNSPropagation:
		return true
	}
	return false
}

// Shape is one armed fault on the simulator. Like every mischief fault it
// is explicit: a simulator with no shape armed serves the HEALTHY control
// responses (and logs them — the control run's negative arm).
type Shape struct {
	// Kind is the layer-I shape to produce.
	Kind ShapeKind
	// Status overrides the shape's default status (0 = shape default).
	Status int
	// RetryAfter is the Retry-After header to attach (RateLimit; <=0 omits
	// the header — the catalog's "with/without Retry-After" pair).
	RetryAfter time.Duration
	// Burst is how many faulted responses fire before the shape
	// self-exhausts (<=0 = unbounded until Disarm).
	Burst int
	// Malformed makes 5xx/OK bodies malformed (truncated JSON) — I-004's
	// "malformed response" leg.
	Malformed bool
	// PaginationBug serves the SAME page marker on every list call
	// (I-004's endless-page leg).
	PaginationBug bool
	// LagWindow is how long reads stay stale after a write (EventualLag).
	LagWindow time.Duration
	// MetadataMode is the I-011 leg: "timeout" | "status500" | "stale-token".
	MetadataMode string
	// StaleSeconds is the staleness a snapshot restore reports (I-013) or
	// the DNS stale window (I-014).
	StaleSeconds int
	// QuotaResource names the exhausted resource (Quota; "instances",
	// "buckets", ...). Empty = "instances".
	QuotaResource string
	// ExpiredPresign makes object-store GETs answer 403 expired-presign.
	ExpiredPresign bool
	// TruncateMultipart truncates object-store list bodies.
	TruncateMultipart bool
	// MissingKey answers 404 for object-store GETs on keys that EXIST in
	// the sim's own store (the I-006 stale-read shape).
	MissingKey bool
}

// RequestLogEntry is one line of the per-run request log — the
// landed-proof record. Every request the simulator answers appends one,
// faulted or healthy; the `Faulted` field is what the AC-17 proof and the
// AC-3 proof read.
type RequestLogEntry struct {
	At        time.Time `json:"at"`
	Method    string    `json:"method"`
	Path      string    `json:"path"`
	Remote    string    `json:"remote"`
	Status    int       `json:"status"`
	Shape     ShapeKind `json:"shape,omitempty"`
	Faulted   bool      `json:"faulted"`
	Detail    string    `json:"detail,omitempty"`
	BodyBytes int       `json:"body_bytes,omitempty"`
}

// LogLine renders the greppable one-line form ("simlog: <ts> <method> <path>
// -> <status> faulted=<t/f> shape=<k> <detail>").
func (e RequestLogEntry) LogLine() string {
	tag := "healthy"
	if e.Faulted {
		tag = "faulted"
	}
	shape := string(e.Shape)
	if shape == "" {
		shape = "-"
	}
	detail := ""
	if e.Detail != "" {
		detail = " " + e.Detail
	}
	return fmt.Sprintf("simlog: %s %s %s -> %d %s shape=%s%s",
		e.At.UTC().Format(time.RFC3339Nano), e.Method, e.Path, e.Status, tag, shape, detail)
}

// Simulator is the rootless provider simulator: one loopback HTTP server,
// one per-run request log, one armed shape at a time (the per-run armed-set
// doctrine — the fault lives in the run, never in global state).
type Simulator struct {
	mu      sync.Mutex
	shape   Shape
	logPath string
	logFile *os.File
	entries []RequestLogEntry
	hits    map[ShapeKind]int
	// object store for the object-store shapes (key -> body)
	store      map[string][]byte
	lastWrite  map[string]time.Time
	burstSpent int
	srv        *http.Server
	ln         net.Listener
	url        string
}

// NewSimulator builds a simulator logging to logPath (created on Start).
func NewSimulator(logPath string) *Simulator {
	return &Simulator{
		logPath:   logPath,
		hits:      map[ShapeKind]int{},
		store:     map[string][]byte{},
		lastWrite: map[string]time.Time{},
	}
}

// LogPath is the per-run request log's path (the proof channel).
func (s *Simulator) LogPath() string { return s.logPath }

// URL is the simulator's base URL after Start ("" before).
func (s *Simulator) URL() string { return s.url }

// Start opens the log file and serves on a loopback listener. The port is
// the kernel's choice (rootless; no well-known port is claimed).
func (s *Simulator) Start() error {
	if err := os.MkdirAll(filepath.Dir(s.logPath), 0o755); err != nil {
		return fmt.Errorf("backend_sim: log dir: %w", err)
	}
	f, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("backend_sim: request log: %w", err)
	}
	s.logFile = f
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		f.Close()
		return fmt.Errorf("backend_sim: listen: %w", err)
	}
	s.ln = ln
	s.url = "http://" + ln.Addr().String()
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handle)
	// srv is captured LOCALLY: Close() nils the field while this goroutine
	// may not have started yet — reading the field here would deref nil.
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	s.srv = srv
	go func() { _ = srv.Serve(ln) }()
	return nil
}

// Close stops the server and closes the log (flushed per line; no buffered
// loss). Safe to call twice.
func (s *Simulator) Close() {
	if s.srv != nil {
		_ = s.srv.Close()
		s.srv = nil
	}
	if s.ln != nil {
		_ = s.ln.Close()
		s.ln = nil
	}
	if s.logFile != nil {
		_ = s.logFile.Sync()
		_ = s.logFile.Close()
		s.logFile = nil
	}
}

// Arm arms one layer-I shape (replacing any previous shape — one fault per
// sim per run). Unknown shapes refuse (the closed vocabulary).
func (s *Simulator) Arm(sh Shape) error {
	if !sh.Kind.Valid() {
		return fmt.Errorf("backend_sim: shape %q is not in the layer-I vocabulary", sh.Kind)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shape = sh
	s.burstSpent = 0
	return nil
}

// Disarm returns the simulator to the healthy control surface (the inverse
// verb; the log keeps recording — the inverse proof reads the healthy hits).
func (s *Simulator) Disarm() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shape = Shape{}
	s.burstSpent = 0
}

// Counters reports the per-shape hit counts (the aggregate read the
// verdict consumes; the request log is the per-hit record).
func (s *Simulator) Counters() map[ShapeKind]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[ShapeKind]int, len(s.hits))
	for k, v := range s.hits {
		out[k] = v
	}
	return out
}

// Entries snapshots the in-memory request log (the file is the durable
// twin; Entries exists for in-process assertions).
func (s *Simulator) Entries() []RequestLogEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]RequestLogEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

// Put seeds the object store (object-store shapes read this).
func (s *Simulator) Put(key string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store[key] = body
}

// appendLog writes one entry to the durable request log (fsynced line) and
// the in-memory twin. It is the ONLY log writer.
func (s *Simulator) appendLog(e RequestLogEntry) {
	line := e.LogLine() + "\n"
	s.entries = append(s.entries, e)
	if s.logFile != nil {
		_, _ = s.logFile.WriteString(line)
		_ = s.logFile.Sync()
	}
}

// faultCountersSafe names the shapes that count as fault hits per armed
// kind (the map key the landed-proof reads).
func faultCounter(kind ShapeKind) ShapeKind { return kind }

// handle is the one request path: log entry construction happens for every
// request; the armed shape decides status/body/detail. The handler owns no
// transport assumptions beyond net/http.
func (s *Simulator) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	sh := s.shape
	st, body, faulted, detail, hdr := s.serve(r, sh)
	s.mu.Unlock()

	for k, v := range hdr {
		w.Header().Set(k, v)
	}
	w.WriteHeader(st)
	_, _ = w.Write(body)

	s.mu.Lock()
	s.appendLog(RequestLogEntry{
		At: time.Now(), Method: r.Method, Path: r.URL.Path,
		Remote: remoteHost(r), Status: st,
		Shape: sh.Kind, Faulted: faulted, Detail: detail, BodyBytes: len(body),
	})
	if faulted {
		s.hits[faultCounter(sh.Kind)]++
		s.burstSpent++
	}
	s.mu.Unlock()
}

func remoteHost(r *http.Request) string {
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return h
	}
	return r.RemoteAddr
}

// serve computes one response from the armed shape. Called under the
// mutex. The healthy surface (no shape) is the control run: 200 + a
// deterministic JSON body.
func (s *Simulator) serve(r *http.Request, sh Shape) (int, []byte, bool, string, map[string]string) {
	switch sh.Kind {
	case ShapeRateLimit:
		return s.serveRateLimit(r, sh)
	case ShapeAPI5xx:
		return s.serveAPI5xx(r, sh)
	case ShapeEventualLag:
		return s.serveEventualLag(r, sh)
	case ShapeObjectStore:
		return s.serveObjectStore(r, sh)
	case ShapeAuthExpiry:
		return s.serveAuthExpiry(r, sh)
	case ShapeMetadata:
		return s.serveMetadata(r, sh)
	case ShapeQuota:
		return s.serveQuota(r, sh)
	case ShapeSnapshotStale:
		return s.serveSnapshotStale(r, sh)
	case ShapeDNSPropagation:
		return s.serveDNS(r, sh)
	default:
		body, _ := json.Marshal(map[string]string{"status": "healthy", "service": "mischief-sim"})
		return http.StatusOK, body, false, "", nil
	}
}

// burstExhausted reports whether a bounded burst has spent itself (the
// shape stops faulting; subsequent requests log healthy — the inverse
// leg is observable without Disarm).
func burstExhausted(sh Shape, spent int) bool {
	return sh.Burst > 0 && spent >= sh.Burst
}

func (s *Simulator) serveRateLimit(r *http.Request, sh Shape) (int, []byte, bool, string, map[string]string) {
	if burstExhausted(sh, s.burstSpent) {
		return s.healthy()
	}
	status := 429
	if sh.Status != 0 {
		status = sh.Status
	}
	detail := "429 burst"
	hdr := map[string]string{}
	if sh.RetryAfter > 0 {
		hdr["Retry-After"] = fmt.Sprintf("%.0f", sh.RetryAfter.Seconds())
		detail = "429 burst with Retry-After=" + hdr["Retry-After"]
	} else {
		detail = "429 burst without Retry-After"
	}
	body, _ := json.Marshal(map[string]string{"error": "rate_limited", "message": detail})
	return status, body, true, detail, hdr
}

func (s *Simulator) serveAPI5xx(r *http.Request, sh Shape) (int, []byte, bool, string, map[string]string) {
	if burstExhausted(sh, s.burstSpent) {
		return s.healthy()
	}
	if sh.PaginationBug {
		body, _ := json.Marshal(map[string]any{"page": 1, "next_page": 1,
			"items": []string{"same-item"}})
		return http.StatusOK, body, true, "pagination bug: same page forever (next_page=1)", nil
	}
	status := 500
	if sh.Status != 0 {
		status = sh.Status
	}
	if sh.Malformed {
		return status, []byte(`{"error": "internal`), true, fmt.Sprintf("%d with malformed (truncated) body", status), nil
	}
	body, _ := json.Marshal(map[string]string{"error": "internal_error"})
	return status, body, true, fmt.Sprintf("%d burst", status), nil
}

func (s *Simulator) serveEventualLag(r *http.Request, sh Shape) (int, []byte, bool, string, map[string]string) {
	key := strings.TrimPrefix(r.URL.Path, "/kv/")
	if r.Method == http.MethodPut || r.Method == http.MethodPost {
		now := time.Now()
		s.store[key] = readBody(r)
		s.lastWrite[key] = now
		body, _ := json.Marshal(map[string]string{"acknowledged": key})
		return http.StatusOK, body, false, "", nil
	}
	wroteAt, ok := s.lastWrite[key]
	lag := sh.LagWindow
	if lag <= 0 {
		lag = 5 * time.Second
	}
	if ok && time.Since(wroteAt) < lag {
		old := "stale-value"
		if b, exists := s.store[key+"\x00old"]; exists {
			old = string(b)
		}
		return http.StatusOK, []byte(old), true,
			fmt.Sprintf("eventual-consistency lag: serving pre-write value for key %q (window %s)", key, lag), nil
	}
	val := "fresh"
	if b, exists := s.store[key]; exists && !strings.HasSuffix(key, "\x00old") {
		val = string(b)
	}
	return http.StatusOK, []byte(val), false, "", nil
}

func (s *Simulator) serveObjectStore(r *http.Request, sh Shape) (int, []byte, bool, string, map[string]string) {
	key := strings.TrimPrefix(r.URL.Path, "/obj/")
	switch {
	case sh.MissingKey && r.Method == http.MethodGet:
		if _, exists := s.store[key]; exists {
			// the I-006 shape: 404 on a key that EXISTS
			body, _ := json.Marshal(map[string]string{"error": "NoSuchKey", "key": key})
			return http.StatusNotFound, body, true, "404 on existing key (stale object read)", nil
		}
	case sh.ExpiredPresign && r.Method == http.MethodGet:
		body, _ := json.Marshal(map[string]string{"error": "AccessDenied",
			"message": "Request has expired (presigned URL)"})
		return http.StatusForbidden, body, true, "403 expired presigned URL", nil
	}
	if r.Method == http.MethodPut || r.Method == http.MethodPost {
		s.store[key] = readBody(r)
		return http.StatusOK, []byte(`{"stored": true}`), false, "", nil
	}
	if b, exists := s.store[key]; exists {
		if sh.TruncateMultipart {
			return http.StatusOK, b[:len(b)/2], true, "truncated multipart body", nil
		}
		return http.StatusOK, b, false, "", nil
	}
	return http.StatusNotFound, []byte(`{"error": "NoSuchKey"}`), false, "", nil
}

func (s *Simulator) serveAuthExpiry(r *http.Request, sh Shape) (int, []byte, bool, string, map[string]string) {
	if burstExhausted(sh, s.burstSpent) {
		return s.healthy()
	}
	body, _ := json.Marshal(map[string]string{"error": "token_expired"})
	return http.StatusUnauthorized, body, true, "401: credential expired mid-run", nil
}

func (s *Simulator) serveMetadata(r *http.Request, sh Shape) (int, []byte, bool, string, map[string]string) {
	if burstExhausted(sh, s.burstSpent) {
		return s.healthyMetadata()
	}
	mode := sh.MetadataMode
	if mode == "" {
		mode = "timeout"
	}
	switch mode {
	case "status500":
		return http.StatusInternalServerError, []byte("metadata unavailable"), true, "metadata 500", nil
	case "stale-token":
		return http.StatusOK, []byte(`{"token": "stale-token-0001", "expires_in": -60}`), true,
			"metadata served a STALE token (expires_in negative)", nil
	default: // timeout
		// hold the request without holding the mutex (serve was called under
		// it — the timeout is served as a slow response bounded by the
		// client; the LOG records it as faulted either way)
		dur := 2 * time.Second
		time.Sleep(dur)
		return http.StatusGatewayTimeout, []byte("metadata timeout"), true,
			fmt.Sprintf("metadata stalled %s then 504", dur), nil
	}
}

func (s *Simulator) serveQuota(r *http.Request, sh Shape) (int, []byte, bool, string, map[string]string) {
	if burstExhausted(sh, s.burstSpent) {
		return s.healthy()
	}
	res := sh.QuotaResource
	if res == "" {
		res = "instances"
	}
	body, _ := json.Marshal(map[string]string{
		"error": "QuotaExceeded", "resource": res,
		"message": "account-level quota exhausted for " + res})
	return http.StatusForbidden, body, true, "quota exhausted: " + res, nil
}

func (s *Simulator) serveSnapshotStale(r *http.Request, sh Shape) (int, []byte, bool, string, map[string]string) {
	stale := sh.StaleSeconds
	if stale <= 0 {
		stale = 600
	}
	restoredAt := time.Now().Add(-time.Duration(stale) * time.Second)
	body, _ := json.Marshal(map[string]any{
		"restore":        "ok",
		"snapshot_taken": restoredAt.UTC().Format(time.RFC3339),
		"lost_seconds":   stale,
	})
	return http.StatusOK, body, true,
		fmt.Sprintf("snapshot restored to a point %ds BEFORE the fault (data delta measurable)", stale), nil
}

func (s *Simulator) serveDNS(r *http.Request, sh Shape) (int, []byte, bool, string, map[string]string) {
	if burstExhausted(sh, s.burstSpent) {
		return s.healthy()
	}
	stale := sh.StaleSeconds
	if stale <= 0 {
		stale = 30
	}
	resolver := r.URL.Query().Get("resolver")
	if resolver == "resolver-b" {
		body, _ := json.Marshal(map[string]any{"resolver": resolver, "a": "203.0.113.1",
			"stale_for_seconds": stale})
		return http.StatusOK, body, true,
			fmt.Sprintf("DNS propagation: %s still serves the OLD A record (window %ds)", resolver, stale), nil
	}
	body, _ := json.Marshal(map[string]any{"resolver": resolver, "a": "198.51.100.77"})
	return http.StatusOK, body, false, "", nil
}

func (s *Simulator) healthy() (int, []byte, bool, string, map[string]string) {
	body, _ := json.Marshal(map[string]string{"status": "healthy", "service": "mischief-sim"})
	return http.StatusOK, body, false, "", nil
}

func (s *Simulator) healthyMetadata() (int, []byte, bool, string, map[string]string) {
	return http.StatusOK, []byte(`{"token": "fresh-token", "expires_in": 3600}`), false, "", nil
}

func readBody(r *http.Request) []byte {
	if r.Body == nil {
		return nil
	}
	var buf [4096]byte
	n, _ := r.Body.Read(buf[:])
	return buf[:n]
}

// SelfPut is the exported single-key write the cmd layer uses to seed an
// eventual-lag window (a real acknowledged PUT against the running sim).
func (s *Simulator) SelfPut(path string) error {
	_, _, err := s.selfCallMethod(http.MethodPut, path, []byte("seed"))
	return err
}

// selfCall issues one real HTTP request against the running simulator's
// own listener (the client leg of the AC-17 redirect proof: a real client,
// a real socket — not a handler call). Returns the status and body.
func (s *Simulator) selfCall(path string) (int, []byte, error) {
	return s.selfCallMethod(http.MethodGet, path, nil)
}

// SelfCall is the exported GET used by the cmd layer's proof driver for
// multi-step shapes (eventual-lag's write-then-read window).
func (s *Simulator) SelfCall(path string) (int, []byte, error) {
	return s.selfCallMethod(http.MethodGet, path, nil)
}

// selfCallMethod issues an arbitrary-method request against the running
// simulator. The cmd layer's proof driver uses it to exercise multi-step
// shapes (eventual-lag's PUT-then-GET window) without touching sim
// internals.
func (s *Simulator) selfCallMethod(method, path string, body []byte) (int, []byte, error) {
	if s.url == "" {
		return 0, nil, fmt.Errorf("backend_sim: selfCall: simulator not started")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, s.url+path, rdr)
	if err != nil {
		return 0, nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, b, nil
}

// ReadRequestLog reads the durable request log back from disk (the proof
// reader: the log, not the in-memory twin, is the landed-proof channel).
func ReadRequestLog(path string) ([]RequestLogEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []RequestLogEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "simlog: ") {
			continue
		}
		out = append(out, parseLogLine(line))
	}
	return out, sc.Err()
}

// parseLogLine parses one LogLine back into an entry (the fields the proof
// assertions read: method, path, status, faulted, shape).
func parseLogLine(line string) RequestLogEntry {
	var e RequestLogEntry
	fields := strings.Fields(line)
	// simlog: <ts> <method> <path> -> <status> <tag> shape=<k>[ <detail>]
	if len(fields) >= 7 {
		if ts, err := time.Parse(time.RFC3339Nano, fields[1]); err == nil {
			e.At = ts
		}
		e.Method, e.Path = fields[2], fields[3]
		fmt.Sscanf(fields[5], "%d", &e.Status)
		e.Faulted = fields[6] == "faulted"
		for _, f := range fields[7:] {
			if v, ok := strings.CutPrefix(f, "shape="); ok && v != "-" {
				e.Shape = ShapeKind(v)
			}
		}
		// the detail is the tail after the shape token (it may contain
		// spaces — "429 burst with Retry-After=30")
		if len(fields) > 8 {
			e.Detail = strings.Join(fields[8:], " ")
		}
	}
	return e
}

// SortedShapeKinds returns the declared shape vocabulary, sorted (docs and
// tests iterate it deterministically).
func SortedShapeKinds() []string {
	out := []string{string(ShapeRateLimit), string(ShapeAPI5xx), string(ShapeEventualLag),
		string(ShapeObjectStore), string(ShapeAuthExpiry), string(ShapeMetadata),
		string(ShapeQuota), string(ShapeSnapshotStale), string(ShapeDNSPropagation)}
	sort.Strings(out)
	return out
}
