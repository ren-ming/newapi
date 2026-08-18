package service

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func newChannelDecisionError(statusCode int, message string) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeBadResponse, statusCode)
}

func TestShouldDisableCodexCredentialError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		message    string
		want       bool
	}{
		{"invalidated oauth token", http.StatusUnauthorized, "Encountered invalidated oauth token for user, failing request", true},
		{"usage limit reached", http.StatusTooManyRequests, "The usage limit has been reached", true},
		{"case insensitive oauth message", http.StatusUnauthorized, "INVALIDATED OAUTH TOKEN FOR USER", true},
		{"unrelated unauthorized", http.StatusUnauthorized, "invalid api key", false},
		{"unrelated rate limit", http.StatusTooManyRequests, "requests per minute exceeded", false},
		{"oauth text with wrong status", http.StatusTooManyRequests, "invalidated oauth token for user", false},
		{"usage text with wrong status", http.StatusUnauthorized, "the usage limit has been reached", false},
		{"nil error", 0, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err *types.NewAPIError
			if tt.message != "" {
				err = newChannelDecisionError(tt.statusCode, tt.message)
			}
			require.Equal(t, tt.want, shouldDisableCodexCredentialError(err))
		})
	}
}

func TestShouldDisableChannel(t *testing.T) {
	originalEnabled := common.AutomaticDisableChannelEnabled
	originalRanges := operation_setting.AutomaticDisableStatusCodeRanges
	originalKeywords := operation_setting.AutomaticDisableKeywords
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = originalEnabled
		operation_setting.AutomaticDisableStatusCodeRanges = originalRanges
		operation_setting.AutomaticDisableKeywords = originalKeywords
	})
	common.AutomaticDisableChannelEnabled = true
	operation_setting.AutomaticDisableStatusCodeRanges = []operation_setting.StatusCodeRange{{Start: http.StatusUnauthorized, End: http.StatusUnauthorized}, {Start: http.StatusInternalServerError, End: http.StatusInternalServerError}}
	operation_setting.AutomaticDisableKeywords = []string{"permission denied"}
	tests := []struct {
		name        string
		channelType int
		err         *types.NewAPIError
		want        bool
	}{
		{"codex exact oauth error", constant.ChannelTypeCodex, newChannelDecisionError(http.StatusUnauthorized, "invalidated oauth token for user"), true},
		{"codex exact usage error", constant.ChannelTypeCodex, newChannelDecisionError(http.StatusTooManyRequests, "The usage limit has been reached"), true},
		{"codex unrelated 401 bypasses generic 401 rule", constant.ChannelTypeCodex, newChannelDecisionError(http.StatusUnauthorized, "invalid api key"), false},
		{"codex unrelated 429 bypasses generic keyword rule", constant.ChannelTypeCodex, newChannelDecisionError(http.StatusTooManyRequests, "permission denied"), false},
		{"non codex keeps generic 401 rule", constant.ChannelTypeOpenAI, newChannelDecisionError(http.StatusUnauthorized, "invalid api key"), true},
		{"codex other status keeps generic status rule", constant.ChannelTypeCodex, newChannelDecisionError(http.StatusInternalServerError, "upstream unavailable"), true},
		{"codex other status keeps generic keyword rule", constant.ChannelTypeCodex, newChannelDecisionError(http.StatusForbidden, "permission denied"), true},
		{"nil error", constant.ChannelTypeCodex, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { require.Equal(t, tt.want, ShouldDisableChannel(tt.channelType, tt.err)) })
	}
}

func TestShouldDisableChannel_GlobalSwitchOff(t *testing.T) {
	originalEnabled := common.AutomaticDisableChannelEnabled
	t.Cleanup(func() { common.AutomaticDisableChannelEnabled = originalEnabled })
	common.AutomaticDisableChannelEnabled = false
	require.False(t, ShouldDisableChannel(constant.ChannelTypeCodex, newChannelDecisionError(http.StatusUnauthorized, "invalidated oauth token for user")))
}
