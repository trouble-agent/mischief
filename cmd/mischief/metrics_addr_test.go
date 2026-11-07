package main

import (
	"testing"

	"github.com/trouble-agent/mischief/internal/daemon"
)

// TestMetricsAddr_EnvResolution pins the serve wiring's address policy: the
// documented default is loopback-only, an explicit address wins, and "off"
// disables the surface without stopping containment.
func TestMetricsAddr_EnvResolution(t *testing.T) {
	cases := []struct {
		env  string
		want string
	}{
		{"", daemon.DefaultMetricsAddr},
		{"127.0.0.1:9999", "127.0.0.1:9999"},
		{" 127.0.0.1:9999 ", "127.0.0.1:9999"},
		{"off", ""},
		{"disabled", ""},
	}
	for _, c := range cases {
		t.Setenv(daemon.MetricsAddrEnv, c.env)
		if got := metricsAddr(); got != c.want {
			t.Fatalf("env=%q: got %q want %q", c.env, got, c.want)
		}
	}
}
