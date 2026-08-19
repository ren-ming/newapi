package accountautomation

import (
	"fmt"
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
