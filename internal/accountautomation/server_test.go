package accountautomation

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubBatchService struct {
	submitRequest CreateBatchRequest
	submitBatch   Batch
	submitErr     error
	batches       []Batch
	batch         Batch
	found         bool
}

func (s *stubBatchService) Submit(_ context.Context, request CreateBatchRequest) (Batch, error) {
	s.submitRequest = request
	return s.submitBatch, s.submitErr
}

func (s *stubBatchService) GetBatch(id string) (Batch, bool) {
	if id != s.batch.ID {
		return Batch{}, false
	}
	return s.batch, s.found
}

func (s *stubBatchService) ListBatches() []Batch { return s.batches }

func TestServerPublicRoutes(t *testing.T) {
	handler := NewServer(&stubBatchService{}, "secret", nil)

	for _, testCase := range []struct {
		path        string
		contentType string
		body        string
	}{
		{path: "/healthz", contentType: "application/json", body: `{"status":"ok"}`},
		{path: "/", contentType: "text/html", body: "账号自动化"},
	} {
		t.Run(testCase.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, testCase.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			assert.Equal(t, http.StatusOK, response.Code)
			assert.Contains(t, response.Header().Get("Content-Type"), testCase.contentType)
			assert.Contains(t, response.Body.String(), testCase.body)
		})
	}
}

func TestServerRequiresExactBearerTokenForAPI(t *testing.T) {
	handler := NewServer(&stubBatchService{}, "secret", nil)

	for _, authorization := range []string{"", "secret", "Bearer wrong", "bearer secret", "Bearer secret extra"} {
		request := httptest.NewRequest(http.MethodGet, "/api/batches", nil)
		request.Header.Set("Authorization", authorization)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		assert.Equal(t, http.StatusUnauthorized, response.Code, authorization)
		assert.Equal(t, "Bearer", response.Header().Get("WWW-Authenticate"))
		assert.NotContains(t, response.Body.String(), "secret")
	}
}

func TestServerBatchEndpoints(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	batch := Batch{ID: "batch-1", Status: BatchStatusCreated, CreatedAt: now, UpdatedAt: now}
	service := &stubBatchService{submitBatch: batch, batches: []Batch{batch}, batch: batch, found: true}
	handler := NewServer(service, "secret", nil)

	t.Run("submit", func(t *testing.T) {
		request := authenticatedRequest(http.MethodPost, "/api/batches", `{"account_text":"12|a@example.com----password","bind_free":true}`)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		assert.Equal(t, http.StatusAccepted, response.Code)
		assert.Equal(t, "12|a@example.com----password", service.submitRequest.AccountText)
		assert.True(t, service.submitRequest.BindFree)
		assert.NotContains(t, response.Body.String(), "password")
		assert.Contains(t, response.Body.String(), `"id":"batch-1"`)
	})

	t.Run("list", func(t *testing.T) {
		response := serveAuthenticated(handler, http.MethodGet, "/api/batches", "")
		assert.Equal(t, http.StatusOK, response.Code)
		assert.Contains(t, response.Body.String(), `"id":"batch-1"`)
	})

	t.Run("get", func(t *testing.T) {
		response := serveAuthenticated(handler, http.MethodGet, "/api/batches/batch-1", "")
		assert.Equal(t, http.StatusOK, response.Code)
		assert.Contains(t, response.Body.String(), `"id":"batch-1"`)
	})

	t.Run("not found", func(t *testing.T) {
		response := serveAuthenticated(handler, http.MethodGet, "/api/batches/missing", "")
		assert.Equal(t, http.StatusNotFound, response.Code)
	})
}

func TestServerRejectsInvalidAndOversizedRequests(t *testing.T) {
	service := &stubBatchService{submitErr: errors.New("empty_batch")}
	handler := NewServer(service, "secret", nil)

	t.Run("invalid JSON", func(t *testing.T) {
		response := serveAuthenticated(handler, http.MethodPost, "/api/batches", "{")
		assert.Equal(t, http.StatusBadRequest, response.Code)
	})

	t.Run("service validation", func(t *testing.T) {
		response := serveAuthenticated(handler, http.MethodPost, "/api/batches", `{"account_text":""}`)
		assert.Equal(t, http.StatusBadRequest, response.Code)
		assert.Contains(t, response.Body.String(), "empty_batch")
	})

	t.Run("too large", func(t *testing.T) {
		response := serveAuthenticated(handler, http.MethodPost, "/api/batches", `{"account_text":"`+strings.Repeat("x", int(maxRequestBodyBytes))+`"}`)
		assert.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
	})

	t.Run("wrong method", func(t *testing.T) {
		response := serveAuthenticated(handler, http.MethodPut, "/api/batches", "")
		assert.Equal(t, http.StatusMethodNotAllowed, response.Code)
	})
}

func TestServerPageAvoidsPersistentStorageAndHandlesSensitiveInput(t *testing.T) {
	handler := NewServer(&stubBatchService{}, "secret", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	page := response.Body.String()

	assert.NotContains(t, page, "localStorage")
	assert.Contains(t, page, "accountText.value = ''")
	assert.Contains(t, page, "setTimeout")
	for _, field := range []string{"masked_email", "channel_id", "status", "error_class"} {
		assert.Contains(t, page, field)
	}
}

func authenticatedRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer secret")
	request.Header.Set("Content-Type", "application/json")
	return request
}

func serveAuthenticated(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(method, path, body))
	return response
}

func TestNewServerRejectsEmptyAdminToken(t *testing.T) {
	require.Panics(t, func() { NewServer(&stubBatchService{}, "", nil) })
}
