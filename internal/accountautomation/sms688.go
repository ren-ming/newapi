package accountautomation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const SMS688MaxResponseBytes = 8 << 20

// DefaultSMS688BaseURL is the shared default for SMS688_BASE_URL overrides.
const DefaultSMS688BaseURL = "https://cdk.sms688.cc"

const sms688TasksPath = "/api/v1/tasks"

type SMS688Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewSMS688Client(baseURL string, apiKey string, httpClient *http.Client) *SMS688Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	// Never follow redirects: Idempotency-Key/X-Submission-Token are not in
	// net/http's cross-host sensitive-header strip list, and a 307/308 would
	// replay the request body (plaintext account lines). Copy the client so
	// the caller's shared instance keeps its own redirect policy.
	ownClient := *httpClient
	ownClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &SMS688Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: &ownClient,
	}
}

func (c *SMS688Client) CreateTask(ctx context.Context, request SMS688CreateRequest, idempotencyKey string) (RemoteBatch, error) {
	var batch RemoteBatch
	if idempotencyKey == "" {
		return batch, errors.New("sms688_invalid_request: idempotency key is required")
	}
	body, err := common.Marshal(request)
	if err != nil {
		return batch, fmt.Errorf("sms688_encode_request: %w", err)
	}
	req, err := c.newRequest(ctx, http.MethodPost, sms688TasksPath, bytes.NewReader(body))
	if err != nil {
		return batch, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	req.Header.Set("X-Submission-Token", idempotencyKey)

	response, err := c.do(req)
	if err != nil {
		return batch, err
	}
	if err := common.Unmarshal(response.body, &batch); err != nil {
		return batch, fmt.Errorf("sms688_decode_error: %w", err)
	}
	if batch.BatchID == "" {
		return batch, errors.New("sms688_invalid_response: missing batch_id")
	}
	return batch, nil
}

func (c *SMS688Client) GetTask(ctx context.Context, batchID string) (RemoteBatch, error) {
	var batch RemoteBatch
	path, err := c.taskPath(batchID, "")
	if err != nil {
		return batch, err
	}
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return batch, err
	}
	response, err := c.do(req)
	if err != nil {
		return batch, err
	}
	var envelope remoteBatchEnvelope
	if err := common.Unmarshal(response.body, &envelope); err != nil {
		return batch, fmt.Errorf("sms688_decode_error: %w", err)
	}
	for _, candidate := range envelope.Batches {
		if candidate.BatchID == batchID {
			return candidate, nil
		}
	}
	return batch, errors.New("sms688_invalid_response: missing batch_id")
}

func (c *SMS688Client) DownloadCPA(ctx context.Context, batchID string) (DownloadedCPA, error) {
	var download DownloadedCPA
	path, err := c.taskPath(batchID, "/download/cpa")
	if err != nil {
		return download, err
	}
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return download, err
	}
	response, err := c.do(req)
	if err != nil {
		return download, err
	}
	return DownloadedCPA{
		ContentType: response.contentType,
		Data:        response.body,
	}, nil
}

func (c *SMS688Client) taskPath(batchID string, suffix string) (string, error) {
	if batchID == "" {
		return "", errors.New("sms688_invalid_request: batch ID is required")
	}
	return sms688TasksPath + "/" + url.PathEscape(batchID) + suffix, nil
}

func (c *SMS688Client) newRequest(ctx context.Context, method string, path string, body io.Reader) (*http.Request, error) {
	if c == nil || c.httpClient == nil || c.baseURL == "" {
		return nil, errors.New("sms688_invalid_request: client is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("sms688_encode_request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	return req, nil
}

type sms688Response struct {
	contentType string
	body        []byte
}

func (c *SMS688Client) do(req *http.Request) (sms688Response, error) {
	var result sms688Response
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return result, fmt.Errorf("sms688_transport_error: %w", err)
	}
	defer resp.Body.Close()

	body, err := readLimited(resp.Body, SMS688MaxResponseBytes)
	if err != nil {
		return result, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return result, fmt.Errorf("sms688_http_error: unexpected HTTP status %d", resp.StatusCode)
	}
	return sms688Response{
		contentType: resp.Header.Get("Content-Type"),
		body:        body,
	}, nil
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, fmt.Errorf("sms688_read_error: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, errors.New("sms688_response_too_large: response exceeds limit")
	}
	return body, nil
}
