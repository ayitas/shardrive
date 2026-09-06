.PHONY: adapter-build adapter-check adapter-test build check compose-config compose-down compose-up compose-proton-down compose-proton-up e2e frontend-build frontend-check fmt integration-test phase0-gate proton-e2e run-api test

BACKEND_DIR := apps/backend
FRONTEND_DIR := apps/frontend
ADAPTER_DIR := apps/proton-adapter
COMPOSE_FILE := deploy/docker-compose.yml

fmt:
	cd $(BACKEND_DIR) && gofmt -w $$(find . -name '*.go' -type f)

test:
	cd $(BACKEND_DIR) && go test ./...

frontend-check:
	cd $(FRONTEND_DIR) && npm run check

frontend-build:
	cd $(FRONTEND_DIR) && npm run build

e2e:
	cd $(FRONTEND_DIR) && npm run test:e2e

proton-e2e:
	cd $(FRONTEND_DIR) && npm exec playwright -- test proton-account.spec.ts --project=firefox

adapter-check:
	cd $(ADAPTER_DIR) && npm run check

adapter-test:
	cd $(ADAPTER_DIR) && npm test

adapter-build:
	cd $(ADAPTER_DIR) && npm run build

integration-test:
	cd $(BACKEND_DIR) && SHARDRIVE_TEST_DATABASE_URL=$${SHARDRIVE_TEST_DATABASE_URL:-postgres://shardrive:shardrive-dev-only@localhost:5432/shardrive?sslmode=disable} go test ./internal/db -run TestMigrateFreshDatabase -count=1

phase0-gate:
	cd $(BACKEND_DIR) && SHARDRIVE_TEST_DATABASE_URL=$${SHARDRIVE_TEST_DATABASE_URL:-postgres://shardrive:shardrive-dev-only@localhost:5432/shardrive?sslmode=disable} go test ./internal/repositorytest -run TestPhase0HTTPRoundTripAfterApplicationRecreation -count=1 -v

build:
	mkdir -p bin
	cd $(BACKEND_DIR) && go build -o ../../bin/shardrive-api ./cmd/api

check: fmt test build frontend-check adapter-check adapter-test

run-api:
	cd $(BACKEND_DIR) && go run ./cmd/api

compose-up:
	docker compose -f $(COMPOSE_FILE) up --build

compose-proton-up:
	SHARDRIVE_PROTON_ADAPTER_ADDRESS=$${SHARDRIVE_PROTON_ADAPTER_ADDRESS:-proton-adapter:50051} docker compose --env-file .env -f $(COMPOSE_FILE) --profile proton up --build

compose-config:
	docker compose -f $(COMPOSE_FILE) config >/dev/null

compose-proton-down:
	docker compose -f $(COMPOSE_FILE) --profile proton down

compose-down:
	docker compose -f $(COMPOSE_FILE) down
