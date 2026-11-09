package application_test

import (
	"context"
	"errors"
	"testing"

	"user-management/internal/application"
	"user-management/internal/domain"
)

func newAuthService(t *testing.T, repo *mockRepo, h *fakeHasher) *application.AuthService {
	t.Helper()
	users := application.NewUserService(repo, h, nil)
	auth, err := application.NewAuthService(users, repo, h, fakeTokens{})
	if err != nil {
		t.Fatal(err)
	}
	return auth
}

func TestAuthService_Login(t *testing.T) {
	alice := &domain.User{ID: "42", Email: "alice@example.com", PasswordHash: "hashed:correct-horse"}
	repo := &mockRepo{GetByEmailFn: func(_ context.Context, email string) (*domain.User, error) {
		if email == alice.Email {
			return alice, nil
		}
		return nil, domain.ErrUserNotFound
	}}

	t.Run("success issues token (email is normalized)", func(t *testing.T) {
		res, err := newAuthService(t, repo, &fakeHasher{}).Login(context.Background(), application.LoginInput{
			Email: " ALICE@example.com ", Password: "correct-horse",
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Token != "token-for-42" || res.User.ID != "42" || !res.ExpiresAt.Equal(fixedExpiry) {
			t.Errorf("unexpected result: %+v", res)
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		_, err := newAuthService(t, repo, &fakeHasher{}).Login(context.Background(), application.LoginInput{
			Email: alice.Email, Password: "wrong-password",
		})
		if !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("err = %v, want ErrInvalidCredentials", err)
		}
	})

	t.Run("unknown email returns the same error and still runs a hash compare", func(t *testing.T) {
		h := &fakeHasher{}
		_, err := newAuthService(t, repo, h).Login(context.Background(), application.LoginInput{
			Email: "nobody@example.com", Password: "whatever1",
		})
		if !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("err = %v, want ErrInvalidCredentials", err)
		}
		if h.compareCalls != 1 {
			t.Errorf("compareCalls = %d, want 1 (timing equalisation)", h.compareCalls)
		}
	})

	t.Run("missing fields", func(t *testing.T) {
		_, err := newAuthService(t, repo, &fakeHasher{}).Login(context.Background(), application.LoginInput{})
		var verr *domain.ValidationError
		if !errors.As(err, &verr) || len(verr.Fields) != 2 {
			t.Fatalf("err = %v, want ValidationError with 2 fields", err)
		}
	})

	t.Run("repository failure is not masked as bad credentials", func(t *testing.T) {
		boom := errors.New("mongo down")
		r := &mockRepo{GetByEmailFn: func(context.Context, string) (*domain.User, error) { return nil, boom }}
		_, err := newAuthService(t, r, &fakeHasher{}).Login(context.Background(), application.LoginInput{
			Email: "a@b.io", Password: "password1",
		})
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want %v", err, boom)
		}
	})
}

func TestAuthService_Register(t *testing.T) {
	repo := &mockRepo{CreateFn: func(_ context.Context, u *domain.User) error { u.ID = "new"; return nil }}
	u, err := newAuthService(t, repo, &fakeHasher{}).Register(context.Background(), application.CreateUserInput{
		Name: "Carol", Email: "carol@example.com", Password: "password1",
	})
	if err != nil || u.ID != "new" {
		t.Fatalf("Register = %v, %v", u, err)
	}
}

func TestAuthService_Authenticate(t *testing.T) {
	auth := newAuthService(t, &mockRepo{}, &fakeHasher{})
	if c, err := auth.Authenticate("token-for-7"); err != nil || c.UserID != "7" {
		t.Errorf("valid token: %v, %v", c, err)
	}
	for _, tok := range []string{"", "garbage"} {
		if _, err := auth.Authenticate(tok); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Errorf("Authenticate(%q) err = %v, want ErrUnauthenticated", tok, err)
		}
	}
}
