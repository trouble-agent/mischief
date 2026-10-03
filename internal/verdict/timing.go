// Package timing is SPEC-05's timing model: the four measured durations
// (t_landed, t_symptom, t_detect, t_recover) every verdict carries
// (docs/prd/mischief-v0.1.md, SPEC-PLAN.md SPEC-05 "timing model").
//
// The timeline is INPUT to grading, not a side product: the recovery budget
// is checked against t_recover, so the durations must parse and stay sane
// (start ≤ end) before any verdict is produced — an unparsable or
// backwards timeline voids the run (AC-16 family: the harness, not the
// target, is the finding).
//
// NOT-LIST:
//
//   - Nothing here reads a clock: the runner stamps wall times; this
//     package only parses and validates declared durations.
//   - No unit conversion is invented: ParseDuration accepts Go duration
//     syntax ("250ms", "2s", "1m30s") and nothing else.
package verdict

import (
	"fmt"
	"time"
)

// Timeline is the four measured durations attached to every verdict. Zero
// durations are legal (a stage that produced no observation); negative or
// end-before-start values are not (Validate refuses them — a timeline that
// runs backwards is a harness defect, AC-16 family).
type Timeline struct {
	// Landed is t_landed: arm → the landed proof fired.
	Landed time.Duration `json:"t_landed"`
	// Symptom is t_symptom: land → the first probe went red.
	Symptom time.Duration `json:"t_symptom"`
	// Detect is t_detect: land → the observer's incident record (when
	// observe.trouble is declared; 0 when not).
	Detect time.Duration `json:"t_detect"`
	// Recover is t_recover: land → the probe set green again.
	Recover time.Duration `json:"t_recover"`
}

// ParseTimeline parses a declared timeline. Every field must parse as a Go
// duration ("250ms", "2s"); an unparsable field is named in the error.
func ParseTimeline(landed, symptom, detect, recover string) (Timeline, error) {
	var tl Timeline
	fields := []struct {
		name string
		raw  string
		dst  *time.Duration
	}{
		{"t_landed", landed, &tl.Landed},
		{"t_symptom", symptom, &tl.Symptom},
		{"t_detect", detect, &tl.Detect},
		{"t_recover", recover, &tl.Recover},
	}
	for _, f := range fields {
		if f.raw == "" {
			continue // absent stage: zero duration is legal
		}
		d, err := time.ParseDuration(f.raw)
		if err != nil {
			return Timeline{}, fmt.Errorf("%s: unparsable duration %q", f.name, f.raw)
		}
		*f.dst = d
	}
	return tl, nil
}

// Validate reports whether the timeline is monotonic-sane: no negative
// duration and no end-before-start stage. The land→symptom, land→detect and
// land→recover stages all start at the same instant (the landing), so each
// must be non-negative on its own; landed is the arm→land stage and shares
// the rule.
func (tl Timeline) Validate() error {
	fields := []struct {
		name string
		d    time.Duration
	}{
		{"t_landed", tl.Landed},
		{"t_symptom", tl.Symptom},
		{"t_detect", tl.Detect},
		{"t_recover", tl.Recover},
	}
	for _, f := range fields {
		if f.d < 0 {
			return fmt.Errorf("%s: negative duration %s", f.name, f.d)
		}
	}
	return nil
}

// String renders the timeline in the canonical field order; the format is
// stable text (durations via time.Duration.String), suitable for journal
// fields and diffing.
func (tl Timeline) String() string {
	return fmt.Sprintf("t_landed=%s t_symptom=%s t_detect=%s t_recover=%s",
		tl.Landed, tl.Symptom, tl.Detect, tl.Recover)
}
