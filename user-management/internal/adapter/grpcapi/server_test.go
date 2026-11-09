package grpcapi_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	userv1 "user-management/gen/user/v1"
	"user-management/internal/adapter/grpcapi"
	"user-management/internal/adapter/repository/memory"
	"user-management/internal/adapter/security"
	"user-management/internal/application"
	"user-management/internal/domain"
)

// setup starts an in-process gRPC server over bufconn and returns a client and
// a valid bearer token.
func setup(t *testing.T) (userv1.UserServiceClient, string) {
	t.Helper()
	repo := memory.NewUserRepository()
	hasher := security.NewBcryptHasher(bcrypt.MinCost)
	tokens, err := security.NewJWTManager("test-secret-test-secret-test-secret", "test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	users := application.NewUserService(repo, hasher, nil)
	auth, err := application.NewAuthService(users, repo, hasher, tokens)
	if err != nil {
		t.Fatal(err)
	}

	lis := bufconn.Listen(1 << 20)
	srv := grpcapi.NewServer(users, auth, slog.New(slog.NewTextHandler(io.Discard, nil)))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	tok, _, err := tokens.Issue(&domain.User{ID: "caller", Email: "caller@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	return userv1.NewUserServiceClient(conn), tok
}

func withToken(tok string) context.Context {
	return metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+tok)
}

func TestGRPC_CreateAndGet(t *testing.T) {
	client, tok := setup(t)
	ctx := withToken(tok)

	created, err := client.CreateUser(ctx, &userv1.CreateUserRequest{
		Name: "Dana", Email: "Dana@Example.com", Password: "password123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.GetUser().GetEmail() != "dana@example.com" || created.GetUser().GetCreatedAt() == nil {
		t.Errorf("unexpected user: %v", created.GetUser())
	}

	got, err := client.GetUser(ctx, &userv1.GetUserRequest{Id: created.GetUser().GetId()})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetUser().GetName() != "Dana" {
		t.Errorf("GetUser = %v", got.GetUser())
	}
}

func TestGRPC_ErrorCodes(t *testing.T) {
	client, tok := setup(t)
	ctx := withToken(tok)

	_, err := client.GetUser(ctx, &userv1.GetUserRequest{Id: "missing"})
	assertCode(t, err, codes.NotFound)

	_, err = client.CreateUser(ctx, &userv1.CreateUserRequest{Name: "", Email: "bad", Password: "x"})
	assertCode(t, err, codes.InvalidArgument)

	req := &userv1.CreateUserRequest{Name: "E", Email: "e@example.com", Password: "password123"}
	_, err = client.CreateUser(ctx, req)
	assertCode(t, err, codes.OK)
	_, err = client.CreateUser(ctx, req)
	assertCode(t, err, codes.AlreadyExists)
}

func TestGRPC_RequiresToken(t *testing.T) {
	client, _ := setup(t)

	_, err := client.GetUser(context.Background(), &userv1.GetUserRequest{Id: "x"})
	assertCode(t, err, codes.Unauthenticated)

	_, err = client.GetUser(withToken("forged.token.value"), &userv1.GetUserRequest{Id: "x"})
	assertCode(t, err, codes.Unauthenticated)
}

func assertCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if got := status.Code(err); got != want {
		t.Errorf("code = %v, want %v (err: %v)", got, want, err)
	}
}
