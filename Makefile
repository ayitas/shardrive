.PHONY: build check compose-down compose-up fmt integration-test phase0-gate run-api test

BACKEND_DIR := apps/backend

fmt:
	cd $(BACKEND_DIR) && gofmt -w $$(find . -name '*.go' -type f)

test:
	cd $(BACKEND_DIR) && go test ./...

integration-test:
	cd $(BACKEND_DIR) && SHARDRIVE_TEST_DATABASE_URL=$${SHARDRIVE_TEST_DATABASE_URL:-postgres://shardrive:shardrive-dev-only@localhost:5432/shardrive?sslmode=disable} go test ./internal/db -run TestMigrateFreshDatabase -count=1

phase0-gate:
	cd $(BACKEND_DIR) && SHARDRIVE_TEST_DATABASE_URL=$${SHARDRIVE_TEST_DATABASE_URL:-postgres://shardrive:shardrive-dev-only@localhost:5432/shardrive?sslmode=disable} go test ./internal/repositorytest -run TestPhase0HTTPRoundTripAfterApplicationRecreation -count=1 -v

build:
	mkdir -p bin
	cd $(BACKEND_DIR) && go build -o ../../bin/shardrive-api ./cmd/api

check: fmt test build

run-api:
	cd $(BACKEND_DIR) && go run ./cmd/api

compose-up:
	docker compose -f deploy/docker-compose.yml up --build

compose-down:
	docker compose -f deploy/docker-compose.yml down
