package accountautomation

import (
	"fmt"
	"net/mail"
	"strings"
)

func MaskEmail(email string) string {
	normalized, ok := normalizeEmail(email)
	if !ok {
		return "***"
	}
	parts := strings.SplitN(normalized, "@", 2)
	local := parts[0]
	if len(local) <= 2 {
		return local[:1] + "***@" + parts[1]
	}
	return local[:1] + "***" + local[len(local)-1:] + "@" + parts[1]
}

func normalizeEmail(value string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(normalized)
	if err != nil || address.Address != normalized || !strings.Contains(normalized, "@") {
		return "", false
	}
	return normalized, true
}

// ParseSingleAccount validates one account line for the given mode and
// returns the normalized email, its masked form, and the trimmed line.
func ParseSingleAccount(mode, text string) (email, masked, line string, err error) {
	switch mode {
	case AccountModeMicrosoft, AccountModeTotp:
	default:
		return "", "", "", fmt.Errorf("account_mode_invalid")
	}
	trimmed := strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if trimmed == "" || strings.Contains(trimmed, "\n") {
		return "", "", "", fmt.Errorf("account_invalid")
	}
	parts := strings.Split(trimmed, "----")
	segmentOK := false
	if mode == AccountModeMicrosoft && (len(parts) == 2 || len(parts) == 4) {
		segmentOK = true
	}
	if mode == AccountModeTotp && len(parts) == 3 {
		segmentOK = true
	}
	if !segmentOK || strings.TrimSpace(parts[len(parts)-1]) == "" {
		return "", "", "", fmt.Errorf("account_invalid")
	}
	normalized, ok := normalizeEmail(parts[0])
	if !ok {
		return "", "", "", fmt.Errorf("account_invalid")
	}
	return normalized, MaskEmail(normalized), trimmed, nil
}
