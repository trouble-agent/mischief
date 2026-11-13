package backend_signal

import (
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

func powerLossTarget(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sh", "-c", `i=1; while [ "$i" -le 20 ]; do printf 'commit-ack %s\n' "$i"; i=$((i+1)); done; while :; do :; done`)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

func TestRunPowerLossExternalWitnessHitsConfiguredOrdinal(t *testing.T) {
	witness, err := RunPowerLoss(powerLossTarget(t), PowerLossConfig{Ordinal: 7})
	if err != nil {
		t.Fatalf("RunPowerLoss: %v", err)
	}
	if witness.Signal != syscall.SIGKILL {
		t.Fatalf("external wait witness signal = %v, want SIGKILL", witness.Signal)
	}
	if witness.LandedOrdinal != 7 || witness.ConfiguredOrdinal != 7 {
		t.Fatalf("external ordinal witness = %+v, want configured=landed=7", witness)
	}
	proof, err := PowerLossProof(witness)
	if err != nil {
		t.Fatalf("PowerLossProof: %v", err)
	}
	if !strings.Contains(proof, "configured_ordinal=7") || !strings.Contains(proof, "landed_ordinal=7") {
		t.Fatalf("proof does not preserve external ordinal: %q", proof)
	}
}

func TestRunPowerLossSameSeedReproducesOrdinal(t *testing.T) {
	cfg := PowerLossConfig{Seed: 20261113, MaxOrdinal: 20}
	first, err := RunPowerLoss(powerLossTarget(t), cfg)
	if err != nil {
		t.Fatalf("first RunPowerLoss: %v", err)
	}
	second, err := RunPowerLoss(powerLossTarget(t), cfg)
	if err != nil {
		t.Fatalf("second RunPowerLoss: %v", err)
	}
	if first.ConfiguredOrdinal != second.ConfiguredOrdinal || first.LandedOrdinal != second.LandedOrdinal {
		t.Fatalf("same-seed ordinals differ: first=%+v second=%+v", first, second)
	}
	if first.ConfiguredOrdinal < 1 || first.ConfiguredOrdinal > cfg.MaxOrdinal {
		t.Fatalf("derived ordinal %d outside 1..%d", first.ConfiguredOrdinal, cfg.MaxOrdinal)
	}
}

func TestPowerLossOrdinalRejectsMissingRange(t *testing.T) {
	if _, err := PowerLossOrdinal(1, 0); err == nil {
		t.Fatal("PowerLossOrdinal accepted a non-positive max ordinal")
	}
}
