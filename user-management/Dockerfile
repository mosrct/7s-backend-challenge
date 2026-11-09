# syntax=docker/dockerfile:1

# ---- build stage ------------------------------------------------------------
FROM golang:1.25-alpine AS build
WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- runtime stage ----------------------------------------------------------
# distroless/static: no shell, no package manager, runs as non-root.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8080 9090
USER nonroot:nonroot
ENTRYPOINT ["/server"]
