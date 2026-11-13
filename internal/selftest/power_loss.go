package selftest

import (
	"fmt"
	"os/exec"

	"github.com/trouble-agent/mischief/internal/backend_signal"
)

// powerLossLandable drives P-020 on a scratch child. The parent receives the
// acknowledgement stream and owns the wait status, so the witness is external
// to the process being killed.
type powerLossLandable struct {
	cmd     *exec.Cmd
	witness backend_signal.PowerLossWitness
	proof   string
}

func (p *powerLossLandable) capability() (string, bool) { return "", false }

func (p *powerLossLandable) prepare() ([]byte, error) {
	p.cmd = exec.Command("sh", "-c", `i=1; while [ "$i" -le 20 ]; do printf 'commit-ack %s\n' "$i"; i=$((i+1)); done; while :; do :; done`)
	return []byte("power-loss target state: absent after externally witnessed crash"), nil
}

func (p *powerLossLandable) land() landOutcome {
	witness, err := backend_signal.RunPowerLoss(p.cmd, backend_signal.PowerLossConfig{Seed: 20261113, MaxOrdinal: 20})
	if err != nil {
		return bad("external SIGKILL witness: %v", err)
	}
	p.witness = witness
	proof, err := backend_signal.PowerLossProof(witness)
	if err != nil {
		return bad("external SIGKILL proof: %v", err)
	}
	p.proof = proof
	return ok(proof)
}

func (p *powerLossLandable) inverse() landOutcome {
	if p.witness.Signal == 0 {
		return bad("inverse: no externally witnessed SIGKILL")
	}
	return ok(fmt.Sprintf("restart inverse: target pid %d is absent after signal=%s; no in-process revert exists", p.witness.PID, p.witness.Signal))
}

func (p *powerLossLandable) post() ([]byte, error) {
	if p.witness.Signal == 0 || p.witness.LandedOrdinal != p.witness.ConfiguredOrdinal {
		return nil, fmt.Errorf("external witness is incomplete: %+v", p.witness)
	}
	return []byte("power-loss target state: absent after externally witnessed crash"), nil
}

func (p *powerLossLandable) cleanup() {
	if p.cmd == nil || p.cmd.Process == nil {
		return
	}
	_ = p.cmd.Process.Kill()
	_, _ = p.cmd.Process.Wait()
}
