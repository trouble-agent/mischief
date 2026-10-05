package selftest

import (
	"bytes"

	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/trouble-agent/mischief/internal/backend_net"
)

// ── N-012: proxy HTTP 429 ───────────────────────────────────────────────────
//
// The M1 proxy is an INJECTION SEAM, not a network forwarder: the caller
// hands HandleRequest the upstream status/body and serves whatever comes
// back (proxy_test drives exactly this over a real httptest.Server with
// real clients — there is no second network hop to an "upstream", and the
// selftest must not invent one; a harness fixture the code never contacts
// is exactly the false surface this tool exists to catch).
//
// The measured loop, on a real socket with a real client:
//
//   - control: the seam passes 200 + the full body through; the proxy's
//     OWN ledger records one passthrough hit (status 200, full body).
//   - land: Arm(429), one real request → the client observes 429 and the
//     proxy's own ledger fires http_429 ≥ 1 (the descriptor's
//     landed-proof: "proxy's own hit counter"), with the passthrough
//     count UNCHANGED (the 429 was synthesized at the seam).
//   - inverse: Disarm, replay → the client observes 200 + the full body
//     again (the descriptor's inverse: "rule removed").
//   - pre/post: the seam's last-served record (status + content hash),
//     captured at control and re-read after the inverse — byte-identical
//     when the disarm restored the passthrough, drifted when it did not.

type proxyLandable struct {
	srv   *httptest.Server
	proxy *backend_net.Proxy
	last  string // the seam's last-served record ("<status>:<content-hash>")
	pre   []byte
}

func (p *proxyLandable) capability() (string, bool) {
	return "", false // loopback-only listener, rootless
}

// upstreamBody is the deterministic body the upstream side hands the seam.
const upstreamBody = "selftest-upstream-body\n"

// snapshot reads the seam's last-served record.
func (p *proxyLandable) snapshot() []byte {
	return []byte(p.last)
}

func (p *proxyLandable) prepare() ([]byte, error) {
	p.proxy = backend_net.NewProxy("")
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// the proxy is the served surface: upstream status/body through
		// HandleRequest, whatever comes back is served (the proxy_test
		// shape — the seam is the contract, not a forwarding hop)
		st, body := p.proxy.HandleRequest(r.Context(), http.StatusOK, []byte(upstreamBody))
		sum := sha256.Sum256(body)
		p.last = fmt.Sprintf("%d:%s", st, hex.EncodeToString(sum[:8]))
		w.WriteHeader(st)
		_, _ = w.Write(body)
	}))

	// control request BEFORE arming: 200 + full body, one passthrough
	// ledger hit.
	code, body, err := p.get()
	if err != nil {
		return nil, fmt.Errorf("control request: %v", err)
	}
	if code != 200 || !bytes.Equal(body, []byte(upstreamBody)) {
		return nil, fmt.Errorf("control request got (%d, %d bytes), want (200, %d bytes)", code, len(body), len(upstreamBody))
	}
	c := p.proxy.Counters()
	// passthrough hits arrive under the zero-value fault kind on a never-
	// armed proxy, so the passthrough count is Total minus the named
	// fault kinds (robust to the kind spelling).
	if c.Total != 1 {
		return nil, fmt.Errorf("control request produced %d ledger hits, want 1", c.Total)
	}
	return p.snapshot(), nil
}

func (p *proxyLandable) land() landOutcome {
	p.proxy.Arm(backend_net.ProxyFault{Kind: backend_net.FaultHTTP429})
	code, _, err := p.get()
	if err != nil {
		return bad("faulted request: %v", err)
	}
	if code != 429 {
		return bad("faulted request observed status %d, want 429 — the fault did not reach the client", code)
	}
	// the out-of-band proof pair: the proxy's own ledger fired AND the
	// passthrough count did not move (the 429 was synthesized at the
	// seam, not passed through).
	c := p.proxy.Counters()
	if c.ByKind[backend_net.FaultHTTP429] < 1 {
		return bad("proxy hit ledger shows no http_429 hits — the landed-proof did not fire")
	}
	if passthrough := c.Total - c.ByKind[backend_net.FaultHTTP429]; passthrough != 1 {
		return bad("passthrough ledger moved during the fault (%d hits), want 1 — the served 429 is not this fault's landing", passthrough)
	}
	return ok(fmt.Sprintf("proxy-counter: http_429 hits=%d; client observed 429; passthrough count unchanged", c.ByKind[backend_net.FaultHTTP429]))
}

func (p *proxyLandable) inverse() landOutcome {
	p.proxy.Disarm()
	code, body, err := p.get()
	if err != nil {
		return bad("post-disarm request: %v", err)
	}
	if code != 200 || !bytes.Equal(body, []byte(upstreamBody)) {
		return bad("post-disarm request observed (%d, %d bytes) — the next request did not get the upstream's whole answer", code, len(body))
	}
	c := p.proxy.Counters()
	passthrough := c.Total - c.ByKind[backend_net.FaultHTTP429]
	return ok(fmt.Sprintf("proxy-disarm: post-disarm request observed 200 + full body; passthrough resumed (passthrough hits=%d, ledger total %d)", passthrough, c.Total))
}

func (p *proxyLandable) post() ([]byte, error) {
	return p.snapshot(), nil
}

func (p *proxyLandable) cleanup() {
	if p.srv != nil {
		p.srv.Close()
	}
}

// get issues one real client request against the served surface.
func (p *proxyLandable) get() (int, []byte, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(p.srv.URL)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, buf.Bytes(), nil
}
