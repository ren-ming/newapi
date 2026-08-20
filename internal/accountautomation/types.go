package accountautomation

import "time"

const (
	AccountModeMicrosoft = "microsoft"
	AccountModeTotp      = "totp"
)

type Credential struct {
	IDToken      string `json:"id_token,omitempty"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	AccountID    string `json:"account_id"`
	LastRefresh  string `json:"last_refresh,omitempty"`
	Email        string `json:"email,omitempty"`
	Type         string `json:"type,omitempty"`
	Expired      string `json:"expired,omitempty"`
}

type SMS688CreateRequest struct {
	AccountMode string `json:"account_mode"`
	AccountText string `json:"account_text"`
	BindFree    bool   `json:"bind_free"`
}

type RemoteBatch struct {
	BatchID     string      `json:"batch_id"`
	AllFinished bool        `json:"all_finished"`
	Total       int         `json:"total"`
	Complete    int         `json:"complete"`
	Error       int         `json:"error"`
	Cancelled   int         `json:"cancelled"`
	Expired     int         `json:"expired"`
	Status      string      `json:"status"`
	Jobs        []RemoteJob `json:"jobs"`
}

type remoteBatchEnvelope struct {
	Batches []RemoteBatch `json:"batches"`
}

type RemoteJob struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	EmailMasked string `json:"email_masked"`
	Status      string `json:"status"`
	Stage       string `json:"stage"`
}

type DownloadedCPA struct {
	ContentType string
	Data        []byte
}

type ChannelTestResult struct {
	Success   bool    `json:"success"`
	Message   string  `json:"message,omitempty"`
	Time      float64 `json:"time,omitempty"`
	ErrorCode string  `json:"error_code,omitempty"`
}

// --- v2 single-account job model ---

type JobStatus string

const (
	JobStatusSubmitting          JobStatus = "submitting"
	JobStatusSMS688Queued        JobStatus = "sms688_queued"
	JobStatusSMS688Running       JobStatus = "sms688_running"
	JobStatusSMS688Waiting       JobStatus = "sms688_waiting"
	JobStatusCredentialReady     JobStatus = "credential_ready"
	JobStatusChannelUpdated      JobStatus = "channel_updated"
	JobStatusTesting             JobStatus = "testing"
	JobStatusSucceeded           JobStatus = "succeeded"
	JobStatusSubmitFailed        JobStatus = "submit_failed"
	JobStatusSMS688Failed        JobStatus = "sms688_failed"
	JobStatusSMS688Expired       JobStatus = "sms688_expired"
	JobStatusSMS688Cancelled     JobStatus = "sms688_cancelled"
	JobStatusDownloadFailed      JobStatus = "download_failed"
	JobStatusCredentialInvalid   JobStatus = "credential_invalid"
	JobStatusChannelUpdateFailed JobStatus = "channel_update_failed"
	JobStatusChannelTestFailed   JobStatus = "channel_test_failed"
)

// IsTerminalJobStatus reports whether the status is final: the job will not
// transition again without a fresh submission.
func IsTerminalJobStatus(status JobStatus) bool {
	switch status {
	case JobStatusSucceeded,
		JobStatusSubmitFailed,
		JobStatusSMS688Failed,
		JobStatusSMS688Expired,
		JobStatusSMS688Cancelled,
		JobStatusDownloadFailed,
		JobStatusCredentialInvalid,
		JobStatusChannelUpdateFailed,
		JobStatusChannelTestFailed:
		return true
	default:
		return false
	}
}

type Job struct {
	ID            string    `json:"id"`
	AccountMode   string    `json:"account_mode"`
	MaskedEmail   string    `json:"masked_email"`
	ChannelID     int       `json:"channel_id"`
	BindFree      bool      `json:"bind_free"`
	Status        JobStatus `json:"status"`
	Stage         string    `json:"stage,omitempty"`
	ErrorClass    string    `json:"error_class,omitempty"`
	SMS688BatchID string    `json:"sms688_batch_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CreateJobRequest struct {
	AccountMode string `json:"account_mode"`
	AccountText string `json:"account_text"`
	ChannelID   int    `json:"channel_id"`
	BindFree    bool   `json:"bind_free"`
}
