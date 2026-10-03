package backend_net

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// bodyOf builds a deterministic body of n bytes.
func bodyOf(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	return b
}

// handler wraps the proxy the way the production HTTP surface does: the
// upstream's status/body go through HandleRequest, whatever comes back is
// served. Loopback only — the httptest server binds 127.0.0.1 on an
// ephemeral port.
func handler(p *Proxy) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status, body := p.HandleRequest(r.Context(), http.StatusOK, bodyOf(1024))
		w.WriteHeader(status)
		_, _ = w.Write(body)
	})
}

// TestProxyFaultInjection is the table-driven fault battery: every fault
// kind is proven LANDED by the hit counters (AC-3), plus the control run
// through the SAME proxy with no fault (counters move, response whole).
func TestProxyFaultInjection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		fault       ProxyFault
		wantStatus  int
		wantBodyLen int
		wantKind    FaultKind
	}{
		{
			name:        "control_no_fault_passes_through_whole",
			fault:       ProxyFault{Kind: FaultNone},
			wantStatus:  http.StatusOK,
			wantBodyLen: 1024,
			wantKind:    FaultNone,
		},
		{
			name:        "http_429_injected",
			fault:       ProxyFault{Kind: FaultHTTP429},
			wantStatus:  429,
			wantBodyLen: 1024,
			wantKind:    FaultHTTP429,
		},
		{
			name:        "http_503_injected_custom_status",
			fault:       ProxyFault{Kind: FaultHTTP429, Status: 503},
			wantStatus:  503,
			wantBodyLen: 1024,
			wantKind:    FaultHTTP429,
		},
		{
			name:        "truncate_to_100",
			fault:       ProxyFault{Kind: FaultTruncate, TruncateTo: 100},
			wantStatus:  http.StatusOK,
			wantBodyLen: 100,
			wantKind:    FaultTruncate,
		},
		{
			name:        "truncate_beyond_body_is_noop",
			fault:       ProxyFault{Kind: FaultTruncate, TruncateTo: 4096},
			wantStatus:  http.StatusOK,
			wantBodyLen: 1024,
			wantKind:    FaultTruncate,
		},
		{
			name:        "stall_delay_then_forward",
			fault:       ProxyFault{Kind: FaultStall, Delay: 50 * time.Millisecond},
			wantStatus:  http.StatusOK,
			wantBodyLen: 1024,
			wantKind:    FaultStall,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			upstream := httptest.NewServer(handler(nil)) // placeholder replaced below
			upstream.Close()
			// The proxy itself is the served surface: one httptest server
			// whose handler is the faulting proxy (loopback, ephemeral).
			p := NewProxy("http://127.0.0.1:1") // upstream unreachable: the proxy owns the response shape
			p.Arm(tc.fault)
			srv := httptest.NewServer(handler(p))
			defer srv.Close()

			start := time.Now()
			resp, err := http.Get(srv.URL)
			if err != nil {
				t.Fatalf("GET through proxy: %v", err)
			}
			defer resp.Body.Close()
			var got bytes.Buffer
			_, _ = got.ReadFrom(resp.Body)
			elapsed := time.Since(start)

			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status: got %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if got.Len() != tc.wantBodyLen {
				t.Errorf("body len: got %d, want %d", got.Len(), tc.wantBodyLen)
			}
			if tc.fault.Kind == FaultStall && elapsed < tc.fault.Delay {
				t.Errorf("stall: response arrived in %s, fault delays %s — not landed", elapsed, tc.fault.Delay)
			}

			// AC-3 landed-proof: the counters MEASURE the hit.
			c := p.Counters()
			if c.Total != 1 {
				t.Errorf("counters: total hits = %d, want 1", c.Total)
			}
			if c.ByKind[tc.wantKind] != 1 {
				t.Errorf("counters: by-kind[%s] = %d, want 1", tc.wantKind, c.ByKind[tc.wantKind])
			}
			hits := p.Hits()
			if len(hits) != 1 || hits[0].Status != tc.wantStatus || hits[0].BodyLen != tc.wantBodyLen {
				t.Errorf("hit ledger: %+v does not record the measured response", hits)
			}
		})
	}
}

// TestProxyControlAfterFaultDisarm: the control run goes through the SAME
// proxy — disarm, hit again, the fault counters stay and the pass-through
// counter moves. Two runs never contaminate each other's fault.
func TestProxyControlAfterFaultDisarm(t *testing.T) {
	t.Parallel()
	p := NewProxy("http://127.0.0.1:1")
	p.Arm(ProxyFault{Kind: FaultHTTP429})
	srv := httptest.NewServer(handler(p))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 429 {
		t.Fatalf("faulted hit: got %d, want 429", resp.StatusCode)
	}

	p.Disarm()
	resp2, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("control hit after disarm: got %d, want 200", resp2.StatusCode)
	}

	c := p.Counters()
	if c.Total != 2 || c.ByKind[FaultHTTP429] != 1 || c.ByKind[FaultNone] != 1 {
		t.Errorf("counters after fault+control: %+v, want total=2, 429=1, none=1", c)
	}
}

// TestProxyConcurrentCounterIntegrity: concurrent clients through one proxy
// never lose or double a hit (the counters are the evidence; a racy ledger
// manufactures proof).
func TestProxyConcurrentCounterIntegrity(t *testing.T) {
	t.Parallel()
	p := NewProxy("http://127.0.0.1:1")
	p.Arm(ProxyFault{Kind: FaultTruncate, TruncateTo: 10})
	srv := httptest.NewServer(handler(p))
	defer srv.Close()

	const n = 32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(srv.URL)
			if err != nil {
				t.Errorf("concurrent GET: %v", err)
				return
			}
			resp.Body.Close()
		}()
	}
	wg.Wait()

	c := p.Counters()
	if c.Total != n {
		t.Errorf("counters: total = %d, want %d (lost/duplicated hits)", c.Total, n)
	}
	if c.ByKind[FaultTruncate] != n {
		t.Errorf("counters: truncate hits = %d, want %d", c.ByKind[FaultTruncate], n)
	}
}

// TestProxyStallContextCancel: a stalled client's context cancel ends the
// stall promptly — the proxy cannot pin a caller past its own budget.
func TestProxyStallContextCancel(t *testing.T) {
	t.Parallel()
	p := NewProxy("http://127.0.0.1:1")
	p.Arm(ProxyFault{Kind: FaultStall, Delay: 30 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _ = p.HandleRequest(ctx, http.StatusOK, bodyOf(16))
	if el := time.Since(start); el > time.Second {
		t.Errorf("stall honoured context cancel: blocked %s", el)
	}
}
