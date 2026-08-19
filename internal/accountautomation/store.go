package accountautomation

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

type JobStore interface {
	CreateJob(Job) error
	GetJob(string) (Job, error)
	ListJobs(offset, limit int) ([]Job, int64, error)
	UpdateJob(id string, change func(*Job)) error
	ActiveJobs() ([]Job, error)
}

type MemoryJobStore struct {
	mu   sync.RWMutex
	jobs map[string]Job
}

func NewMemoryJobStore() *MemoryJobStore {
	return &MemoryJobStore{jobs: make(map[string]Job)}
}

func (s *MemoryJobStore) CreateJob(job Job) error {
	if job.ID == "" {
		return fmt.Errorf("job_id_required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[job.ID]; exists {
		return fmt.Errorf("job_exists")
	}
	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	s.jobs[job.ID] = cloneJob(job)
	return nil
}

func (s *MemoryJobStore) GetJob(id string) (Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	if !ok {
		return Job{}, fmt.Errorf("job_not_found")
	}
	return cloneJob(job), nil
}

func (s *MemoryJobStore) ListJobs(offset, limit int) ([]Job, int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ordered := make([]Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		ordered = append(ordered, cloneJob(job))
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].UpdatedAt.Equal(ordered[j].UpdatedAt) {
			return ordered[i].ID > ordered[j].ID
		}
		return ordered[i].UpdatedAt.After(ordered[j].UpdatedAt)
	})
	total := int64(len(ordered))
	if offset < 0 {
		offset = 0
	}
	if offset >= len(ordered) {
		return []Job{}, total, nil
	}
	if limit <= 0 {
		limit = len(ordered) - offset
	}
	end := offset + limit
	if end > len(ordered) {
		end = len(ordered)
	}
	return ordered[offset:end], total, nil
}

func (s *MemoryJobStore) UpdateJob(id string, change func(*Job)) error {
	if change == nil {
		return fmt.Errorf("change_required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.jobs[id]
	if !ok {
		return fmt.Errorf("job_not_found")
	}
	next := cloneJob(current)
	change(&next)
	next.ID = current.ID
	next.CreatedAt = current.CreatedAt
	next.UpdatedAt = time.Now().UTC()
	s.jobs[id] = next
	return nil
}

func (s *MemoryJobStore) ActiveJobs() ([]Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	active := make([]Job, 0)
	for _, job := range s.jobs {
		if !IsTerminalJobStatus(job.Status) {
			active = append(active, cloneJob(job))
		}
	}
	return active, nil
}

func cloneJob(job Job) Job {
	return job
}
