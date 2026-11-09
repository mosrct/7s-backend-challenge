# Implementation Notes

This document covers **what** was built, **why** each choice was made, and the **trade-offs** accepted. For setup and usage, see [README.md](README.md).

---

## 1. Summary

| Requirement | Status | Implementation |
|---|---|---|
| User model (ID, Name, Email unique, hashed Password, CreatedAt) | ✅ | `domain.User` + Mongo `userDocument` with a unique index on `email` |
| Registration, and login that returns a JWT | ✅ | `AuthService.Register` / `Login`; `POST /auth/register`, `POST /auth/login` |
| JWT-protected endpoints, middleware validation, HS256 | ✅ | `httpapi.RequireAuth`, `security.JWTManager` (HS256 only) |
| Create, get, list, update (name/email), delete | ✅ | `UserService` + REST handlers |
| Official MongoDB Go driver | ✅ | `go.mongodb.org/mongo-driver/v2` |
| Logging middleware (method, path, execution time) | ✅ | `httpapi.Logging` (also status and bytes); gRPC `LoggingInterceptor` |
| Goroutine logging the user count every 10 s | ✅ | `worker.UserCountReporter` |
| Unit tests with `testing`, MongoDB mocked | ✅ | Hand-written repository mock, in-memory adapter, `httptest`, `bufconn` |
| Bonus: Docker and docker-compose | ✅ | Multi-stage distroless image, Mongo healthcheck |
| Bonus: interfaces abstracting MongoDB | ✅ | `port.UserRepository` |
| Bonus: input validation | ✅ | Domain validation rules + strict JSON decoding |
| Bonus: graceful shutdown with `context.Context` | ✅ | `signal.NotifyContext` → ordered shutdown |
| Bonus: gRPC `CreateUser` / `GetUser` with token metadata | ✅ | `grpcapi` + auth interceptor |
| Bonus: hexagonal architecture | ✅ | `domain` / `port` / `application` / `adapter` |
| Extra: Swagger UI / OpenAPI spec | ✅ | swaggo annotations → `docs/`, served at `/swagger/` |

I checked everything end to end, not just with unit tests. I ran the stack with `docker compose`, drove every REST endpoint with curl, called both gRPC methods with grpcurl, watched the 10-second user-count log, and confirmed that `docker compose stop` (SIGTERM) produces a clean shutdown.

---

## 2. Architecture

### 2.1 Hexagonal layout (ports and adapters)

```
                 ┌──────────────── driving adapters ────────────────┐
                 │  httpapi (REST)   grpcapi (gRPC)   worker (ticker) │
                 └───────────────┬──────────────────────────────────┘
                                 │ calls
                 ┌───────────────▼──────────────────┐
                 │  application  (use cases)        │
                 │  UserService, AuthService        │
                 │        uses ▼          ▼ uses    │
                 │  domain (entity,   port (interfaces)
                 │  rules, errors)    UserRepository, PasswordHasher, TokenManager
                 └──────────────────────────▲───────┘
                                            │ implements
                 ┌──────────────────────────┴───────────────────────┐
                 │  driven adapters: mongorepo, memory, security     │
                 └───────────────────────────────────────────────────┘
                     cmd/server/main.go = composition root (wires it all)
```

**Dependency rule.** Dependencies point inward only:

- `domain` imports only the standard library.
- `application` imports `domain` and `port`. It never imports Mongo, `net/http`, gRPC or JWT libraries.
- Adapters import the core. The core never imports adapters.
- Only `cmd/server/main.go` knows about every concrete type.

**Why:**

- **Business rules are framework-free.** Validation, "only you can edit yourself", and timing-safe login all live in plain Go and are tested without a database or HTTP.
- **REST and gRPC share one implementation.** Both call the same `UserService`, so their behaviour cannot drift. Adding gRPC took about 150 lines.
- **Persistence is swappable.** Two `UserRepository` implementations exist (Mongo and in-memory), which shows the abstraction is real rather than nominal.

**Trade-off:** more packages and some mapping code (domain ↔ document ↔ DTO ↔ protobuf) than a small CRUD service strictly needs. A flat `handlers/models/db` layout would be shorter. I accepted the extra structure because the brief explicitly evaluates architecture and testability, and the mapping layers give real safety: for example, the password hash physically cannot appear in an HTTP response, because the response DTO has no such field.

### 2.2 Port granularity

The ports are small, behaviour-focused interfaces defined by the core (`UserRepository`, `PasswordHasher`, `TokenManager`), not by the infrastructure. Go favours small consumer-defined interfaces, and the worker goes further: it depends on a one-method `UserCounter` interface that it defines itself.

Inbound ports (interfaces in front of `UserService`) were **not** added. The HTTP and gRPC adapters depend on the concrete services. An interface with a single implementation adds indirection without value here. Adapter tests instead run against the real services backed by the in-memory repository, which also tests the integration between layers.

### 2.3 Request flow (example: `PATCH /api/v1/users/{id}`)

```
Logging → Recover → ServeMux → RequireAuth (verify JWT, put claims in ctx)
  → handler.updateUser (strict JSON decode → UpdateUserInput)
    → UserService.Update(actorID=claims.sub, id, input)
        - authorization: actorID must equal id   → ErrForbidden
        - validate + normalize provided fields   → *ValidationError
        → UserRepository.Update (Mongo FindOneAndUpdate, $set)
            - duplicate key                       → ErrEmailTaken
            - no document                         → ErrUserNotFound
  ← writeError maps domain errors → 403/400/409/404, otherwise 200 + DTO
```

---

## 3. Key decisions and trade-offs

### 3.1 HTTP routing: standard library `net/http`

Since Go 1.22, `http.ServeMux` supports method and path-parameter patterns (`"PATCH /api/v1/users/{id}"`), which covers everything this API needs.

- **Why:** zero dependencies, idiomatic, nothing to learn, and middleware is plain `func(http.Handler) http.Handler`.
- **Trade-off:** no built-in route groups, binding or validation tags as in Gin or Echo. Unmatched routes and methods get Go's default plain-text 404/405 rather than the JSON error envelope. Both are acceptable for this scope.

### 3.2 MongoDB integration

- **Driver v2** (`go.mongodb.org/mongo-driver/v2`) is the current official major version.
- **Separate persistence model.** `userDocument` carries the BSON tags and `bson.ObjectID`. `domain.User` uses a plain `string` ID. Storage concerns never leak into the core.
- **IDs** are generated client-side with `bson.NewObjectID()`, so `Create` doesn't depend on the type of `InsertedID`. ObjectIDs are time-ordered, so sorting by `_id` gives creation order for free.
- **Malformed IDs return 404**, not 400. Callers shouldn't need to know the ID format. "This ID doesn't exist" is true for both cases and doesn't leak the storage engine. *Trade-off:* a client with a typo gets 404 rather than a more specific "bad ID format" error.
- **Email uniqueness** is enforced by a **unique index** (`uniq_email`, created idempotently at startup). Duplicate-key errors map to `ErrEmailTaken` → 409. *Why not check first, then insert?* That has a race: two concurrent registrations could both pass the check. The index is atomic. *Trade-off:* a startup dependency on index creation, and an error-translation step in the adapter.
- **Updates** use `FindOneAndUpdate` with `$set` and `ReturnDocument(After)`: one atomic round trip that returns the updated document.
- **Timestamps:** `CreatedAt` is truncated to milliseconds before saving, because BSON dates have millisecond precision. Without this, the value returned on create would differ from the value read back later.
- **Count** uses `CountDocuments` (exact) rather than `EstimatedDocumentCount` (metadata-based, faster). At this scale, exactness wins. On a very large collection with a frequent ticker, I would switch to the estimate.

### 3.3 Password handling

- **bcrypt** at `DefaultCost` (10) via `golang.org/x/crypto/bcrypt`. It is salted and deliberately slow. Tests use `MinCost` for speed.
- **Max 72 bytes:** bcrypt silently ignores bytes beyond 72. Rejecting longer passwords is more honest than truncating them quietly.
- The hash is never serialized. The HTTP and gRPC response types have no password field, and a test asserts that the word "password" never appears in a register response.
- *Trade-off:* argon2id is the newer recommendation. I chose bcrypt because it is ubiquitous, in the extended standard library, and well understood. The `PasswordHasher` port makes a later swap a one-file change.

### 3.4 JWT design

| Aspect | Choice | Reason |
|---|---|---|
| Library | `github.com/golang-jwt/jwt/v5` v5 has strict validation options |
| Algorithm | HS256 only, enforced with `WithValidMethods` | Required by the brief. Pinning the algorithm blocks `alg:none` and algorithm-confusion attacks; tests cover both. |
| Secret | Required, at least 32 bytes, **no default** | RFC 7518 requires an HMAC key at least as long as the hash output. A default secret in code or compose is a classic production leak, so the server refuses to start without one. |
| Claims | `sub` (user ID), `email`, `iss`, `iat`, `nbf`, `exp` | `sub` drives authorization. `iss` is checked so tokens from another service sharing the secret are rejected. |
| Expiry | `exp` required; default TTL 1 h | Limits the damage window if a token leaks |
| Transport | `Authorization: Bearer` header (REST), `authorization` metadata (gRPC) | Standard. Not cookies, so there is no CSRF surface. |

**Main trade-off: stateless tokens, no revocation.** The middleware verifies tokens cryptographically and **does not hit the database**. That makes it fast, with no DB load per request, and it scales horizontally. The costs:

- A token stays valid until `exp` even if the user is deleted, changes their email, or logs out. (During testing, a deleted user's token still verified.)
- There is no logout or refresh-token flow.

The mitigation is the short TTL. If revocation became a requirement, options include a `token_version` field on the user that is checked against a claim (one DB read per request), a deny-list keyed by `jti` in Redis, or short-lived access tokens plus revocable refresh tokens. I left these out as beyond the brief.

### 3.5 Login hardening

- **No user enumeration.** An unknown email and a wrong password both return exactly `401 {"error":"invalid email or password"}`. A test asserts the two bodies are byte-identical.
- **Timing equalisation.** When the email doesn't exist, the service still runs a bcrypt comparison against a dummy hash computed at startup. Otherwise "unknown email" would return in about 1 ms and "wrong password" in about 50 ms, which leaks account existence. A test asserts the comparison runs.
- **Infrastructure errors are not disguised.** If Mongo is down, login returns 500, not 401, so operators aren't misled.
- *Not implemented:* rate limiting and account lockout. These belong in a gateway or middleware with shared state, and are out of scope.

### 3.6 Authorization model

The brief specifies authentication but not roles. I chose:

| Operation | Who |
|---|---|
| Register, log in | Anyone |
| Create, get, list users | Any authenticated user |
| Update or delete user `{id}` | **Only that user** (`token.sub == id`), otherwise 403 |

**Why:** letting any logged-in user edit or delete anyone else would be a serious flaw in any real system. Self-only is the safest rule that needs no role model. The check lives in the **application layer**, so it applies identically to any future transport, and it runs **before** the database lookup, so a 403 never reveals whether a user exists.

**Trade-off:** there is no admin role, so nobody can manage other accounts. Adding RBAC means adding a `role` to the user and token, plus a policy check in the same place.

**Register vs `POST /users`.** Both exist because the brief lists "registration" and "create a new user" separately. Register is public sign-up. `POST /users` is the protected operation, for example an authenticated client provisioning an account. They share `UserService.Create`.

### 3.7 Validation

- **Where:** the rules live in `domain` as plain functions. Normalization (trim, lower-case email) and validation run in the application layer, so REST and gRPC share them.
- **How:** hand-written, using `net/mail.ParseAddress`, plus checks that the input is a bare address (not `Name <addr>`) whose domain contains a dot.
- **Why not `go-playground/validator`?** Struct tags would sit on transport DTOs, not the domain, and would need duplicating for gRPC. With only three fields, plain functions are clearer and dependency-free.
- **Errors** collect *all* failing fields into one `ValidationError` (`{"details":{"name":..., "email":...}}`), so clients can show every problem at once.
- **Strict JSON decoding:** unknown fields are rejected (so `{"password":"x"}` in a PATCH fails loudly instead of being silently ignored), the body is capped at 1 MiB, and trailing data is rejected.
- **400 vs 422:** I used 400 for all client input errors. 422 is arguably more precise for semantic errors. Keeping one status, with field details in the body, is simpler for clients.
- **PATCH semantics:** pointer fields distinguish "absent" from "empty". `{"name":""}` is a validation error; an absent `name` means "leave unchanged".

### 3.8 Pagination

The brief says "list all users". Returning a whole collection unbounded is a denial-of-service and memory risk, so `GET /users` is paginated with `offset` and `limit` (default 50, max 100). The response includes `total`.

- **Trade-off:** `skip` gets slower at large offsets, and `total` costs a second query. Cursor pagination (`?after=<lastId>`, using the `_id` index) would scale better but is less convenient for clients. Offset/limit is the pragmatic choice at this scale. The repository signature can be changed to cursors without touching handlers much.

### 3.9 Error handling

The domain defines sentinel errors (`ErrUserNotFound`, `ErrEmailTaken`, `ErrForbidden`, …) plus `*ValidationError`. The repository translates driver errors into them. Each transport has **one** mapping function (`writeError` for HTTP, `toStatus` for gRPC). Unknown errors are logged server-side and returned as a generic 500 / `Internal`, so internal details never reach clients.

`ValidationError.OrNil()` avoids Go's typed-nil-in-interface trap: returning a nil `*ValidationError` as `error` would be non-nil.

### 3.10 Logging and middleware

- **`log/slog` with a JSON handler.** Structured, standard library, and ready for log aggregators. The level comes from `LOG_LEVEL`.
- The **logging middleware** records `method`, `path`, `status`, `bytes` and `duration_ms` (a float, easy to aggregate). It wraps the `ResponseWriter` to capture the status and implements `Unwrap()` so `http.ResponseController` keeps working.
- **Order:** `Logging(Recover(mux))`. Logging is outermost, so even a recovered panic is logged with its final 500 status.
- gRPC gets an equivalent `LoggingInterceptor` (method, code, duration).
- *Trade-off:* the logs have no request ID or correlation ID. I'd add that next for multi-service tracing.

### 3.11 Background worker

`UserCountReporter.Run(ctx)` uses a `time.Ticker` and a `select` on `ctx.Done()`:

- It is started from `main` with `sync.WaitGroup.Go` (Go 1.25), and `main` waits for it during shutdown, so no goroutine leaks.
- Each tick's query has a timeout equal to the interval, so a slow database cannot pile up overlapping work.
- During shutdown, a query cancelled by the parent context is **not** logged as an error (tested).
- It depends on a one-method `UserCounter` interface, so tests run it at a 10 ms interval with a fake. No sleeping for 10 s in tests.
- *Trade-off:* the first log arrives after one interval, not at startup, which follows "every 10 seconds" literally. With several replicas, every replica logs; a leader election or lock would be needed for exactly one.

### 3.12 Graceful shutdown

```
SIGINT/SIGTERM → signal.NotifyContext cancels ctx
  1. stop()                      → worker exits its loop
  2. httpSrv.Shutdown(ctx 15s)   → stop accepting, drain in-flight requests
  3. grpcSrv.GracefulStop()      → drain RPCs; forced Stop() if the 15 s deadline passes
  4. wg.Wait()                   → worker confirmed stopped
  5. deferred client.Disconnect  → Mongo closed last, after nothing uses it
```

If either server fails to start (port in use, for example), the same path runs and the process exits non-zero. The HTTP server also sets `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout` and `IdleTimeout`, guarding against slowloris-style connections. Docker Compose sets `stop_grace_period: 20s`, longer than the 15 s drain window.

### 3.13 gRPC

- **Contract:** `api/proto/user/v1/user.proto`, versioned package `user.v1`. `created_at` is a `google.protobuf.Timestamp`.
- **Auth:** a unary interceptor requires `authorization: Bearer <jwt>` metadata on **all** RPCs, including `CreateUser`. This mirrors REST, where `POST /users` is protected. Tokens come from the REST login, since the brief only asks for `CreateUser`/`GetUser` over gRPC.
- **Server reflection** is enabled so `grpcurl` works out of the box. *Trade-off:* it exposes the schema. I'd disable it or gate it in production.
- **Generated code is committed** (`gen/`), so `go build` and `docker build` need no `protoc`. *Trade-off:* the generated files must be regenerated (`make proto`) when the `.proto` changes.
- **No TLS** on either listener. In production, TLS would normally terminate at a load balancer or ingress, or be configured with `credentials.NewServerTLSFromFile`. Bearer tokens over plaintext are only acceptable locally.

### 3.14 Testing strategy

| Layer | Technique | Rationale |
|---|---|---|
| Domain | Table-driven tests | Pure functions |
| Application | **Hand-written mock of `UserRepository`** with function fields and call recording; any unexpected call panics | "Mock MongoDB interactions". Lets tests inject failures (duplicate key, DB down) and assert *non*-calls (for example, "validation failure never touches the DB", "403 never touches the DB"). |
| HTTP | `httptest.Server` + real services + **in-memory repository** + real JWT and bcrypt (`MinCost`) | Exercises routing, middleware, JSON, status codes and auth together, without being tied to implementation details |
| gRPC | `bufconn` in-process server | Real gRPC stack, no network ports |
| Security | Crafted malicious tokens | Proves `alg:none`, HS512, wrong secret, tampering, expiry, wrong issuer and missing `exp`/`sub` are all rejected |
| Mongo adapter | **Integration tests against real MongoDB**, skipped unless `MONGO_TEST_URI` is set | Verifies the adapter honours the port contract (unique index, not-found mapping, `$set`, ordering) |

**Why a hand-written mock rather than gomock or mockery?** The interface is small, a hand-written mock is readable in one screen, and it adds no code-generation step. **Why not mock the Mongo driver itself?** The driver's mock deployment (`mtest`) lives in `internal/` in driver v2 and can't be imported. More fundamentally, mocking a driver mostly tests that you called the driver the way you wrote the mock. Mocking at the **port** boundary, then testing the adapter against a real database, gives stronger guarantees for less effort.

All tests pass with `-race`. Coverage: domain 97%, application 94%, config 95%, httpapi 91%, security 89%, grpcapi 85%, mongorepo 83% (integration), worker 100%.

### 3.15 Docker

- A **multi-stage build:** `golang:1.25-alpine` compiles a static binary (`CGO_ENABLED=0`, `-trimpath -ldflags="-s -w"`), and it runs on **`distroless/static:nonroot`**, which has no shell, no package manager and runs as a non-root user. The result is a small image with a minimal attack surface.
- `go.mod` and `go.sum` are copied before the source, so the module download layer is cached.
- Compose waits for Mongo's **healthcheck** (`mongosh ping`) before starting the API (`depends_on: condition: service_healthy`), avoiding startup races.
- Host ports are configurable (`HTTP_PORT`, `GRPC_PORT`) because 8080 is often already taken. That happened during development.
- *Trade-off:* the distroless image has no shell for debugging inside the container, and the API image has no `HEALTHCHECK` because there is no curl or wget in it. An orchestrator would probe `/healthz` externally.

### 3.16 Swagger UI (OpenAPI documentation)

The REST API is documented as an OpenAPI (Swagger 2.0) spec and served with an interactive Swagger UI at `/swagger/index.html`. Reviewers can try every endpoint from a browser without curl.

- **Tooling:** [swaggo/swag](https://github.com/swaggo/swag) generates `docs/` (`docs.go`, `swagger.json`, `swagger.yaml`) from `// @...` comments on the handlers. [swaggo/http-swagger/v2](https://github.com/swaggo/http-swagger) serves the UI. Both are the de facto standard in Go, and they work with the standard-library `net/http` mux, so no web framework is needed.
- **Code-first:** annotations live next to the handlers they describe, so a change to an endpoint and its docs land in the same diff. *Alternative considered:* a hand-written `openapi.yaml` (spec-first), possibly with code generated from it. That gives tighter control over the contract, but it can silently drift from the code unless you also adopt code generation. For a small API, code-first is cheaper.
- **Drift guard:** annotations can still be forgotten, so `TestSwaggerSpecCoversAllRoutes` fails if any route in the router is missing from the generated spec.
- **Clean schema names:** DTOs stay unexported (they are adapter internals), and `// @name User`-style comments give them clean names in the spec (`User`, `Error`, `LoginResponse`…) instead of `httpapi.userResponse`. `example` tags pre-fill the "Try it out" forms with valid data.
- **Placement:** Swagger is wired inside the HTTP adapter (`httpapi.WithSwaggerUI()`) and imported only there. The domain and application layers know nothing about it, consistent with the hexagonal rule. `main.go` holds only the API-level annotations (title, security scheme) and switches the UI on from config.
- **No fixed `host`** in the spec, so the UI calls whatever host and port it was loaded from. That matters because the port is configurable (`HTTP_PORT`).
- **Toggle:** `SWAGGER_ENABLED` (default `true`, for evaluation convenience). *Trade-off:* exposing API docs publicly reveals the attack surface. In production, set it to `false` or put the route behind the gateway's auth.
- **Generated files are committed**, as with `gen/`, so building needs no `swag` binary. `make swagger` pins the swag version (`go run …/swag@v1.16.6`) so everyone generates identical output.

**Trade-offs and limitations:**

- `swag` v1 emits **Swagger 2.0**, not OpenAPI 3. Swagger 2.0 has no `http`/`bearer` security scheme, so JWT is modelled as an `apiKey` in the `Authorization` header, and users must type the `Bearer ` prefix in the Authorize dialog. I kept the middleware strict (requiring `Bearer`) rather than also accepting a bare token just to suit the docs tool. swag v2 (OpenAPI 3) would fix this but was still pre-release.
- Annotations are comments, so the compiler doesn't check them. The drift test covers missing routes, but not, for example, a wrong response type in an annotation.
- Swagger UI covers REST only. gRPC is self-describing through server reflection (`grpcurl … list`).

### 3.17 Dependency versions

The project targets Go **1.25**, the locally installed toolchain. The newest `google.golang.org/grpc` (1.85) and `golang.org/x/crypto` (0.58) require Go 1.26, so they are pinned to **grpc v1.80.0** and **x/crypto v0.54.0**, the newest versions compatible with 1.25. Direct dependencies are deliberately few: the Mongo driver, `golang-jwt`, `x/crypto`, gRPC and protobuf, plus `swaggo/swag` and `swaggo/http-swagger/v2` for the Swagger UI.

---

## 4. Assumptions

1. **Email is case-insensitive** and stored lower-cased and trimmed. `Alice@X.com` and `alice@x.com` are the same account.
2. **Users may only modify or delete their own account.** Read, list and create are open to any authenticated user. There are no roles.
3. **Passwords cannot be changed** through the update endpoint. The brief says "update a user's name or email". A `password` field in a PATCH body is rejected with 400 rather than silently ignored.
4. **Changing your email doesn't invalidate existing tokens.** The `sub` (ID) is the identity, and the `email` claim is informational.
5. **"List all users" means paginated listing,** with a default page of 50 and a maximum of 100. `total` tells clients how many exist.
6. **Registration does not log you in.** Clients call `/auth/login` afterwards. This keeps the endpoints single-purpose.
7. **IDs are opaque strings** to clients: 24-character hex MongoDB ObjectIDs.
8. **Timestamps are UTC**, RFC 3339 in JSON and `google.protobuf.Timestamp` in gRPC, with millisecond precision.
9. **Name:** 1–100 Unicode characters after trimming. **Password:** 8 characters to 72 bytes, with no composition rules (in line with NIST SP 800-63B).
10. **TLS is out of scope.** It is expected to terminate at the edge in a real deployment.

---

## 5. Known limitations and next steps

| Limitation | Next step |
|---|---|
| No token revocation, logout or refresh | Refresh tokens + `token_version` claim, or a `jti` deny-list |
| No rate limiting on `/auth/login` | Per-IP and per-account limiter (middleware or gateway) |
| No RBAC | Add `role` to the user and token; policy checks in `application` |
| Offset pagination | Cursor (`_id`-based) pagination |
| No request IDs or tracing | Request-ID middleware + OpenTelemetry (the Mongo driver already supports it) |
| No readiness probe (only liveness `/healthz`) | `/readyz` that pings Mongo |
| gRPC covers only Create and Get | Add Update, Delete and List, plus a gRPC `Login` so gRPC clients don't need REST |
| No TLS | Configure TLS on both listeners, or document ingress termination |
| Swagger 2.0 (requires typing the `Bearer ` prefix in Swagger UI) | Move to swag v2 / OpenAPI 3 with a native `bearer` scheme once it is stable |
