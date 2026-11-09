package httpapi_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"user-management/docs"
	"user-management/internal/adapter/httpapi"
	"user-management/internal/adapter/repository/memory"
	"user-management/internal/adapter/security"
	"user-management/internal/application"
)

func newSwaggerServer(t *testing.T, opts ...httpapi.Option) *httptest.Server {
	t.Helper()
	repo := memory.NewUserRepository()
	hasher := security.NewBcryptHasher(bcrypt.MinCost)
	tokens, _ := security.NewJWTManager("test-secret-test-secret-test-secret", "test", time.Hour)
	users := application.NewUserService(repo, hasher, nil)
	auth, err := application.NewAuthService(users, repo, hasher, tokens)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(httpapi.NewRouter(users, auth, slog.New(slog.NewTextHandler(io.Discard, nil)), opts...))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestSwaggerUI_Enabled(t *testing.T) {
	srv := newSwaggerServer(t, httpapi.WithSwaggerUI())

	if code, body := get(t, srv.URL+"/swagger/index.html"); code != http.StatusOK || !strings.Contains(body, "swagger-ui") {
		t.Errorf("index.html: status %d", code)
	}
	code, body := get(t, srv.URL+"/swagger/doc.json")
	if code != http.StatusOK || !strings.Contains(body, `"BearerAuth"`) {
		t.Errorf("doc.json: status %d, body %.200s", code, body)
	}
	// /swagger redirects to the UI (http.Get follows the redirect).
	if code, _ := get(t, srv.URL+"/swagger"); code != http.StatusOK {
		t.Errorf("/swagger redirect: status %d", code)
	}
}

func TestSwaggerUI_DisabledByDefault(t *testing.T) {
	srv := newSwaggerServer(t)
	if code, _ := get(t, srv.URL+"/swagger/index.html"); code != http.StatusNotFound {
		t.Errorf("status %d, want 404 when swagger is disabled", code)
	}
}

// TestSwaggerSpecCoversAllRoutes guards against the generated spec drifting
// from the router: every route must be documented. If this fails, add swag
// annotations to the handler and run `make swagger`.
func TestSwaggerSpecCoversAllRoutes(t *testing.T) {
	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal([]byte(docs.SwaggerInfo.ReadDoc()), &spec); err != nil {
		t.Fatal(err)
	}
	routes := map[string][]string{
		"/healthz":              {"get"},
		"/api/v1/auth/register": {"post"},
		"/api/v1/auth/login":    {"post"},
		"/api/v1/users":         {"get", "post"},
		"/api/v1/users/{id}":    {"get", "patch", "delete"},
	}
	for path, methods := range routes {
		for _, m := range methods {
			if _, ok := spec.Paths[path][m]; !ok {
				t.Errorf("spec is missing %s %s", strings.ToUpper(m), path)
			}
		}
	}
}
