package controller

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accountautomation"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupAccountAutomationTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.AccountAutomationJob{}))
	// The in-memory database is per-connection: keep a single connection so
	// every query sees the same schema and rows.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous })
}

var accountAutomationChannelSeq atomic.Int64

func createAccountAutomationChannel(t *testing.T, channelType int) int {
	t.Helper()
	channel := &model.Channel{
		Id:     int(accountAutomationChannelSeq.Add(1)),
		Type:   channelType,
		Name:   "automation-test",
		Key:    "old-key",
		Models: "gpt-5",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	return channel.Id
}

func stubChannelTestResult(t *testing.T, result testResult) {
	t.Helper()
	previous := accountAutomationRunChannelTest
	accountAutomationRunChannelTest = func(*model.Channel, string, string, bool) testResult {
		return result
	}
	t.Cleanup(func() { accountAutomationRunChannelTest = previous })
}

func TestAccountAutomationChannelServiceUpdateChannel(t *testing.T) {
	setupAccountAutomationTestDB(t)
	service := AccountAutomationChannelService{}
	ctx := context.Background()
	credential := accountautomation.Credential{AccessToken: "at-1", AccountID: "aid-1"}

	t.Run("channel not found", func(t *testing.T) {
		err := service.UpdateChannel(ctx, 99, credential)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "newapi_channel_not_found")
	})

	t.Run("non codex channel rejected", func(t *testing.T) {
		id := createAccountAutomationChannel(t, 1)
		err := service.UpdateChannel(ctx, id, credential)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "newapi_channel_precondition_failed")
	})

	t.Run("invalid credential rejected", func(t *testing.T) {
		id := createAccountAutomationChannel(t, 57)
		err := service.UpdateChannel(ctx, id, accountautomation.Credential{AccessToken: "at-1"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "newapi_invalid_credential")
		stored, getErr := model.GetChannelById(id, true)
		require.NoError(t, getErr)
		assert.Equal(t, "old-key", stored.Key, "key must stay untouched on validation failure")
	})

	t.Run("codex channel key replaced with credential json", func(t *testing.T) {
		id := createAccountAutomationChannel(t, 57)
		require.NoError(t, service.UpdateChannel(ctx, id, credential))
		stored, err := model.GetChannelById(id, true)
		require.NoError(t, err)
		var decoded accountautomation.Credential
		require.NoError(t, common.Unmarshal([]byte(stored.Key), &decoded))
		assert.Equal(t, "at-1", decoded.AccessToken)
		assert.Equal(t, "aid-1", decoded.AccountID)
		assert.NotContains(t, stored.Key, "old-key")
	})
}

func TestAccountAutomationChannelServiceTestChannel(t *testing.T) {
	setupAccountAutomationTestDB(t)
	service := AccountAutomationChannelService{}
	ctx := context.Background()

	t.Run("channel not found", func(t *testing.T) {
		_, err := service.TestChannel(ctx, 99)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "newapi_channel_not_found")
	})

	t.Run("non codex channel rejected", func(t *testing.T) {
		id := createAccountAutomationChannel(t, 1)
		_, err := service.TestChannel(ctx, id)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "newapi_channel_precondition_failed")
	})

	t.Run("passing test reports success", func(t *testing.T) {
		stubChannelTestResult(t, testResult{})
		id := createAccountAutomationChannel(t, 57)
		result, err := service.TestChannel(ctx, id)
		require.NoError(t, err)
		assert.True(t, result.Success)
	})

	t.Run("passing test enables disabled channel", func(t *testing.T) {
		stubChannelTestResult(t, testResult{})
		id := createAccountAutomationChannel(t, 57)
		require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", id).
			Update("status", common.ChannelStatusManuallyDisabled).Error)
		result, err := service.TestChannel(ctx, id)
		require.NoError(t, err)
		assert.True(t, result.Success)
		stored, getErr := model.GetChannelById(id, true)
		require.NoError(t, getErr)
		assert.Equal(t, common.ChannelStatusEnabled, stored.Status,
			"a passing test must enable the channel for traffic")
	})

	t.Run("passing test leaves enabled channel untouched", func(t *testing.T) {
		stubChannelTestResult(t, testResult{})
		id := createAccountAutomationChannel(t, 57)
		result, err := service.TestChannel(ctx, id)
		require.NoError(t, err)
		assert.True(t, result.Success)
		stored, getErr := model.GetChannelById(id, true)
		require.NoError(t, getErr)
		assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	})

	t.Run("failing test reports failure without transport error", func(t *testing.T) {
		stubChannelTestResult(t, testResult{localErr: fmt.Errorf("upstream 401")})
		id := createAccountAutomationChannel(t, 57)
		result, err := service.TestChannel(ctx, id)
		require.NoError(t, err)
		assert.False(t, result.Success)
		assert.Contains(t, result.Message, "upstream 401")
	})

	t.Run("implements orchestrator NewAPIService", func(t *testing.T) {
		var _ accountautomation.NewAPIService = AccountAutomationChannelService{}
	})
}

type resumeFakeSMS688 struct {
	createCalled atomic.Bool
	pollDone     atomic.Bool
}

func (f *resumeFakeSMS688) CreateTask(context.Context, accountautomation.SMS688CreateRequest, string) (accountautomation.RemoteBatch, error) {
	f.createCalled.Store(true)
	return accountautomation.RemoteBatch{BatchID: "remote-resume"}, nil
}

func (f *resumeFakeSMS688) GetTask(_ context.Context, _ string) (accountautomation.RemoteBatch, error) {
	f.pollDone.Store(true)
	return accountautomation.RemoteBatch{
		BatchID:     "remote-resume",
		AllFinished: true,
		Complete:    1,
		Jobs:        []accountautomation.RemoteJob{{ID: "job-1", Email: "user@example.com", Status: "completed"}},
	}, nil
}

func (f *resumeFakeSMS688) DownloadCPA(_ context.Context, _ string) (accountautomation.DownloadedCPA, error) {
	data, err := common.Marshal(accountautomation.Credential{AccessToken: "at-1", AccountID: "aid-1", Email: "user@example.com"})
	if err != nil {
		return accountautomation.DownloadedCPA{}, err
	}
	return accountautomation.DownloadedCPA{ContentType: "application/json", Data: data}, nil
}

func TestAccountAutomationResumeOnBoot(t *testing.T) {
	setupAccountAutomationTestDB(t)
	stubChannelTestResult(t, testResult{})
	store := model.NewAccountAutomationJobStore()
	now := time.Now().UTC()
	require.NoError(t, store.CreateJob(accountautomation.Job{
		ID:            "job-boot",
		AccountMode:   accountautomation.AccountModeMicrosoft,
		MaskedEmail:   "u***r@example.com",
		ChannelID:     createAccountAutomationChannel(t, 57),
		Status:        accountautomation.JobStatusSMS688Running,
		SMS688BatchID: "remote-resume",
		CreatedAt:     now,
		UpdatedAt:     now,
	}))

	sms := &resumeFakeSMS688{}
	orchestrator := accountautomation.NewOrchestrator(store, sms, AccountAutomationChannelService{}, accountAutomationLogger{}, accountautomation.OrchestratorConfig{
		PollInterval:  time.Millisecond,
		BatchDeadline: time.Minute,
	})
	resumeAccountAutomationJobs(context.Background(), store, orchestrator)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := store.GetJob("job-boot")
		require.NoError(t, err)
		if accountautomation.IsTerminalJobStatus(job.Status) {
			assert.Equal(t, accountautomation.JobStatusSucceeded, job.Status)
			assert.False(t, sms.createCalled.Load(), "resume must not re-submit to SMS688")
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("resumed job did not reach terminal status")
}
