package model

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/internal/accountautomation"
	"gorm.io/gorm"
)

// AccountAutomationJob persists one automation job per submitted account.
// Only non-sensitive fields are stored: the account line itself, passwords,
// and downloaded credentials never reach this table.
type AccountAutomationJob struct {
	Id          string `gorm:"primaryKey;size:64"`
	AccountMode string `gorm:"size:16;index"`
	MaskedEmail string `gorm:"size:128"`
	ChannelId   int
	BindFree    bool
	Status      string `gorm:"size:32;index"`
	Stage       string `gorm:"size:64"`
	ErrorClass  string `gorm:"size:64"`
	SmsBatchId  string `gorm:"size:64"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (AccountAutomationJob) TableName() string {
	return "account_automation_jobs"
}

// AccountAutomationJobStore implements accountautomation.JobStore on GORM.
// It is safe for concurrent use; all database access goes through the
// package-level DB handle.
type AccountAutomationJobStore struct{}

func NewAccountAutomationJobStore() *AccountAutomationJobStore {
	return &AccountAutomationJobStore{}
}

func (AccountAutomationJobStore) CreateJob(job accountautomation.Job) error {
	if job.ID == "" {
		return fmt.Errorf("job_id_required")
	}
	record := jobRecordFromDomain(job)
	if err := DB.Create(&record).Error; err != nil {
		return fmt.Errorf("job_create_failed: %w", err)
	}
	return nil
}

func (AccountAutomationJobStore) GetJob(id string) (accountautomation.Job, error) {
	var record AccountAutomationJob
	if err := DB.Where("id = ?", id).First(&record).Error; err != nil {
		return accountautomation.Job{}, fmt.Errorf("job_not_found: %w", err)
	}
	return jobRecordToDomain(record), nil
}

func (AccountAutomationJobStore) ListJobs(offset, limit int) ([]accountautomation.Job, int64, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 50
	}
	var total int64
	if err := DB.Model(&AccountAutomationJob{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("job_count_failed: %w", err)
	}
	var records []AccountAutomationJob
	if err := DB.Order("updated_at DESC").Offset(offset).Limit(limit).Find(&records).Error; err != nil {
		return nil, 0, fmt.Errorf("job_list_failed: %w", err)
	}
	jobs := make([]accountautomation.Job, 0, len(records))
	for _, record := range records {
		jobs = append(jobs, jobRecordToDomain(record))
	}
	return jobs, total, nil
}

func (AccountAutomationJobStore) UpdateJob(id string, change func(*accountautomation.Job)) error {
	if change == nil {
		return fmt.Errorf("change_required")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var record AccountAutomationJob
		if err := tx.Where("id = ?", id).First(&record).Error; err != nil {
			return fmt.Errorf("job_not_found: %w", err)
		}
		job := jobRecordToDomain(record)
		change(&job)
		next := jobRecordFromDomain(job)
		next.Id = record.Id
		next.CreatedAt = record.CreatedAt
		return tx.Model(&AccountAutomationJob{}).Where("id = ?", id).Updates(map[string]any{
			"account_mode": next.AccountMode,
			"masked_email": next.MaskedEmail,
			"channel_id":   next.ChannelId,
			"bind_free":    next.BindFree,
			"status":       next.Status,
			"stage":        next.Stage,
			"error_class":  next.ErrorClass,
			"sms_batch_id": next.SmsBatchId,
			"updated_at":   time.Now().UTC(),
		}).Error
	})
}

func (AccountAutomationJobStore) ActiveJobs() ([]accountautomation.Job, error) {
	var records []AccountAutomationJob
	terminal := []string{
		string(accountautomation.JobStatusSucceeded),
		string(accountautomation.JobStatusSubmitFailed),
		string(accountautomation.JobStatusSMS688Failed),
		string(accountautomation.JobStatusSMS688Expired),
		string(accountautomation.JobStatusSMS688Cancelled),
		string(accountautomation.JobStatusDownloadFailed),
		string(accountautomation.JobStatusCredentialInvalid),
		string(accountautomation.JobStatusChannelUpdateFailed),
		string(accountautomation.JobStatusChannelTestFailed),
	}
	if err := DB.Where("status NOT IN ?", terminal).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("job_active_list_failed: %w", err)
	}
	jobs := make([]accountautomation.Job, 0, len(records))
	for _, record := range records {
		jobs = append(jobs, jobRecordToDomain(record))
	}
	return jobs, nil
}

func jobRecordFromDomain(job accountautomation.Job) AccountAutomationJob {
	return AccountAutomationJob{
		Id:          job.ID,
		AccountMode: job.AccountMode,
		MaskedEmail: job.MaskedEmail,
		ChannelId:   job.ChannelID,
		BindFree:    job.BindFree,
		Status:      string(job.Status),
		Stage:       job.Stage,
		ErrorClass:  job.ErrorClass,
		SmsBatchId:  job.SMS688BatchID,
		CreatedAt:   job.CreatedAt,
		UpdatedAt:   job.UpdatedAt,
	}
}

func jobRecordToDomain(record AccountAutomationJob) accountautomation.Job {
	return accountautomation.Job{
		ID:            record.Id,
		AccountMode:   record.AccountMode,
		MaskedEmail:   record.MaskedEmail,
		ChannelID:     record.ChannelId,
		BindFree:      record.BindFree,
		Status:        accountautomation.JobStatus(record.Status),
		Stage:         record.Stage,
		ErrorClass:    record.ErrorClass,
		SMS688BatchID: record.SmsBatchId,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
	}
}
