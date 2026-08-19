package accountautomation

import "time"

type BatchStatus string

const (
	BatchStatusCreated          BatchStatus = "created"
	BatchStatusSubmitting       BatchStatus = "submitting"
	BatchStatusSubmitted        BatchStatus = "submitted"
	BatchStatusPolling          BatchStatus = "polling"
	BatchStatusDownloading      BatchStatus = "downloading"
	BatchStatusProcessing       BatchStatus = "processing"
	BatchStatusCompleted        BatchStatus = "completed"
	BatchStatusPartialCompleted BatchStatus = "partial_completed"
	BatchStatusFailed           BatchStatus = "failed"
)

type AccountStatus string

const (
	AccountStatusPending             AccountStatus = "pending"
	AccountStatusSMS688Queued        AccountStatus = "sms688_queued"
	AccountStatusSMS688Running       AccountStatus = "sms688_running"
	AccountStatusSMS688Waiting       AccountStatus = "sms688_waiting"
	AccountStatusSMS688Failed        AccountStatus = "sms688_failed"
	AccountStatusSMS688Expired       AccountStatus = "sms688_expired"
	AccountStatusSMS688Cancelled     AccountStatus = "sms688_cancelled"
	AccountStatusCredentialReady     AccountStatus = "credential_ready"
	AccountStatusDownloadFailed      AccountStatus = "download_failed"
	AccountStatusCredentialInvalid   AccountStatus = "credential_invalid"
	AccountStatusChannelReserved     AccountStatus = "channel_reserved"
	AccountStatusChannelUpdated      AccountStatus = "channel_updated"
	AccountStatusTesting             AccountStatus = "testing"
	AccountStatusSucceeded           AccountStatus = "succeeded"
	AccountStatusChannelUpdateFailed AccountStatus = "channel_update_failed"
	AccountStatusChannelTestFailed   AccountStatus = "channel_test_failed"
)

type AccountSubmission struct {
	ChannelID   int    `json:"channel_id"`
	AccountLine string `json:"-"`
	Email       string `json:"-"`
	MaskedEmail string `json:"masked_email"`
}

type CreateBatchRequest struct {
	AccountText string `json:"account_text"`
	BindFree    bool   `json:"bind_free"`
}

type Batch struct {
	ID            string         `json:"id"`
	RemoteBatchID string         `json:"remote_batch_id,omitempty"`
	Status        BatchStatus    `json:"status"`
	Accounts      []BatchAccount `json:"accounts"`
	ErrorClass    string         `json:"error_class,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type BatchAccount struct {
	ID          string        `json:"id"`
	MaskedEmail string        `json:"masked_email"`
	ChannelID   int           `json:"channel_id"`
	Status      AccountStatus `json:"status"`
	Stage       string        `json:"stage,omitempty"`
	ErrorClass  string        `json:"error_class,omitempty"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

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
	Jobs        []RemoteJob `json:"jobs"`
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

const (
	AccountModeMicrosoft = "microsoft"
	AccountModeTotp      = "totp"
)

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
