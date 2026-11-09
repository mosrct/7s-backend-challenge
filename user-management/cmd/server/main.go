// Command server runs the User Management REST and gRPC APIs.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"user-management/internal/adapter/grpcapi"
	"user-management/internal/adapter/httpapi"
	"user-management/internal/adapter/repository/mongorepo"
	"user-management/internal/adapter/security"
	"user-management/internal/adapter/worker"
	"user-management/internal/application"
	"user-management/internal/config"
)

// General API information for swag (https://github.com/swaggo/swag).
// Regenerate the spec with `make swagger` after changing any annotation.
//
//	@title						User Management API
//	@version					1.0
//	@description				REST API for managing users (MongoDB + JWT/HS256).
//	@description				To call protected endpoints: 1) POST /api/v1/auth/register, 2) POST /api/v1/auth/login, 3) click "Authorize" and enter `Bearer <access_token>`.
//	@BasePath					/
//	@schemes					http
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Type "Bearer" followed by a space and the access_token from /api/v1/auth/login. Example: Bearer eyJhbGciOi...
func main() {
	cfg, err := config.Load()
	logger := newLogger(cfg.LogLevel)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	if err := run(cfg, logger); err != nil {
		logger.Error("server exited with error", "error", err)
		os.Exit(1)
	}
	logger.Info("server stopped cleanly")
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}

// run is the composition root: it wires adapters to the application core,
// starts the servers and the background worker, and performs a graceful
// shutdown on SIGINT/SIGTERM.
func run(cfg config.Config, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Infrastructure -----------------------------------------------------
	// Validate the signing key before touching the network: fail fast.
	tokens, err := security.NewJWTManager(cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTTTL)
	if err != nil {
		return err
	}

	client, err := mongorepo.Connect(ctx, cfg.MongoURI)
	if err != nil {
		return err
	}
	defer func() {
		dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Disconnect(dctx); err != nil {
			logger.Error("mongo disconnect", "error", err)
		}
	}()

	repo := mongorepo.NewUserRepository(client.Database(cfg.MongoDatabase))
	if err := repo.EnsureIndexes(ctx); err != nil {
		return err
	}
	hasher := security.NewBcryptHasher(0)

	// --- Application core ---------------------------------------------------
	users := application.NewUserService(repo, hasher, nil)
	auth, err := application.NewAuthService(users, repo, hasher, tokens)
	if err != nil {
		return err
	}

	// --- Driving adapters ---------------------------------------------------
	var routerOpts []httpapi.Option
	if cfg.SwaggerEnabled {
		routerOpts = append(routerOpts, httpapi.WithSwaggerUI())
	}
	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewRouter(users, auth, logger, routerOpts...),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	grpcSrv := grpcapi.NewServer(users, auth, logger)
	grpcLis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("grpc listen: %w", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	wg.Go(func() {
		worker.NewUserCountReporter(users, cfg.UserCountInterval, logger).Run(ctx)
	})
	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr, "swagger_ui", cfg.SwaggerEnabled)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()
	go func() {
		logger.Info("grpc server listening", "addr", cfg.GRPCAddr)
		if err := grpcSrv.Serve(grpcLis); err != nil {
			errCh <- fmt.Errorf("grpc server: %w", err)
		}
	}()

	// --- Wait for a signal or a fatal server error ---------------------------
	var runErr error
	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case runErr = <-errCh:
		logger.Error("server failed, shutting down", "error", runErr)
	}
	stop() // cancels ctx -> stops the worker

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown", "error", err)
	}
	stopGRPC(shutdownCtx, grpcSrv)
	wg.Wait()
	return runErr
}

// stopGRPC drains in-flight RPCs, forcing a hard stop if ctx expires first.
func stopGRPC(ctx context.Context, srv *grpc.Server) {
	done := make(chan struct{})
	go func() {
		srv.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		srv.Stop()
	}
}
