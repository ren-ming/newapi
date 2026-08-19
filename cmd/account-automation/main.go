package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/QuantumNous/new-api/internal/accountautomation"
)

const (
	defaultSMS688BaseURL = "https://cdk.sms688.cc"
	defaultListenAddr    = ":8080"
	defaultHTTPTimeout   = 30 * time.Second
	defaultPollInterval  = 2 * time.Second
	defaultMaxBatchSize  = 100
	defaultBatchDeadline = 45 * time.Minute
	shutdownTimeout      = 10 * time.Second
)

var requiredEnvironmentVariables = []string{
	"SMS688_ACCOUNT_API_KEY",
	"NEWAPI_BASE_URL",
	"NEWAPI_ACCESS_TOKEN",
	"NEWAPI_USER_ID",
	"AUTOMATION_ADMIN_TOKEN",
}

type config struct {
	SMS688BaseURL   string
	SMS688APIKey    string
	NewAPIBaseURL   string
	NewAPIAccessKey string
	NewAPIUserID    string
	AdminToken      string
	ListenAddr      string
	HTTPTimeout     time.Duration
	PollInterval    time.Duration
	MaxBatchSize    int
}

type lookupEnv func(string) (string, bool)

func main() {
	if err := run(); err != nil {
		log.Printf("account automation stopped: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig(os.LookupEnv)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           wire(cfg),
		ReadHeaderTimeout: cfg.HTTPTimeout,
		ReadTimeout:       cfg.HTTPTimeout,
		WriteTimeout:      cfg.HTTPTimeout,
		IdleTimeout:       2 * cfg.HTTPTimeout,
	}

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen account automation: %w", err)
	}
	return serveAndWait(server, listener, signalContext.Done())
}

// serveAndWait runs the HTTP server until shutdownSignal closes or the server
// itself fails, then drains in-flight requests within shutdownTimeout.
func serveAndWait(server *http.Server, listener net.Listener, shutdownSignal <-chan struct{}) error {
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("account automation listening on %s", listener.Addr())
		serverErrors <- server.Serve(listener)
	}()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve account automation: %w", err)
	case <-shutdownSignal:
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shutdown account automation: %w", err)
	}
	return nil
}

func wire(cfg config) http.Handler {
	httpClient := &http.Client{Timeout: cfg.HTTPTimeout}
	store := accountautomation.NewMemoryStore()
	sms688 := accountautomation.NewSMS688Client(cfg.SMS688BaseURL, cfg.SMS688APIKey, httpClient)
	newAPI := accountautomation.NewNewAPIClient(cfg.NewAPIBaseURL, cfg.NewAPIAccessKey, cfg.NewAPIUserID, httpClient)
	logger := structuredLogger{}
	orchestrator := accountautomation.NewOrchestrator(store, sms688, newAPI, logger, accountautomation.OrchestratorConfig{
		PollInterval:  cfg.PollInterval,
		BatchDeadline: defaultBatchDeadline,
		MaxBatchSize:  cfg.MaxBatchSize,
	})
	return accountautomation.NewServer(orchestrator, cfg.AdminToken, nil)
}

func loadConfig(lookup lookupEnv) (config, error) {
	values := make(map[string]string, len(requiredEnvironmentVariables))
	for _, name := range requiredEnvironmentVariables {
		value, _ := lookup(name)
		value = strings.TrimSpace(value)
		if value == "" {
			return config{}, fmt.Errorf("required environment variable %s is empty", name)
		}
		values[name] = value
	}
	return config{
		SMS688BaseURL:   optionalEnvironment(lookup, "SMS688_BASE_URL", defaultSMS688BaseURL),
		SMS688APIKey:    values["SMS688_ACCOUNT_API_KEY"],
		NewAPIBaseURL:   values["NEWAPI_BASE_URL"],
		NewAPIAccessKey: values["NEWAPI_ACCESS_TOKEN"],
		NewAPIUserID:    values["NEWAPI_USER_ID"],
		AdminToken:      values["AUTOMATION_ADMIN_TOKEN"],
		ListenAddr:      optionalEnvironment(lookup, "AUTOMATION_LISTEN_ADDR", defaultListenAddr),
		HTTPTimeout:     defaultHTTPTimeout,
		PollInterval:    defaultPollInterval,
		MaxBatchSize:    defaultMaxBatchSize,
	}, nil
}

func optionalEnvironment(lookup lookupEnv, name string, fallback string) string {
	value, _ := lookup(name)
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

type structuredLogger struct{}

func (structuredLogger) Info(event string, fields map[string]any) {
	log.Printf("level=info event=%s fields=%v", event, fields)
}

func (structuredLogger) Error(event string, fields map[string]any) {
	log.Printf("level=error event=%s fields=%v", event, fields)
}
