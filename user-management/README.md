# User Management API

A REST and gRPC API in Go for managing users. It stores data in MongoDB, authenticates with JWT (HS256), and is built with a hexagonal (ports and adapters) architecture.

| Feature | Where |
|---|---|
| REST API: register, login, and user create/get/list/update/delete | [internal/adapter/httpapi](internal/adapter/httpapi) |
| JWT auth (HMAC-SHA256), checked by middleware | [internal/adapter/security/jwt.go](internal/adapter/security/jwt.go), [middleware.go](internal/adapter/httpapi/middleware.go) |
| MongoDB using the official driver (v2), unique email index | [internal/adapter/repository/mongorepo](internal/adapter/repository/mongorepo) |
| Logging middleware (method, path, status, duration) | [internal/adapter/httpapi/middleware.go](internal/adapter/httpapi/middleware.go) |
| Background goroutine that logs the user count every 10 s | [internal/adapter/worker](internal/adapter/worker) |
| Unit tests with a mocked MongoDB, plus integration tests against real MongoDB | `*_test.go` |
| **Swagger UI** for trying the API in a browser (OpenAPI spec generated with `swag init`) | `/swagger/`, [docs/](docs) |
| **Bonus:** Docker and docker-compose, interfaces, validation, graceful shutdown, gRPC (`CreateUser`, `GetUser`) with token metadata, hexagonal layout | see [implementation.md](implementation.md) |

Design decisions, trade-offs and assumptions are in **[implementation.md](implementation.md)**.

---

## Contents

1. [Quick start (Docker)](#1-quick-start-docker)
2. [Run locally without Docker](#2-run-locally-without-docker)
3. [Configuration](#3-configuration)
4. [API reference](#4-api-reference) (including [Swagger UI](#41-try-it-in-the-browser-swagger-ui))
5. [JWT guide: generating and using tokens](#5-jwt-guide-generating-and-using-tokens)
6. [Sample requests and responses](#6-sample-requests-and-responses)
7. [gRPC](#7-grpc)
8. [Testing](#8-testing)
9. [Project structure](#9-project-structure)

---

## 1. Quick start (Docker)

You need Docker with the Compose plugin.

```bash
cp .env.example .env            # PowerShell: Copy-Item .env.example .env
# Edit .env and set JWT_SECRET to a random value of at least 32 bytes, e.g.
#   openssl rand -base64 48
docker compose up --build -d
```

| Service | Host address |
|---|---|
| REST API | http://localhost:8080 |
| **Swagger UI** | **http://localhost:8080/swagger/index.html** |
| gRPC API | localhost:9090 |
| MongoDB | mongodb://localhost:27017 |

If port 8080 or 9090 is already in use, set `HTTP_PORT` or `GRPC_PORT` in `.env`.

Check that it is running:

```bash
curl http://localhost:8080/healthz
# {"status":"ok"}
docker compose logs -f api      # JSON logs, including "total users" every 10 s
```

Stop it with `docker compose down`. Add `-v` to also delete the MongoDB data volume.

> The API **refuses to start** without `JWT_SECRET`, or with a secret shorter than 32 bytes. There is intentionally no built-in default secret.

## 2. Run locally without Docker

You need Go **1.25+** and a running MongoDB. The easiest way to get MongoDB is `docker compose up -d mongo`.

**bash / zsh**

```bash
export JWT_SECRET="$(openssl rand -base64 48)"
export MONGO_URI="mongodb://localhost:27017"
go run ./cmd/server
```

**PowerShell**

```powershell
$env:JWT_SECRET = -join ((1..48) | ForEach-Object { [char](Get-Random -Minimum 97 -Maximum 123) })
$env:MONGO_URI  = "mongodb://localhost:27017"
go run ./cmd/server
```

Press `Ctrl+C` for a graceful shutdown. The server stops accepting connections, drains in-flight HTTP and gRPC requests, stops the background worker, and disconnects from MongoDB.

A `Makefile` has shortcuts: `make run`, `make test`, `make test-integration`, `make cover`, `make proto`, `make swagger`, `make docker-up`.

## 3. Configuration

All configuration comes from environment variables.

| Variable | Default | Description |
|---|---|---|
| `JWT_SECRET` | *(required)* | HMAC key for signing tokens. At least 32 bytes. |
| `JWT_TTL` | `1h` | Token lifetime (Go duration: `15m`, `1h`, …) |
| `JWT_ISSUER` | `user-management-api` | `iss` claim; set on tokens and checked on verification |
| `HTTP_ADDR` | `:8080` | REST listen address |
| `GRPC_ADDR` | `:9090` | gRPC listen address |
| `MONGO_URI` | `mongodb://localhost:27017` | MongoDB connection string |
| `MONGO_DATABASE` | `user_management` | Database name (collection: `users`) |
| `USER_COUNT_INTERVAL` | `10s` | How often the background worker logs the user count |
| `SHUTDOWN_TIMEOUT` | `15s` | Maximum time allowed for draining requests on shutdown |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `SWAGGER_ENABLED` | `true` | Serve Swagger UI at `/swagger/`. Set `false` in production. |
| `HTTP_PORT` / `GRPC_PORT` | `8080` / `9090` | *docker-compose only:* host ports to publish |

## 4. API reference

Base path: `/api/v1`. Bodies are JSON. Unknown JSON fields are rejected, and bodies are limited to 1 MiB.

| Method | Path | Auth | Description | Success |
|---|---|---|---|---|
| `GET` | `/healthz` | – | Liveness check | `200` |
| `POST` | `/api/v1/auth/register` | – | Register a new account | `201` + `Location` |
| `POST` | `/api/v1/auth/login` | – | Exchange credentials for a JWT | `200` |
| `POST` | `/api/v1/users` | Bearer | Create a user (as an authenticated caller) | `201` + `Location` |
| `GET` | `/api/v1/users?offset=0&limit=50` | Bearer | List users (paginated, `limit` 1–100, default 50) | `200` |
| `GET` | `/api/v1/users/{id}` | Bearer | Fetch a user by ID | `200` |
| `PATCH` | `/api/v1/users/{id}` | Bearer, **self only** | Update `name` and/or `email` | `200` |
| `DELETE` | `/api/v1/users/{id}` | Bearer, **self only** | Delete a user | `204` |

### 4.1 Try it in the browser (Swagger UI)

With the server running, open **http://localhost:8080/swagger/index.html**. If you changed `HTTP_PORT` in `.env`, use that port instead, for example `http://localhost:8090/swagger/index.html`. `/swagger` also redirects there.

Each endpoint has a **Try it out** button, and the request bodies are pre-filled with example values. To call the protected endpoints:

1. **auth → `POST /api/v1/auth/register`** → *Try it out* → *Execute*. The example body works as-is.
2. **auth → `POST /api/v1/auth/login`** → *Try it out* → *Execute*. Copy `access_token` from the response.
3. Click the **Authorize** 🔒 button at the top. In the value box, type **`Bearer `** followed by the token: `Bearer eyJhbGciOi...`. Then click *Authorize* and *Close*.
4. Every endpoint under **users** now sends the token. The token is kept across page reloads until you click *Logout* in the same dialog.

> You must type the `Bearer ` prefix yourself. Swagger 2.0, which `swag` generates, has no native "bearer" scheme, so the header is modelled as an API key and the value is sent exactly as typed. Without the prefix, protected calls return `401`.

The raw spec is at `/swagger/doc.json`. It is also committed as [docs/swagger.json](docs/swagger.json) and [docs/swagger.yaml](docs/swagger.yaml), so you can import it into Postman or Insomnia.

**Regenerating the spec.** The spec is generated from `// @...` annotations on the handlers in [internal/adapter/httpapi/handlers.go](internal/adapter/httpapi/handlers.go) and the general info in [cmd/server/main.go](cmd/server/main.go). After changing an endpoint, run:

```bash
make swagger
# or, without make:
go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g cmd/server/main.go -o docs --parseInternal --outputTypes go,json,yaml
```

A test (`TestSwaggerSpecCoversAllRoutes`) fails if a route is missing from the spec.

### 4.2 Validation rules and errors

**Validation rules**

| Field | Rule |
|---|---|
| `name` | Required. Trimmed. 1–100 characters. |
| `email` | Required. Trimmed and lower-cased. Must be a bare valid address with a domain containing a dot. At most 254 characters. Unique, case-insensitive. |
| `password` | Required. At least 8 characters and at most 72 bytes (bcrypt's limit). |

**Error format.** Every error has the same shape:

```json
{ "error": "validation failed", "details": { "email": "must be a valid email address" } }
```

`details` appears only on validation errors.

| Status | When |
|---|---|
| `400` | Validation failed, malformed or unknown JSON, or bad pagination |
| `401` | Missing, invalid or expired token, or wrong credentials. Also sets `WWW-Authenticate: Bearer` on protected routes. |
| `403` | Updating or deleting a user other than yourself |
| `404` | User not found. A malformed ID also returns 404. |
| `409` | Email already in use |
| `500` | Unexpected error. Details are logged, not returned. |

## 5. JWT guide: generating and using tokens

### 5.1 Configure the signing secret

Tokens are signed with **HMAC-SHA256 (HS256)** using `JWT_SECRET`. Generate a strong secret:

```bash
openssl rand -base64 48
```

Put it in `.env` for Docker, or export it for local runs. Anyone who has this secret can mint valid tokens, so keep it out of version control. `.env` is already in `.gitignore`. Changing the secret invalidates every token already issued.

### 5.2 Get a token

1. **Register** (public):

   ```bash
   curl -X POST http://localhost:8080/api/v1/auth/register \
     -H "Content-Type: application/json" \
     -d '{"name":"Alice","email":"alice@example.com","password":"s3cure-pass"}'
   ```

2. **Log in** to receive the token:

   ```bash
   curl -X POST http://localhost:8080/api/v1/auth/login \
     -H "Content-Type: application/json" \
     -d '{"email":"alice@example.com","password":"s3cure-pass"}'
   ```

   ```json
   {
     "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJlbWFpbCI6...",
     "token_type": "Bearer",
     "expires_at": "2026-11-08T11:47:10Z",
     "expires_in": 3599,
     "user": { "id": "6af0532d5b25c4c7a5e43caa", "name": "Alice", "email": "alice@example.com", "created_at": "2026-11-08T10:47:09.956Z" }
   }
   ```

### 5.3 Use the token

Send it in the `Authorization` header on every protected request:

```bash
TOKEN="<access_token from login>"
curl http://localhost:8080/api/v1/users -H "Authorization: Bearer $TOKEN"
```

PowerShell:

```powershell
$login = Invoke-RestMethod -Method Post http://localhost:8080/api/v1/auth/login `
  -ContentType 'application/json' -Body '{"email":"alice@example.com","password":"s3cure-pass"}'
$h = @{ Authorization = "Bearer $($login.access_token)" }
Invoke-RestMethod http://localhost:8080/api/v1/users -Headers $h
```

For gRPC, send the same value as metadata: `authorization: Bearer <token>`. See [§7](#7-grpc).

### 5.4 What is inside the token

A JWT is `base64url(header).base64url(payload).signature`. Decoding the token above gives:

```jsonc
// header
{ "alg": "HS256", "typ": "JWT" }
// payload
{
  "sub":   "6af0532d5b25c4c7a5e43caa", // user ID; used for authorization
  "email": "alice@example.com",
  "iss":   "user-management-api",
  "iat":   1794134830,                  // issued at
  "nbf":   1794134830,                  // not valid before
  "exp":   1794138430                   // expires (iat + JWT_TTL)
}
```

The payload is **only encoded, not encrypted**, so never put secrets in it.

### 5.5 What the middleware checks

A request is rejected with `401` unless all of these hold:

- The header has the form `Authorization: Bearer <token>`. The scheme is case-insensitive.
- The signature is valid HMAC-SHA256 with the server secret.
- The `alg` header is exactly `HS256`. `none`, `HS512` and RS/ES tokens are rejected, which blocks algorithm-confusion attacks.
- `exp` is present and in the future, and `nbf` and `iat` are not in the future.
- `iss` equals `JWT_ISSUER`, and `sub` is not empty.

When a token expires, log in again. There is no refresh-token flow; see implementation.md for why.

## 6. Sample requests and responses

These were captured from a real run against MongoDB 7. IDs and timestamps will differ on your machine.

<details open>
<summary><b>Register</b>: <code>POST /api/v1/auth/register</code></summary>

```http
POST /api/v1/auth/register
Content-Type: application/json

{"name":"Alice","email":"alice@example.com","password":"s3cure-pass"}
```

```http
HTTP/1.1 201 Created
Content-Type: application/json
Location: /api/v1/users/6af0532d5b25c4c7a5e43caa

{"id":"6af0532d5b25c4c7a5e43caa","name":"Alice","email":"alice@example.com","created_at":"2026-11-08T10:47:09.956Z"}
```
</details>

<details>
<summary><b>Create user (protected)</b>: <code>POST /api/v1/users</code></summary>

```bash
curl -X POST http://localhost:8080/api/v1/users \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"Bob","email":"bob@example.com","password":"another-pass"}'
```

```json
201 Created
{"id":"6af0532e5b25c4c7a5e43cab","name":"Bob","email":"bob@example.com","created_at":"2026-11-08T10:47:10.323Z"}
```
</details>

<details>
<summary><b>Get user</b>: <code>GET /api/v1/users/{id}</code></summary>

```bash
curl http://localhost:8080/api/v1/users/6af0532e5b25c4c7a5e43cab -H "Authorization: Bearer $TOKEN"
```

```json
200 OK
{"id":"6af0532e5b25c4c7a5e43cab","name":"Bob","email":"bob@example.com","created_at":"2026-11-08T10:47:10.323Z"}
```
</details>

<details>
<summary><b>List users</b>: <code>GET /api/v1/users?limit=10&amp;offset=0</code></summary>

```bash
curl "http://localhost:8080/api/v1/users?limit=10&offset=0" -H "Authorization: Bearer $TOKEN"
```

```json
200 OK
{
  "users": [
    {"id":"6af0532d5b25c4c7a5e43caa","name":"Alice","email":"alice@example.com","created_at":"2026-11-08T10:47:09.956Z"},
    {"id":"6af0532e5b25c4c7a5e43cab","name":"Bob","email":"bob@example.com","created_at":"2026-11-08T10:47:10.323Z"}
  ],
  "total": 2,
  "offset": 0,
  "limit": 10
}
```
</details>

<details>
<summary><b>Update own name or email</b>: <code>PATCH /api/v1/users/{id}</code></summary>

```bash
curl -X PATCH http://localhost:8080/api/v1/users/6af0532d5b25c4c7a5e43caa \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"Alice Smith"}'
```

```json
200 OK
{"id":"6af0532d5b25c4c7a5e43caa","name":"Alice Smith","email":"alice@example.com","created_at":"2026-11-08T10:47:09.956Z"}
```

Updating someone else's account:

```json
403 Forbidden
{"error":"operation not permitted"}
```
</details>

<details>
<summary><b>Delete own account</b>: <code>DELETE /api/v1/users/{id}</code></summary>

```bash
curl -X DELETE http://localhost:8080/api/v1/users/6af0532d5b25c4c7a5e43caa -H "Authorization: Bearer $TOKEN"
```

```
204 No Content
```
</details>

<details>
<summary><b>Error examples</b></summary>

```text
# Missing token
GET /api/v1/users
401 Unauthorized
WWW-Authenticate: Bearer realm="user-management"
{"error":"authentication required"}

# Validation
POST /api/v1/auth/register {"name":"","email":"nope","password":"123"}
400 Bad Request
{"error":"validation failed","details":{"email":"must be a valid email address","name":"is required","password":"must be at least 8 characters"}}

# Duplicate email (case-insensitive)
POST /api/v1/auth/register {"name":"A","email":"ALICE@example.com","password":"s3cure-pass"}
409 Conflict
{"error":"email already in use"}

# Wrong password (same response as an unknown email)
POST /api/v1/auth/login {"email":"alice@example.com","password":"wrong"}
401 Unauthorized
{"error":"invalid email or password"}

# Unknown ID
GET /api/v1/users/000000000000000000000000
404 Not Found
{"error":"user not found"}
```
</details>

**Server logs** are structured JSON on stdout. Example:

```json
{"time":"2026-11-08T10:47:10.32Z","level":"INFO","msg":"http request","method":"POST","path":"/api/v1/users","status":201,"bytes":113,"duration_ms":52.1}
{"time":"2026-11-08T10:48:02.34Z","level":"INFO","msg":"total users","count":3}
```

## 7. gRPC

The service is defined in [api/proto/user/v1/user.proto](api/proto/user/v1/user.proto):

```proto
service UserService {
  rpc CreateUser(CreateUserRequest) returns (CreateUserResponse);
  rpc GetUser(GetUserRequest) returns (GetUserResponse);
}
```

Every RPC requires the metadata `authorization: Bearer <jwt>`. Get the token from the REST login endpoint. Server reflection is enabled, so [grpcurl](https://github.com/fullstorydev/grpcurl) works without the proto file:

```bash
grpcurl -plaintext localhost:9090 list
# user.v1.UserService

grpcurl -plaintext -H "authorization: Bearer $TOKEN" \
  -d '{"name":"Dave","email":"dave@example.com","password":"password123"}' \
  localhost:9090 user.v1.UserService/CreateUser
```

```json
{
  "user": {
    "id": "6af053615b25c4c7a5e43cae",
    "name": "Dave",
    "email": "dave@example.com",
    "createdAt": "2026-11-08T10:48:01.992Z"
  }
}
```

```bash
grpcurl -plaintext -H "authorization: Bearer $TOKEN" -d '{"id":"6af053615b25c4c7a5e43cae"}' \
  localhost:9090 user.v1.UserService/GetUser

grpcurl -plaintext -d '{"id":"6af053615b25c4c7a5e43cae"}' localhost:9090 user.v1.UserService/GetUser
# ERROR: Code: Unauthenticated  Message: missing or invalid bearer token
```

Domain errors map to gRPC codes: validation → `InvalidArgument`, not found → `NotFound`, duplicate email → `AlreadyExists`, bad token → `Unauthenticated`.

The generated code in `gen/` is committed. To regenerate it you need `protoc`, plus these plugins:

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
make proto
```

## 8. Testing

```bash
# Unit tests. MongoDB is mocked through the port.UserRepository interface; no database needed.
go test -race ./...

# Also run the MongoDB adapter integration tests against a real database
docker compose up -d mongo
MONGO_TEST_URI=mongodb://localhost:27017 go test -race ./...
# PowerShell: $env:MONGO_TEST_URI="mongodb://localhost:27017"; go test ./...

# Coverage
go test -cover ./internal/...
```

| Package | What is tested | How | Coverage |
|---|---|---|---|
| `domain` | Validation and normalization rules | Table-driven | 97% |
| `application` | Every use case, including error paths, authorization, and timing-safe login | **Hand-written mock repository** (MongoDB mocked), fake hasher and token manager | 94% |
| `adapter/httpapi` | Full HTTP flows, status codes, auth middleware, logging, panic recovery, strict JSON; Swagger UI on/off; spec covers every route | `httptest` + in-memory repository + real JWT and bcrypt | 91% |
| `adapter/grpcapi` | RPCs, error-code mapping, auth interceptor | In-process `bufconn` server | 85% |
| `adapter/security` | JWT round trip; rejects tampered, expired, wrong-secret, `alg:none`, HS512, wrong-issuer and missing-claim tokens; bcrypt | Direct | 89% |
| `adapter/worker` | Ticks, logs, stops on cancel, stays quiet on shutdown | 10 ms interval, captured logs | 100% |
| `adapter/repository/mongorepo` | Repository contract against real MongoDB: unique index, not-found, update, pagination | Integration test; skipped without `MONGO_TEST_URI` | 83% |
| `config` | Defaults, overrides, required secret, invalid durations | Injected `getenv` | 95% |

## 9. Project structure

```
.
├── cmd/server/main.go                 # Composition root: wiring, servers, worker, graceful shutdown
├── api/proto/user/v1/user.proto       # gRPC contract
├── gen/user/v1/                       # Generated protobuf/gRPC code (committed)
├── docs/                              # Generated OpenAPI/Swagger spec (`make swagger`, committed)
├── internal/
│   ├── domain/                        # Entity, validation rules, domain errors (no dependencies)
│   ├── port/                          # Interfaces: UserRepository, PasswordHasher, TokenManager
│   ├── application/                   # Use cases: UserService, AuthService
│   ├── adapter/
│   │   ├── httpapi/                   # REST: router, handlers, DTOs, middleware (logging, auth, recover)
│   │   ├── grpcapi/                   # gRPC server and interceptors
│   │   ├── repository/mongorepo/      # MongoDB implementation of UserRepository
│   │   ├── repository/memory/         # In-memory implementation (used in tests)
│   │   ├── security/                  # bcrypt hasher, HS256 JWT manager
│   │   └── worker/                    # Periodic user-count reporter
│   └── config/                        # Environment-based configuration
├── Dockerfile                         # Multi-stage build → distroless, non-root
├── docker-compose.yml                 # API + MongoDB (with healthcheck)
├── Makefile
├── README.md
└── implementation.md                  # Design decisions, trade-offs, assumptions
```
