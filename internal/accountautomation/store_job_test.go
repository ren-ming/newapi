package accountautomation

import (
	"fmt"
	"testing"
	"time"
)

func newTestJob(offset time.Duration) Job {
	now := time.Now().UTC().Add(offset)
	return Job{
		ID:          fmt.Sprintf("job-%d", now.UnixNano()),
		AccountMode: AccountModeMicrosoft,
		MaskedEmail: "u***r@example.com",
		ChannelID:   42,
		Status:      JobStatusSMS688Running,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func TestMemoryJobStoreCRUD(t *testing.T) {
	t.Parallel()
	store := NewMemoryJobStore()
	job := newTestJob(0)
	if err := store.CreateJob(job); err != nil {
		t.Fatalf("CreateJob() error = %v", err)
	}
	if err := store.CreateJob(job); err == nil {
		t.Fatal("CreateJob() duplicate should fail")
	}
	loaded, err := store.GetJob(job.ID)
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	if loaded.Status != JobStatusSMS688Running {
		t.Fatalf("status = %q", loaded.Status)
	}
	if err := store.UpdateJob(job.ID, func(j *Job) { j.Status = JobStatusSucceeded; j.ErrorClass = "" }); err != nil {
		t.Fatalf("UpdateJob() error = %v", err)
	}
	loaded, _ = store.GetJob(job.ID)
	if loaded.Status != JobStatusSucceeded {
		t.Fatalf("updated status = %q, want succeeded", loaded.Status)
	}
	if _, err := store.GetJob("missing"); err == nil {
		t.Fatal("GetJob(missing) should fail")
	}
}

func TestMemoryJobStoreListOrderedAndPaged(t *testing.T) {
	t.Parallel()
	store := NewMemoryJobStore()
	for i := 0; i < 5; i++ {
		job := newTestJob(time.Duration(i) * time.Minute)
		job.ID = fmt.Sprintf("job-%02d", i)
		if err := store.CreateJob(job); err != nil {
			t.Fatalf("CreateJob() error = %v", err)
		}
	}
	jobs, total, err := store.ListJobs(0, 3)
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	if total != 5 {
		t.Fatalf("total = %d, want 5", total)
	}
	if len(jobs) != 3 {
		t.Fatalf("len(jobs) = %d, want 3", len(jobs))
	}
	if jobs[0].ID != "job-04" {
		t.Fatalf("newest first: jobs[0].ID = %q, want job-04", jobs[0].ID)
	}
	tail, total, err := store.ListJobs(4, 3)
	if err != nil {
		t.Fatalf("ListJobs(tail) error = %v", err)
	}
	if total != 5 || len(tail) != 1 || tail[0].ID != "job-00" {
		t.Fatalf("tail = %v total=%d, want single oldest", tail, total)
	}
}

func TestMemoryJobStoreActiveJobs(t *testing.T) {
	t.Parallel()
	store := NewMemoryJobStore()
	active := newTestJob(0)
	active.ID = "job-active"
	failed := newTestJob(time.Minute)
	failed.ID = "job-failed"
	failed.Status = JobStatusSMS688Failed
	failed.ErrorClass = "sms688_failed"
	done := newTestJob(2 * time.Minute)
	done.ID = "job-done"
	done.Status = JobStatusSucceeded
	for _, job := range []Job{active, failed, done} {
		if err := store.CreateJob(job); err != nil {
			t.Fatalf("CreateJob() error = %v", err)
		}
	}
	running, err := store.ActiveJobs()
	if err != nil {
		t.Fatalf("ActiveJobs() error = %v", err)
	}
	if len(running) != 1 || running[0].ID != "job-active" {
		t.Fatalf("ActiveJobs() = %v, want only job-active", running)
	}
}
