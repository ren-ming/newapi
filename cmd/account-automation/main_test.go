package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigRequiresSecrets(t *testing.T) {
	values := map[string]string{
		"SMS688_ACCOUNT_API_KEY": "sms-key",
		"NEWAPI_BASE_URL":        "https://newapi.example",
		"NEWAPI_ACCESS_TOKEN":    "access-token",
		"NEWAPI_USER_ID":         "1",
		"AUTOMATION_ADMIN_TOKEN": "admin-token",
	}

	for _, key := range requiredEnvironmentVariables {
		t.Run(key, func(t *testing.T) {
			lookup := func(name string) (string, bool) {
				value, found := values[name]
				if name == key {
					return "", true
				}
				return value, found
			}
			_, err := loadConfig(lookup)
			require.Error(t, err)
			assert.Contains(t, err.Error(), key)
			assert.NotContains(t, err.Error(), values[key])
		})
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	values := map[string]string{
		"SMS688_ACCOUNT_API_KEY": "sms-key",
		"NEWAPI_BASE_URL":        "https://newapi.example/",
		"NEWAPI_ACCESS_TOKEN":    "access-token",
		"NEWAPI_USER_ID":         "1",
		"AUTOMATION_ADMIN_TOKEN": "admin-token",
	}
	config, err := loadConfig(func(name string) (string, bool) {
		value, found := values[name]
		return value, found
	})
	require.NoError(t, err)

	assert.Equal(t, "https://cdk.sms688.cc", config.SMS688BaseURL)
	assert.Equal(t, ":8080", config.ListenAddr)
	assert.Equal(t, 30*time.Second, config.HTTPTimeout)
	assert.Equal(t, 2*time.Second, config.PollInterval)
}

func testConfig(t *testing.T) config {
	t.Helper()
	values := map[string]string{
		"SMS688_ACCOUNT_API_KEY": "sms-key",
		"NEWAPI_BASE_URL":        "https://newapi.example",
		"NEWAPI_ACCESS_TOKEN":    "access-token",
		"NEWAPI_USER_ID":         "1",
		"AUTOMATION_ADMIN_TOKEN": "admin-token",
	}
	cfg, err := loadConfig(func(name string) (string, bool) {
		value, found := values[name]
		return value, found
	})
	require.NoError(t, err)
	return cfg
}

func TestWireServesHealthEndpoint(t *testing.T) {
	handler := wire(testConfig(t))
	require.NotNil(t, handler)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"status":"ok"`)
}

func TestServeAndWaitStopsOnShutdownSignal(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &http.Server{Handler: http.NotFoundHandler()}
	shutdown := make(chan struct{})

	done := make(chan error, 1)
	go func() { done <- serveAndWait(server, listener, shutdown) }()

	require.Eventually(t, func() bool {
		response, err := http.Get("http://" + listener.Addr().String())
		if err != nil {
			return false
		}
		_ = response.Body.Close()
		return response.StatusCode == http.StatusNotFound
	}, 2*time.Second, 5*time.Millisecond, "server did not start serving")

	close(shutdown)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("serveAndWait did not return after shutdown signal")
	}
}

func TestServeAndWaitReportsServerFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	// Closing the listener makes Serve fail inside serveAndWait.
	require.NoError(t, listener.Close())
	server := &http.Server{Handler: http.NotFoundHandler()}

	done := make(chan error, 1)
	go func() { done <- serveAndWait(server, listener, nil) }()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "serve account automation")
	case <-time.After(2 * time.Second):
		t.Fatal("serveAndWait did not report listener failure")
	}
}
