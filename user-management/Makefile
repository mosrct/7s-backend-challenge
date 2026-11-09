.PHONY: run build test test-integration cover proto swagger docker-up docker-down

SWAG_VERSION := v1.16.6

run:            ## Run the API locally (needs MongoDB and JWT_SECRET)
	go run ./cmd/server

build:
	go build -o bin/server ./cmd/server

test:           ## Unit tests (MongoDB is mocked)
	go test -race -count=1 ./...

test-integration: ## Includes MongoDB adapter tests (needs `docker compose up -d mongo`)
	MONGO_TEST_URI=mongodb://localhost:27017 go test -race -count=1 ./...

cover:
	go test -coverprofile=coverage.out ./internal/...
	go tool cover -func=coverage.out | tail -n 1

proto:          ## Regenerate gRPC code (needs protoc, protoc-gen-go, protoc-gen-go-grpc)
	protoc --proto_path=api/proto \
		--go_out=. --go_opt=module=user-management \
		--go-grpc_out=. --go-grpc_opt=module=user-management \
		user/v1/user.proto

swagger:        ## Regenerate the OpenAPI spec in docs/ from swag annotations (Swagger UI at /swagger/)
	go run github.com/swaggo/swag/cmd/swag@$(SWAG_VERSION) init \
		-g cmd/server/main.go -o docs --parseInternal --outputTypes go,json,yaml

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down
