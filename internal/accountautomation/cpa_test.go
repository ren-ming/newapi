package accountautomation

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestParseCPAJSON(t *testing.T) {
	want := Credential{AccessToken: "secret-token", AccountID: "account-1", Email: "one@example.com"}
	data, err := common.Marshal(want)
	if err != nil {
		t.Fatalf("marshal credential: %v", err)
	}

	got, err := ParseCPA(DownloadedCPA{ContentType: "application/json", Data: data})
	if err != nil {
		t.Fatalf("ParseCPA() error = %v", err)
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ParseCPA() = %#v, want %#v", got, []Credential{want})
	}
}

func TestParseCPAZIPUsesCredentialIdentityNotArchivePosition(t *testing.T) {
	second := Credential{AccessToken: "token-2", AccountID: "account-2", Email: "two@example.com"}
	first := Credential{AccessToken: "token-1", AccountID: "account-1", Email: "one@example.com"}
	data := makeCPAZIP(t, []zipTestEntry{
		{name: "unrelated-name-z.json", credential: second},
		{name: "unrelated-name-a.json", credential: first},
	})

	got, err := ParseCPA(DownloadedCPA{ContentType: "application/zip", Data: data})
	if err != nil {
		t.Fatalf("ParseCPA() error = %v", err)
	}
	byAccountID := make(map[string]Credential, len(got))
	for _, credential := range got {
		byAccountID[credential.AccountID] = credential
	}
	if byAccountID[first.AccountID] != first || byAccountID[second.AccountID] != second {
		t.Fatalf("ParseCPA() identities = %#v", byAccountID)
	}
}

func TestParseCPARejectsInvalidCredentialsWithoutLeakingBody(t *testing.T) {
	secret := "must-not-appear-in-errors"
	tests := []struct {
		name string
		body any
	}{
		{name: "missing access token", body: map[string]any{"account_id": "account-1", "refresh_token": secret}},
		{name: "empty access token", body: map[string]any{"access_token": "", "account_id": "account-1", "refresh_token": secret}},
		{name: "non-string access token", body: map[string]any{"access_token": 7, "account_id": "account-1", "refresh_token": secret}},
		{name: "missing account id", body: map[string]any{"access_token": secret}},
		{name: "empty account id", body: map[string]any{"access_token": secret, "account_id": ""}},
		{name: "non-string account id", body: map[string]any{"access_token": secret, "account_id": false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := common.Marshal(tt.body)
			if err != nil {
				t.Fatalf("marshal input: %v", err)
			}
			_, err = ParseCPA(DownloadedCPA{ContentType: "application/json", Data: data})
			if err == nil {
				t.Fatal("ParseCPA() error = nil")
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), string(data)) {
				t.Fatalf("ParseCPA() leaked credential content: %q", err)
			}
		})
	}
}

func TestParseCPARejectsOversizedResponse(t *testing.T) {
	data := bytes.Repeat([]byte("x"), maxCPAResponseSize+1)
	_, err := ParseCPA(DownloadedCPA{ContentType: "application/json", Data: data})
	if err == nil {
		t.Fatal("ParseCPA() error = nil")
	}
}

func TestParseCPAZIPRejectsUnsafeEntries(t *testing.T) {
	valid := Credential{AccessToken: "token", AccountID: "account"}
	tests := []struct {
		name  string
		entry zipTestEntry
	}{
		{name: "parent traversal", entry: zipTestEntry{name: "../credential.json", credential: valid}},
		{name: "embedded parent traversal", entry: zipTestEntry{name: "safe/../credential.json", credential: valid}},
		{name: "absolute path", entry: zipTestEntry{name: "/credential.json", credential: valid}},
		{name: "windows absolute path", entry: zipTestEntry{name: `C:\credential.json`, credential: valid}},
		{name: "backslash traversal", entry: zipTestEntry{name: `..\credential.json`, credential: valid}},
		{name: "symbolic link", entry: zipTestEntry{name: "credential.json", credential: valid, symlink: true}},
		{name: "nested archive extension", entry: zipTestEntry{name: "nested.zip", raw: []byte("PK\x03\x04")}},
		{name: "nested archive signature", entry: zipTestEntry{name: "credential.json", raw: []byte("PK\x03\x04nested")}},
		{name: "non json", entry: zipTestEntry{name: "credential.txt", credential: valid}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := makeCPAZIP(t, []zipTestEntry{tt.entry})
			_, err := ParseCPA(DownloadedCPA{ContentType: "application/zip", Data: data})
			if err == nil {
				t.Fatal("ParseCPA() error = nil")
			}
		})
	}
}

func TestParseCPAZIPEnforcesResourceLimits(t *testing.T) {
	valid := Credential{AccessToken: "token", AccountID: "account"}
	t.Run("file count", func(t *testing.T) {
		entries := make([]zipTestEntry, maxCPAZIPFiles+1)
		for i := range entries {
			entries[i] = zipTestEntry{name: fmt.Sprintf("%03d.json", i), credential: valid}
		}
		_, err := ParseCPA(DownloadedCPA{ContentType: "application/zip", Data: makeCPAZIP(t, entries)})
		if err == nil {
			t.Fatal("ParseCPA() error = nil")
		}
	})
	t.Run("single file expanded size", func(t *testing.T) {
		data := makeCPAZIP(t, []zipTestEntry{{name: "large.json", raw: bytes.Repeat([]byte(" "), maxCPAZIPFileSize+1)}})
		_, err := ParseCPA(DownloadedCPA{ContentType: "application/zip", Data: data})
		if err == nil {
			t.Fatal("ParseCPA() error = nil")
		}
	})
	t.Run("total expanded size", func(t *testing.T) {
		chunk := bytes.Repeat([]byte(" "), maxCPAZIPTotalSize/2+1)
		data := makeCPAZIP(t, []zipTestEntry{{name: "one.json", raw: chunk}, {name: "two.json", raw: chunk}})
		_, err := ParseCPA(DownloadedCPA{ContentType: "application/zip", Data: data})
		if err == nil {
			t.Fatal("ParseCPA() error = nil")
		}
	})
}

func TestParseCPARejectsUnsupportedAndEmptyDocuments(t *testing.T) {
	tests := []DownloadedCPA{
		{},
		{ContentType: "text/plain", Data: []byte("not-json")},
		{ContentType: "application/json", Data: []byte("[]")},
		{ContentType: "application/json", Data: []byte("{}")},
		{ContentType: "application/zip", Data: makeCPAZIP(t, nil)},
	}
	for i, download := range tests {
		if _, err := ParseCPA(download); err == nil {
			t.Fatalf("case %d: ParseCPA() error = nil", i)
		}
	}
}

type zipTestEntry struct {
	name       string
	credential Credential
	raw        []byte
	symlink    bool
}

func makeCPAZIP(t *testing.T, entries []zipTestEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		if entry.symlink {
			header.SetMode(0o777 | 0o120000)
			header.CreatorVersion = 3 << 8
			header.ExternalAttrs = uint32(0o120777) << 16
		}
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("create ZIP entry: %v", err)
		}
		data := entry.raw
		if data == nil {
			data, err = common.Marshal(entry.credential)
			if err != nil {
				t.Fatalf("marshal ZIP credential: %v", err)
			}
		}
		if _, err := file.Write(data); err != nil {
			t.Fatalf("write ZIP entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close ZIP: %v", err)
	}
	return buffer.Bytes()
}
