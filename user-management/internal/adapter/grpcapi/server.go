// Package grpcapi is the gRPC driving adapter.
package grpcapi

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	userv1 "user-management/gen/user/v1"
	"user-management/internal/application"
	"user-management/internal/domain"
	"user-management/internal/port"
)

// Server implements userv1.UserServiceServer.
type Server struct {
	userv1.UnimplementedUserServiceServer
	users *application.UserService
}

// NewServer builds a *grpc.Server with logging and JWT auth interceptors,
// the UserService registered, and server reflection enabled (for grpcurl).
func NewServer(users *application.UserService, auth *application.AuthService, logger *slog.Logger) *grpc.Server {
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(
		LoggingInterceptor(logger),
		AuthInterceptor(auth),
	))
	userv1.RegisterUserServiceServer(srv, &Server{users: users})
	reflection.Register(srv)
	return srv
}

// CreateUser creates a user.
func (s *Server) CreateUser(ctx context.Context, req *userv1.CreateUserRequest) (*userv1.CreateUserResponse, error) {
	user, err := s.users.Create(ctx, application.CreateUserInput{
		Name:     req.GetName(),
		Email:    req.GetEmail(),
		Password: req.GetPassword(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &userv1.CreateUserResponse{User: toProto(user)}, nil
}

// GetUser fetches a user by ID.
func (s *Server) GetUser(ctx context.Context, req *userv1.GetUserRequest) (*userv1.GetUserResponse, error) {
	user, err := s.users.Get(ctx, req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	return &userv1.GetUserResponse{User: toProto(user)}, nil
}

func toProto(u *domain.User) *userv1.User {
	return &userv1.User{
		Id:        u.ID,
		Name:      u.Name,
		Email:     u.Email,
		CreatedAt: timestamppb.New(u.CreatedAt),
	}
}

// toStatus maps domain errors to gRPC status codes.
func toStatus(err error) error {
	var verr *domain.ValidationError
	switch {
	case errors.As(err, &verr):
		return status.Error(codes.InvalidArgument, verr.Error())
	case errors.Is(err, domain.ErrUserNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, domain.ErrEmailTaken):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, domain.ErrUnauthenticated), errors.Is(err, domain.ErrInvalidCredentials):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, domain.ErrForbidden):
		return status.Error(codes.PermissionDenied, err.Error())
	default:
		return status.Error(codes.Internal, "internal error")
	}
}

type claimsKey struct{}

// ClaimsFromContext returns claims stored by AuthInterceptor.
func ClaimsFromContext(ctx context.Context) (*port.Claims, bool) {
	c, ok := ctx.Value(claimsKey{}).(*port.Claims)
	return c, ok
}

// AuthInterceptor requires metadata "authorization: Bearer <jwt>" on every
// unary RPC.
func AuthInterceptor(auth *application.AuthService) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		var token string
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if vals := md.Get("authorization"); len(vals) > 0 {
				token = bearerToken(vals[0])
			}
		}
		claims, err := auth.Authenticate(token)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "missing or invalid bearer token")
		}
		return handler(context.WithValue(ctx, claimsKey{}, claims), req)
	}
}

// LoggingInterceptor logs method, status code and execution time.
func LoggingInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		logger.LogAttrs(ctx, slog.LevelInfo, "grpc request",
			slog.String("method", info.FullMethod),
			slog.String("code", status.Code(err).String()),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
		)
		return resp, err
	}
}

func bearerToken(v string) string {
	scheme, token, ok := strings.Cut(strings.TrimSpace(v), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}
