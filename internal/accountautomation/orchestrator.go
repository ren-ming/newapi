package accountautomation

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

type SMS688Service interface {
	CreateTask(ctx context.Context, request SMS688CreateRequest, idempotencyKey string) (RemoteBatch, error)
	GetTask(ctx context.Context, batchID string) (RemoteBatch, error)
	DownloadCPA(ctx context.Context, batchID string) (DownloadedCPA, error)
}

type NewAPIService interface {
	UpdateChannel(ctx context.Context, channelID int, credential Credential) error
	TestChannel(ctx context.Context, channelID int) (ChannelTestResult, error)
}

type Logger interface {
	Info(event string, fields map[string]any)
	Error(event string, fields map[string]any)
}

type OrchestratorConfig struct {
	PollInterval  time.Duration
	BatchDeadline time.Duration
	MaxBatchSize  int
}

type Orchestrator struct {
	store  Store
	sms688 SMS688Service
	newAPI NewAPIService
	logger Logger
	config OrchestratorConfig
}

var batchSequence atomic.Uint64

func NewOrchestrator(store Store, sms688 SMS688Service, newAPI NewAPIService, logger Logger, config OrchestratorConfig) *Orchestrator {
	if logger == nil {
		logger = noopLogger{}
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 2 * time.Second
	}
	if config.BatchDeadline <= 0 {
		config.BatchDeadline = 45 * time.Minute
	}
	return &Orchestrator{store: store, sms688: sms688, newAPI: newAPI, logger: logger, config: config}
}

func (o *Orchestrator) Submit(ctx context.Context, request CreateBatchRequest) (Batch, error) {
	if o.store == nil || o.sms688 == nil || o.newAPI == nil {
		return Batch{}, fmt.Errorf("orchestrator_dependency_missing")
	}
	submissions, err := ParseAccountLines(request.AccountText)
	if err != nil {
		return Batch{}, err
	}
	if o.config.MaxBatchSize > 0 && len(submissions) > o.config.MaxBatchSize {
		return Batch{}, fmt.Errorf("batch_too_large")
	}
	batch := newBatch(submissions)
	if err = o.store.Create(batch, submissions); err != nil {
		return Batch{}, fmt.Errorf("create batch: %w", err)
	}
	o.log(ctx, batch.ID, "created", "", AccountSubmission{})
	go o.run(context.WithoutCancel(ctx), batch.ID, request.BindFree)
	return batch, nil
}

func (o *Orchestrator) GetBatch(id string) (Batch, bool) {
	if o.store == nil {
		return Batch{}, false
	}
	batch, err := o.store.Get(id)
	if err != nil {
		return Batch{}, false
	}
	return batch, true
}

func (o *Orchestrator) ListBatches() []Batch {
	if o.store == nil {
		return nil
	}
	return o.store.List()
}

func newBatch(submissions []AccountSubmission) Batch {
	now := time.Now().UTC()
	id := fmt.Sprintf("batch-%d-%d", now.UnixNano(), batchSequence.Add(1))
	accounts := make([]BatchAccount, len(submissions))
	for index, submission := range submissions {
		accounts[index] = BatchAccount{
			ID:          fmt.Sprintf("%s-account-%d", id, index+1),
			MaskedEmail: submission.MaskedEmail,
			ChannelID:   submission.ChannelID,
			Status:      AccountStatusPending,
			UpdatedAt:   now,
		}
	}
	return Batch{ID: id, Status: BatchStatusCreated, Accounts: accounts, CreatedAt: now, UpdatedAt: now}
}

func (o *Orchestrator) run(ctx context.Context, batchID string, bindFree bool) {
	submissions, err := o.store.Submissions(batchID)
	if err != nil {
		o.failBatch(ctx, batchID, "store_error")
		return
	}
	o.setBatchStatus(ctx, batchID, BatchStatusSubmitting, "")
	remote, err := o.sms688.CreateTask(ctx, SMS688CreateRequest{
		AccountMode: "microsoft",
		AccountText: submissionText(submissions),
		BindFree:    bindFree,
	}, batchID)
	if err != nil || remote.BatchID == "" {
		o.failBatch(ctx, batchID, errorClass(err, "sms688_submit_failed"))
		return
	}
	o.updateBatch(batchID, func(batch Batch) Batch {
		batch.RemoteBatchID = remote.BatchID
		batch.Status = BatchStatusSubmitted
		for index := range batch.Accounts {
			batch.Accounts[index].Status = AccountStatusSMS688Queued
			batch.Accounts[index].UpdatedAt = time.Now().UTC()
		}
		return batch
	})
	o.log(ctx, batchID, "submitted", "", AccountSubmission{})

	remote, err = o.poll(ctx, batchID, remote.BatchID)
	if err != nil {
		o.failBatch(ctx, batchID, errorClass(err, "sms688_poll_failed"))
		return
	}
	o.process(ctx, batchID, submissions, remote)
}

func (o *Orchestrator) poll(ctx context.Context, batchID, remoteBatchID string) (RemoteBatch, error) {
	pollCtx, cancel := context.WithTimeout(ctx, o.config.BatchDeadline)
	defer cancel()
	ticker := time.NewTicker(o.config.PollInterval)
	defer ticker.Stop()
	o.setBatchStatus(ctx, batchID, BatchStatusPolling, "")
	for {
		remote, err := o.sms688.GetTask(pollCtx, remoteBatchID)
		if err != nil {
			return RemoteBatch{}, fmt.Errorf("sms688_poll_failed: %w", err)
		}
		o.applyRemoteStatuses(batchID, remote)
		if remote.AllFinished {
			return remote, nil
		}
		select {
		case <-pollCtx.Done():
			return RemoteBatch{}, fmt.Errorf("sms688_poll_timeout: %w", pollCtx.Err())
		case <-ticker.C:
		}
	}
}

func (o *Orchestrator) process(ctx context.Context, batchID string, submissions []AccountSubmission, remote RemoteBatch) {
	o.setBatchStatus(ctx, batchID, BatchStatusDownloading, "")
	jobs := jobsByEmail(remote.Jobs)
	credentials, downloadClass := o.downloadCredentials(ctx, batchID, remote)
	credentialsByEmail, ambiguous := uniqueCredentialsByEmail(credentials)

	for _, submission := range submissions {
		job, ok := jobs[submission.Email]
		if !ok || !remoteJobSucceeded(job.Status) {
			o.failAccount(ctx, batchID, submission, remoteFailureClass(job.Status))
			continue
		}
		if downloadClass != "" {
			o.failAccount(ctx, batchID, submission, downloadClass)
			continue
		}
		if _, clash := ambiguous[submission.Email]; clash {
			o.failAccount(ctx, batchID, submission, "credential_invalid")
			continue
		}
		credential, ok := credentialsByEmail[submission.Email]
		if !ok {
			o.failAccount(ctx, batchID, submission, "credential_invalid")
			continue
		}
		o.updateAccount(batchID, submission, AccountStatusChannelReserved, "")
		if err := o.newAPI.UpdateChannel(ctx, submission.ChannelID, credential); err != nil {
			o.failAccount(ctx, batchID, submission, "channel_update_failed")
			continue
		}
		o.updateAccount(batchID, submission, AccountStatusTesting, "")
		result, testErr := o.newAPI.TestChannel(ctx, submission.ChannelID)
		if testErr != nil || !result.Success {
			o.failAccount(ctx, batchID, submission, "channel_test_failed")
			continue
		}
		o.updateAccount(batchID, submission, AccountStatusSucceeded, "")
		o.log(ctx, batchID, string(AccountStatusSucceeded), "", submission)
	}
	o.finishBatch(ctx, batchID)
}

// downloadCredentials downloads the batch-level CPA archive once and parses it
// through the hardened CPA parser. It returns a stable error class ("download_failed"
// or "credential_invalid") that applies to every successful account in the batch.
func (o *Orchestrator) downloadCredentials(ctx context.Context, batchID string, remote RemoteBatch) ([]Credential, string) {
	if remote.Complete == 0 {
		return nil, ""
	}
	download, err := o.sms688.DownloadCPA(ctx, remote.BatchID)
	if err != nil {
		o.log(ctx, batchID, "download_failed", "download_failed", AccountSubmission{})
		return nil, "download_failed"
	}
	o.setBatchStatus(ctx, batchID, BatchStatusProcessing, "")
	credentials, err := ParseCPA(download)
	if err != nil {
		o.log(ctx, batchID, "credential_invalid", "credential_invalid", AccountSubmission{})
		return nil, "credential_invalid"
	}
	return credentials, ""
}

// uniqueCredentialsByEmail maps credentials by normalized email. Emails that
// appear more than once are removed and reported as ambiguous so accounts are
// never assigned a credential by archive position.
func uniqueCredentialsByEmail(credentials []Credential) (map[string]Credential, map[string]struct{}) {
	result := make(map[string]Credential, len(credentials))
	ambiguous := make(map[string]struct{})
	for _, credential := range credentials {
		email, ok := normalizeEmail(credential.Email)
		if !ok {
			continue
		}
		if _, exists := result[email]; exists {
			ambiguous[email] = struct{}{}
			delete(result, email)
			continue
		}
		result[email] = credential
	}
	return result, ambiguous
}

func (o *Orchestrator) applyRemoteStatuses(batchID string, remote RemoteBatch) {
	jobs := jobsByEmail(remote.Jobs)
	o.updateBatch(batchID, func(batch Batch) Batch {
		for index := range batch.Accounts {
			job, ok := jobsByMaskedEmail(jobs, batch.Accounts[index].MaskedEmail)
			if !ok {
				continue
			}
			batch.Accounts[index].Stage = job.Stage
			batch.Accounts[index].Status = mapRemoteStatus(job.Status)
			batch.Accounts[index].UpdatedAt = time.Now().UTC()
		}
		return batch
	})
}

func (o *Orchestrator) updateAccount(batchID string, submission AccountSubmission, status AccountStatus, class string) {
	o.updateBatch(batchID, func(batch Batch) Batch {
		for index := range batch.Accounts {
			if batch.Accounts[index].ChannelID == submission.ChannelID {
				batch.Accounts[index].Status = status
				batch.Accounts[index].ErrorClass = class
				batch.Accounts[index].UpdatedAt = time.Now().UTC()
				break
			}
		}
		return batch
	})
}

func (o *Orchestrator) failAccount(ctx context.Context, batchID string, submission AccountSubmission, class string) {
	status := statusForError(class)
	o.updateAccount(batchID, submission, status, class)
	o.log(ctx, batchID, string(status), class, submission)
}

func (o *Orchestrator) finishBatch(ctx context.Context, batchID string) {
	batch, err := o.updateBatch(batchID, func(batch Batch) Batch {
		succeeded := 0
		for _, account := range batch.Accounts {
			if account.Status == AccountStatusSucceeded {
				succeeded++
			}
		}
		switch {
		case succeeded == len(batch.Accounts):
			batch.Status = BatchStatusCompleted
		case succeeded > 0:
			batch.Status = BatchStatusPartialCompleted
		default:
			batch.Status = BatchStatusFailed
		}
		return batch
	})
	if err == nil {
		o.log(ctx, batchID, string(batch.Status), "", AccountSubmission{})
	}
}

func (o *Orchestrator) failBatch(ctx context.Context, batchID, class string) {
	o.updateBatch(batchID, func(batch Batch) Batch {
		batch.Status = BatchStatusFailed
		batch.ErrorClass = class
		return batch
	})
	o.log(ctx, batchID, string(BatchStatusFailed), class, AccountSubmission{})
}

func (o *Orchestrator) setBatchStatus(ctx context.Context, batchID string, status BatchStatus, class string) {
	o.updateBatch(batchID, func(batch Batch) Batch {
		batch.Status = status
		batch.ErrorClass = class
		return batch
	})
	o.log(ctx, batchID, string(status), class, AccountSubmission{})
}

func (o *Orchestrator) updateBatch(id string, change func(Batch) Batch) (Batch, error) {
	return o.store.Update(id, func(batch Batch) (Batch, error) { return change(batch), nil })
}

func (o *Orchestrator) log(_ context.Context, batchID, status, class string, submission AccountSubmission) {
	fields := map[string]any{"batch_id": batchID, "status": status}
	if submission.MaskedEmail != "" {
		fields["masked_email"] = submission.MaskedEmail
	}
	if submission.ChannelID != 0 {
		fields["channel_id"] = submission.ChannelID
	}
	if class != "" {
		fields["error_class"] = class
		o.logger.Error(status, fields)
		return
	}
	o.logger.Info(status, fields)
}

type noopLogger struct{}

func (noopLogger) Info(string, map[string]any)  {}
func (noopLogger) Error(string, map[string]any) {}

func submissionText(submissions []AccountSubmission) string {
	lines := make([]string, len(submissions))
	for index, submission := range submissions {
		lines[index] = submission.AccountLine
	}
	return strings.Join(lines, "\n")
}

func jobsByEmail(jobs []RemoteJob) map[string]RemoteJob {
	result := make(map[string]RemoteJob, len(jobs))
	for _, job := range jobs {
		email, ok := normalizeEmail(job.Email)
		if ok {
			result[email] = job
		}
	}
	return result
}

func jobsByMaskedEmail(jobs map[string]RemoteJob, masked string) (RemoteJob, bool) {
	for email, job := range jobs {
		if MaskEmail(email) == masked {
			return job, true
		}
	}
	return RemoteJob{}, false
}

func remoteJobSucceeded(status string) bool {
	switch strings.ToLower(status) {
	case "completed", "complete", "success", "succeeded", "finished":
		return true
	default:
		return false
	}
}

func mapRemoteStatus(status string) AccountStatus {
	switch strings.ToLower(status) {
	case "queued", "pending":
		return AccountStatusSMS688Queued
	case "running", "processing":
		return AccountStatusSMS688Running
	case "waiting":
		return AccountStatusSMS688Waiting
	case "expired":
		return AccountStatusSMS688Expired
	case "cancelled", "canceled":
		return AccountStatusSMS688Cancelled
	case "completed", "complete", "success", "succeeded", "finished":
		return AccountStatusCredentialReady
	default:
		return AccountStatusSMS688Failed
	}
}

func remoteFailureClass(status string) string {
	switch mapRemoteStatus(status) {
	case AccountStatusSMS688Expired:
		return "sms688_expired"
	case AccountStatusSMS688Cancelled:
		return "sms688_cancelled"
	default:
		return "sms688_failed"
	}
}

func statusForError(class string) AccountStatus {
	switch class {
	case "download_failed":
		return AccountStatusDownloadFailed
	case "credential_invalid":
		return AccountStatusCredentialInvalid
	case "channel_update_failed":
		return AccountStatusChannelUpdateFailed
	case "channel_test_failed":
		return AccountStatusChannelTestFailed
	case "sms688_expired":
		return AccountStatusSMS688Expired
	case "sms688_cancelled":
		return AccountStatusSMS688Cancelled
	default:
		return AccountStatusSMS688Failed
	}
}

func errorClass(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	message := err.Error()
	if index := strings.IndexByte(message, ':'); index > 0 {
		return message[:index]
	}
	return fallback
}
