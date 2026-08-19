package accountautomation

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestMemoryJobStoreReturnsImmutableSnapshots(t *testing.T) {
	t.Parallel()
	store := NewMemoryJobStore()
	job := Job{ID: "job-1", MaskedEmail: "u***r@example.com", ChannelID: 7, Status: JobStatusSMS688Running}
	if err := store.CreateJob(job); err != nil {
		t.Fatalf("CreateJob() error = %v", err)
	}
	job.Status = JobStatusSucceeded
	first, err := store.GetJob("job-1")
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	first.Status = JobStatusSucceeded
	got, err := store.GetJob("job-1")
	if err != nil {
		t.Fatalf("GetJob() second error = %v", err)
	}
	if got.Status != JobStatusSMS688Running {
		t.Fatalf("stored status = %q, want sms688_running", got.Status)
	}
}

func TestMemoryJobStoreConcurrentUpdates(t *testing.T) {
	t.Parallel()
	store := NewMemoryJobStore()
	if err := store.CreateJob(Job{ID: "job-1", Status: JobStatusSubmitting}); err != nil {
		t.Fatalf("CreateJob() error = %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		stage := fmt.Sprintf("stage-%d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.UpdateJob("job-1", func(job *Job) { job.Stage = stage }); err != nil {
				t.Errorf("UpdateJob() error = %v", err)
			}
		}()
	}
	wg.Wait()
	if _, err := store.GetJob("job-1"); err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
}

func TestRemoteJobStatusMappings(t *testing.T) {
	t.Parallel()
	cases := map[string]JobStatus{
		"pending": JobStatusSMS688Queued, "processing": JobStatusSMS688Running,
		"waiting": JobStatusSMS688Waiting, "canceled": JobStatusSMS688Cancelled,
		"success": JobStatusCredentialReady, "unknown": JobStatusSMS688Failed,
	}
	for status, want := range cases {
		if got := mapRemoteJobStatus(status); got != want {
			t.Errorf("mapRemoteJobStatus(%q) = %q, want %q", status, got, want)
		}
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
	updateErr   error
	updates     []int
	testResults map[int]ChannelTestResult
}

func (f *fakeNewAPI) UpdateChannel(_ context.Context, channelID int, _ Credential) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, channelID)
	return f.updateErr
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
