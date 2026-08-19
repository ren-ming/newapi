package accountautomation

import (
	"context"
	"crypto/subtle"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// JobService is the smallest service contract required by the HTTP layer.
type JobService interface {
	SubmitJob(context.Context, CreateJobRequest) (Job, error)
	GetJob(string) (Job, bool)
	ListJobs(offset, limit int) ([]Job, int64, error)
}

type ServerLogger = *log.Logger

const (
	maxRequestBodyBytes int64 = 1 << 20
	defaultJobListLimit       = 50
	maxJobListLimit           = 200
)

func NewServer(service JobService, adminToken string, logger ServerLogger) http.Handler {
	if service == nil || strings.TrimSpace(adminToken) == "" {
		panic("accountautomation: service and admin token are required")
	}
	server := &server{service: service, adminToken: adminToken, logger: logger}
	return http.HandlerFunc(server.serveHTTP)
}

// NewTrustedServer serves the same routes as NewServer but skips Bearer
// authentication: it must only be mounted behind a host that authenticates
// requests itself (e.g. the new-api admin middleware in front of
// /api/account-automation).
func NewTrustedServer(service JobService, logger ServerLogger) http.Handler {
	if service == nil {
		panic("accountautomation: service is required")
	}
	server := &server{service: service, logger: logger}
	return http.HandlerFunc(server.serveHTTP)
}

type server struct {
	service    JobService
	adminToken string
	logger     ServerLogger
}

func (s *server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/healthz":
		s.health(w, r)
	case r.URL.Path == "/jobs":
		s.apiJobs(w, r)
	case strings.HasPrefix(r.URL.Path, "/jobs/"):
		s.apiJob(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) apiJobs(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		offset, limit := jobListParams(r)
		jobs, total, err := s.service.ListJobs(offset, limit)
		if err != nil {
			statusError(w, http.StatusInternalServerError, "list jobs failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs, "total": total})
	case http.MethodPost:
		defer r.Body.Close()
		data, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes+1))
		if err != nil {
			statusError(w, http.StatusBadRequest, "invalid request")
			return
		}
		if int64(len(data)) > maxRequestBodyBytes {
			statusError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		var request CreateJobRequest
		if err := common.Unmarshal(data, &request); err != nil {
			statusError(w, http.StatusBadRequest, "invalid request")
			return
		}
		job, err := s.service.SubmitJob(r.Context(), request)
		if err != nil {
			statusError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, job)
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *server) apiJob(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/jobs/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	job, found := s.service.GetJob(id)
	if !found {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func jobListParams(r *http.Request) (offset, limit int) {
	offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = defaultJobListLimit
	}
	if limit > maxJobListLimit {
		limit = maxJobListLimit
	}
	return offset, limit
}

func (s *server) authorized(r *http.Request) bool {
	// Trusted mode (NewTrustedServer): the hosting process authenticates.
	if s.adminToken == "" {
		return true
	}
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") || strings.Count(value, " ") != 1 {
		return false
	}
	token := strings.TrimPrefix(value, "Bearer ")
	return len(token) == len(s.adminToken) && subtle.ConstantTimeCompare([]byte(token), []byte(s.adminToken)) == 1
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := common.Marshal(value)
	if err != nil {
		statusError(w, http.StatusInternalServerError, "encode response")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func statusError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
