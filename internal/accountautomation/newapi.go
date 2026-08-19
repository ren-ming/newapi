package accountautomation

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const maxNewAPIResponseBytes = 1 << 20

type NewAPIClient struct {
	baseURL     string
	accessToken string
	userID      string
	httpClient  *http.Client
}

func NewNewAPIClient(baseURL, accessToken, userID string, httpClient *http.Client) *NewAPIClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &NewAPIClient{
		baseURL: strings.TrimRight(baseURL, "/"), accessToken: accessToken,
		userID: userID, httpClient: httpClient,
	}
}

func (c *NewAPIClient) UpdateChannel(ctx context.Context, channelID int, credential Credential) error {
	if channelID <= 0 {
		return fmt.Errorf("newapi_invalid_channel")
	}
	if strings.TrimSpace(credential.AccessToken) == "" || strings.TrimSpace(credential.AccountID) == "" {
		return fmt.Errorf("newapi_invalid_credential")
	}
	if err := c.verifyCodexChannel(ctx, channelID); err != nil {
		return err
	}
	key, err := common.Marshal(credential)
	if err != nil {
		return fmt.Errorf("newapi_encode_request")
	}
	body, err := common.Marshal(struct {
		ID   int    `json:"id"`
		Type int    `json:"type"`
		Key  string `json:"key"`
	}{ID: channelID, Type: 57, Key: string(key)})
	if err != nil {
		return fmt.Errorf("newapi_encode_request")
	}
	response, err := c.do(ctx, http.MethodPut, "/api/channel/", body)
	if err != nil {
		return err
	}
	return parseNewAPIResponse(response)
}

func (c *NewAPIClient) TestChannel(ctx context.Context, channelID int) (ChannelTestResult, error) {
	if channelID <= 0 {
		return ChannelTestResult{}, fmt.Errorf("newapi_invalid_channel")
	}
	response, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/channel/test/%d", channelID), nil)
	if err != nil {
		return ChannelTestResult{}, err
	}
	var result ChannelTestResult
	if err := parseNewAPIEnvelope(response, &result); err != nil {
		return ChannelTestResult{}, err
	}
	return result, nil
}

func (c *NewAPIClient) verifyCodexChannel(ctx context.Context, channelID int) error {
	response, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/channel/%d", channelID), nil)
	if err != nil {
		return err
	}
	var envelope struct {
		Success bool `json:"success"`
		Data    struct {
			ID   int `json:"id"`
			Type int `json:"type"`
		} `json:"data"`
	}
	if err := common.Unmarshal(response, &envelope); err != nil {
		return fmt.Errorf("newapi_invalid_response")
	}
	if !envelope.Success || envelope.Data.ID != channelID || envelope.Data.Type != 57 {
		return fmt.Errorf("newapi_channel_precondition_failed")
	}
	return nil
}

func (c *NewAPIClient) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("newapi_transport_error")
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", c.accessToken)
	request.Header.Set("New-Api-User", c.userID)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("newapi_transport_error")
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxNewAPIResponseBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("newapi_read_error")
	}
	if len(payload) > maxNewAPIResponseBytes {
		return nil, fmt.Errorf("newapi_response_too_large")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("newapi_http_error")
	}
	return payload, nil
}

func parseNewAPIResponse(response []byte) error {
	return parseNewAPIEnvelope(response, nil)
}

func parseNewAPIEnvelope(response []byte, result *ChannelTestResult) error {
	var envelope struct {
		Success bool               `json:"success"`
		Data    *ChannelTestResult `json:"data"`
	}
	if err := common.Unmarshal(response, &envelope); err != nil {
		return fmt.Errorf("newapi_invalid_response")
	}
	if !envelope.Success {
		return fmt.Errorf("newapi_business_error")
	}
	if result != nil {
		if envelope.Data != nil {
			*result = *envelope.Data
			return nil
		}
		if err := common.Unmarshal(response, result); err != nil {
			return fmt.Errorf("newapi_invalid_response")
		}
	}
	return nil
}
