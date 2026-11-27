package profile

import "testing"

func TestProbeErrnoHandled(t *testing.T) {
	for _, tc := range []struct {
		name string
		obs  Observation
		want bool
	}{
		{"handled", Observation{ErrnoObserved: true, ErrnoHandled: true}, true},
		{"swallowed", Observation{ErrnoObserved: true}, false},
		{"no fault", Observation{ErrnoHandled: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := probeErrnoHandled(tc.obs); got.Passed != tc.want {
				t.Fatalf("passed=%v, want %v", got.Passed, tc.want)
			}
		})
	}
}
