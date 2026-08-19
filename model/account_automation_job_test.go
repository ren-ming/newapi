package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/accountautomation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearAccountAutomationJobs(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Where("1 = 1").Delete(&AccountAutomationJob{}).Error)
}

func TestAccountAutomationJobStoreCRUD(t *testing.T) {
	clearAccountAutomationJobs(t)
	store := NewAccountAutomationJobStore()
	now := time.Now().UTC()
	job := accountautomation.Job{
		ID:            "job-1",
		AccountMode:   accountautomation.AccountModeMicrosoft,
		MaskedEmail:   "u***r@example.com",
		ChannelID:     42,
		BindFree:      true,
		Status:        accountautomation.JobStatusSMS688Running,
		SMS688BatchID: "remote-1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	require.NoError(t, store.CreateJob(job))
	require.Error(t, store.CreateJob(job), "duplicate create should fail")

	loaded, err := store.GetJob("job-1")
	require.NoError(t, err)
	assert.Equal(t, job.ID, loaded.ID)
	assert.Equal(t, job.AccountMode, loaded.AccountMode)
	assert.Equal(t, job.MaskedEmail, loaded.MaskedEmail)
	assert.Equal(t, job.ChannelID, loaded.ChannelID)
	assert.True(t, loaded.BindFree)
	assert.Equal(t, job.Status, loaded.Status)
	assert.Equal(t, job.SMS688BatchID, loaded.SMS688BatchID)

	require.NoError(t, store.UpdateJob("job-1", func(stored *accountautomation.Job) {
		stored.Status = accountautomation.JobStatusSucceeded
		stored.ErrorClass = ""
	}))
	loaded, err = store.GetJob("job-1")
	require.NoError(t, err)
	assert.Equal(t, accountautomation.JobStatusSucceeded, loaded.Status)

	_, err = store.GetJob("missing")
	assert.Error(t, err)
	assert.Error(t, store.UpdateJob("missing", func(stored *accountautomation.Job) {}))
}

func TestAccountAutomationJobStoreListOrderedAndPaged(t *testing.T) {
	clearAccountAutomationJobs(t)
	store := NewAccountAutomationJobStore()
	base := time.Now().UTC()
	for i := 0; i < 5; i++ {
		job := accountautomation.Job{
			ID:          fmt.Sprintf("job-page-%d", i),
			AccountMode: accountautomation.AccountModeTotp,
			MaskedEmail: fmt.Sprintf("u%d***r@example.com", i),
			ChannelID:   i + 1,
			Status:      accountautomation.JobStatusSucceeded,
			CreatedAt:   base.Add(time.Duration(i) * time.Minute),
			UpdatedAt:   base.Add(time.Duration(i) * time.Minute),
		}
		require.NoError(t, store.CreateJob(job))
	}
	jobs, total, err := store.ListJobs(0, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	assert.Len(t, jobs, 3)
	assert.Equal(t, "job-page-4", jobs[0].ID, "newest first")

	tail, total, err := store.ListJobs(4, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	assert.Len(t, tail, 1)
	assert.Equal(t, "job-page-0", tail[0].ID)
}

func TestAccountAutomationJobStoreActiveJobs(t *testing.T) {
	clearAccountAutomationJobs(t)
	store := NewAccountAutomationJobStore()
	base := time.Now().UTC()
	jobs := []accountautomation.Job{
		{ID: "job-active", AccountMode: accountautomation.AccountModeMicrosoft, MaskedEmail: "a***@example.com", ChannelID: 1, Status: accountautomation.JobStatusSMS688Queued, CreatedAt: base, UpdatedAt: base},
		{ID: "job-done", AccountMode: accountautomation.AccountModeMicrosoft, MaskedEmail: "b***@example.com", ChannelID: 2, Status: accountautomation.JobStatusSucceeded, CreatedAt: base, UpdatedAt: base},
		{ID: "job-failed", AccountMode: accountautomation.AccountModeMicrosoft, MaskedEmail: "c***@example.com", ChannelID: 3, Status: accountautomation.JobStatusSMS688Failed, ErrorClass: "sms688_failed", CreatedAt: base, UpdatedAt: base},
	}
	for _, job := range jobs {
		require.NoError(t, store.CreateJob(job))
	}
	active, err := store.ActiveJobs()
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, "job-active", active[0].ID)
}
