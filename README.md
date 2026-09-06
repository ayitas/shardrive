# Shardrive

Shardrive is a self-hosted virtual storage pool. The LocalProvider Phase 0
engine and Phase 1 product UI are complete, and the one-account Proton Phase 2
gate has passed. The backend has PostgreSQL-backed domain repositories, a
provider-neutral storage boundary, deterministic placement, streaming chunk
ingestion, sequential integrity-checked completion/download, and durable
account lifecycle wiring. Phase 3 multi-account Proton is the next product
slice.

Phase 1 now includes a strict TypeScript SvelteKit scaffold and a typed API
client. Frontend dependency installation and checks require Node.js 20+ and
npm 10+.

The Upload API exposes create, status, completed-chunk resume, chunk PUT, and
completion endpoints. Chunk bodies stream through SHA-256 into LocalProvider
without whole-file staging. Placement is quota-aware, provider failures before
body consumption can retry another eligible account, and committed duplicate
chunks are idempotent. Global and per-account upload limits bound concurrency.
Completion reads stored chunks sequentially, revalidates every chunk checksum,
and calculates a valid whole-file SHA-256 before atomically making the file
available.

`GET /api/v1/files/{fileId}/download` loads and validates PostgreSQL metadata
and remote object stats before sending headers. It then downloads one chunk at
a time under the account's configured worker limit. Memory is bounded to one
chunk so its exact size and SHA-256 are verified before those bytes are written
to the client; the continuous whole-file SHA-256 is checked at the end.

During Phase 0 these routes use the existing user UUID configured by
`SHARDRIVE_LOCAL_ACCOUNT_USER_ID`; if it is unset, they fail closed with HTTP
503. Request-provided user IDs are never trusted. The available routes are:

```text
POST /api/v1/uploads
DELETE /api/v1/uploads/{uploadId}
GET  /api/v1/uploads
GET  /api/v1/uploads/{uploadId}
GET  /api/v1/uploads/{uploadId}/chunks
PUT  /api/v1/uploads/{uploadId}/chunks/{index}
POST /api/v1/uploads/{uploadId}/complete
POST /api/v1/auth/login
POST /api/v1/auth/logout
GET  /api/v1/auth/me
GET  /api/v1/files
PATCH /api/v1/files/{fileId}
DELETE /api/v1/files/{fileId}
GET  /api/v1/accounts
POST /api/v1/accounts
POST /api/v1/accounts/{accountId}/refresh
GET  /api/v1/directories?parentId={directoryId}
POST /api/v1/directories
PATCH /api/v1/directories/{directoryId}
DELETE /api/v1/directories/{directoryId}
GET  /api/v1/files/{fileId}/download
```

To connect one Proton account, send `POST /api/v1/accounts` with
`{"name":"Personal Proton","provider":"proton","credentialRef":"account-1"}`.
`credentialRef` is only an opaque adapter-session reference; passwords, access
tokens, and Proton credentials are never accepted by this API.
Run the adapter's local `proton:session:import` setup command first to create
that encrypted session reference from the official CLI OS keychain.

## Requirements

- Go 1.26 or newer (for local development)
- Docker with the Compose plugin (for the complete local stack)
- `curl` for the health-check examples

## Start with Docker Compose

```sh
cp .env.example .env
docker compose --env-file .env -f deploy/docker-compose.yml up --build
```

The API listens on `http://localhost:8080` by default.

The web frontend listens on `http://localhost:3000` when Compose is running.

The default Compose profile uses LocalProvider only. To run the one-account
Proton adapter, first follow the encrypted session import instructions in
[`apps/proton-adapter/README.md`](apps/proton-adapter/README.md), set
`SHARDRIVE_PROTON_ADAPTER_ADDRESS=proton-adapter:50051` in `.env`, and start:

```sh
docker compose --env-file .env -f deploy/docker-compose.yml --profile proton up --build
```

The Proton profile is opt-in and never places credentials in PostgreSQL, the
frontend, or the Go/gRPC request payload.

Authentication cookies are `Secure` by default and therefore require HTTPS in
a browser. For local HTTP-only Compose testing, set
`SHARDRIVE_COOKIE_SECURE=false` in `.env`; never use that setting in
production.

```sh
curl --fail http://localhost:8080/health/live
curl --fail http://localhost:8080/health/ready
```

`/health/live` reports whether the HTTP process is running. `/health/ready`
also verifies its PostgreSQL connection. Compose waits for PostgreSQL to become
healthy before starting the API. On startup, the API applies pending versioned
database migrations under an advisory lock.

LocalProvider data is persisted in the `local-storage` Compose volume. To
provision the default five 1 GiB development accounts, set
`SHARDRIVE_LOCAL_ACCOUNT_USER_ID` to the UUID of an existing Shardrive user.
Provisioning is idempotent and never creates a placeholder user. Account count,
quota, storage root, chunk size, upload lifetime, global upload limit, and
per-account worker limits are configurable through [`.env.example`](.env.example).

To create a development login, run the one-shot bootstrap command with a
temporary password supplied through the environment (the password is never
printed):

```sh
docker compose -f deploy/docker-compose.yml run --rm \
  -e SHARDRIVE_BOOTSTRAP_EMAIL=you@example.test \
  -e SHARDRIVE_BOOTSTRAP_PASSWORD='use-a-local-password' \
  api /shardrive-api bootstrap-user
```

Copy the printed `user_id` into `SHARDRIVE_LOCAL_ACCOUNT_USER_ID` in `.env`,
then recreate the API and frontend containers.

Stop the stack with:

```sh
docker compose -f deploy/docker-compose.yml down
```

To also remove the development database volume, explicitly run
`docker compose -f deploy/docker-compose.yml down -v`.

## Local backend development

Start PostgreSQL (for example, with the Compose `postgres` service), then:

```sh
cp .env.example .env
set -a
. ./.env
set +a
make run-api
```

Useful commands:

```sh
make fmt
make test
make frontend-check
make adapter-test
make integration-test
make phase0-gate
make build
make check
make compose-config
```

For the Proton Compose profile, configure the encrypted session and master-key
file in `.env`, then run `make compose-proton-up`. The live browser gate is
`make proton-e2e` and requires `E2E_EMAIL`, `E2E_PASSWORD`,
`E2E_PROTON_ACCOUNT_NAME`, and `E2E_PROTON_ACCOUNT_REF`.

Configuration is provided through environment variables documented in
[`.env.example`](.env.example). Do not commit `.env` or real credentials.

## Repository layout

```text
apps/backend/       Go modular monolith
apps/frontend/      SvelteKit application (Phase 1 product UI)
apps/proton-adapter Proton Drive adapter (Phase 2 complete; Phase 3 next)
deploy/             Local deployment files
proto/              Provider-neutral adapter protocol definitions
```

See [`PROGRESS.md`](PROGRESS.md) for the exact implementation status and actual
verification commands. The LocalProvider Phase 0 gate, Phase 1 release-candidate
gate, and one-account Proton Phase 2 gate are complete. Multi-account Proton is
the next product slice, starting with two configured accounts.
