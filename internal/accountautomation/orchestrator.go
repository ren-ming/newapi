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
}

type Orchestrator struct {
	store  JobStore
	sms688 SMS688Service
	newAPI NewAPIService
	logger Logger
	config OrchestratorConfig
}

var jobSequence atomic.Uint64

func NewOrchestrator(store JobStore, sms688 SMS688Service, newAPI NewAPIService, logger Logger, config OrchestratorConfig) *Orchestrator {
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

// SubmitJob validates a single-account request, persists it as a job and runs
// it asynchronously. The returned job is a snapshot in status submitting.
func (o *Orchestrator) SubmitJob(ctx context.Context, request CreateJobRequest) (Job, error) {
	if o.store == nil || o.sms688 == nil || o.newAPI == nil {
		return Job{}, fmt.Errorf("orchestrator_dependency_missing")
	}
	if request.ChannelID <= 0 {
		return Job{}, fmt.Errorf("channel_id_invalid")
	}
	_, masked, line, err := ParseSingleAccount(request.AccountMode, request.AccountText)
	if err != nil {
		return Job{}, err
	}
	now := time.Now().UTC()
	job := Job{
		ID:          fmt.Sprintf("job-%d-%d", now.UnixNano(), jobSequence.Add(1)),
		AccountMode: request.AccountMode,
		MaskedEmail: masked,
		ChannelID:   request.ChannelID,
		BindFree:    request.BindFree,
		Status:      JobStatusSubmitting,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err = o.store.CreateJob(job); err != nil {
		return Job{}, fmt.Errorf("create job: %w", err)
	}
	o.logJob(job, string(JobStatusSubmitting), "")
	go o.runJob(context.WithoutCancel(ctx), job, line)
	return job, nil
}

func (o *Orchestrator) GetJob(id string) (Job, bool) {
	if o.store == nil {
		return Job{}, false
	}
	job, err := o.store.GetJob(id)
	if err != nil {
		return Job{}, false
	}
	return job, true
}

func (o *Orchestrator) ListJobs(offset, limit int) ([]Job, int64, error) {
	if o.store == nil {
		return []Job{}, 0, nil
	}
	return o.store.ListJobs(offset, limit)
}

// Resume continues a job that was in flight when the process restarted. Jobs
// with a remote batch keep polling; anything else is marked failed because the
// account line is never persisted and cannot be re-submitted.
func (o *Orchestrator) Resume(ctx context.Context, job Job) {
	if o.store == nil || o.sms688 == nil || o.newAPI == nil {
		return
	}
	// Re-read from the store: the caller's snapshot may be stale.
	stored, err := o.store.GetJob(job.ID)
	if err != nil {
		return
	}
	if IsTerminalJobStatus(stored.Status) {
		return
	}
	if stored.SMS688BatchID == "" {
		o.failJob(stored, JobStatusSubmitFailed, "interrupted")
		return
	}
	if time.Since(stored.CreatedAt) > o.config.BatchDeadline {
		o.failJob(stored, JobStatusSMS688Failed, "interrupted")
		return
	}
	go o.runJob(context.WithoutCancel(ctx), stored, "")
}

func (o *Orchestrator) runJob(ctx context.Context, job Job, accountLine string) {
	if accountLine != "" {
		o.setJob(job.ID, JobStatusSubmitting, "", "")
		remote, err := o.sms688.CreateTask(ctx, SMS688CreateRequest{
			AccountMode: job.AccountMode,
			AccountText: accountLine,
			BindFree:    job.BindFree,
		}, job.ID)
		if err != nil || remote.BatchID == "" {
			o.failJob(job, JobStatusSubmitFailed, errorClass(err, "sms688_submit_failed"))
			return
		}
		job.SMS688BatchID = remote.BatchID
		o.setJob(job.ID, JobStatusSMS688Queued, "", remote.BatchID)
		o.logJob(job, "submitted", "")
	}

	remoteJob, ok := o.pollJob(ctx, job)
	if !ok {
		return
	}
	if !remoteJobSucceeded(remoteJob.Status) {
		o.failJob(job, remoteFailureStatus(remoteJob.Status), remoteFailureClass(remoteJob.Status))
		return
	}

	o.setJob(job.ID, JobStatusCredentialReady, remoteJob.Stage, job.SMS688BatchID)
	credential, ok := o.matchCredential(ctx, job)
	if !ok {
		return
	}
	o.setJob(job.ID, JobStatusChannelUpdated, "", job.SMS688BatchID)
	if err := o.newAPI.UpdateChannel(ctx, job.ChannelID, credential); err != nil {
		o.failJob(job, JobStatusChannelUpdateFailed, errorClass(err, "channel_update_failed"))
		return
	}
	o.setJob(job.ID, JobStatusTesting, "", job.SMS688BatchID)
	result, err := o.newAPI.TestChannel(ctx, job.ChannelID)
	if err != nil || !result.Success {
		o.failJob(job, JobStatusChannelTestFailed, "channel_test_failed")
		return
	}
	o.setJob(job.ID, JobStatusSucceeded, "", job.SMS688BatchID)
	o.logJob(job, string(JobStatusSucceeded), "")
}

// pollJob polls SMS688 until the batch finishes, mirroring remote job status
// and stage into the stored job. It returns false when the job already
// reached a terminal failure state.
func (o *Orchestrator) pollJob(ctx context.Context, job Job) (RemoteJob, bool) {
	pollCtx, cancel := context.WithTimeout(ctx, o.config.BatchDeadline)
	defer cancel()
	ticker := time.NewTicker(o.config.PollInterval)
	defer ticker.Stop()
	for {
		remote, err := o.sms688.GetTask(pollCtx, job.SMS688BatchID)
		if err != nil {
			o.failJob(job, JobStatusSMS688Failed, errorClass(err, "sms688_poll_failed"))
			return RemoteJob{}, false
		}
		remoteJob, found := remoteJobByMaskedEmail(remote.Jobs, job.MaskedEmail)
		if found {
			o.setJob(job.ID, mapRemoteJobStatus(remoteJob.Status), remoteJob.Stage, job.SMS688BatchID)
		}
		if remote.AllFinished {
			if !found {
				o.failJob(job, JobStatusSMS688Failed, "sms688_job_missing")
				return RemoteJob{}, false
			}
			return remoteJob, true
		}
		select {
		case <-pollCtx.Done():
			o.failJob(job, JobStatusSMS688Failed, "sms688_poll_timeout")
			return RemoteJob{}, false
		case <-ticker.C:
		}
	}
}

// matchCredential downloads the batch-level CPA archive once and selects the
// credential whose email matches the job's masked email. Ambiguous or missing
// matches fail the job instead of guessing by archive position.
func (o *Orchestrator) matchCredential(ctx context.Context, job Job) (Credential, bool) {
	download, err := o.sms688.DownloadCPA(ctx, job.SMS688BatchID)
	if err != nil {
		o.failJob(job, JobStatusDownloadFailed, "download_failed")
		return Credential{}, false
	}
	credentials, err := ParseCPA(download)
	if err != nil {
		o.failJob(job, JobStatusCredentialInvalid, "credential_invalid")
		return Credential{}, false
	}
	byMasked, ambiguous := uniqueCredentialsByMaskedEmail(credentials)
	if _, clash := ambiguous[job.MaskedEmail]; clash {
		o.failJob(job, JobStatusCredentialInvalid, "credential_invalid")
		return Credential{}, false
	}
	credential, ok := byMasked[job.MaskedEmail]
	if !ok {
		o.failJob(job, JobStatusCredentialInvalid, "credential_invalid")
		return Credential{}, false
	}
	return credential, true
}

func (o *Orchestrator) setJob(id string, status JobStatus, stage, batchID string) {
	err := o.store.UpdateJob(id, func(job *Job) {
		job.Status = status
		job.Stage = stage
		job.ErrorClass = statusErrorClass(status)
		if batchID != "" {
			job.SMS688BatchID = batchID
		}
	})
	if err == nil {
		o.logger.Info(string(status), map[string]any{"job_id": id, "status": string(status)})
	}
}

func (o *Orchestrator) failJob(job Job, status JobStatus, class string) {
	err := o.store.UpdateJob(job.ID, func(stored *Job) {
		stored.Status = status
		stored.ErrorClass = class
	})
	if err == nil {
		o.logger.Error(string(status), map[string]any{
			"job_id": job.ID, "masked_email": job.MaskedEmail,
			"channel_id": job.ChannelID, "status": string(status), "error_class": class,
		})
	}
}

func (o *Orchestrator) logJob(job Job, status, class string) {
	fields := map[string]any{
		"job_id":       job.ID,
		"masked_email": job.MaskedEmail,
		"channel_id":   job.ChannelID,
		"status":       status,
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

func remoteJobByMaskedEmail(jobs []RemoteJob, masked string) (RemoteJob, bool) {
	for _, job := range jobs {
		candidate := job.EmailMasked
		if candidate == "" {
			if email, ok := normalizeEmail(job.Email); ok {
				candidate = MaskEmail(email)
			}
		}
		if candidate == masked {
			return job, true
		}
	}
	return RemoteJob{}, false
}

// uniqueCredentialsByMaskedEmail maps credentials by masked email. Masked
// values that appear more than once are removed and reported as ambiguous so
// jobs are never assigned a credential by archive position.
func uniqueCredentialsByMaskedEmail(credentials []Credential) (map[string]Credential, map[string]struct{}) {
	result := make(map[string]Credential, len(credentials))
	ambiguous := make(map[string]struct{})
	for _, credential := range credentials {
		email, ok := normalizeEmail(credential.Email)
		if !ok {
			continue
		}
		masked := MaskEmail(email)
		if _, exists := result[masked]; exists {
			ambiguous[masked] = struct{}{}
			delete(result, masked)
			continue
		}
		result[masked] = credential
	}
	return result, ambiguous
}

func remoteJobSucceeded(status string) bool {
	switch strings.ToLower(status) {
	case "completed", "complete", "success", "succeeded", "finished":
		return true
	default:
		return false
	}
}

func mapRemoteJobStatus(status string) JobStatus {
	switch strings.ToLower(status) {
	case "queued", "pending":
		return JobStatusSMS688Queued
	case "running", "processing":
		return JobStatusSMS688Running
	case "waiting":
		return JobStatusSMS688Waiting
	case "expired":
		return JobStatusSMS688Expired
	case "cancelled", "canceled":
		return JobStatusSMS688Cancelled
	case "completed", "complete", "success", "succeeded", "finished":
		return JobStatusCredentialReady
	default:
		return JobStatusSMS688Failed
	}
}

func remoteFailureStatus(status string) JobStatus {
	switch mapRemoteJobStatus(status) {
	case JobStatusSMS688Expired:
		return JobStatusSMS688Expired
	case JobStatusSMS688Cancelled:
		return JobStatusSMS688Cancelled
	default:
		return JobStatusSMS688Failed
	}
}

func remoteFailureClass(status string) string {
	switch mapRemoteJobStatus(status) {
	case JobStatusSMS688Expired:
		return "sms688_expired"
	case JobStatusSMS688Cancelled:
		return "sms688_cancelled"
	default:
		return "sms688_failed"
	}
}

func statusErrorClass(status JobStatus) string {
	switch status {
	case JobStatusSMS688Expired:
		return "sms688_expired"
	case JobStatusSMS688Cancelled:
		return "sms688_cancelled"
	case JobStatusSMS688Failed:
		return "sms688_failed"
	default:
		return ""
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
