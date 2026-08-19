package accountautomation

import (
	"strings"
	"testing"
)

func TestParseAccountLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		want        []AccountSubmission
		wantErr     string
		secretParts []string
	}{
		{
			name:  "parses microsoft accounts and ignores blank lines",
			input: "\r\n  12 | User@Example.com----password----recovery@example.com  \r\n\n13|other@example.com----secret\n",
			want: []AccountSubmission{
				{ChannelID: 12, AccountLine: "User@Example.com----password----recovery@example.com", Email: "user@example.com", MaskedEmail: "u***r@example.com"},
				{ChannelID: 13, AccountLine: "other@example.com----secret", Email: "other@example.com", MaskedEmail: "o***r@example.com"},
			},
		},
		{name: "rejects empty batch", input: " \n\t", wantErr: "empty_batch"},
		{name: "rejects missing separator", input: "user@example.com----password", wantErr: "line 1: missing_separator", secretParts: []string{"user@example.com", "password"}},
		{name: "rejects empty channel", input: "|user@example.com----password", wantErr: "line 1: invalid_channel_id", secretParts: []string{"user@example.com", "password"}},
		{name: "rejects non numeric channel", input: "abc|user@example.com----password", wantErr: "line 1: invalid_channel_id", secretParts: []string{"user@example.com", "password"}},
		{name: "rejects zero channel", input: "0|user@example.com----password", wantErr: "line 1: invalid_channel_id", secretParts: []string{"user@example.com", "password"}},
		{name: "rejects empty account", input: "12|  ", wantErr: "line 1: empty_account", secretParts: []string{"12|"}},
		{name: "rejects invalid email", input: "12|not-an-email----password", wantErr: "line 1: invalid_email", secretParts: []string{"not-an-email", "password"}},
		{name: "rejects duplicate channel", input: "12|one@example.com----first\n12|two@example.com----second", wantErr: "line 2: duplicate_channel", secretParts: []string{"two@example.com", "second"}},
		{name: "rejects duplicate normalized email", input: "12|User@example.com----first\n13| user@EXAMPLE.COM----second", wantErr: "line 2: duplicate_email", secretParts: []string{"user@EXAMPLE.COM", "second"}},
		{
			name:  "keeps additional separators opaque",
			input: "12|user@example.com----password|with|pipes",
			want:  []AccountSubmission{{ChannelID: 12, AccountLine: "user@example.com----password|with|pipes", Email: "user@example.com", MaskedEmail: "u***r@example.com"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseAccountLines(tt.input)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("ParseAccountLines() error = %v, want %q", err, tt.wantErr)
				}
				for _, secret := range tt.secretParts {
					if strings.Contains(err.Error(), secret) {
						t.Fatalf("error leaked input fragment %q", secret)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAccountLines() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ParseAccountLines() length = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("submission %d = %#v, want %#v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

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
