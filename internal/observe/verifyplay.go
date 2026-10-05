package observe

import (
	"fmt"
	"strings"
)

// verify-play (PRD §7 integration point 4): "after trouble records a play
// that fired, re-inject the original fault to check the fix held. Needs
// trouble's ledger schema read-only; the only hook that makes mischief
// part of a closed remediation loop."
//
// It is v0.2, deliberately: the re-injection engine does not exist in this
// build, and a stub that pretended otherwise would be the exact
// green-but-meaningless shape the PRD §6.4 vocabulary exists to kill. The
// surface is the REFUSAL: VerifyPlay fails closed under every option
// combination — including --allow-real — and the error names what is
// absent, so a caller wiring the verb today gets an honest exit-2-shaped
// refusal, not a silent no-op and not a partial engine.

// VerifyPlayRequest is the declared verify-play request (the v0.2 verb's
// input shape, fixed now so the refusal is testable and the v0.2 engine
// has its contract pre-named).
type VerifyPlayRequest struct {
	// PlayID is the trouble play record id to verify ("TRBL-031-play-2").
	PlayID string
	// Fault is the original primitive id to re-inject ("I-002").
	Fault string
	// Target is the selector to re-inject against.
	Target string
	// AllowReal is the L5 opt-in flag (--allow-real). It arms the
	// real-provider plane for faults that need it; it does NOT and can
	// NOT substitute for the missing engine.
	AllowReal bool
}

// ErrVerifyPlayV02 is the v0.2 refusal. One value, quoted by tests: the
// wording is part of the contract (the refusal must name what is absent).
var ErrVerifyPlayV02 = fmt.Errorf(
	"verify-play: refused: the re-injection engine does not exist in v0.1 (SPEC-12 marks it v0.2); " +
		"absent: trouble's live findings-ledger schema read (play record → original fault + target mapping) " +
		"and the re-injection executor. --allow-real arms the L5 real-provider plane; it does not provide either. " +
		"File the play id on the board and re-run when v0.2 lands")

// VerifyPlay is the verify-play entry point: it fails closed, always. The
// request is validated first (a malformed request is refused for ITS own
// reason, so the v0.2 engine's contract is exercised even by the refusal)
// and the refusal then fires regardless of AllowReal.
func VerifyPlay(req VerifyPlayRequest) error {
	var missing []string
	if req.PlayID == "" {
		missing = append(missing, "play_id")
	}
	if req.Fault == "" {
		missing = append(missing, "fault")
	}
	if req.Target == "" {
		missing = append(missing, "target")
	}
	if len(missing) > 0 {
		return fmt.Errorf("verify-play: refused: missing required declaration(s): %s; then: %w",
			strings.Join(missing, ", "), ErrVerifyPlayV02)
	}
	return ErrVerifyPlayV02
}
