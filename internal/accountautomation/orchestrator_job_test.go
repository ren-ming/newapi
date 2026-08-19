package accountautomation

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func waitForTerminalJob(t *testing.T, store JobStore, id string) Job {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := store.GetJob(id)
		if err != nil {
			t.Fatalf("GetJob() error = %v", err)
		}
		if IsTerminalJobStatus(job.Status) {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job did not reach terminal status")
	return Job{}
}

func TestOrchestratorSubmitJobSucceeds(t *testing.T) {
	t.Parallel()
	store := NewMemoryJobStore()
	sms := &fakeSMS688{
		createResult: RemoteBatch{BatchID: "remote-1"},
		pollResults: []RemoteBatch{
			{BatchID: "remote-1", Jobs: []RemoteJob{{ID: "job-1", Email: "user@example.com", Status: "running", Stage: "login"}}},
			{BatchID: "remote-1", AllFinished: true, Complete: 1, Jobs: []RemoteJob{{ID: "job-1", Email: "user@example.com", Status: "completed"}}},
		},
		download: jsonCPA(t, Credential{AccessToken: "access-1", AccountID: "account-1", Email: "user@example.com"}),
	}
	newAPI := &fakeNewAPI{testResults: map[int]ChannelTestResult{42: {Success: true}}}
	logger := &recordingLogger{}
	o := NewOrchestrator(store, sms, newAPI, logger, OrchestratorConfig{PollInterval: time.Millisecond, BatchDeadline: time.Second})

	job, err := o.SubmitJob(context.Background(), CreateJobRequest{
		AccountMode: AccountModeMicrosoft,
		AccountText: "user@example.com----secret",
		ChannelID:   42,
		BindFree:    true,
	})
	if err != nil {
		t.Fatalf("SubmitJob() error = %v", err)
	}
	if job.Status != JobStatusSubmitting || job.MaskedEmail != "u***r@example.com" {
		t.Fatalf("created job = %#v", job)
	}
	done := waitForTerminalJob(t, store, job.ID)
	if done.Status != JobStatusSucceeded || done.ErrorClass != "" {
		t.Fatalf("terminal job = %#v, want succeeded", done)
	}
	if done.SMS688BatchID != "remote-1" {
		t.Fatalf("sms688 batch id = %q, want remote-1", done.SMS688BatchID)
	}
	if sms.createdRequest.AccountMode != AccountModeMicrosoft || sms.createdRequest.AccountText != "user@example.com----secret" || !sms.createdRequest.BindFree {
		t.Fatalf("SMS688 request = %#v", sms.createdRequest)
	}
	if sms.idempotencyKey != job.ID {
		t.Fatalf("idempotency key = %q, want job ID %q", sms.idempotencyKey, job.ID)
	}
	if got := newAPI.updatedChannels(); !equalInts(got, []int{42}) {
		t.Fatalf("updated channels = %v, want [42]", got)
	}
	assertSafeLogs(t, logger.events(), []string{"secret", "user@example.com", "access-1"})
}

func TestOrchestratorSubmitJobPassesTotpMode(t *testing.T) {
	t.Parallel()
	store := NewMemoryJobStore()
	sms := &fakeSMS688{
		createResult: RemoteBatch{BatchID: "remote-1"},
		pollResults: []RemoteBatch{
			{BatchID: "remote-1", AllFinished: true, Complete: 1, Jobs: []RemoteJob{{ID: "job-1", Email: "user@example.com", Status: "completed"}}},
		},
		download: jsonCPA(t, Credential{AccessToken: "access-1", AccountID: "account-1", Email: "user@example.com"}),
	}
	newAPI := &fakeNewAPI{testResults: map[int]ChannelTestResult{7: {Success: true}}}
	o := NewOrchestrator(store, sms, newAPI, discardLogger{}, OrchestratorConfig{PollInterval: time.Millisecond, BatchDeadline: time.Second})

	job, err := o.SubmitJob(context.Background(), CreateJobRequest{
		AccountMode: AccountModeTotp,
		AccountText: "user@example.com----secret----JBSWY3DPEHPK3PXP",
		ChannelID:   7,
	})
	if err != nil {
		t.Fatalf("SubmitJob() error = %v", err)
	}
	done := waitForTerminalJob(t, store, job.ID)
	if done.Status != JobStatusSucceeded {
		t.Fatalf("terminal job = %#v, want succeeded", done)
	}
	if sms.createdRequest.AccountMode != AccountModeTotp || sms.createdRequest.AccountText != "user@example.com----secret----JBSWY3DPEHPK3PXP" {
		t.Fatalf("SMS688 request = %#v", sms.createdRequest)
	}
	assertSafeLogs(t, []loggedEvent{}, nil)
}

func TestOrchestratorSubmitJobValidation(t *testing.T) {
	t.Parallel()
	o := NewOrchestrator(NewMemoryJobStore(), &fakeSMS688{}, &fakeNewAPI{}, nil, OrchestratorConfig{})
	cases := []struct {
		name    string
		request CreateJobRequest
		wantErr string
	}{
		{name: "unknown mode", request: CreateJobRequest{AccountMode: "link", AccountText: "user@example.com----secret", ChannelID: 1}, wantErr: "account_mode_invalid"},
		{name: "multiline text", request: CreateJobRequest{AccountMode: AccountModeMicrosoft, AccountText: "user@example.com----secret\nother@example.com----pw", ChannelID: 1}, wantErr: "account_invalid"},
		{name: "missing channel", request: CreateJobRequest{AccountMode: AccountModeMicrosoft, AccountText: "user@example.com----secret"}, wantErr: "channel_id_invalid"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := o.SubmitJob(context.Background(), tt.request)
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("SubmitJob() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestOrchestratorSubmitJobMarksSubmitFailure(t *testing.T) {
	t.Parallel()
	store := NewMemoryJobStore()
	sms := &fakeSMS688{createErr: fmt.Errorf("boom")}
	o := NewOrchestrator(store, sms, &fakeNewAPI{}, discardLogger{}, OrchestratorConfig{PollInterval: time.Millisecond, BatchDeadline: time.Second})
	created, err := o.SubmitJob(context.Background(), CreateJobRequest{
		AccountMode: AccountModeMicrosoft,
		AccountText: "user@example.com----secret",
		ChannelID:   42,
	})
	if err != nil {
		t.Fatalf("SubmitJob() error = %v", err)
	}
	done := waitForTerminalJob(t, store, created.ID)
	if done.Status != JobStatusSubmitFailed || done.ErrorClass != "sms688_submit_failed" {
		t.Fatalf("job = %#v, want submit_failed/sms688_submit_failed", done)
	}
}

func TestOrchestratorJobFailureClassification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		status        string
		complete      int
		downloadFails bool
		download      DownloadedCPA
		want          JobStatus
	}{
		{name: "remote expired", status: "expired", want: JobStatusSMS688Expired},
		{name: "remote error", status: "error", want: JobStatusSMS688Failed},
		{name: "download failure", status: "completed", complete: 1, downloadFails: true, want: JobStatusDownloadFailed},
		{name: "invalid credential", status: "completed", complete: 1, download: DownloadedCPA{ContentType: "application/json", Data: []byte(`{}`)}, want: JobStatusCredentialInvalid},
		{name: "credential email mismatch", status: "completed", complete: 1, download: jsonCPA(t, Credential{AccessToken: "access", AccountID: "account", Email: "other@example.com"}), want: JobStatusCredentialInvalid},
		{name: "ambiguous credentials", status: "completed", complete: 1, download: zipCPA(t,
			Credential{AccessToken: "access-1", AccountID: "account-1", Email: "user@example.com"},
			Credential{AccessToken: "access-2", AccountID: "account-2", Email: "user@example.com"},
		), want: JobStatusCredentialInvalid},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := NewMemoryJobStore()
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
			created, err := o.SubmitJob(context.Background(), CreateJobRequest{AccountMode: AccountModeMicrosoft, AccountText: "user@example.com----secret", ChannelID: 42})
			if err != nil {
				t.Fatalf("SubmitJob() error = %v", err)
			}
			done := waitForTerminalJob(t, store, created.ID)
			if done.Status != tt.want {
				t.Fatalf("status = %q, want %q (error=%q)", done.Status, tt.want, done.ErrorClass)
			}
		})
	}
}

func TestOrchestratorJobChannelFailureClassification(t *testing.T) {
	t.Parallel()
	t.Run("channel update failure", func(t *testing.T) {
		t.Parallel()
		store := NewMemoryJobStore()
		sms := jobDoneFixtures(t)
		newAPI := &fakeNewAPI{updateErr: fmt.Errorf("boom"), testResults: map[int]ChannelTestResult{42: {Success: true}}}
		o := NewOrchestrator(store, sms, newAPI, discardLogger{}, OrchestratorConfig{PollInterval: time.Millisecond, BatchDeadline: time.Second})
		created, err := o.SubmitJob(context.Background(), CreateJobRequest{AccountMode: AccountModeMicrosoft, AccountText: "user@example.com----secret", ChannelID: 42})
		if err != nil {
			t.Fatalf("SubmitJob() error = %v", err)
		}
		done := waitForTerminalJob(t, store, created.ID)
		if done.Status != JobStatusChannelUpdateFailed || done.ErrorClass != "channel_update_failed" {
			t.Fatalf("job = %#v, want channel_update_failed", done)
		}
	})
	t.Run("channel test failure", func(t *testing.T) {
		t.Parallel()
		store := NewMemoryJobStore()
		newAPI := &fakeNewAPI{testResults: map[int]ChannelTestResult{42: {Success: false, ErrorCode: "upstream_rejected"}}}
		o := NewOrchestrator(store, jobDoneFixtures(t), newAPI, discardLogger{}, OrchestratorConfig{PollInterval: time.Millisecond, BatchDeadline: time.Second})
		created, err := o.SubmitJob(context.Background(), CreateJobRequest{AccountMode: AccountModeMicrosoft, AccountText: "user@example.com----secret", ChannelID: 42})
		if err != nil {
			t.Fatalf("SubmitJob() error = %v", err)
		}
		done := waitForTerminalJob(t, store, created.ID)
		if done.Status != JobStatusChannelTestFailed || done.ErrorClass != "channel_test_failed" {
			t.Fatalf("job = %#v, want channel_test_failed", done)
		}
		if newAPI.updateCount(42) != 1 {
			t.Fatalf("channel update count = %d, want 1", newAPI.updateCount(42))
		}
	})
}

func jobDoneFixtures(t *testing.T) *fakeSMS688 {
	t.Helper()
	return &fakeSMS688{
		createResult: RemoteBatch{BatchID: "remote-1"},
		pollResults:  []RemoteBatch{{BatchID: "remote-1", AllFinished: true, Complete: 1, Jobs: []RemoteJob{{ID: "job-1", Email: "user@example.com", Status: "completed"}}}},
		download:     jsonCPA(t, Credential{AccessToken: "access-1", AccountID: "account-1", Email: "user@example.com"}),
	}
}

func TestOrchestratorResumeContinuesPolling(t *testing.T) {
	t.Parallel()
	store := NewMemoryJobStore()
	sms := &fakeSMS688{
		pollResults: []RemoteBatch{{BatchID: "remote-1", AllFinished: true, Complete: 1, Jobs: []RemoteJob{{ID: "job-1", Email: "user@example.com", Status: "completed"}}}},
		download:    jsonCPA(t, Credential{AccessToken: "access-1", AccountID: "account-1", Email: "user@example.com"}),
	}
	newAPI := &fakeNewAPI{testResults: map[int]ChannelTestResult{42: {Success: true}}}
	o := NewOrchestrator(store, sms, newAPI, discardLogger{}, OrchestratorConfig{PollInterval: time.Millisecond, BatchDeadline: time.Minute})
	job := Job{ID: "job-resume", AccountMode: AccountModeMicrosoft, MaskedEmail: "u***r@example.com", ChannelID: 42, Status: JobStatusSMS688Running, SMS688BatchID: "remote-1"}
	if err := store.CreateJob(job); err != nil {
		t.Fatalf("CreateJob() error = %v", err)
	}

	o.Resume(context.Background(), job)
	done := waitForTerminalJob(t, store, job.ID)
	if done.Status != JobStatusSucceeded {
		t.Fatalf("status = %q, want succeeded (error=%q)", done.Status, done.ErrorClass)
	}
	if sms.idempotencyKey != "" {
		t.Fatalf("Resume re-submitted to SMS688 with key %q", sms.idempotencyKey)
	}
}

func TestOrchestratorResumeWithoutBatchIDFails(t *testing.T) {
	t.Parallel()
	store := NewMemoryJobStore()
	o := NewOrchestrator(store, &fakeSMS688{}, &fakeNewAPI{}, discardLogger{}, OrchestratorConfig{})
	job := Job{ID: "job-interrupted", AccountMode: AccountModeMicrosoft, MaskedEmail: "u***r@example.com", ChannelID: 42, Status: JobStatusSubmitting}
	if err := store.CreateJob(job); err != nil {
		t.Fatalf("CreateJob() error = %v", err)
	}
	o.Resume(context.Background(), job)
	done := waitForTerminalJob(t, store, job.ID)
	if done.Status != JobStatusSubmitFailed || done.ErrorClass != "interrupted" {
		t.Fatalf("job = %#v, want submit_failed/interrupted", done)
	}
}

func TestOrchestratorResumeExpiredJobFails(t *testing.T) {
	t.Parallel()
	store := NewMemoryJobStore()
	o := NewOrchestrator(store, &fakeSMS688{}, &fakeNewAPI{}, discardLogger{}, OrchestratorConfig{BatchDeadline: time.Minute})
	job := Job{
		ID: "job-stale", AccountMode: AccountModeMicrosoft, MaskedEmail: "u***r@example.com",
		ChannelID: 42, Status: JobStatusSMS688Running, SMS688BatchID: "remote-1",
		CreatedAt: time.Now().UTC().Add(-2 * time.Minute), UpdatedAt: time.Now().UTC().Add(-2 * time.Minute),
	}
	if err := store.CreateJob(job); err != nil {
		t.Fatalf("CreateJob() error = %v", err)
	}
	o.Resume(context.Background(), job)
	done := waitForTerminalJob(t, store, job.ID)
	if done.Status != JobStatusSMS688Failed || done.ErrorClass != "interrupted" {
		t.Fatalf("job = %#v, want sms688_failed/interrupted", done)
	}
}

func TestOrchestratorJobAccessors(t *testing.T) {
	t.Parallel()
	store := NewMemoryJobStore()
	job := newTestJob(0)
	job.ID = "job-1"
	if err := store.CreateJob(job); err != nil {
		t.Fatalf("CreateJob() error = %v", err)
	}
	o := NewOrchestrator(store, &fakeSMS688{}, &fakeNewAPI{}, nil, OrchestratorConfig{})
	got, found := o.GetJob(job.ID)
	if !found || got.ID != job.ID {
		t.Fatalf("GetJob() = %#v, %v", got, found)
	}
	if _, found = o.GetJob("missing"); found {
		t.Fatal("GetJob(missing) reported found")
	}
	jobs, total, err := o.ListJobs(0, 10)
	if err != nil || total != 1 || len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("ListJobs() = %#v, %d, %v", jobs, total, err)
	}
}
