package application_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"user-management/internal/application"
	"user-management/internal/domain"
	"user-management/internal/port"
)

var fixedNow = time.Date(2026, 1, 2, 3, 4, 5, 678_901_234, time.UTC)

func newUserService(repo port.UserRepository) *application.UserService {
	return application.NewUserService(repo, &fakeHasher{}, func() time.Time { return fixedNow })
}

func ptr[T any](v T) *T { return &v }

func TestUserService_Create(t *testing.T) {
	t.Run("normalizes, hashes and persists", func(t *testing.T) {
		var stored *domain.User
		repo := &mockRepo{CreateFn: func(_ context.Context, u *domain.User) error {
			stored = u
			u.ID = "abc"
			return nil
		}}
		u, err := newUserService(repo).Create(context.Background(), application.CreateUserInput{
			Name: "  Alice ", Email: " Alice@Example.com", Password: "s3cret-pass",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if u.ID != "abc" || u.Name != "Alice" || u.Email != "alice@example.com" {
			t.Errorf("unexpected user: %+v", u)
		}
		if stored.PasswordHash != "hashed:s3cret-pass" {
			t.Errorf("password not hashed: %q", stored.PasswordHash)
		}
		if want := fixedNow.Truncate(time.Millisecond); !u.CreatedAt.Equal(want) {
			t.Errorf("CreatedAt = %v, want %v", u.CreatedAt, want)
		}
	})

	t.Run("validation errors do not touch the repository", func(t *testing.T) {
		repo := &mockRepo{}
		_, err := newUserService(repo).Create(context.Background(), application.CreateUserInput{
			Name: "", Email: "not-an-email", Password: "short",
		})
		var verr *domain.ValidationError
		if !errors.As(err, &verr) {
			t.Fatalf("want ValidationError, got %v", err)
		}
		for _, f := range []string{"name", "email", "password"} {
			if _, ok := verr.Fields[f]; !ok {
				t.Errorf("missing field error for %q", f)
			}
		}
		if len(repo.calls) != 0 {
			t.Errorf("repository must not be called, got %v", repo.calls)
		}
	})

	t.Run("duplicate email propagates", func(t *testing.T) {
		repo := &mockRepo{CreateFn: func(context.Context, *domain.User) error { return domain.ErrEmailTaken }}
		_, err := newUserService(repo).Create(context.Background(), application.CreateUserInput{
			Name: "Bob", Email: "bob@example.com", Password: "password1",
		})
		if !errors.Is(err, domain.ErrEmailTaken) {
			t.Fatalf("want ErrEmailTaken, got %v", err)
		}
	})
}

func TestUserService_Get(t *testing.T) {
	repo := &mockRepo{GetByIDFn: func(_ context.Context, id string) (*domain.User, error) {
		if id == "1" {
			return &domain.User{ID: "1", Name: "A"}, nil
		}
		return nil, domain.ErrUserNotFound
	}}
	svc := newUserService(repo)
	if u, err := svc.Get(context.Background(), "1"); err != nil || u.ID != "1" {
		t.Errorf("Get(1) = %v, %v", u, err)
	}
	if _, err := svc.Get(context.Background(), "2"); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("Get(2) err = %v, want ErrUserNotFound", err)
	}
}

func TestUserService_List(t *testing.T) {
	t.Run("applies default limit and returns total", func(t *testing.T) {
		var gotOffset, gotLimit int64
		repo := &mockRepo{
			ListFn: func(_ context.Context, off, lim int64) ([]domain.User, error) {
				gotOffset, gotLimit = off, lim
				return []domain.User{{ID: "1"}, {ID: "2"}}, nil
			},
			CountFn: func(context.Context) (int64, error) { return 7, nil },
		}
		page, err := newUserService(repo).List(context.Background(), application.ListUsersInput{Offset: 5})
		if err != nil {
			t.Fatal(err)
		}
		if gotOffset != 5 || gotLimit != application.DefaultListLimit {
			t.Errorf("repo called with offset=%d limit=%d", gotOffset, gotLimit)
		}
		if len(page.Users) != 2 || page.Total != 7 || page.Limit != application.DefaultListLimit {
			t.Errorf("unexpected page: %+v", page)
		}
	})

	t.Run("rejects out-of-range pagination", func(t *testing.T) {
		for _, in := range []application.ListUsersInput{
			{Offset: -1},
			{Limit: -1},
			{Limit: application.MaxListLimit + 1},
		} {
			_, err := newUserService(&mockRepo{}).List(context.Background(), in)
			var verr *domain.ValidationError
			if !errors.As(err, &verr) {
				t.Errorf("List(%+v) err = %v, want ValidationError", in, err)
			}
		}
	})

	t.Run("repository error propagates", func(t *testing.T) {
		boom := errors.New("connection reset")
		repo := &mockRepo{ListFn: func(context.Context, int64, int64) ([]domain.User, error) { return nil, boom }}
		if _, err := newUserService(repo).List(context.Background(), application.ListUsersInput{}); !errors.Is(err, boom) {
			t.Errorf("err = %v, want %v", err, boom)
		}
	})
}

func TestUserService_Update(t *testing.T) {
	t.Run("updates own account with normalized values", func(t *testing.T) {
		var got port.UserChanges
		repo := &mockRepo{UpdateFn: func(_ context.Context, id string, c port.UserChanges) (*domain.User, error) {
			got = c
			return &domain.User{ID: id, Name: *c.Name, Email: *c.Email}, nil
		}}
		u, err := newUserService(repo).Update(context.Background(), "1", "1", application.UpdateUserInput{
			Name: ptr(" New Name "), Email: ptr("NEW@example.com"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if *got.Name != "New Name" || *got.Email != "new@example.com" || u.Email != "new@example.com" {
			t.Errorf("changes not normalized: name=%q email=%q", *got.Name, *got.Email)
		}
	})

	t.Run("only name leaves email nil", func(t *testing.T) {
		repo := &mockRepo{UpdateFn: func(_ context.Context, id string, c port.UserChanges) (*domain.User, error) {
			if c.Email != nil {
				t.Error("email should be nil")
			}
			return &domain.User{ID: id}, nil
		}}
		if _, err := newUserService(repo).Update(context.Background(), "1", "1", application.UpdateUserInput{Name: ptr("X")}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("forbidden for another user", func(t *testing.T) {
		repo := &mockRepo{}
		_, err := newUserService(repo).Update(context.Background(), "1", "2", application.UpdateUserInput{Name: ptr("X")})
		if !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("err = %v, want ErrForbidden", err)
		}
		if len(repo.calls) != 0 {
			t.Errorf("repository must not be called, got %v", repo.calls)
		}
	})

	t.Run("empty patch and invalid fields are rejected", func(t *testing.T) {
		for _, in := range []application.UpdateUserInput{
			{},
			{Name: ptr("   ")},
			{Email: ptr("nope")},
		} {
			_, err := newUserService(&mockRepo{}).Update(context.Background(), "1", "1", in)
			var verr *domain.ValidationError
			if !errors.As(err, &verr) {
				t.Errorf("Update(%+v) err = %v, want ValidationError", in, err)
			}
		}
	})

	t.Run("email conflict propagates", func(t *testing.T) {
		repo := &mockRepo{UpdateFn: func(context.Context, string, port.UserChanges) (*domain.User, error) {
			return nil, domain.ErrEmailTaken
		}}
		_, err := newUserService(repo).Update(context.Background(), "1", "1", application.UpdateUserInput{Email: ptr("x@y.io")})
		if !errors.Is(err, domain.ErrEmailTaken) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestUserService_Delete(t *testing.T) {
	repo := &mockRepo{DeleteFn: func(_ context.Context, id string) error {
		if id != "1" {
			return domain.ErrUserNotFound
		}
		return nil
	}}
	svc := newUserService(repo)
	if err := svc.Delete(context.Background(), "1", "1"); err != nil {
		t.Errorf("delete own: %v", err)
	}
	if err := svc.Delete(context.Background(), "1", "2"); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("delete other: %v, want ErrForbidden", err)
	}
	if !slices.Equal(repo.calls, []string{"Delete"}) {
		t.Errorf("calls = %v", repo.calls)
	}
}
