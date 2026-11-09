package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"user-management/internal/adapter/httpapi"
	"user-management/internal/adapter/repository/memory"
	"user-management/internal/adapter/security"
	"user-management/internal/application"
)

type testAPI struct {
	t      *testing.T
	server *httptest.Server
	logs   *bytes.Buffer
}

func newTestAPI(t *testing.T) *testAPI {
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
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	srv := httptest.NewServer(httpapi.NewRouter(users, auth, logger))
	t.Cleanup(srv.Close)
	return &testAPI{t: t, server: srv, logs: logs}
}

type response struct {
	Status int
	Header http.Header
	Body   map[string]any
	Raw    string
}

func (a *testAPI) do(method, path, body, token string) response {
	a.t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, a.server.URL+path, rdr)
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := response{Status: res.StatusCode, Header: res.Header, Raw: string(raw)}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out.Body)
	}
	return out
}

func (a *testAPI) mustStatus(r response, want int) response {
	a.t.Helper()
	if r.Status != want {
		a.t.Fatalf("status = %d, want %d; body: %s", r.Status, want, r.Raw)
	}
	return r
}

func (a *testAPI) registerAndLogin(name, email, password string) (id, token string) {
	a.t.Helper()
	reg := a.mustStatus(a.do("POST", "/api/v1/auth/register",
		`{"name":"`+name+`","email":"`+email+`","password":"`+password+`"}`, ""), http.StatusCreated)
	login := a.mustStatus(a.do("POST", "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+password+`"}`, ""), http.StatusOK)
	return reg.Body["id"].(string), login.Body["access_token"].(string)
}

func TestFullUserLifecycle(t *testing.T) {
	api := newTestAPI(t)

	// Register.
	reg := api.mustStatus(api.do("POST", "/api/v1/auth/register",
		`{"name":"Alice","email":"Alice@Example.com","password":"password123"}`, ""), http.StatusCreated)
	aliceID := reg.Body["id"].(string)
	if reg.Body["email"] != "alice@example.com" {
		t.Errorf("email not normalized: %v", reg.Body["email"])
	}
	if reg.Header.Get("Location") != "/api/v1/users/"+aliceID {
		t.Errorf("Location = %q", reg.Header.Get("Location"))
	}
	if strings.Contains(strings.ToLower(reg.Raw), "password") {
		t.Errorf("response leaks password data: %s", reg.Raw)
	}

	// Login.
	login := api.mustStatus(api.do("POST", "/api/v1/auth/login",
		`{"email":"alice@example.com","password":"password123"}`, ""), http.StatusOK)
	token := login.Body["access_token"].(string)
	if login.Body["token_type"] != "Bearer" || login.Body["expires_in"].(float64) <= 0 {
		t.Errorf("unexpected login body: %v", login.Body)
	}

	// Create another user via protected endpoint.
	bob := api.mustStatus(api.do("POST", "/api/v1/users",
		`{"name":"Bob","email":"bob@example.com","password":"password123"}`, token), http.StatusCreated)
	bobID := bob.Body["id"].(string)

	// Get.
	got := api.mustStatus(api.do("GET", "/api/v1/users/"+bobID, "", token), http.StatusOK)
	if got.Body["name"] != "Bob" {
		t.Errorf("get: %v", got.Body)
	}

	// List.
	list := api.mustStatus(api.do("GET", "/api/v1/users?limit=1", "", token), http.StatusOK)
	if list.Body["total"].(float64) != 2 || len(list.Body["users"].([]any)) != 1 || list.Body["limit"].(float64) != 1 {
		t.Errorf("list: %v", list.Body)
	}

	// Update own account.
	upd := api.mustStatus(api.do("PATCH", "/api/v1/users/"+aliceID,
		`{"name":"Alice Smith"}`, token), http.StatusOK)
	if upd.Body["name"] != "Alice Smith" || upd.Body["email"] != "alice@example.com" {
		t.Errorf("update: %v", upd.Body)
	}

	// Email conflict on update.
	api.mustStatus(api.do("PATCH", "/api/v1/users/"+aliceID, `{"email":"bob@example.com"}`, token), http.StatusConflict)

	// Cannot update or delete someone else.
	api.mustStatus(api.do("PATCH", "/api/v1/users/"+bobID, `{"name":"Hacked"}`, token), http.StatusForbidden)
	api.mustStatus(api.do("DELETE", "/api/v1/users/"+bobID, "", token), http.StatusForbidden)

	// Delete own account.
	api.mustStatus(api.do("DELETE", "/api/v1/users/"+aliceID, "", token), http.StatusNoContent)
	api.mustStatus(api.do("GET", "/api/v1/users/"+aliceID, "", token), http.StatusNotFound)
}

func TestProtectedRoutesRequireValidToken(t *testing.T) {
	api := newTestAPI(t)
	routes := []struct{ method, path string }{
		{"POST", "/api/v1/users"},
		{"GET", "/api/v1/users"},
		{"GET", "/api/v1/users/000000000000000000000001"},
		{"PATCH", "/api/v1/users/000000000000000000000001"},
		{"DELETE", "/api/v1/users/000000000000000000000001"},
	}
	for _, rt := range routes {
		for _, tok := range []string{"", "invalid.token.value"} {
			r := api.do(rt.method, rt.path, `{}`, tok)
			if r.Status != http.StatusUnauthorized {
				t.Errorf("%s %s token=%q: status %d, want 401", rt.method, rt.path, tok, r.Status)
			}
			if !strings.HasPrefix(r.Header.Get("WWW-Authenticate"), "Bearer") {
				t.Errorf("%s %s: missing WWW-Authenticate header", rt.method, rt.path)
			}
		}
	}
}

func TestRegisterValidationAndConflicts(t *testing.T) {
	api := newTestAPI(t)

	r := api.mustStatus(api.do("POST", "/api/v1/auth/register",
		`{"name":"","email":"bad","password":"short"}`, ""), http.StatusBadRequest)
	details := r.Body["details"].(map[string]any)
	for _, f := range []string{"name", "email", "password"} {
		if _, ok := details[f]; !ok {
			t.Errorf("missing detail for %s: %v", f, details)
		}
	}

	api.mustStatus(api.do("POST", "/api/v1/auth/register", ``, ""), http.StatusBadRequest)
	api.mustStatus(api.do("POST", "/api/v1/auth/register", `{not json`, ""), http.StatusBadRequest)
	api.mustStatus(api.do("POST", "/api/v1/auth/register",
		`{"name":"A","email":"a@b.io","password":"password1","role":"admin"}`, ""), http.StatusBadRequest)
	api.mustStatus(api.do("POST", "/api/v1/auth/register",
		`{"name":"A","email":"a@b.io","password":"password1"}{}`, ""), http.StatusBadRequest)

	api.mustStatus(api.do("POST", "/api/v1/auth/register",
		`{"name":"A","email":"dup@b.io","password":"password1"}`, ""), http.StatusCreated)
	api.mustStatus(api.do("POST", "/api/v1/auth/register",
		`{"name":"B","email":"DUP@b.io","password":"password1"}`, ""), http.StatusConflict)
}

func TestLoginFailures(t *testing.T) {
	api := newTestAPI(t)
	api.registerAndLogin("A", "a@b.io", "password1")

	wrong := api.mustStatus(api.do("POST", "/api/v1/auth/login", `{"email":"a@b.io","password":"nope-nope"}`, ""), http.StatusUnauthorized)
	unknown := api.mustStatus(api.do("POST", "/api/v1/auth/login", `{"email":"x@b.io","password":"nope-nope"}`, ""), http.StatusUnauthorized)
	if wrong.Raw != unknown.Raw {
		t.Errorf("responses differ, enabling user enumeration: %q vs %q", wrong.Raw, unknown.Raw)
	}
	api.mustStatus(api.do("POST", "/api/v1/auth/login", `{}`, ""), http.StatusBadRequest)
}

func TestUpdateRequestValidation(t *testing.T) {
	api := newTestAPI(t)
	id, tok := api.registerAndLogin("A", "a@b.io", "password1")

	api.mustStatus(api.do("PATCH", "/api/v1/users/"+id, `{}`, tok), http.StatusBadRequest)
	api.mustStatus(api.do("PATCH", "/api/v1/users/"+id, `{"email":"nope"}`, tok), http.StatusBadRequest)
	api.mustStatus(api.do("PATCH", "/api/v1/users/"+id, `{"password":"newpassword"}`, tok), http.StatusBadRequest)
}

func TestListPaginationValidation(t *testing.T) {
	api := newTestAPI(t)
	_, tok := api.registerAndLogin("A", "a@b.io", "password1")
	for _, q := range []string{"limit=abc", "limit=0", "limit=101", "offset=-1", "offset=x"} {
		api.mustStatus(api.do("GET", "/api/v1/users?"+q, "", tok), http.StatusBadRequest)
	}
	r := api.mustStatus(api.do("GET", "/api/v1/users?offset=10", "", tok), http.StatusOK)
	if users := r.Body["users"].([]any); len(users) != 0 {
		t.Errorf("expected empty page, got %v", users)
	}
}

func TestLoggingMiddlewareRecordsRequest(t *testing.T) {
	api := newTestAPI(t)
	api.do("GET", "/healthz", "", "")

	var entry map[string]any
	for _, line := range strings.Split(strings.TrimSpace(api.logs.String()), "\n") {
		_ = json.Unmarshal([]byte(line), &entry)
		if entry["msg"] == "http request" {
			break
		}
	}
	if entry["method"] != "GET" || entry["path"] != "/healthz" || entry["status"].(float64) != 200 {
		t.Errorf("unexpected log entry: %v", entry)
	}
	if _, ok := entry["duration_ms"].(float64); !ok {
		t.Errorf("duration_ms missing: %v", entry)
	}
}

func TestRecoverMiddleware(t *testing.T) {
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	h := httpapi.Logging(logger)(httpapi.Recover(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(logs.String(), `"status":500`) {
		t.Errorf("logging did not record 500: %s", logs.String())
	}
}

func TestBearerToken(t *testing.T) {
	tests := map[string]string{
		"Bearer abc":   "abc",
		"bearer abc":   "abc",
		"BEARER  abc ": "abc",
		"Basic abc":    "",
		"Bearer":       "",
		"":             "",
		"abc":          "",
	}
	for in, want := range tests {
		if got := httpapi.BearerToken(in); got != want {
			t.Errorf("BearerToken(%q) = %q, want %q", in, got, want)
		}
	}
}
