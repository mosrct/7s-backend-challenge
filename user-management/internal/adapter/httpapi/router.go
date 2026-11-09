// Package httpapi is the REST (JSON over HTTP) driving adapter.
package httpapi

import (
	"log/slog"
	"net/http"

	httpSwagger "github.com/swaggo/http-swagger/v2"

	_ "user-management/docs" // registers the generated OpenAPI spec with swag

	"user-management/internal/application"
)

type routerOptions struct {
	swaggerUI bool
}

// Option configures NewRouter.
type Option func(*routerOptions)

// WithSwaggerUI serves the interactive API documentation at /swagger/.
func WithSwaggerUI() Option {
	return func(o *routerOptions) { o.swaggerUI = true }
}

// NewRouter wires all routes and middleware and returns the root handler.
//
//	Public:
//	  GET    /healthz
//	  POST   /api/v1/auth/register
//	  POST   /api/v1/auth/login
//	  GET    /swagger/              (only WithSwaggerUI)
//	Protected (Authorization: Bearer <jwt>):
//	  POST   /api/v1/users
//	  GET    /api/v1/users
//	  GET    /api/v1/users/{id}
//	  PATCH  /api/v1/users/{id}
//	  DELETE /api/v1/users/{id}
func NewRouter(users *application.UserService, auth *application.AuthService, logger *slog.Logger, opts ...Option) http.Handler {
	var o routerOptions
	for _, opt := range opts {
		opt(&o)
	}

	h := &handler{users: users, auth: auth, logger: logger}
	requireAuth := RequireAuth(auth)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)

	mux.HandleFunc("POST /api/v1/auth/register", h.register)
	mux.HandleFunc("POST /api/v1/auth/login", h.login)

	mux.Handle("POST /api/v1/users", requireAuth(http.HandlerFunc(h.createUser)))
	mux.Handle("GET /api/v1/users", requireAuth(http.HandlerFunc(h.listUsers)))
	mux.Handle("GET /api/v1/users/{id}", requireAuth(http.HandlerFunc(h.getUser)))
	mux.Handle("PATCH /api/v1/users/{id}", requireAuth(http.HandlerFunc(h.updateUser)))
	mux.Handle("DELETE /api/v1/users/{id}", requireAuth(http.HandlerFunc(h.deleteUser)))

	if o.swaggerUI {
		mux.Handle("GET /swagger/", httpSwagger.Handler(
			httpSwagger.URL("/swagger/doc.json"),
			httpSwagger.PersistAuthorization(true), // keep the token across page reloads
		))
		mux.Handle("GET /swagger", http.RedirectHandler("/swagger/index.html", http.StatusMovedPermanently))
	}

	// Outermost first: logging sees the final status, including recovered panics.
	return Logging(logger)(Recover(logger)(mux))
}
