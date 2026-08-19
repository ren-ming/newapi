package accountautomation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestNewAPIClientUpdateChannel(t *testing.T) {
	t.Parallel()

	credential := Credential{AccessToken: "access-secret", AccountID: "account-secret", RefreshToken: "refresh-secret"}
	var gotMethod, gotPath, gotAuthorization, gotUser string
	var gotBody struct {
		ID   int    `json:"id"`
		Type int    `json:"type"`
		Key  string `json:"key"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":17,"type":57}}`))
			return
		}
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")
		gotUser = r.Header.Get("New-Api-User")
		if err := common.DecodeJson(r.Body, &gotBody); err != nil {
			t.Errorf("decode update body: %v", err)
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	client := NewNewAPIClient(server.URL, "admin-token", "42", server.Client())
	if err := client.UpdateChannel(context.Background(), 17, credential); err != nil {
		t.Fatalf("UpdateChannel() error = %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/channel/" {
		t.Errorf("request = %s %s, want PUT /api/channel/", gotMethod, gotPath)
	}
	if gotAuthorization != "admin-token" {
		t.Errorf("Authorization = %q, want raw token", gotAuthorization)
	}
	if gotUser != "42" {
		t.Errorf("New-Api-User = %q, want 42", gotUser)
	}
	if gotBody.ID != 17 || gotBody.Type != 57 {
		t.Errorf("body identity = {%d %d}, want {17 57}", gotBody.ID, gotBody.Type)
	}
	var gotCredential Credential
	if err := common.Unmarshal([]byte(gotBody.Key), &gotCredential); err != nil {
		t.Fatalf("decode key: %v", err)
	}
	if gotCredential != credential {
		t.Errorf("credential = %#v, want %#v", gotCredential, credential)
	}
}

func TestNewAPIClientUpdateChannelPreflightRejectsUnsafeChannelWithoutPut(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "non Codex", body: `{"success":true,"data":{"id":17,"type":1}}`},
		{name: "id mismatch", body: `{"success":true,"data":{"id":18,"type":57}}`},
		{name: "business failure", body: `{"success":false,"data":{"id":17,"type":57}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var puts int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "admin-token" || r.Header.Get("New-Api-User") != "42" {
					t.Errorf("authentication headers = %q, %q", r.Header.Get("Authorization"), r.Header.Get("New-Api-User"))
				}
				if r.Method == http.MethodPut {
					puts++
				}
				if r.Method != http.MethodGet || r.URL.Path != "/api/channel/17" {
					t.Errorf("preflight request = %s %s, want GET /api/channel/17", r.Method, r.URL.Path)
				}
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := NewNewAPIClient(server.URL, "admin-token", "42", server.Client())
			err := client.UpdateChannel(context.Background(), 17, Credential{AccessToken: "token", AccountID: "account"})
			if err == nil || err.Error() != "newapi_channel_precondition_failed" {
				t.Fatalf("UpdateChannel() error = %v, want newapi_channel_precondition_failed", err)
			}
			if puts != 0 {
				t.Fatalf("PUT requests = %d, want 0", puts)
			}
		})
	}
}

func TestNewAPIClientUpdateChannelRejectsInvalidInputWithoutRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		channelID  int
		credential Credential
		wantClass  string
	}{
		{name: "invalid channel", channelID: 0, credential: Credential{AccessToken: "token", AccountID: "account"}, wantClass: "newapi_invalid_channel"},
		{name: "missing access token", channelID: 1, credential: Credential{AccountID: "account"}, wantClass: "newapi_invalid_credential"},
		{name: "missing account id", channelID: 1, credential: Credential{AccessToken: "token"}, wantClass: "newapi_invalid_credential"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
			defer server.Close()

			client := NewNewAPIClient(server.URL, "admin-token", "42", server.Client())
			err := client.UpdateChannel(context.Background(), tt.channelID, tt.credential)
			if err == nil || err.Error() != tt.wantClass {
				t.Fatalf("UpdateChannel() error = %v, want %q", err, tt.wantClass)
			}
			if requests != 0 {
				t.Fatalf("requests = %d, want 0", requests)
			}
		})
	}
}

func TestNewAPIClientTestChannel(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/channel/test/17" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "admin-token" || r.Header.Get("New-Api-User") != "42" {
			t.Errorf("authentication headers = %q, %q", r.Header.Get("Authorization"), r.Header.Get("New-Api-User"))
		}
		_, _ = w.Write([]byte(`{"success":true,"message":"","time":1.25}`))
	}))
	defer server.Close()

	client := NewNewAPIClient(server.URL, "admin-token", "42", server.Client())
	got, err := client.TestChannel(context.Background(), 17)
	if err != nil {
		t.Fatalf("TestChannel() error = %v", err)
	}
	if !got.Success || got.Time != 1.25 {
		t.Errorf("TestChannel() = %#v", got)
	}
}

func TestNewAPIClientClassifiesFailuresWithoutLeakingSecrets(t *testing.T) {
	t.Parallel()

	secrets := []string{"admin-secret", "access-secret", "account-secret", "refresh-secret"}
	tests := []struct {
		name      string
		status    int
		body      string
		operation string
		wantClass string
	}{
		{name: "update business failure", status: http.StatusOK, body: `{"success":false,"message":"access-secret rejected"}`, operation: "update", wantClass: "newapi_business_error"},
		{name: "test business failure", status: http.StatusOK, body: `{"success":false,"message":"account-secret rejected","error_code":"bad_key"}`, operation: "test", wantClass: "newapi_business_error"},
		{name: "http failure", status: http.StatusUnauthorized, body: `refresh-secret`, operation: "test", wantClass: "newapi_http_error"},
		{name: "invalid json", status: http.StatusOK, body: `access-secret`, operation: "test", wantClass: "newapi_invalid_response"},
		{name: "oversized response", status: http.StatusOK, body: strings.Repeat("x", maxNewAPIResponseBytes+1), operation: "test", wantClass: "newapi_response_too_large"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.operation == "update" && r.Method == http.MethodGet {
					_, _ = w.Write([]byte(`{"success":true,"data":{"id":17,"type":57}}`))
					return
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := NewNewAPIClient(server.URL, secrets[0], "42", server.Client())
			var err error
			if tt.operation == "update" {
				err = client.UpdateChannel(context.Background(), 17, Credential{AccessToken: secrets[1], AccountID: secrets[2], RefreshToken: secrets[3]})
			} else {
				_, err = client.TestChannel(context.Background(), 17)
			}
			if err == nil || err.Error() != tt.wantClass {
				t.Fatalf("error = %v, want %q", err, tt.wantClass)
			}
			for _, secret := range secrets {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("error leaked secret %q", secret)
				}
			}
		})
	}
}

func TestNewAPIClientTransportErrorIsClassified(t *testing.T) {
	t.Parallel()

	client := NewNewAPIClient("http://127.0.0.1:1", "admin-secret", "42", &http.Client{})
	_, err := client.TestChannel(context.Background(), 17)
	if err == nil || err.Error() != "newapi_transport_error" {
		t.Fatalf("TestChannel() error = %v, want newapi_transport_error", err)
	}
	if strings.Contains(err.Error(), "admin-secret") {
		t.Fatal("transport error leaked admin token")
	}
}
