package models

import "testing"

func TestParsePathDataRejectsNonFiniteValues(t *testing.T) {
	for _, data := range []string{
		"M NaN 0",
		"L 0 +Inf",
		"A 1 2 NaN 0 0 3 4",
	} {
		t.Run(data, func(t *testing.T) {
			if _, err := ParsePathData(data); err == nil {
				t.Fatal("expected non-finite path value to be rejected")
			}
		})
	}
}
