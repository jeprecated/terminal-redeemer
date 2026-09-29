package termquery

import (
	"os"
	"testing"
)

func TestSuppressRestoresPriorCIState(t *testing.T) {
	cases := []struct {
		name       string
		set        bool
		value      string
		suppressed bool
	}{
		{name: "unset", suppressed: true},
		{name: "set but empty", set: true, value: "", suppressed: true},
		{name: "non-empty", set: true, value: "true"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(suppressVariable, "sentinel") // registers cleanup of the original
			if tc.set {
				_ = os.Setenv(suppressVariable, tc.value)
			} else {
				_ = os.Unsetenv(suppressVariable)
			}
			undo := suppress()
			if got := os.Getenv(suppressVariable); got == "" {
				t.Fatalf("termenv would still query: CI=%q", got)
			}
			if (undo != nil) != tc.suppressed {
				t.Fatalf("suppressed=%t want %t", undo != nil, tc.suppressed)
			}
			if undo != nil {
				undo()
			}
			value, set := os.LookupEnv(suppressVariable)
			if set != tc.set || value != tc.value {
				t.Fatalf("restored set=%t value=%q, want set=%t value=%q", set, value, tc.set, tc.value)
			}
		})
	}
}
