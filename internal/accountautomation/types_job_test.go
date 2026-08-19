package accountautomation

import "testing"

func TestIsTerminalJobStatus(t *testing.T) {
	t.Parallel()
	terminal := []JobStatus{
		JobStatusSucceeded,
		JobStatusSubmitFailed,
		JobStatusSMS688Failed,
		JobStatusSMS688Expired,
		JobStatusSMS688Cancelled,
		JobStatusDownloadFailed,
		JobStatusCredentialInvalid,
		JobStatusChannelUpdateFailed,
		JobStatusChannelTestFailed,
	}
	for _, status := range terminal {
		if !IsTerminalJobStatus(status) {
			t.Errorf("IsTerminalJobStatus(%q) = false, want true", status)
		}
	}
	active := []JobStatus{
		JobStatusSubmitting,
		JobStatusSMS688Queued,
		JobStatusSMS688Running,
		JobStatusSMS688Waiting,
		JobStatusCredentialReady,
		JobStatusChannelUpdated,
		JobStatusTesting,
	}
	for _, status := range active {
		if IsTerminalJobStatus(status) {
			t.Errorf("IsTerminalJobStatus(%q) = true, want false", status)
		}
	}
}
