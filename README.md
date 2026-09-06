# Shardrive

Shardrive is a self-hosted virtual storage pool. The current Phase 0 backend has
PostgreSQL-backed domain repositories, a provider-neutral storage boundary, and
a tested quota-limited LocalProvider. Runtime account wiring and the deterministic
placement engine are complete. Upload creation, resume status, streaming chunk
ingestion, sequential integrity-checked completion, and sequential
integrity-checked download are implemented. The LocalProvider Phase 0 gate now
passes an HTTP round trip with application-state recreation.

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
POST /api/v1/accounts/{accountId}/refresh
GET  /api/v1/directories?parentId={directoryId}
POST /api/v1/directories
PATCH /api/v1/directories/{directoryId}
DELETE /api/v1/directories/{directoryId}
GET  /api/v1/files/{fileId}/download
```

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
make integration-test
make phase0-gate
make build
make check
```

Configuration is provided through environment variables documented in
[`.env.example`](.env.example). Do not commit `.env` or real credentials.

## Repository layout

```text
apps/backend/       Go modular monolith
apps/frontend/      SvelteKit application (Phase 1)
apps/proton-adapter Proton Drive adapter (Phase 2, after the Phase 1 gate)
deploy/             Local deployment files
proto/              Provider adapter protocol definitions (Phase 2)
```

See [`PROGRESS.md`](PROGRESS.md) for the exact implementation status and actual
verification commands. The Phase 1 release-candidate gate now has an automated
Firefox test; Proton integration starts after this LocalProvider gate remains
green.
