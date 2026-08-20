package accountautomation

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestSMS688ClientCreateTask(t *testing.T) {
	t.Parallel()
	const apiKey, idempotencyKey = "secret-api-key", "submission-123"
	wantRequest := SMS688CreateRequest{AccountMode: "email", AccountText: "private@example.test", BindFree: true}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(t, r, http.MethodPost, "/api/v1/tasks", apiKey)
		if got := r.Header.Get("Idempotency-Key"); got != idempotencyKey {
			t.Errorf("Idempotency-Key = %q", got)
		}
		if got := r.Header.Get("X-Submission-Token"); got != idempotencyKey {
			t.Errorf("X-Submission-Token = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var got SMS688CreateRequest
		if err := common.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if got != wantRequest {
			t.Errorf("request = %#v, want %#v", got, wantRequest)
		}
		_, _ = io.WriteString(w, `{"batch_id":"batch-1","all_finished":false,"total":2,"complete":1,"unknown":"ignored","jobs":[{"id":"job-1","email":"a@example.test","email_masked":"a***@example.test","status":"running","stage":"signup"}]}`)
	}))
	defer server.Close()

	got, err := NewSMS688Client(server.URL, apiKey, server.Client()).CreateTask(context.Background(), wantRequest, idempotencyKey)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if got.BatchID != "batch-1" || got.Total != 2 || got.Complete != 1 || len(got.Jobs) != 1 || got.Jobs[0].ID != "job-1" {
		t.Fatalf("response = %#v", got)
	}
}

func TestSMS688ClientGetTask(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(t, r, http.MethodGet, "/api/v1/tasks/batch-1", "key")
		_, _ = io.WriteString(w, `{"batches":[{"batch_id":"batch-1","all_finished":true,"total":5,"complete":2,"error":1,"cancelled":1,"expired":1,"status":"complete"}]}`)
	}))
	defer server.Close()

	got, err := NewSMS688Client(server.URL, "key", server.Client()).GetTask(context.Background(), "batch-1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !got.AllFinished || got.Total != 5 || got.Complete != 2 || got.Error != 1 || got.Cancelled != 1 || got.Expired != 1 {
		t.Fatalf("response = %#v", got)
	}
}

func TestSMS688ClientGetTaskSelectsRequestedBatch(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"batches":[{"batch_id":"other","all_finished":true},{"batch_id":"batch-1","all_finished":true,"total":1,"complete":1}]}`)
	}))
	defer server.Close()

	got, err := NewSMS688Client(server.URL, "key", server.Client()).GetTask(context.Background(), "batch-1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.BatchID != "batch-1" || got.Total != 1 || got.Complete != 1 {
		t.Fatalf("response = %#v", got)
	}
}

func TestSMS688ClientGetTaskRejectsMissingRequestedBatch(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"batches":[{"batch_id":"other"}]}`)
	}))
	defer server.Close()

	_, err := NewSMS688Client(server.URL, "key", server.Client()).GetTask(context.Background(), "batch-1")
	if err == nil || !strings.HasPrefix(err.Error(), "sms688_invalid_response:") {
		t.Fatalf("error = %v", err)
	}
}

func TestSMS688ClientDownloadCPA(t *testing.T) {
	t.Parallel()
	const cpa = "private,cpa,body\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(t, r, http.MethodGet, "/api/v1/tasks/batch-1/download/cpa", "key")
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		_, _ = io.WriteString(w, cpa)
	}))
	defer server.Close()

	got, err := NewSMS688Client(server.URL, "key", server.Client()).DownloadCPA(context.Background(), "batch-1")
	if err != nil {
		t.Fatalf("DownloadCPA: %v", err)
	}
	if got.ContentType != "text/csv; charset=utf-8" || string(got.Data) != cpa {
		t.Fatalf("download = %#v", got)
	}
}

func TestSMS688ClientRejectsOversizedResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", SMS688MaxResponseBytes+1))
	}))
	defer server.Close()

	_, err := NewSMS688Client(server.URL, "key", server.Client()).DownloadCPA(context.Background(), "batch-1")
	if err == nil || !strings.Contains(err.Error(), "response exceeds limit") {
		t.Fatalf("error = %v", err)
	}
}

func TestSMS688ClientErrorsDoNotLeakSecrets(t *testing.T) {
	t.Parallel()
	const apiKey, accountText, responseBody = "secret-key", "raw-account:password", "private CPA/error body"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, responseBody)
	}))
	defer server.Close()
	client := NewSMS688Client(server.URL, apiKey, server.Client())

	calls := []func() error{
		func() error {
			_, err := client.CreateTask(context.Background(), SMS688CreateRequest{AccountText: accountText}, "secret-idempotency")
			return err
		},
		func() error { _, err := client.GetTask(context.Background(), "batch-1"); return err },
		func() error { _, err := client.DownloadCPA(context.Background(), "batch-1"); return err },
	}
	for _, call := range calls {
		err := call()
		if err == nil {
			t.Fatal("expected error")
		}
		for _, secret := range []string{apiKey, accountText, responseBody, "secret-idempotency"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error leaked secret: %q", err)
			}
		}
	}
}

func TestSMS688ClientRejectsEmptyIdentifiers(t *testing.T) {
	t.Parallel()
	client := NewSMS688Client("http://127.0.0.1", "key", http.DefaultClient)
	if _, err := client.CreateTask(context.Background(), SMS688CreateRequest{}, ""); err == nil {
		t.Fatal("expected empty idempotency key error")
	}
	if _, err := client.GetTask(context.Background(), ""); err == nil {
		t.Fatal("expected empty batch ID error")
	}
}

func TestSMS688ClientDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()
	var redirectHits int32
	evil := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&redirectHits, 1)
	}))
	defer evil.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", evil.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	_, err := NewSMS688Client(server.URL, "key", server.Client()).GetTask(context.Background(), "batch-1")
	if err == nil || !strings.HasPrefix(err.Error(), "sms688_http_error:") {
		t.Fatalf("error = %v", err)
	}
	if atomic.LoadInt32(&redirectHits) != 0 {
		t.Fatal("client followed a redirect to an external host")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestSMS688ClientPreservesErrorChain(t *testing.T) {
	t.Parallel()
	transportErr := errors.New("connection reset by peer")
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return nil, transportErr
	})}

	_, err := NewSMS688Client("http://127.0.0.1", "key", client).GetTask(context.Background(), "batch-1")
	if err == nil || !strings.HasPrefix(err.Error(), "sms688_transport_error:") {
		t.Fatalf("error = %v", err)
	}
	if !errors.Is(err, transportErr) {
		t.Fatalf("error chain lost: %v", err)
	}
}

func TestSMS688ClientRequiresBatchIDInResponses(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"all_finished":false}`)
	}))
	defer server.Close()
	client := NewSMS688Client(server.URL, "key", server.Client())

	_, err := client.CreateTask(context.Background(), SMS688CreateRequest{}, "submission-1")
	if err == nil || !strings.HasPrefix(err.Error(), "sms688_invalid_response:") {
		t.Fatalf("CreateTask error = %v", err)
	}
	_, err = client.GetTask(context.Background(), "batch-1")
	if err == nil || !strings.HasPrefix(err.Error(), "sms688_invalid_response:") {
		t.Fatalf("GetTask error = %v", err)
	}
}

func TestSMS688ClientInvalidResponseDoesNotLeakBody(t *testing.T) {
	t.Parallel()
	const responseBody = "private-upstream-error-detail"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "not-json"+responseBody)
	}))
	defer server.Close()

	_, err := NewSMS688Client(server.URL, "key", server.Client()).GetTask(context.Background(), "batch-1")
	if err == nil || !strings.HasPrefix(err.Error(), "sms688_decode_error:") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), responseBody) {
		t.Fatalf("error leaked response body: %q", err)
	}
}

func assertRequest(t *testing.T, r *http.Request, method, path, apiKey string) {
	t.Helper()
	if r.Method != method || r.URL.EscapedPath() != path {
		t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
	}
	if got := r.Header.Get("Authorization"); got != "Bearer "+apiKey {
		t.Errorf("Authorization = %q", got)
	}
}
