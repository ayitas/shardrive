# AGENTS.md — Shardrive

## Mission

Build **Shardrive** from zero as a self-hosted virtual storage pool that combines multiple storage accounts into one logical drive.

A file uploaded by the user is split into chunks. Chunks are distributed across configurable storage accounts. PostgreSQL is the source of truth for file metadata, chunk ordering, placement, state, and recovery information. On download, Shardrive retrieves chunks in the correct order and streams the original file back to the user.

Primary stack:
- Backend: Go 1.26+
- Frontend: SvelteKit + TypeScript
- Database: PostgreSQL
- First provider: LocalProvider
- First real cloud provider: Proton Drive
- Proton integration: isolated adapter, preferably TypeScript using the official Proton Drive SDK when practical
- Go ↔ Proton adapter: gRPC streaming
- Deployment: Docker Compose initially
- Observability later: Prometheus + Grafana

Initial target is 5–10 Proton Drive accounts, configurable at runtime. Never hard-code the account count.

## Hard Architecture Rules

1. PostgreSQL is the metadata source of truth. Cloud providers are opaque object stores.
2. Core Go services MUST NOT depend on Proton-specific SDK/API details.
3. Use a provider-neutral `StorageProvider` abstraction.
4. Build a modular monolith first. Do not split core Go modules into microservices.
5. Stream data. Never require a complete uploaded/downloaded file to be staged on backend disk.
6. Correctness before optimization: sequential download first, bounded concurrency only.
7. Do not implement Reed-Solomon, replication, deduplication, rebalance, Redis, Kafka, or Kubernetes in initial V1.
8. Build and prove LocalProvider before Proton integration.
9. Never store provider credentials in plaintext or log secrets.
10. Never claim a build/test succeeded unless it was actually executed successfully.

## Target Repository Structure

```text
shardrive/
├── apps/
│   ├── backend/
│   │   ├── cmd/api/
│   │   ├── cmd/worker/
│   │   ├── internal/
│   │   │   ├── account/
│   │   │   ├── auth/
│   │   │   ├── chunk/
│   │   │   ├── config/
│   │   │   ├── db/
│   │   │   ├── directory/
│   │   │   ├── download/
│   │   │   ├── file/
│   │   │   ├── integrity/
│   │   │   ├── job/
│   │   │   ├── placement/
│   │   │   ├── storage/
│   │   │   └── upload/
│   │   ├── migrations/
│   │   ├── go.mod
│   │   └── Dockerfile
│   ├── frontend/
│   └── proton-adapter/
├── proto/storage.proto
├── deploy/docker-compose.yml
├── AGENTS.md
├── PROGRESS.md
├── README.md
├── Makefile
└── .env.example
```

Do not implement the real proton-adapter until the LocalProvider Phase 0 acceptance gate passes.

## Core Provider Interface

Conceptually:

```go
type Provider interface {
    Upload(ctx context.Context, account StorageAccount, req UploadRequest) (*StoredObject, error)
    Download(ctx context.Context, account StorageAccount, objectID string) (io.ReadCloser, error)
    Delete(ctx context.Context, account StorageAccount, objectID string) error
    Stat(ctx context.Context, account StorageAccount, objectID string) (*ObjectInfo, error)
    Usage(ctx context.Context, account StorageAccount) (*StorageUsage, error)
    Health(ctx context.Context, account StorageAccount) error
}
```

Initial implementations:
- `local`
- `proton` later

Future providers may include S3, Google Drive, and OneDrive without changing the core upload/download engine.

## Domain Model

### StorageAccount

Important fields:
- id UUID
- user_id
- name
- provider
- status
- total_bytes
- used_bytes
- free_bytes
- priority
- max_upload_workers
- max_download_workers
- credential_ref
- rate_limited_until
- last_health_check
- last_error
- timestamps

Statuses:
`ACTIVE`, `DEGRADED`, `FULL`, `OFFLINE`, `RATE_LIMITED`, `AUTH_FAILED`, `DISABLED`, optionally `AUTH_REQUIRED`.

### Directory

Virtual filesystem metadata only:
- id
- user_id
- parent_id nullable
- name
- timestamps

Never depend on provider folders to represent the user's logical directory tree.

### File

Fields:
- id
- user_id
- directory_id
- name
- mime_type
- size_bytes
- chunk_size
- chunk_count
- checksum_sha256
- state
- timestamps
- deleted_at

States:
`UPLOADING`, `VERIFYING`, `AVAILABLE`, `DEGRADED`, `DELETING`, `DELETED`, `FAILED`.

### Chunk

Fields:
- id
- file_id
- chunk_index
- size_bytes
- checksum_sha256
- storage_account_id
- remote_object_id
- remote_path optional
- state
- retry_count
- timestamps

Database MUST enforce:

```sql
UNIQUE(file_id, chunk_index)
```

States:
`PENDING`, `UPLOADING`, `STORED`, `CORRUPT`, `DELETING`, `DELETED`, `FAILED`.

### UploadSession

Fields:
- id
- user_id
- file_id
- expected_size
- received_bytes
- expected_chunks
- completed_chunks
- state
- expires_at
- timestamps

### Job

Use PostgreSQL as the initial durable queue with `FOR UPDATE SKIP LOCKED`.

Initial jobs:
- CLEANUP_UPLOAD
- DELETE_FILE
- DELETE_ORPHAN
- REFRESH_ACCOUNT_QUOTA
- ACCOUNT_HEALTH_CHECK

## Initial Defaults

- chunk size: 32 MiB
- browser parallel chunk uploads: 3
- global backend upload limit: 6
- per-account upload limit: 2
- download: sequential initially
- future download prefetch: 2–3
- retries: max 5
- retry: exponential backoff + jitter
- account reserve: `max(256 MiB, 4 × chunkSize)`
- incomplete upload cleanup: 24h

Make relevant values configurable.

## LocalProvider First

Simulate independent cloud accounts:

```text
./data/storage/account-1/
./data/storage/account-2/
./data/storage/account-3/
./data/storage/account-4/
./data/storage/account-5/
```

Seed five development accounts, suggested quota 1 GiB/account.

LocalProvider must implement:
- Upload
- Download
- Delete
- Stat
- Usage
- Health
- quota enforcement
- opaque UUID object names

Optional fault injection after basics:
- latency
- offline
- quota exceeded
- upload failure rate
- download failure rate

## Placement Engine V1

Candidate filtering:
1. status ACTIVE
2. not rate-limited
3. enough capacity for chunk + reserve
4. not explicitly excluded

Suggested initial score:
- free-capacity ratio: +60%
- configured priority: +20%
- current load: -15%
- recent error rate: -5%

Apply a diversity penalty to the account used for the previous chunk.

If upload to a selected account fails before commit, retry using another eligible account.

Do not create duplicate committed mappings for the same `(file_id, chunk_index)`.

## Upload API

### Create upload

`POST /api/v1/uploads`

Request:
- name
- size
- mimeType
- directoryId optional

Response:
- uploadId
- fileId
- chunkSize
- chunkCount

### Upload chunk

`PUT /api/v1/uploads/{uploadId}/chunks/{index}`

The handler must:
1. validate upload state and chunk index
2. enforce expected maximum chunk size
3. make already-STORED chunks idempotent
4. choose account via Placement Engine
5. generate opaque remote object UUID before upload
6. stream request through SHA-256 into provider
7. after provider success, use DB transaction to insert mapping and update progress
8. return index, state, bytes and checksum

Do not stage the complete file on disk.

### Complete upload

`POST /api/v1/uploads/{uploadId}/complete`

Only mark AVAILABLE when:
- all expected chunk indexes exist
- completed count matches expected count
- stored bytes match expected file size

Do not invent a whole-file SHA-256 during parallel upload. Implement a valid verification strategy before storing it.

### Resume

Expose upload state/completed chunk indexes so the browser sends only missing chunks.

## Browser Upload

Use `File.slice(start, end)`.

Never load the entire file into browser memory.

Initial concurrency: 3.

States:
`QUEUED`, `CREATING`, `UPLOADING`, `PAUSED`, `VERIFYING`, `COMPLETED`, `FAILED`.

Pause stops scheduling new chunks and allows in-flight chunks to finish.

IndexedDB may persist upload-session metadata.

## Download API

`GET /api/v1/files/{fileId}/download`

V1:
1. file must be AVAILABLE
2. load all required metadata before response streaming
3. query chunks ordered by `chunk_index`
4. validate sequence completeness
5. open correct provider/account for each chunk
6. stream sequentially to HTTP response
7. verify SHA-256 while reading
8. close readers promptly
9. set Content-Length, Content-Type, Content-Disposition safely

Do not implement prefetch until sequential reconstruction is proven correct.

Later:
- bounded prefetch
- HTTP Range
- media seeking

## Delete Flow

`DELETE /api/v1/files/{id}`:
- mark DELETING
- enqueue DELETE_FILE
- return 202

Worker deletes remote chunks, records per-chunk state, retries transient failures, then marks file DELETED.

Never discard metadata before remote deletion/recovery is resolved.

## Orphan Recovery

Critical failure:

```text
provider upload succeeds
→ PostgreSQL commit fails
```

Generate remote object UUID before upload. The object becomes recoverable garbage if DB commit fails.

Later orphan cleanup should identify application-owned remote objects without DB mappings.

## Provider Physical Layout

Where provider folders exist:

```text
/shardrive/
├── objects/
├── manifests/
└── system/
```

Use opaque IDs, optionally prefix-sharded:

```text
objects/01/<uuid>
objects/af/<uuid>
```

Never use the original filename as physical identity.

## Disaster Recovery

PostgreSQL is primary and must be backed up.

Later generate an encrypted per-file manifest containing:
- version
- file ID
- size
- chunk size
- file checksum
- ordered chunks
- provider/account
- remote object IDs
- per-chunk checksum

Manifest is recovery metadata, not the runtime database.

## Proton Integration Boundary

Do NOT implement Proton private cryptography/raw protocol in Go.

Preferred:

```text
Go Core
  ↓ gRPC streaming
TypeScript proton-adapter
  ↓
Official Proton Drive SDK
  ↓
Proton Drive
```

Eventually `proto/storage.proto` supports:
- Upload client streaming
- Download server streaming
- Delete
- Stat
- Usage
- Health

Go sends internal `account_id`; it does not send credentials on every request.

The adapter owns Proton-specific auth/session/client lifecycle/rate-limit translation.

Before Phase 2, inspect CURRENT official Proton Drive SDK documentation/source. Do not rely on stale assumptions in this document.

## Security

- Argon2id for Shardrive user password.
- Secure + HttpOnly + SameSite session cookie.
- No web auth token in localStorage.
- `storage_accounts` stores `credential_ref`, never raw Proton credentials.
- Provider credentials/session secrets encrypted at rest, e.g. AES-256-GCM.
- Master key comes from environment/Docker secret initially.
- Never commit or log credentials, passwords, cookies, tokens, or encryption keys.

## Concurrency

Bound everything.

Never create an unbounded goroutine per chunk.

Use:
- global semaphore
- per-account semaphore
- request context cancellation

## Remote + DB Transaction Rule

Provider and PostgreSQL cannot share one ACID transaction.

Chunk upload:
1. remote upload
2. DB transaction persists mapping/progress
3. DB failure => orphan cleanup later

Future rebalance:
1. copy destination
2. verify
3. update DB
4. delete old copy

Never delete the source before replacement is verified and committed.

## Testing Requirements

### Unit
At minimum:
- chunk-count calculation
- final chunk size
- placement filtering/scoring
- retry/backoff
- state transitions

### Integration with PostgreSQL + LocalProvider
- create upload
- upload all chunks
- complete
- download
- original SHA-256 == downloaded SHA-256
- interrupted upload resume
- duplicate chunk request is idempotent
- account full causes alternate placement
- offline account excluded
- remote success + DB failure is recoverable as orphan
- delete removes remote objects

### Phase 0 Gate

Automated test:

```text
generate file
→ SHA-256 original
→ upload
→ verify chunks distributed across multiple local accounts
→ complete
→ restart/recreate application without in-memory state
→ download
→ SHA-256 downloaded
```

`ORIGINAL_SHA256` MUST equal `DOWNLOADED_SHA256`.

Do not begin Proton integration before this passes.

## Development Phases

### Phase 0 — Core Local Storage Engine
Go + PostgreSQL + LocalProvider + placement + chunk upload + sequential download + integrity + integration tests.

### Phase 1 — SvelteKit
Login, file browser, browser chunking, upload queue, progress, pause/resume, storage dashboard, download/delete.

### Phase 2 — Proton Adapter, One Account
Current official SDK investigation, gRPC, TypeScript adapter, session handling, upload/download/delete/stat/usage/health.

### Phase 3 — Multi-Account Proton
5–10 configurable accounts, quota/health-aware placement, per-account concurrency, retry/rate-limit handling.

### Phase 4 — Reliability
Durable workers, cleanup, health/quota jobs, metrics, logging, DB backup, encrypted recovery manifests.

### Phase 5 — Advanced
Only after previous phases:
parallel download/prefetch, Range, rebalance, replication, Reed-Solomon, self-healing, additional providers.

## Explicitly Out of Scope for Initial V1

Do not add:
- Kubernetes
- core microservices
- Redis just for jobs
- Kafka
- Reed-Solomon
- deduplication
- content-defined chunking
- compression
- versioning
- additional cloud providers
- distributed Shardrive cluster

## Coding Standards

Go:
- Go 1.26+
- propagate `context.Context`
- constructor dependency injection
- no global mutable business state
- provider-neutral interfaces
- preserve `errors.Is/As`
- structured logging
- pgx preferred for PostgreSQL
- versioned migrations
- gofmt
- standard Go tests

Svelte/TypeScript:
- strict TypeScript
- separate API client from UI
- dedicated upload state module/store
- no provider credentials in frontend

SQL:
- UUID PKs
- foreign keys
- explicit constraints
- useful indexes
- no business-critical state existing only in process memory

## AI Agent Rules

For every coding session:

1. Read `AGENTS.md` and `PROGRESS.md`.
2. Inspect current code before editing.
3. Work on the next incomplete milestone.
4. Do not jump ahead to Proton/advanced features.
5. Do not silently change architecture.
6. If a change is necessary, record problem, proposal, tradeoff and migration impact.
7. Prefer a working vertical slice over unused abstractions.
8. Add relevant tests.
9. Run formatter/build/tests after meaningful changes.
10. Fix failures before claiming completion.
11. Update `PROGRESS.md` at the end.
12. Record exact tests/commands actually run.
13. Never fabricate test results.
14. Never commit real secrets.
15. Keep README startup instructions current.

## Definition of V1 Success

A user can:
1. start Shardrive with Docker Compose
2. log in
3. see one logical storage pool
4. upload a large file from Svelte
5. browser chunks the file
6. Go distributes chunks across multiple accounts
7. PostgreSQL persists mappings
8. restart the application
9. download the file
10. backend reconstructs it in order as a stream
11. downloaded SHA-256 equals original
12. delete the file and background cleanup removes stored objects

Prove this with LocalProvider first, then add ProtonProvider without rewriting the core storage engine.
