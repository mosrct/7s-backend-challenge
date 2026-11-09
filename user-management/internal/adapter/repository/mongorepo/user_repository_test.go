package mongorepo_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"user-management/internal/adapter/repository/mongorepo"
	"user-management/internal/domain"
	"user-management/internal/port"
)

// These are integration tests against a real MongoDB. They are skipped unless
// MONGO_TEST_URI is set, e.g.:
//
//	docker compose up -d mongo
//	MONGO_TEST_URI=mongodb://localhost:27017 go test ./internal/adapter/repository/mongorepo/...
//
// Unit tests elsewhere mock this adapter through the port.UserRepository
// interface; these tests verify the adapter itself honours that contract.
func newRepo(t *testing.T) *mongorepo.UserRepository {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("MONGO_TEST_URI not set; skipping MongoDB integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client, err := mongorepo.Connect(ctx, uri)
	if err != nil {
		t.Fatal(err)
	}
	db := client.Database(fmt.Sprintf("usermgmt_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})

	repo := mongorepo.NewUserRepository(db)
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestMongoRepository_Contract(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	alice := &domain.User{Name: "Alice", Email: "alice@example.com", PasswordHash: "h1", CreatedAt: now}
	if err := repo.Create(ctx, alice); err != nil {
		t.Fatal(err)
	}
	if len(alice.ID) != 24 {
		t.Fatalf("ID not assigned as ObjectID hex: %q", alice.ID)
	}

	dup := &domain.User{Name: "Other", Email: "alice@example.com", PasswordHash: "h", CreatedAt: now}
	if err := repo.Create(ctx, dup); !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatalf("duplicate email: err = %v, want ErrEmailTaken", err)
	}

	got, err := repo.GetByID(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != alice.Email || got.PasswordHash != "h1" || !got.CreatedAt.Equal(now) {
		t.Errorf("GetByID = %+v", got)
	}
	if _, err := repo.GetByEmail(ctx, "alice@example.com"); err != nil {
		t.Errorf("GetByEmail: %v", err)
	}
	for _, id := range []string{"not-hex", "000000000000000000000000"} {
		if _, err := repo.GetByID(ctx, id); !errors.Is(err, domain.ErrUserNotFound) {
			t.Errorf("GetByID(%q) err = %v, want ErrUserNotFound", id, err)
		}
	}

	bob := &domain.User{Name: "Bob", Email: "bob@example.com", PasswordHash: "h2", CreatedAt: now}
	if err := repo.Create(ctx, bob); err != nil {
		t.Fatal(err)
	}

	users, err := repo.List(ctx, 0, 10)
	if err != nil || len(users) != 2 || users[0].ID != alice.ID {
		t.Fatalf("List = %v, %v", users, err)
	}
	if page, _ := repo.List(ctx, 1, 10); len(page) != 1 || page[0].ID != bob.ID {
		t.Errorf("List offset=1 = %v", page)
	}
	if n, err := repo.Count(ctx); err != nil || n != 2 {
		t.Errorf("Count = %d, %v", n, err)
	}

	newName := "Alice Smith"
	updated, err := repo.Update(ctx, alice.ID, port.UserChanges{Name: &newName})
	if err != nil || updated.Name != newName || updated.Email != alice.Email {
		t.Fatalf("Update = %+v, %v", updated, err)
	}
	taken := "bob@example.com"
	if _, err := repo.Update(ctx, alice.ID, port.UserChanges{Email: &taken}); !errors.Is(err, domain.ErrEmailTaken) {
		t.Errorf("Update to taken email: err = %v", err)
	}
	if _, err := repo.Update(ctx, "000000000000000000000000", port.UserChanges{Name: &newName}); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("Update missing: err = %v", err)
	}

	if err := repo.Delete(ctx, alice.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, alice.ID); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("second Delete err = %v", err)
	}
}
