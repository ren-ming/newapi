package accountautomation

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
)

func TestMemoryStoreReturnsImmutableSnapshots(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	batch := Batch{ID: "batch-1", Status: BatchStatusCreated, Accounts: []BatchAccount{{ID: "account-1", ChannelID: 7, Status: AccountStatusPending}}}
	submissions := []AccountSubmission{{ChannelID: 7, AccountLine: "user@example.com----secret", Email: "user@example.com", MaskedEmail: "u***r@example.com"}}
	if err := store.Create(batch, submissions); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	batch.Accounts[0].Status = AccountStatusSucceeded
	submissions[0].AccountLine = "changed"
	first, err := store.Get("batch-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	first.Accounts[0].Status = AccountStatusSucceeded
	got, err := store.Get("batch-1")
	if err != nil {
		t.Fatalf("Get() second error = %v", err)
	}
	gotSubmissions, err := store.Submissions("batch-1")
	if err != nil {
		t.Fatalf("Submissions() error = %v", err)
	}
	if got.Accounts[0].Status != AccountStatusPending {
		t.Fatalf("stored account status = %q, want %q", got.Accounts[0].Status, AccountStatusPending)
	}
	if gotSubmissions[0].AccountLine != "user@example.com----secret" {
		t.Fatalf("stored account line was mutated: %q", gotSubmissions[0].AccountLine)
	}
}

func TestMemoryStoreConcurrentUpdates(t *testing.T) {
	store := NewMemoryStore()
	batch := Batch{ID: "batch-1", Status: BatchStatusCreated, Accounts: make([]BatchAccount, 32)}
	for i := range batch.Accounts {
		batch.Accounts[i] = BatchAccount{ID: fmt.Sprintf("account-%d", i), Status: AccountStatusPending}
	}
	if err := store.Create(batch, nil); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	var wg sync.WaitGroup
	for i := range batch.Accounts {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Update("batch-1", func(current Batch) (Batch, error) {
				current.Accounts[i].Status = AccountStatusSucceeded
				return current, nil
			})
			if err != nil {
				t.Errorf("Update(%d) error = %v", i, err)
			}
		}()
	}
	wg.Wait()
	got, err := store.Get("batch-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	for i, account := range got.Accounts {
		if account.Status != AccountStatusSucceeded {
			t.Errorf("account %d status = %q", i, account.Status)
		}
	}
}

func TestOrchestratorCompletesBatchAndUsesExplicitChannelIDs(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	sms := &fakeSMS688{
		createResult: RemoteBatch{BatchID: "remote-1"},
		pollResults: []RemoteBatch{
			{BatchID: "remote-1", Jobs: []RemoteJob{{ID: "job-1", Email: "first@example.com", Status: "running"}}},
			{BatchID: "remote-1", AllFinished: true, Complete: 2, Jobs: []RemoteJob{
				{ID: "job-1", Email: "first@example.com", Status: "completed"},
				{ID: "job-2", Email: "second@example.com", Status: "completed"},
			}},
		},
		download: zipCPA(t,
			Credential{AccessToken: "access-1", AccountID: "account-1", Email: "first@example.com"},
			Credential{AccessToken: "access-2", AccountID: "account-2", Email: "second@example.com"},
		),
	}
	newAPI := &fakeNewAPI{testResults: map[int]ChannelTestResult{17: {Success: true}, 29: {Success: true}}}
	logger := &recordingLogger{}
	orchestrator := NewOrchestrator(store, sms, newAPI, logger, OrchestratorConfig{PollInterval: time.Millisecond, BatchDeadline: time.Second})

	created, err := orchestrator.Submit(context.Background(), CreateBatchRequest{
		AccountText: "17|first@example.com----secret-one\n29|second@example.com----secret-two",
		BindFree:    true,
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	completed := waitForTerminalBatch(t, store, created.ID)
	if completed.Status != BatchStatusCompleted {
		t.Fatalf("batch status = %q, want %q (error=%q)", completed.Status, BatchStatusCompleted, completed.ErrorClass)
	}
	if got := newAPI.updatedChannels(); !equalInts(got, []int{17, 29}) {
		t.Fatalf("updated channels = %v, want [17 29]", got)
	}
	if sms.createdRequest.AccountText != "first@example.com----secret-one\nsecond@example.com----secret-two" {
		t.Fatalf("SMS688 account text = %q", sms.createdRequest.AccountText)
	}
	if sms.idempotencyKey != created.ID {
		t.Fatalf("idempotency key = %q, want local batch ID %q", sms.idempotencyKey, created.ID)
	}
	if sms.downloadCalls != 1 {
		t.Fatalf("batch-level downloads = %d, want 1", sms.downloadCalls)
	}
	assertSafeLogs(t, logger.events(), []string{"secret-one", "secret-two", "first@example.com", "second@example.com", "access-1", "access-2"})
}

func TestOrchestratorMarksChannelTestFailureWithoutRollback(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	sms := &fakeSMS688{
		createResult: RemoteBatch{BatchID: "remote-1"},
		pollResults:  []RemoteBatch{{BatchID: "remote-1", AllFinished: true, Complete: 1, Jobs: []RemoteJob{{ID: "job-1", Email: "user@example.com", Status: "completed"}}}},
		download:     jsonCPA(t, Credential{AccessToken: "new-token", AccountID: "account-1", Email: "user@example.com"}),
	}
	newAPI := &fakeNewAPI{testResults: map[int]ChannelTestResult{42: {Success: false, ErrorCode: "upstream_rejected"}}}
	orchestrator := NewOrchestrator(store, sms, newAPI, discardLogger{}, OrchestratorConfig{PollInterval: time.Millisecond, BatchDeadline: time.Second})

	created, err := orchestrator.Submit(context.Background(), CreateBatchRequest{AccountText: "42|user@example.com----secret"})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	completed := waitForTerminalBatch(t, store, created.ID)
	if completed.Status != BatchStatusFailed {
		t.Fatalf("batch status = %q, want %q", completed.Status, BatchStatusFailed)
	}
	if got := completed.Accounts[0]; got.Status != AccountStatusChannelTestFailed || got.ErrorClass != "channel_test_failed" {
		t.Fatalf("account = %#v, want channel_test_failed", got)
	}
	if newAPI.updateCount(42) != 1 {
		t.Fatalf("channel update count = %d, want 1", newAPI.updateCount(42))
	}
}

func TestOrchestratorReportsPartialCompletion(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	sms := &fakeSMS688{
		createResult: RemoteBatch{BatchID: "remote-1"},
		pollResults: []RemoteBatch{{BatchID: "remote-1", AllFinished: true, Complete: 1, Error: 1, Jobs: []RemoteJob{
			{ID: "job-1", Email: "good@example.com", Status: "completed"},
			{ID: "job-2", Email: "bad@example.com", Status: "error"},
		}}},
		download: zipCPA(t, Credential{AccessToken: "access-1", AccountID: "account-1", Email: "good@example.com"}),
	}
	newAPI := &fakeNewAPI{testResults: map[int]ChannelTestResult{17: {Success: true}}}
	orchestrator := NewOrchestrator(store, sms, newAPI, discardLogger{}, OrchestratorConfig{PollInterval: time.Millisecond, BatchDeadline: time.Second})

	created, err := orchestrator.Submit(context.Background(), CreateBatchRequest{
		AccountText: "17|good@example.com----secret-one\n29|bad@example.com----secret-two",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	completed := waitForTerminalBatch(t, store, created.ID)
	if completed.Status != BatchStatusPartialCompleted {
		t.Fatalf("batch status = %q, want %q", completed.Status, BatchStatusPartialCompleted)
	}
	if got := newAPI.updatedChannels(); !equalInts(got, []int{17}) {
		t.Fatalf("updated channels = %v, want [17]", got)
	}
}

type fakeSMS688 struct {
	mu             sync.Mutex
	createResult   RemoteBatch
	createErr      error
	pollResults    []RemoteBatch
	pollIndex      int
	download       DownloadedCPA
	downloadErr    error
	downloadCalls  int
	createdRequest SMS688CreateRequest
	idempotencyKey string
}

func (f *fakeSMS688) CreateTask(_ context.Context, request SMS688CreateRequest, idempotencyKey string) (RemoteBatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createdRequest = request
	f.idempotencyKey = idempotencyKey
	return f.createResult, f.createErr
}

func (f *fakeSMS688) GetTask(_ context.Context, _ string) (RemoteBatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pollResults) == 0 {
		return RemoteBatch{}, fmt.Errorf("no_poll_result")
	}
	index := f.pollIndex
	if index >= len(f.pollResults) {
		index = len(f.pollResults) - 1
	} else {
		f.pollIndex++
	}
	return f.pollResults[index], nil
}

func (f *fakeSMS688) DownloadCPA(_ context.Context, _ string) (DownloadedCPA, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloadCalls++
	if f.downloadErr != nil {
		return DownloadedCPA{}, f.downloadErr
	}
	return f.download, nil
}

type fakeNewAPI struct {
	mu          sync.Mutex
	updates     []int
	testResults map[int]ChannelTestResult
}

func (f *fakeNewAPI) UpdateChannel(_ context.Context, channelID int, _ Credential) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, channelID)
	return nil
}

func (f *fakeNewAPI) TestChannel(_ context.Context, channelID int) (ChannelTestResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.testResults[channelID], nil
}

func (f *fakeNewAPI) updatedChannels() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.updates...)
}

func (f *fakeNewAPI) updateCount(channelID int) int {
	count := 0
	for _, id := range f.updatedChannels() {
		if id == channelID {
			count++
		}
	}
	return count
}

type recordingLogger struct {
	mu     sync.Mutex
	logged []loggedEvent
}

type loggedEvent struct {
	event  string
	fields map[string]any
}

func (l *recordingLogger) Info(event string, fields map[string]any) {
	l.record(event, fields)
}

func (l *recordingLogger) Error(event string, fields map[string]any) {
	l.record(event, fields)
}

func (l *recordingLogger) record(event string, fields map[string]any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logged = append(l.logged, loggedEvent{event: event, fields: fields})
}

func (l *recordingLogger) events() []loggedEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]loggedEvent(nil), l.logged...)
}

type discardLogger struct{}

func (discardLogger) Info(string, map[string]any)  {}
func (discardLogger) Error(string, map[string]any) {}

func waitForTerminalBatch(t *testing.T, store Store, id string) Batch {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		batch, err := store.Get(id)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if batch.Status == BatchStatusCompleted || batch.Status == BatchStatusPartialCompleted || batch.Status == BatchStatusFailed {
			return batch
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("batch did not reach terminal status")
	return Batch{}
}

func jsonCPA(t *testing.T, credential Credential) DownloadedCPA {
	t.Helper()
	data, err := common.Marshal(credential)
	if err != nil {
		t.Fatalf("marshal credential: %v", err)
	}
	return DownloadedCPA{ContentType: "application/json", Data: data}
}

func zipCPA(t *testing.T, credentials ...Credential) DownloadedCPA {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for index, credential := range credentials {
		data, err := common.Marshal(credential)
		if err != nil {
			t.Fatalf("marshal credential: %v", err)
		}
		file, err := writer.Create(fmt.Sprintf("credential-%d.json", index+1))
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err = file.Write(data); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return DownloadedCPA{ContentType: "application/zip", Data: buffer.Bytes()}
}

func TestOrchestratorAccessorsAndValidation(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	batch := Batch{ID: "batch-1", Status: BatchStatusCreated, Accounts: []BatchAccount{{ID: "account-1"}}}
	if err := store.Create(batch, nil); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	orchestrator := NewOrchestrator(store, &fakeSMS688{}, &fakeNewAPI{}, nil, OrchestratorConfig{MaxBatchSize: 1})
	got, found := orchestrator.GetBatch(batch.ID)
	if !found || got.ID != batch.ID {
		t.Fatalf("GetBatch() = %#v, %v", got, found)
	}
	if _, found := orchestrator.GetBatch("missing"); found {
		t.Fatal("GetBatch(missing) reported found")
	}
	listed := orchestrator.ListBatches()
	if len(listed) != 1 || listed[0].ID != batch.ID {
		t.Fatalf("ListBatches() = %#v", listed)
	}
	listed[0].Accounts[0].Status = AccountStatusSucceeded
	stored, _ := store.Get(batch.ID)
	if stored.Accounts[0].Status == AccountStatusSucceeded {
		t.Fatal("ListBatches() returned a mutable snapshot")
	}
	_, err := orchestrator.Submit(context.Background(), CreateBatchRequest{AccountText: "1|one@example.com----a\n2|two@example.com----b"})
	if err == nil || err.Error() != "batch_too_large" {
		t.Fatalf("Submit() error = %v, want batch_too_large", err)
	}
}

func TestOrchestratorClassifiesRemoteAndCredentialFailures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		status        string
		complete      int
		downloadFails bool
		download      DownloadedCPA
		want          AccountStatus
	}{
		{name: "remote expired", status: "expired", want: AccountStatusSMS688Expired},
		{name: "download failure", status: "completed", complete: 1, downloadFails: true, want: AccountStatusDownloadFailed},
		{name: "invalid credential", status: "completed", complete: 1, download: DownloadedCPA{ContentType: "application/json", Data: []byte(`{}`)}, want: AccountStatusCredentialInvalid},
		{name: "invalid zip", status: "completed", complete: 1, download: DownloadedCPA{ContentType: "application/zip", Data: []byte("bad")}, want: AccountStatusCredentialInvalid},
		{name: "credential email mismatch", status: "completed", complete: 1, download: jsonCPA(t, Credential{AccessToken: "access", AccountID: "account", Email: "other@example.com"}), want: AccountStatusCredentialInvalid},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			sms := &fakeSMS688{
				createResult: RemoteBatch{BatchID: "remote-1"},
				pollResults:  []RemoteBatch{{BatchID: "remote-1", AllFinished: true, Complete: tt.complete, Jobs: []RemoteJob{{ID: "job-1", Email: "user@example.com", Status: tt.status}}}},
			}
			if tt.downloadFails {
				sms.downloadErr = fmt.Errorf("download_failed")
			} else {
				sms.download = tt.download
			}
			o := NewOrchestrator(store, sms, &fakeNewAPI{}, discardLogger{}, OrchestratorConfig{PollInterval: time.Millisecond, BatchDeadline: time.Second})
			created, err := o.Submit(context.Background(), CreateBatchRequest{AccountText: "42|user@example.com----secret"})
			if err != nil {
				t.Fatalf("Submit() error = %v", err)
			}
			completed := waitForTerminalBatch(t, store, created.ID)
			if completed.Accounts[0].Status != tt.want {
				t.Fatalf("status = %q, want %q", completed.Accounts[0].Status, tt.want)
			}
		})
	}
}

func TestRemoteStatusMappings(t *testing.T) {
	t.Parallel()
	cases := map[string]AccountStatus{
		"pending": AccountStatusSMS688Queued, "processing": AccountStatusSMS688Running,
		"waiting": AccountStatusSMS688Waiting, "canceled": AccountStatusSMS688Cancelled,
		"success": AccountStatusCredentialReady, "unknown": AccountStatusSMS688Failed,
	}
	for status, want := range cases {
		if got := mapRemoteStatus(status); got != want {
			t.Errorf("mapRemoteStatus(%q) = %q, want %q", status, got, want)
		}
	}
}

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func assertSafeLogs(t *testing.T, events []loggedEvent, forbidden []string) {
	t.Helper()
	for _, event := range events {
		data, err := common.Marshal(event)
		if err != nil {
			t.Fatalf("marshal log event: %v", err)
		}
		for _, secret := range forbidden {
			if bytes.Contains(data, []byte(secret)) {
				t.Fatalf("log leaked %q: %s", secret, data)
			}
		}
	}
}
