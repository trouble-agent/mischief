package profile

import (
	"fmt"
)

// probe.go — the probe vocabulary. A probe is a GRADER: it receives the
// observation an actuator measured and decides whether the observation
// satisfies the probe's expectation. It never measures anything itself, so a
// probe cannot be satisfied by the injector's own bookkeeping.

// probePayloadComplete grades S-004: the target's write loop must land every
// byte of the payload it was asked to write. A short payload (0 bytes) is a
// permanently-failed premise — an empty expectation must never grade as met
// (the vacuous-pass shape).
func probePayloadComplete(obs Observation) ProbeResult {
	pr := ProbeResult{
		Name:     ProbePayloadComplete,
		Expected: fmt.Sprintf("the target's write loop lands all %d bytes of the payload", obs.PayloadLen),
		Observed: fmt.Sprintf("%d of %d bytes on disk after %d write(2) call(s), %d short",
			obs.OnDisk, obs.PayloadLen, obs.Attempts, obs.ShortWrites),
	}
	if obs.Note != "" {
		pr.Observed += " (" + obs.Note + ")"
	}
	pr.Passed = obs.PayloadLen > 0 && obs.OnDisk == int64(obs.PayloadLen)
	return pr
}
