package accountautomation

import "testing"

func TestParseSingleAccount(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		mode       string
		text       string
		wantErr    string
		wantEmail  string
		wantMasked string
	}{
		{
			name:       "microsoft two segments",
			mode:       AccountModeMicrosoft,
			text:       "user@example.com----secret",
			wantEmail:  "user@example.com",
			wantMasked: "u***r@example.com",
		},
		{
			name:       "microsoft four segments",
			mode:       AccountModeMicrosoft,
			text:       "user@example.com----secret----client-1----token-1",
			wantEmail:  "user@example.com",
			wantMasked: "u***r@example.com",
		},
		{
			name:    "microsoft three segments rejected",
			mode:    AccountModeMicrosoft,
			text:    "user@example.com----secret----extra",
			wantErr: "account_invalid",
		},
		{
			name:       "totp three segments",
			mode:       AccountModeTotp,
			text:       "user@example.com----secret----JBSWY3DPEHPK3PXP",
			wantEmail:  "user@example.com",
			wantMasked: "u***r@example.com",
		},
		{
			name:    "totp two segments rejected",
			mode:    AccountModeTotp,
			text:    "user@example.com----secret",
			wantErr: "account_invalid",
		},
		{
			name:    "unknown mode",
			mode:    "link",
			text:    "user@example.com----secret",
			wantErr: "account_mode_invalid",
		},
		{
			name:    "empty text",
			mode:    AccountModeMicrosoft,
			text:    "   ",
			wantErr: "account_invalid",
		},
		{
			name:    "multiline rejected",
			mode:    AccountModeMicrosoft,
			text:    "user@example.com----secret\nother@example.com----pw",
			wantErr: "account_invalid",
		},
		{
			name:    "invalid email",
			mode:    AccountModeMicrosoft,
			text:    "not-an-email----secret",
			wantErr: "account_invalid",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			email, masked, _, err := ParseSingleAccount(tt.mode, tt.text)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ParseSingleAccount() error = nil, want %q", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("ParseSingleAccount() error = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSingleAccount() error = %v", err)
			}
			if email != tt.wantEmail {
				t.Fatalf("email = %q, want %q", email, tt.wantEmail)
			}
			if masked != tt.wantMasked {
				t.Fatalf("masked = %q, want %q", masked, tt.wantMasked)
			}
		})
	}
}
