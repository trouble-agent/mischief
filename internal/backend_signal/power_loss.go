package backend_signal

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// PowerLossConfig describes a deterministic crash point. Ordinal is the
// one-based acknowledgement at which the target is killed. When Ordinal is
// zero, MaxOrdinal is required and the ordinal is derived from Seed.
type PowerLossConfig struct {
	Seed       int64
	Ordinal    int
	MaxOrdinal int
	AckPrefix  string
}

// PowerLossWitness is the external observation of a power-loss injection. The
// parent process counts acknowledgements and reads the child's wait status;
// the target cannot manufacture either observation.
type PowerLossWitness struct {
	PID               int
	Seed              int64
	ConfiguredOrdinal int
	LandedOrdinal     int
	Signal            syscall.Signal
}

// PowerLossOrdinal deterministically selects a one-based ordinal from a seed.
// It uses a small local mixer rather than global PRNG state, so equal seeds
// reproduce across concurrent runs and tests.
func PowerLossOrdinal(seed int64, maxOrdinal int) (int, error) {
	if maxOrdinal < 1 {
		return 0, fmt.Errorf("power-loss ordinal: max ordinal must be positive, got %d", maxOrdinal)
	}
	x := uint64(seed) ^ 0x9e3779b97f4a7c15
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return int(x%uint64(maxOrdinal)) + 1, nil
}

func resolvePowerLossOrdinal(cfg PowerLossConfig) (int, error) {
	if cfg.Ordinal > 0 {
		return cfg.Ordinal, nil
	}
	return PowerLossOrdinal(cfg.Seed, cfg.MaxOrdinal)
}

// RunPowerLoss starts cmd, counts acknowledgement lines from its stdout, and
// sends SIGKILL immediately after the configured acknowledgement. The caller
// must provide a target that emits one acknowledgement per completed step.
// The wait status is read by this parent, making it an external witness.
func RunPowerLoss(cmd *exec.Cmd, cfg PowerLossConfig) (PowerLossWitness, error) {
	ordinal, err := resolvePowerLossOrdinal(cfg)
	if err != nil {
		return PowerLossWitness{}, err
	}
	prefix := cfg.AckPrefix
	if prefix == "" {
		prefix = "commit-ack "
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return PowerLossWitness{}, fmt.Errorf("power-loss stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return PowerLossWitness{}, fmt.Errorf("power-loss start: %w", err)
	}
	witness := PowerLossWitness{
		PID:               cmd.Process.Pid,
		Seed:              cfg.Seed,
		ConfiguredOrdinal: ordinal,
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if !strings.HasPrefix(scanner.Text(), prefix) {
			continue
		}
		witness.LandedOrdinal++
		if witness.LandedOrdinal != ordinal {
			continue
		}
		if err := cmd.Process.Kill(); err != nil {
			_, _ = cmd.Process.Wait()
			return witness, fmt.Errorf("power-loss SIGKILL pid %d at ordinal %d: %w", witness.PID, ordinal, err)
		}
		waitErr := cmd.Wait()
		if scanner.Err() != nil {
			return witness, fmt.Errorf("power-loss witness scan: %w", scanner.Err())
		}
		if waitErr == nil {
			return witness, fmt.Errorf("power-loss target pid %d exited cleanly at ordinal %d", witness.PID, ordinal)
		}
		if exitErr, ok := waitErr.(*exec.ExitError); !ok || exitErr.ProcessState == nil || !waitStatusSignaled(exitErr.ProcessState) {
			return witness, fmt.Errorf("power-loss target pid %d did not die by SIGKILL at ordinal %d: %v", witness.PID, ordinal, waitErr)
		}
		witness.Signal = syscall.SIGKILL
		return witness, nil
	}
	if err := scanner.Err(); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return witness, fmt.Errorf("power-loss witness scan: %w", err)
	}
	waitErr := cmd.Wait()
	return witness, fmt.Errorf("power-loss target ended before ordinal %d (observed %d acknowledgements, wait=%v)", ordinal, witness.LandedOrdinal, waitErr)
}

// PowerLossProof renders the externally witnessed landing in operator-facing
// form. It refuses to describe a proof until the ordinal and signal agree.
func PowerLossProof(w PowerLossWitness) (string, error) {
	if w.Signal != syscall.SIGKILL {
		return "", fmt.Errorf("power-loss proof: external wait status was %v, want SIGKILL", w.Signal)
	}
	if w.ConfiguredOrdinal < 1 || w.LandedOrdinal != w.ConfiguredOrdinal {
		return "", fmt.Errorf("power-loss proof: configured ordinal %d, observed ordinal %d", w.ConfiguredOrdinal, w.LandedOrdinal)
	}
	return fmt.Sprintf("external-witness: pid %d wait-status signal=killed configured_ordinal=%d landed_ordinal=%d seed=%d", w.PID, w.ConfiguredOrdinal, w.LandedOrdinal, w.Seed), nil
}

func waitStatusSignaled(state interface{ Sys() any }) bool {
	status, ok := state.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && status.Signal() == syscall.SIGKILL
}
