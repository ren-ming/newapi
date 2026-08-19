package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/accountautomation"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

// AccountAutomationChannelService implements accountautomation.NewAPIService
// by calling the new-api model and controller layers directly: no HTTP hop
// and no admin access token. Error classes mirror the HTTP client in
// internal/accountautomation/newapi.go so orchestrator accounting stays stable.
type AccountAutomationChannelService struct{}

var _ accountautomation.NewAPIService = AccountAutomationChannelService{}

// accountAutomationRunChannelTest is a seam for tests; production calls the
// package's testChannel, which performs a live upstream request.
var accountAutomationRunChannelTest = testChannel

func (AccountAutomationChannelService) UpdateChannel(_ context.Context, channelID int, credential accountautomation.Credential) error {
	if channelID <= 0 {
		return errors.New("newapi_invalid_channel")
	}
	if strings.TrimSpace(credential.AccessToken) == "" || strings.TrimSpace(credential.AccountID) == "" {
		return errors.New("newapi_invalid_credential")
	}
	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		return errors.New("newapi_channel_not_found")
	}
	if channel.Type != constant.ChannelTypeCodex {
		return errors.New("newapi_channel_precondition_failed")
	}
	key, err := common.Marshal(credential)
	if err != nil {
		return errors.New("newapi_encode_request")
	}
	channel.Key = string(key)
	if err := channel.Update(); err != nil {
		return fmt.Errorf("newapi_channel_update_failed: %w", err)
	}
	model.InitChannelCache()
	service.ResetProxyClientCache()
	return nil
}

func (AccountAutomationChannelService) TestChannel(_ context.Context, channelID int) (accountautomation.ChannelTestResult, error) {
	if channelID <= 0 {
		return accountautomation.ChannelTestResult{}, errors.New("newapi_invalid_channel")
	}
	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		return accountautomation.ChannelTestResult{}, errors.New("newapi_channel_not_found")
	}
	if channel.Type != constant.ChannelTypeCodex {
		return accountautomation.ChannelTestResult{}, errors.New("newapi_channel_precondition_failed")
	}
	// Empty testModel/endpointType mirrors the HTTP handler defaults; the
	// endpoint normalizer maps Codex channels onto the responses endpoint.
	result := accountAutomationRunChannelTest(channel, "", "", false)
	if result.localErr != nil {
		return accountautomation.ChannelTestResult{Success: false, Message: result.localErr.Error()}, nil
	}
	return accountautomation.ChannelTestResult{Success: true}, nil
}

// --- Boot wiring for the embedded new-api deployment ---

var (
	accountAutomationOnce   sync.Once
	accountAutomationServer http.Handler
)

type accountAutomationLogger struct{}

func (accountAutomationLogger) Info(event string, fields map[string]any) {
	common.SysLog(fmt.Sprintf("level=info event=%s fields=%v", event, fields))
}

func (accountAutomationLogger) Error(event string, fields map[string]any) {
	common.SysError(fmt.Sprintf("level=error event=%s fields=%v", event, fields))
}

// InitAccountAutomation wires the SMS688→NewAPI orchestrator for in-process
// use behind the admin API. It is a no-op (AccountAutomationHandler stays nil)
// when SMS688_ACCOUNT_API_KEY is unset, in which case the routes don't mount.
// Call it once at startup, before SetApiRouter.
func InitAccountAutomation() {
	accountAutomationOnce.Do(func() {
		apiKey := strings.TrimSpace(os.Getenv("SMS688_ACCOUNT_API_KEY"))
		if apiKey == "" {
			common.SysLog("account automation disabled: SMS688_ACCOUNT_API_KEY is not set")
			return
		}
		baseURL := strings.TrimSpace(os.Getenv("SMS688_BASE_URL"))
		if baseURL == "" {
			baseURL = accountautomation.DefaultSMS688BaseURL
		}
		orchestrator := accountautomation.NewOrchestrator(
			accountautomation.NewMemoryJobStore(),
			accountautomation.NewSMS688Client(baseURL, apiKey, &http.Client{Timeout: 30 * time.Second}),
			AccountAutomationChannelService{},
			accountAutomationLogger{},
			accountautomation.OrchestratorConfig{},
		)
		accountAutomationServer = accountautomation.NewTrustedServer(orchestrator, nil)
		common.SysLog("account automation enabled")
	})
}

// AccountAutomationHandler returns the trusted handler mounted under the
// /api/account-automation admin group, or nil when automation is disabled.
func AccountAutomationHandler() http.Handler {
	return accountAutomationServer
}
