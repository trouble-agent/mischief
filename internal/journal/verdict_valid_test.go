package journal

import "testing"

// ValidVerdict gates the closed verdict vocabulary for writers that take
// a verdict as free text (the chaos finding CLI flag, the findings-row
// validator): every declared §6.4 value accepts, anything else refuses.
func TestValidVerdictClosedVocabulary(t *testing.T) {
	declared := []Verdict{
		VerdictRecovered, VerdictDegraded, VerdictNotRecovered, VerdictHung,
		VerdictCorrupted, VerdictNoOp, VerdictAborted, VerdictFlaky, VerdictVoid,
	}
	for _, v := range declared {
		if !ValidVerdict(string(v)) {
			t.Errorf("ValidVerdict(%q) = false, want true (declared §6.4 value)", v)
		}
	}
	for _, bad := range []string{"banana", "pass", "PASS", "recovered ", " recovered", "recovered\n", "recovered,", ""} {
		if ValidVerdict(bad) {
			t.Errorf("ValidVerdict(%q) = true, want false (outside the closed vocabulary)", bad)
		}
	}
}
