package backend_net

import (
	"context"
	"sync"
	"time"
)

// FaultKind is the closed set of rootless proxy faults.
type FaultKind string

const (
	// FaultNone: the proxy passes traffic through unchanged — the control
	// run shape (a control through the same proxy is the landed-proof's
	// negative arm: the counters stay zero and the response arrives whole).
	FaultNone FaultKind = "none"
	// FaultHTTP429: the proxy answers the upstream with a synthesized 429
	// (retry-later / rate-limit shape, PRD §4 cloud shapes).
	FaultHTTP429 FaultKind = "http_429"
	// FaultTruncate: the proxy forwards a truncated body.
	FaultTruncate FaultKind = "truncate"
	// FaultStall: the proxy delays the response before forwarding it.
	FaultStall FaultKind = "stall"
)

// ProxyFault is one fault armed on the proxy. Like the rest of mischief it
// is explicit: a proxy with no fault armed passes traffic through, and the
// hit counters still move — that is the control run.
type ProxyFault struct {
	Kind FaultKind
	// Status is the synthesized status code for FaultHTTP429 (default 429).
	Status int
	// TruncateTo is the byte length the response body is cut to.
	TruncateTo int
	// Delay is how long the proxy stalls before forwarding.
	Delay time.Duration
}

// ProxyHit is the landed-proof record for ONE request through the proxy
// (AC-3: never assert, measure — each handled request appends a hit).
type ProxyHit struct {
	// Fault is the fault kind that fired on this hit (FaultNone for pass-through).
	Fault FaultKind
	// Status is the status served to the client.
	Status int
	// BodyLen is the body length the client received.
	BodyLen int
	// At is when the hit completed.
	At time.Time
}

// ProxyCounters are the aggregate hit counters, split per fault kind — the
// numbers the verdict reads to prove landing (a run that armed 429s and
// shows zero 429 hits is a no_op, AC-3).
type ProxyCounters struct {
	Total  int
	ByKind map[FaultKind]int
}

// Proxy is the rootless userspace HTTP fault proxy (SPEC-08). It is an
// httptest-compatible http.Handler: point a real httptest.Server or client
// at it. It holds the per-run armed fault and the hit ledger; like the
// rails' ArmedSet, the fault lives in the run, not in global state.
type Proxy struct {
	mu     sync.Mutex
	fault  ProxyFault
	hits   []ProxyHit
	target string // upstream URL the proxy forwards to
}

// NewProxy builds a proxy forwarding to target (an http://host:port base).
func NewProxy(target string) *Proxy {
	return &Proxy{target: target}
}

// Arm arms one fault on this proxy. Arming replaces any previous fault
// (one fault per proxy per run; the armed set's per-run doctrine).
func (p *Proxy) Arm(f ProxyFault) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if f.Kind == FaultHTTP429 && f.Status == 0 {
		f.Status = 429
	}
	p.fault = f
}

// Disarm clears the fault (the control run is Arm-none, not a new proxy).
func (p *Proxy) Disarm() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fault = ProxyFault{Kind: FaultNone}
}

// Counters returns the measured hit counters (AC-3 landed-proof read).
func (p *Proxy) Counters() ProxyCounters {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := ProxyCounters{Total: len(p.hits), ByKind: make(map[FaultKind]int)}
	for _, h := range p.hits {
		c.ByKind[h.Fault]++
	}
	return c
}

// Hits returns a copy of the per-hit ledger.
func (p *Proxy) Hits() []ProxyHit {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]ProxyHit, len(p.hits))
	copy(out, p.hits)
	return out
}

// apply mutates (status, body, delay) per the armed fault and records the
// hit. It is the fault injection point and the only counter writer; the
// handler wrapping it stays transport-neutral so HTTP/1.1 keep-alive and
// TLS termination upstream of it see identical counters.
func (p *Proxy) apply(ctx context.Context, status int, body []byte) (int, []byte, ProxyFault) {
	p.mu.Lock()
	f := p.fault
	p.mu.Unlock()

	switch f.Kind {
	case FaultHTTP429:
		status = f.Status
	case FaultTruncate:
		if f.TruncateTo >= 0 && f.TruncateTo < len(body) {
			body = body[:f.TruncateTo]
		}
	case FaultStall:
		// The stall itself must not hold the mutex — it delays the
		// response, not the counters. It honours the caller's context so
		// a stalled client's run cannot pin a worker goroutine forever.
		if f.Delay > 0 {
			select {
			case <-time.After(f.Delay):
			case <-ctx.Done():
			}
		}
	}

	p.mu.Lock()
	p.hits = append(p.hits, ProxyHit{Fault: f.Kind, Status: status, BodyLen: len(body), At: time.Now()})
	p.mu.Unlock()
	return status, body, f
}

// HandleRequest is the transport-neutral injection seam: hand it the
// upstream status and body, get the (possibly faulted) response the client
// must receive. The HTTP handler is a thin wrapper over this so tests can
// drive the same path without a socket and the socket path stays exactly
// as measured.
func (p *Proxy) HandleRequest(ctx context.Context, status int, body []byte) (int, []byte) {
	st, b, _ := p.apply(ctx, status, body)
	return st, b
}
