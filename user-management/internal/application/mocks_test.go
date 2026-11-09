package application_test

import (
	"context"
	"errors"
	"strings"
	"time"

	"user-management/internal/domain"
	"user-management/internal/port"
)

// mockRepo is a hand-written mock of port.UserRepository (i.e. of MongoDB).
// Each test sets only the function fields it expects to be called; any
// unexpected call panics, which fails the test loudly.
type mockRepo struct {
	CreateFn     func(ctx context.Context, u *domain.User) error
	GetByIDFn    func(ctx context.Context, id string) (*domain.User, error)
	GetByEmailFn func(ctx context.Context, email string) (*domain.User, error)
	ListFn       func(ctx context.Context, offset, limit int64) ([]domain.User, error)
	UpdateFn     func(ctx context.Context, id string, c port.UserChanges) (*domain.User, error)
	DeleteFn     func(ctx context.Context, id string) error
	CountFn      func(ctx context.Context) (int64, error)

	calls []string
}

var _ port.UserRepository = (*mockRepo)(nil)

func (m *mockRepo) record(name string) { m.calls = append(m.calls, name) }

func (m *mockRepo) Create(ctx context.Context, u *domain.User) error {
	m.record("Create")
	if m.CreateFn == nil {
		panic("unexpected call to Create")
	}
	return m.CreateFn(ctx, u)
}

func (m *mockRepo) GetByID(ctx context.Context, id string) (*domain.User, error) {
	m.record("GetByID")
	if m.GetByIDFn == nil {
		panic("unexpected call to GetByID")
	}
	return m.GetByIDFn(ctx, id)
}

func (m *mockRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	m.record("GetByEmail")
	if m.GetByEmailFn == nil {
		panic("unexpected call to GetByEmail")
	}
	return m.GetByEmailFn(ctx, email)
}

func (m *mockRepo) List(ctx context.Context, offset, limit int64) ([]domain.User, error) {
	m.record("List")
	if m.ListFn == nil {
		panic("unexpected call to List")
	}
	return m.ListFn(ctx, offset, limit)
}

func (m *mockRepo) Update(ctx context.Context, id string, c port.UserChanges) (*domain.User, error) {
	m.record("Update")
	if m.UpdateFn == nil {
		panic("unexpected call to Update")
	}
	return m.UpdateFn(ctx, id, c)
}

func (m *mockRepo) Delete(ctx context.Context, id string) error {
	m.record("Delete")
	if m.DeleteFn == nil {
		panic("unexpected call to Delete")
	}
	return m.DeleteFn(ctx, id)
}

func (m *mockRepo) Count(ctx context.Context) (int64, error) {
	m.record("Count")
	if m.CountFn == nil {
		panic("unexpected call to Count")
	}
	return m.CountFn(ctx)
}

// fakeHasher "hashes" by prefixing, which keeps tests fast and deterministic.
type fakeHasher struct {
	compareCalls int
}

func (h *fakeHasher) Hash(p string) (string, error) { return "hashed:" + p, nil }

func (h *fakeHasher) Compare(hash, p string) error {
	h.compareCalls++
	if strings.TrimPrefix(hash, "hashed:") == p && strings.HasPrefix(hash, "hashed:") {
		return nil
	}
	return errors.New("mismatch")
}

// fakeTokens issues "token-for-<id>".
type fakeTokens struct{}

var fixedExpiry = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

func (fakeTokens) Issue(u *domain.User) (string, time.Time, error) {
	return "token-for-" + u.ID, fixedExpiry, nil
}

func (fakeTokens) Verify(tok string) (*port.Claims, error) {
	id, ok := strings.CutPrefix(tok, "token-for-")
	if !ok {
		return nil, errors.New("bad token")
	}
	return &port.Claims{UserID: id, ExpiresAt: fixedExpiry}, nil
}
