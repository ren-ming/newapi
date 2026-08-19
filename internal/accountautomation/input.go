package accountautomation

import (
	"fmt"
	"net/mail"
	"strconv"
	"strings"
)

func ParseAccountLines(text string) ([]AccountSubmission, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	submissions := make([]AccountSubmission, 0, len(lines))
	channels := make(map[int]struct{}, len(lines))
	emails := make(map[string]struct{}, len(lines))

	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		parts := strings.SplitN(trimmed, "|", 2)
		if len(parts) != 2 {
			return nil, inputError(index+1, "missing_separator")
		}
		channelID, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil || channelID <= 0 {
			return nil, inputError(index+1, "invalid_channel_id")
		}
		accountLine := strings.TrimSpace(parts[1])
		if accountLine == "" {
			return nil, inputError(index+1, "empty_account")
		}
		email, ok := accountEmail(accountLine)
		if !ok {
			return nil, inputError(index+1, "invalid_email")
		}
		if _, exists := channels[channelID]; exists {
			return nil, inputError(index+1, "duplicate_channel")
		}
		if _, exists := emails[email]; exists {
			return nil, inputError(index+1, "duplicate_email")
		}
		channels[channelID] = struct{}{}
		emails[email] = struct{}{}
		submissions = append(submissions, AccountSubmission{
			ChannelID:   channelID,
			AccountLine: accountLine,
			Email:       email,
			MaskedEmail: MaskEmail(email),
		})
	}
	if len(submissions) == 0 {
		return nil, fmt.Errorf("empty_batch")
	}
	return submissions, nil
}

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

func accountEmail(accountLine string) (string, bool) {
	fields := strings.SplitN(accountLine, "----", 2)
	return normalizeEmail(fields[0])
}

func normalizeEmail(value string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(normalized)
	if err != nil || address.Address != normalized || !strings.Contains(normalized, "@") {
		return "", false
	}
	return normalized, true
}

func inputError(line int, class string) error {
	return fmt.Errorf("line %d: %s", line, class)
}
