package accountautomation

import (
	"testing"
)

func TestMaskEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{input: "user@example.com", want: "u***r@example.com"},
		{input: "ab@example.com", want: "a***@example.com"},
		{input: "a@example.com", want: "a***@example.com"},
		{input: " User@Example.COM ", want: "u***r@example.com"},
		{input: "invalid", want: "***"},
		{input: "", want: "***"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			if got := MaskEmail(tt.input); got != tt.want {
				t.Errorf("MaskEmail(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
