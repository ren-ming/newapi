package accountautomation

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
)

type fakeJobService struct {
	submitted CreateJobRequest
	submitJob Job
	submitErr error
	getJob    Job
	found     bool
	jobs      []Job
	total     int64
}

func (f *fakeJobService) SubmitJob(_ context.Context, request CreateJobRequest) (Job, error) {
	f.submitted = request
	return f.submitJob, f.submitErr
}

func (f *fakeJobService) GetJob(id string) (Job, bool) {
	if f.found {
		f.getJob.ID = id
		return f.getJob, true
	}
	return Job{}, false
}

func (f *fakeJobService) ListJobs(offset, limit int) ([]Job, int64, error) {
	if offset < int(f.total) && limit > 0 {
		return f.jobs, f.total, nil
	}
	return []Job{}, f.total, nil
}

func TestServerJobsLifecycle(t *testing.T) {
	t.Parallel()
	service := &fakeJobService{
		submitJob: Job{ID: "job-1", MaskedEmail: "u***r@example.com", Status: JobStatusSubmitting},
		found:     true,
		getJob:    Job{MaskedEmail: "u***r@example.com"},
		jobs:      []Job{{ID: "job-1", MaskedEmail: "u***r@example.com"}},
		total:     1,
	}
	handler := NewServer(service, "secret-token", nil)

	body := `{"account_mode":"microsoft","account_text":"user@example.com----secret","channel_id":42,"bind_free":true}`
	request := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer secret-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("POST /jobs status = %d, want 202: %s", recorder.Code, recorder.Body.String())
	}
	if service.submitted.AccountMode != AccountModeMicrosoft || service.submitted.ChannelID != 42 || !service.submitted.BindFree {
		t.Fatalf("submitted request = %#v", service.submitted)
	}
	var created Job
	if err := common.Unmarshal(recorder.Body.Bytes(), &created); err != nil || created.ID != "job-1" {
		t.Fatalf("response = %s (%v)", recorder.Body.String(), err)
	}

	request = httptest.NewRequest(http.MethodGet, "/jobs/job-1", nil)
	request.Header.Set("Authorization", "Bearer secret-token")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "u***r@example.com") {
		t.Fatalf("GET /jobs/job-1 = %d %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/jobs?offset=0&limit=10", nil)
	request.Header.Set("Authorization", "Bearer secret-token")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"total":1`) {
		t.Fatalf("GET /jobs = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestServerJobsValidation(t *testing.T) {
	t.Parallel()
	service := &fakeJobService{submitErr: fmt.Errorf("account_mode_invalid")}
	handler := NewServer(service, "secret-token", nil)

	request := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(`{"account_mode":"link"}`))
	request.Header.Set("Authorization", "Bearer secret-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "account_mode_invalid") {
		t.Fatalf("validation status = %d %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader("not-json"))
	request.Header.Set("Authorization", "Bearer secret-token")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("bad body status = %d %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/jobs", nil)
	request.Header.Set("Authorization", "Bearer secret-token")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "total") {
		t.Fatalf("list status = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestServerJobsAuth(t *testing.T) {
	t.Parallel()
	service := &fakeJobService{jobs: []Job{}, total: 0}
	handler := NewServer(service, "secret-token", nil)

	for name, header := range map[string]string{
		"missing token": "",
		"wrong token":   "Bearer wrong",
	} {
		request := httptest.NewRequest(http.MethodGet, "/jobs", nil)
		if header != "" {
			request.Header.Set("Authorization", header)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401", name, recorder.Code)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	request.Header.Set("Authorization", "Bearer secret-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("valid token status = %d, want 200", recorder.Code)
	}
}

func TestServerHealthzAndLegacyRoutesRemoved(t *testing.T) {
	t.Parallel()
	service := &fakeJobService{}
	handler := NewTrustedServer(service, nil)

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "ok") {
		t.Fatalf("healthz = %d %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/batches", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("legacy /batches = %d, want 404", recorder.Code)
	}
}
