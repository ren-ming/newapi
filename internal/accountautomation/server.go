package accountautomation

import (
	"context"
	"crypto/subtle"
	"embed"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// BatchService is the smallest service contract required by the HTTP layer.
type BatchService interface {
	Submit(context.Context, CreateBatchRequest) (Batch, error)
	GetBatch(string) (Batch, bool)
	ListBatches() []Batch
}

type ServerLogger = *log.Logger

const maxRequestBodyBytes int64 = 1 << 20

//go:embed web/index.html
var webFiles embed.FS

func NewServer(service BatchService, adminToken string, logger ServerLogger) http.Handler {
	if service == nil || strings.TrimSpace(adminToken) == "" {
		panic("accountautomation: service and admin token are required")
	}
	server := &server{service: service, adminToken: adminToken, logger: logger}
	return http.HandlerFunc(server.serveHTTP)
}

type server struct {
	service    BatchService
	adminToken string
	logger     ServerLogger
}

func (s *server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/healthz":
		s.health(w, r)
	case r.URL.Path == "/":
		s.index(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/batches"):
		s.apiBatches(w, r)
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

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := webFiles.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *server) apiBatches(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.URL.Path == "/api/batches" {
		s.collection(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/batches/") && r.Method == http.MethodGet {
		id := strings.TrimPrefix(r.URL.Path, "/api/batches/")
		if id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}
		batch, found := s.service.GetBatch(id)
		if !found {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, batch)
		return
	}
	w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (s *server) collection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.ListBatches())
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
		var request CreateBatchRequest
		if err := common.Unmarshal(data, &request); err != nil {
			statusError(w, http.StatusBadRequest, "invalid request")
			return
		}
		batch, err := s.service.Submit(r.Context(), request)
		if err != nil {
			statusError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, batch)
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *server) authorized(r *http.Request) bool {
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
