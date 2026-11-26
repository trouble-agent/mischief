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

// probeCommitDurable grades S-010: the target's commit path must make every
// acknowledged commit durable — a successful fsync/fdatasync of the database
// file must cover each ack (the target's own report), and the target's
// recovery report may claim durable only what stable storage actually holds
// (the parent's independent post-exit read is the authority). A record
// absent from stable storage that the target still claims durable is the
// storage-ack-not-durable bug class; so is an ack that never had a sync
// before it. A zero-ack observation is a permanently-failed premise — an
// empty expectation must never grade as met (the vacuous-pass shape).
func probeCommitDurable(obs Observation) ProbeResult {
	pr := ProbeResult{
		Name: ProbeCommitDurable,
		Expected: fmt.Sprintf(
			"every one of the %d acked commits was covered by a successful fsync/fdatasync of the database file before its ack, and recovery claims durable only what stable storage holds",
			obs.Acks),
		Observed: fmt.Sprintf(
			"acks=%d syncs=%d syncfails=%d; stable storage holds %d of the acked records after the commit child exited; recovery claims durable=%d lost=%d",
			obs.Acks, obs.Syncs, obs.SyncFails, obs.StableRecords, obs.ClaimedDurable, obs.Lost),
	}
	if obs.Note != "" {
		pr.Observed += " (" + obs.Note + ")"
	}
	pr.Passed = obs.Acks > 0 &&
		obs.Syncs >= obs.Acks &&
		obs.SyncFails == 0 &&
		obs.ClaimedDurable <= obs.StableRecords
	return pr
}
