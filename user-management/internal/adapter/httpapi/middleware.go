package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"user-management/internal/application"
	"user-management/internal/domain"
	"user-management/internal/port"
)

// statusRecorder captures the status code and response size.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Logging logs HTTP method, path, status and execution time for every request.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			elapsed := time.Since(start)
			logger.LogAttrs(r.Context(), slog.LevelInfo, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Float64("duration_ms", float64(elapsed.Microseconds())/1000),
			)
		})
	}
}

// Recover converts panics into 500 responses instead of dropping the connection.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler {
						panic(v)
					}
					logger.Error("panic recovered", "panic", v, "path", r.URL.Path)
					writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

type claimsKey struct{}

// ContextWithClaims stores verified claims in ctx.
func ContextWithClaims(ctx context.Context, c *port.Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, c)
}

// ClaimsFromContext returns the claims stored by RequireAuth, if any.
func ClaimsFromContext(ctx context.Context) (*port.Claims, bool) {
	c, ok := ctx.Value(claimsKey{}).(*port.Claims)
	return c, ok
}

// RequireAuth validates the "Authorization: Bearer <token>" header and stores
// the verified claims in the request context. Requests without a valid token
// receive 401.
func RequireAuth(auth *application.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := BearerToken(r.Header.Get("Authorization"))
			claims, err := auth.Authenticate(token)
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Bearer realm="user-management"`)
				writeError(w, nil, domain.ErrUnauthenticated)
				return
			}
			next.ServeHTTP(w, r.WithContext(ContextWithClaims(r.Context(), claims)))
		})
	}
}

// BearerToken extracts the token from an Authorization header value. The
// scheme is matched case-insensitively per RFC 7235.
func BearerToken(header string) string {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}
