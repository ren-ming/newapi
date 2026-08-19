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
