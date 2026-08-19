package accountautomation

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

type Store interface {
	Create(Batch, []AccountSubmission) error
	Get(string) (Batch, error)
	List() []Batch
	Submissions(string) ([]AccountSubmission, error)
	Update(string, func(Batch) (Batch, error)) (Batch, error)
}

type MemoryStore struct {
	mu      sync.RWMutex
	batches map[string]Batch
	inputs  map[string][]AccountSubmission
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{batches: make(map[string]Batch), inputs: make(map[string][]AccountSubmission)}
}

func (s *MemoryStore) Create(batch Batch, submissions []AccountSubmission) error {
	if batch.ID == "" {
		return fmt.Errorf("batch_id_required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.batches[batch.ID]; exists {
		return fmt.Errorf("batch_exists")
	}
	now := time.Now().UTC()
	if batch.CreatedAt.IsZero() {
		batch.CreatedAt = now
	}
	batch.UpdatedAt = now
	s.batches[batch.ID] = cloneBatch(batch)
	s.inputs[batch.ID] = cloneSubmissions(submissions)
	return nil
}

func (s *MemoryStore) Get(id string) (Batch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	batch, ok := s.batches[id]
	if !ok {
		return Batch{}, fmt.Errorf("batch_not_found")
	}
	return cloneBatch(batch), nil
}

func (s *MemoryStore) List() []Batch {
	s.mu.RLock()
	defer s.mu.RUnlock()
	batches := make([]Batch, 0, len(s.batches))
	for _, batch := range s.batches {
		batches = append(batches, cloneBatch(batch))
	}
	return batches
}

func (s *MemoryStore) Submissions(id string) ([]AccountSubmission, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.batches[id]; !ok {
		return nil, fmt.Errorf("batch_not_found")
	}
	return cloneSubmissions(s.inputs[id]), nil
}

func (s *MemoryStore) Update(id string, update func(Batch) (Batch, error)) (Batch, error) {
	if update == nil {
		return Batch{}, fmt.Errorf("update_required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.batches[id]
	if !ok {
		return Batch{}, fmt.Errorf("batch_not_found")
	}
	next, err := update(cloneBatch(current))
	if err != nil {
		return Batch{}, err
	}
	next.ID = current.ID
	next.CreatedAt = current.CreatedAt
	next.UpdatedAt = time.Now().UTC()
	s.batches[id] = cloneBatch(next)
	return cloneBatch(next), nil
}

func cloneBatch(batch Batch) Batch {
	batch.Accounts = append([]BatchAccount(nil), batch.Accounts...)
	return batch
}

func cloneSubmissions(submissions []AccountSubmission) []AccountSubmission {
	return append([]AccountSubmission(nil), submissions...)
}

// --- v2 single-account job store ---

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
