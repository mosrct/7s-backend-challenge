// Package memory provides a thread-safe in-memory port.UserRepository. It is
// used by HTTP/gRPC adapter tests and demonstrates that the core does not
// depend on MongoDB.
package memory

import (
	"context"
	"fmt"
	"sync"

	"user-management/internal/domain"
	"user-management/internal/port"
)

// UserRepository stores users in a map.
type UserRepository struct {
	mu      sync.RWMutex
	seq     int
	order   []string
	byID    map[string]domain.User
	byEmail map[string]string
}

var _ port.UserRepository = (*UserRepository)(nil)

// NewUserRepository returns an empty repository.
func NewUserRepository() *UserRepository {
	return &UserRepository{byID: map[string]domain.User{}, byEmail: map[string]string{}}
}

func (r *UserRepository) Create(_ context.Context, u *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byEmail[u.Email]; ok {
		return domain.ErrEmailTaken
	}
	r.seq++
	u.ID = fmt.Sprintf("%024x", r.seq) // same shape as a Mongo ObjectID
	r.byID[u.ID] = *u
	r.byEmail[u.Email] = u.ID
	r.order = append(r.order, u.ID)
	return nil
}

func (r *UserRepository) GetByID(_ context.Context, id string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrUserNotFound
	}
	return &u, nil
}

func (r *UserRepository) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.byEmail[email]
	if !ok {
		return nil, domain.ErrUserNotFound
	}
	u := r.byID[id]
	return &u, nil
}

func (r *UserRepository) List(_ context.Context, offset, limit int64) ([]domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []domain.User{}
	for i := offset; i < int64(len(r.order)) && int64(len(out)) < limit; i++ {
		out = append(out, r.byID[r.order[i]])
	}
	return out, nil
}

func (r *UserRepository) Update(_ context.Context, id string, c port.UserChanges) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrUserNotFound
	}
	if c.Email != nil && *c.Email != u.Email {
		if _, taken := r.byEmail[*c.Email]; taken {
			return nil, domain.ErrEmailTaken
		}
		delete(r.byEmail, u.Email)
		u.Email = *c.Email
		r.byEmail[u.Email] = id
	}
	if c.Name != nil {
		u.Name = *c.Name
	}
	r.byID[id] = u
	return &u, nil
}

func (r *UserRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.byID[id]
	if !ok {
		return domain.ErrUserNotFound
	}
	delete(r.byID, id)
	delete(r.byEmail, u.Email)
	for i, oid := range r.order {
		if oid == id {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	return nil
}

func (r *UserRepository) Count(_ context.Context) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return int64(len(r.byID)), nil
}
