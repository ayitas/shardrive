# PROGRESS.md — Shardrive

Last updated: 2026-09-06

## Current State

**Phase:** Phase 2A — Proton SDK investigation and one-account spike preparation
**Status:** Phase 0 COMPLETE; Phase 1 product release-candidate browser gate passed;
Phase 2A investigation and provider-neutral boundary design are complete. The
one-account Proton adapter implementation remains intentionally gated on an
immutable SDK snapshot and a testable session-recovery environment.

Change review completed: frontend/auth/directory additions were audited, API
routes were synchronized in README, and no code rollback was required.

Phase 1 frontend scaffold is now present with strict TypeScript configuration
and a typed cookie-based API client. `npm run check` and `npm run build` now pass
with Node.js 24.20.0 LTS; npm reports four low-severity audit findings that
remain intentionally unmodified pending dependency review.

The browser upload queue is now implemented as a bounded `File.slice` worker
queue (three workers by default). It resumes from the server's completed chunk
indexes, supports pause/resume without cancelling in-flight requests, and only
calls completion after every scheduled chunk has finished.

The first user-scoped browser listing slice is also wired: PostgreSQL lists
non-deleted files for the configured user, the API exposes `GET /api/v1/files`,
and the Svelte page renders loading, empty, error, and file states.

Directory navigation groundwork is now user-scoped as well: `GET
/api/v1/directories` accepts an optional UUID `parentId`, applies a bounded
ordered query, and is exposed through the typed frontend client.

The frontend now uses that contract for root/child navigation, breadcrumbs,
and an explicit parent-directory action while filtering files by directory.

The Phase 1 Firefox gate found and fixed two frontend integration bugs: a
successful chunk worker did not decrement its in-flight counter, so a complete
upload could remain stuck at `UPLOADING`; and the API client attempted to parse
empty `202 Accepted` delete responses as JSON. Both fixes pass the frontend
check/build and the browser flow was rerun successfully.

Argon2id password hashing groundwork is now available in `internal/auth` with
PHC-style encoded hashes and constant-time verification. Login/session routes
remain pending until the durable session schema and cookie lifecycle are
implemented together.

The durable session foundation is now added: migration `000002_sessions` stores
only SHA-256 token digests, expiry and last-seen timestamps; the repository
supports create, expiry-aware lookup, and revoke operations.

Login, logout, and current-user HTTP handlers are now wired. They verify
Argon2id hashes, create/revoke durable sessions, and use Secure HttpOnly
SameSite cookies; the frontend client exposes matching typed methods.

The Svelte page now checks `/auth/me` on mount, presents a login form when
unauthenticated, clears the password after login, and provides cookie-backed
logout without browser token storage.

Frontend is now included in Docker Compose via `apps/frontend/Dockerfile` and
the `frontend` service on port 3000, using SvelteKit `adapter-node`. The rebuilt
container is healthy and its root endpoint returned HTTP 200.

Local frontend-to-API CORS is now enabled for localhost ports 3000, with
credentialed requests and OPTIONS preflight handling; the recreated API
returned the expected 204 preflight response.

A one-shot `bootstrap-user` API command now creates a development user with an
Argon2id password and prints only the resulting UUID/email, unblocking real
frontend-to-backend login/upload testing once that UUID is configured.

Development user bootstrap and Compose wiring were executed successfully. The
API login returned the test user UUID, `/api/v1/auth/me` returned the same UUID,
and the frontend root returned successfully on port 3000.

Upload UI is now connected to the bounded queue: file selection, directory-aware
upload creation, progress, pause/resume controls, and non-secret IndexedDB
session metadata persistence are implemented.

Chunk uploads now retry transient request failures up to five times by default
with exponential backoff and bounded jitter; worker concurrency remains capped.
The UI now exposes the active retry attempt while a chunk is backing off.

IndexedDB upload metadata can now be listed and deleted; successful completion
cleans the corresponding record while failed/interrupted sessions remain
available for later reconciliation.

On page load, pending upload metadata is now reconciled into a visible list and
can be removed explicitly. Resuming still requires reselecting the original
file and is kept as the next explicit UI action rather than guessing at file
identity.

Selecting a file with matching persisted name and size now resumes its existing
upload session, using the stored upload ID/chunk size and skipping completed
indexes. Successful completion removes that pending record.

Pending sessions now require explicit selection before resume, preventing an
ambiguous duplicate name/size from selecting the wrong upload.

User-scoped file and directory handler tests now cover unconfigured/invalid
identity responses, invalid `parentId`, and valid root listing responses.

Auth handler tests now cover fail-closed login and secure logout-cookie
attributes; the logout max-age precision bug was caught and fixed by using a
full negative second duration.

PostgreSQL session integration coverage now verifies durable creation, lookup,
and revocation against a fresh migrated database. The schema test now includes
the sessions table and expects both migrations.

The PostgreSQL auth integration test now exercises the complete login → secure
cookie → `/me` → logout → unauthorized flow with a real Argon2id password.
It also asserts wrong credentials return generic `401 invalid_credentials`
without issuing a cookie.

Codebase hygiene audit completed on 2026-09-06. Go tests/vet and Svelte
type-checking remain clean, and the frontend dependency graph was reviewed for
unused packages. The unused `@sveltejs/adapter-auto` dependency was removed;
the active production adapter is `@sveltejs/adapter-node`. The Proton protocol
boundary and ignored local build/configuration artifacts remain intentionally
because they are part of the planned Phase 2 boundary or local development
state, not dead tracked code.

Phase 2A now has an adapter-local encrypted session vault foundation in
`apps/proton-adapter`. It uses AES-256-GCM, authenticated account IDs, atomic
0600 file writes, restart recovery, and rejects path traversal. It deliberately
does not include Proton authentication or SDK types until the immutable SDK
snapshot and live one-account test environment are available.

Read `AGENTS.md` before doing any work.

## Locked Decisions

- [x] Name: Shardrive
- [x] Backend: Go 1.26+
- [x] Frontend: SvelteKit + TypeScript
- [x] Metadata DB: PostgreSQL
- [x] Modular monolith
- [x] Provider abstraction mandatory
- [x] LocalProvider must be implemented first
- [x] First real provider: Proton Drive
- [x] Preferred Proton boundary: TypeScript adapter + gRPC
- [x] Default chunk: 32 MiB
- [x] Initial browser upload concurrency: 3
- [x] PostgreSQL durable jobs initially
- [x] SHA-256 per chunk
- [x] Sequential download first
- [x] No Reed-Solomon in V1
- [x] No Redis requirement in V1
- [x] PostgreSQL is source of truth

Do not change these silently.

# Phase 0 Checklist

## 0.1 Bootstrap

- [x] Create repository layout
- [x] Initialize Go 1.26+ module
- [x] `.gitignore`
- [x] `.env.example`
- [x] Makefile
- [x] backend Dockerfile
- [x] Docker Compose PostgreSQL
- [x] Docker Compose API
- [x] README startup instructions
- [x] API health endpoint

Acceptance:
- [x] `docker compose up` works
- [x] API connects to PostgreSQL
- [x] health endpoint healthy
- [x] build/tests pass

## 0.2 PostgreSQL

Migrations:
- [x] users
- [x] directories
- [x] storage_accounts
- [x] files
- [x] upload_sessions
- [x] chunks
- [x] jobs
- [ ] secrets if implemented now (deferred until provider credentials are introduced)

Constraints:
- [x] UUID PKs
- [x] `UNIQUE(file_id, chunk_index)`
- [x] foreign keys
- [x] chunk ordering index
- [x] account/status indexes
- [x] pending job index

Acceptance:
- [x] fresh migrations pass
- [x] schema integration tests pass

## 0.3 Domain + Repositories

- [x] file states
- [x] chunk states
- [x] upload states
- [x] account states
- [x] typed errors
- [x] account repository
- [x] file repository
- [x] chunk repository
- [x] upload repository

Acceptance:
- [x] PostgreSQL repository tests pass
- [x] duplicate chunk race safely handled

## 0.4 Storage Provider

- [x] Provider interface
- [x] UploadRequest
- [x] StoredObject
- [x] ObjectInfo
- [x] StorageUsage
- [x] Provider Registry

Acceptance:
- [x] core services depend only on provider-neutral abstraction

## 0.5 LocalProvider

- [x] Upload
- [x] Download
- [x] Delete
- [x] Stat
- [x] Usage
- [x] Health
- [x] opaque object UUID
- [x] quota

Dev accounts:
- [x] local-1
- [x] local-2
- [x] local-3
- [x] local-4
- [x] local-5

Suggested quota: 1 GiB each.

Acceptance:
- [x] distinct account directories
- [x] quota enforcement
- [x] usage calculation
- [x] provider tests

## 0.6 Placement

- [x] ACTIVE filtering
- [x] capacity/reserve filtering
- [x] rate-limit filtering
- [x] priority score
- [x] load penalty
- [x] error penalty
- [x] previous-account diversity penalty
- [x] excluded accounts on retry
- [x] ErrInsufficientStorage
- [x] unit tests

Acceptance:
- [x] offline/full/disabled never selected
- [x] chunks spread over multiple accounts

## 0.7 Upload API

- [x] `POST /api/v1/uploads`
- [x] `GET /api/v1/uploads/{id}`
- [x] `GET /api/v1/uploads/{id}/chunks`
- [x] `PUT /api/v1/uploads/{id}/chunks/{index}`
- [x] `POST /api/v1/uploads/{id}/complete`

Behavior:
- [x] chunk count calculation
- [x] persist UPLOADING file/session
- [x] chunk index validation
- [x] chunk size validation
- [x] idempotent already-stored chunk
- [x] placement
- [x] streaming SHA-256
- [x] provider upload
- [x] DB mapping/progress transaction
- [x] complete validation
- [x] AVAILABLE transition
- [x] resume missing chunks

Acceptance:
- [x] multi-chunk upload
- [x] physical distribution across >1 account
- [x] duplicate upload safe
- [x] interrupted upload resumable

## 0.8 Sequential Download

- [x] `GET /api/v1/files/{id}/download`
- [x] AVAILABLE validation
- [x] metadata loaded before streaming
- [x] chunks ordered by index
- [x] sequence validation
- [x] correct provider/account lookup
- [x] sequential streaming
- [x] SHA-256 verification
- [x] Content-Length
- [x] Content-Type
- [x] safe Content-Disposition

Acceptance:
- [x] output equals original
- [x] still works after API restart
- [x] missing/corrupt chunk detected

## 0.9 Phase 0 Gate

Automated E2E:
- [x] generate test file
- [x] original SHA-256
- [x] upload chunks
- [x] verify distribution across accounts
- [x] complete
- [x] recreate/restart application state
- [x] download
- [x] downloaded SHA-256
- [x] compare

**GATE:**
- [x] `ORIGINAL_SHA256 == DOWNLOADED_SHA256`

Do not start Proton integration before this gate passes. The gate now passes;
Phase 1 frontend work may begin.

# Phase 1 — SvelteKit

Status: UNBLOCKED — Phase 0 gate passed.

- [x] initialize SvelteKit strict TypeScript
- [x] bounded browser `File.slice` upload queue (pause/resume and server resume)
- [x] user-scoped file listing API and basic browser rendering
- [x] user-scoped directory listing API and typed client method
- [x] basic directory breadcrumbs and parent navigation UI
- [x] login
- [x] file browser
- [x] folder navigation
- [x] folder creation
- [x] upload drop zone
- [x] File.slice chunking
- [x] 3-worker queue
- [x] progress
- [x] speed/ETA
- [x] pause/resume
- [x] retry
- [x] IndexedDB upload metadata
- [x] active upload status panel
- [x] pending upload resume/remove list
- [x] storage dashboard
- [x] human-readable storage meter
- [x] file download
- [x] file delete
- [x] file rename
- [x] folder rename

## Phase 1 Product Beta Scope

The next product hardening slice is intentionally limited to the virtual-drive
workflow before Proton integration:

- [x] upload queue status and pending-session actions
- [x] rename files and folders
- [x] safe folder deletion with descendant handling (empty folders only; non-empty returns 409)
- [x] sorting by name, size, and updated time
- [x] simple file search within the current user scope
- [x] responsive/mobile layout pass
- [x] Firefox and Chromium beta gate

# Phase 1 Release Candidate Gate

- [x] Docker Compose rebuilt and healthy
- [x] Firefox login/logout session flow
- [x] browser upload through `File.slice`
- [x] completed upload survives page reload/resume
- [x] sequential download from the UI
- [x] downloaded SHA-256 equals original SHA-256
- [x] `202 Accepted` file deletion handled by frontend
- [x] worker cleanup reaches `DELETED` and releases quota

Manual gate result on 2026-09-06:

```text
ORIGINAL_SHA256=4d87bf3d7436b73c9810ef8a5fabcfcbce3a79a17e66df91088d69347223b942
DOWNLOADED_SHA256=4d87bf3d7436b73c9810ef8a5fabcfcbce3a79a17e66df91088d69347223b942
file state after cleanup: DELETED
chunk state after cleanup: DELETED
storage usage after cleanup: 0 / 5368709120 bytes
```

The repeatable Firefox Playwright gate also passed on 2026-09-06 with
`npm run test:e2e`. It uses `E2E_EMAIL` and `E2E_PASSWORD` from the environment,
creates a 1 MiB in-memory fixture, compares the downloaded bytes and SHA-256,
then waits for the worker-backed delete cleanup.

Product polish now includes user-created folders, folder-scoped uploads, a
human-readable storage meter, clearer file/folder counts, and improved empty,
loading, and action states. The Firefox E2E gate also creates and enters a
unique folder before uploading its fixture.

The product gate also caught and fixed a real Svelte reactivity issue where the
storage API returned the configured 5 GiB pool but the UI remained at `0 B / 0
B`. Storage totals are now stored as direct reactive aggregates and Firefox
renders `0 B / 5.0 GiB` after cleanup.

The upload experience now has a dedicated active-upload panel with filename,
state, byte/chunk progress, speed/ETA, retry feedback, and pause/resume/cancel
actions. Pending IndexedDB sessions are presented as a compact resume/remove
list instead of an undifferentiated metadata block.

Product Beta now includes file and folder rename through user-scoped `PATCH`
routes. The browser gate caught the missing `PATCH` CORS allowance during this
slice; it is fixed and the renamed-folder upload/download/delete flow passes.

Exact frontend RC validation commands run:

```text
bash -lc 'nvm use --lts >/dev/null && npm install -D @playwright/test'
bash -lc 'nvm use --lts >/dev/null && npx playwright install firefox'
bash -lc 'nvm use --lts >/dev/null && E2E_EMAIL=e2e@example.test E2E_PASSWORD=e2e-local-only-password npm run test:e2e'
bash -lc 'nvm use --lts >/dev/null && npm run check && npm run build'
git diff --check
```

Final product-gate result:

```text
1 passed (6.9s)
```

After the upload queue panel changes, the rebuilt-container Firefox gate also
passed:

```text
1 passed (7.0s)
```

After the rename and CORS changes, the Firefox gate including folder rename also
passed:

```text
1 passed (7.0s)
```

After safe folder deletion was added, the rebuilt-container Firefox gate also
passed the renamed-folder → upload → download → file cleanup → empty-folder
deletion flow:

```text
1 passed (8.9s)
```

The product gate now also selects `Name A–Z` in the browser before executing
that flow. The async delete assertion intentionally waits for worker cleanup
before requiring the file row to disappear.

The same browser gate now verifies file search in the active folder: a matching
filename remains visible, a non-matching query hides it, and clearing the query
restores it.

The responsive pass stacks search/sort and folder actions below 640px, makes
file actions wrap vertically, and the Firefox gate now runs at 390x844 while
asserting there is no horizontal overflow.

Final search-slice Firefox result:

```text
1 passed (13.1s)
```

Final mobile-layout Firefox result:

```text
1 passed (11.0s)
```

The Playwright configuration now runs the Product Beta gate in both Firefox
and Chromium. Final two-browser result after installing Chromium:

```text
2 passed (9.8s)
```

# Phase 2 — Proton Adapter, One Account

Status: SPIKE ONLY — LocalProvider and the Phase 1 release-candidate browser
gate passed. Official SDK investigation completed; production integration is
conditional on a successful one-account adapter spike and session recovery
proof.

Before coding:
- [x] inspect current official Proton Drive SDK
- [x] record selected SDK source/version
- [x] document auth/session requirements
- [x] document breaking-change risk

## Phase 2A SDK Investigation — 2026-09-06

Sources reviewed:

- Official SDK repository: [`ProtonDriveApps/sdk`](https://github.com/ProtonDriveApps/sdk), `main`
- TypeScript package: [`@protontech/drive-sdk`](https://github.com/ProtonDriveApps/sdk/tree/main/client/js), source package version `0.0.1`
- Official TypeScript client README and package manifest
- Official Proton Drive SDK status and CLI documentation

Revalidation on 2026-09-06 confirmed the same constraints against the current
official sources. The JavaScript changelog currently identifies `js/v0.20.0`,
while the source package manifest still identifies version `0.0.1`; this is not
yet a sufficient immutable production pin. The live spike must record the
resolved npm tarball and the exact upstream commit together.

The published npm package was then checked directly on 2026-09-06: npm reports
`@protontech/drive-sdk@0.21.0`, which is now pinned exactly in the adapter lockfile.
The current upstream `main` ref resolves to
`c8d03244938a6b4d107c755df8904d7d971ed1c2`. The package was bundle-smoke-tested
successfully; direct unbundled Node ESM import is not usable because the package
entrypoint contains extensionless internal imports.

Findings:

- The TypeScript SDK exposes a public `ProtonDriveClient` with high-level node,
  upload, and download operations. Use only exported APIs; internal modules may
  change without warning.
- The SDK explicitly does not include authentication/login flows, session
  management, or the user address provider. The adapter must own those pieces.
- Proton's documentation says standalone SDK documentation is still being
  prepared. The official clients are the current living integration reference.
- Proton permits personal, non-commercial projects, but says the SDK is not yet
  ready for third-party production use and its public interface may change.
- Proton currently targets a cryptographic model migration for late 2026/early
  2027. Older SDK releases may stop interoperating after the service migration.
- Proton requires official endpoints, honest `x-pm-appversion` identification,
  event-based synchronization, bounded parallelism, and exponential backoff.
- The SDK handles Proton encryption and metadata processing; Shardrive must not
  reimplement Proton cryptography in Go.

Decision:

```text
PROCEED with a time-boxed one-account technical spike.
DO NOT claim Proton production readiness or start multi-account integration.
```

Phase 2B boundary design is now recorded in `proto/storage.proto` and keeps
credentials, Proton node IDs, and SDK types inside the adapter. Generated
bindings remain deferred until the adapter runtime is selected.

The session handoff design is now recorded in `apps/proton-adapter/README.md`:
the adapter owns authentication and encrypted session persistence, PostgreSQL
stores only `credential_ref`, and no password or provider token crosses gRPC.
This is a design decision only; session recovery remains unproven until a
real one-account test environment is available.

The official CLI login was verified locally on 2026-09-06. Its OS-secret-store
snapshot was validated and imported into the adapter's encrypted vault using a
test `credential_ref`; a separate process recovered it with file mode `0600`.
No token values were printed or committed. This proves the local session import
and vault recovery path, but not Proton SDK client construction or remote file
transfer.

Phase 2A implementation slice:

- [x] adapter TypeScript package/toolchain scaffold
- [x] adapter-local AES-256-GCM session vault
- [x] atomic encrypted session persistence with restart recovery test
- [x] file permissions and account-ID path safety tests
- [x] exact npm SDK package pin (`@protontech/drive-sdk@0.21.0`)
- [x] public-export bundle smoke test
- [x] injected Proton SDK client factory and auth-required state
- [x] validated Proton CLI session snapshot importer
- [x] imported and recovered one real CLI session into the encrypted vault
- [ ] live Proton session bootstrap and SDK client construction

Factory compatibility note: SDK 0.21.0's declaration graph currently pulls
TypeScript source from its crypto peer and fails this package's strict compiler
inside `node_modules`. The factory therefore uses a structural generic seam;
the bundled SDK runtime remains pinned and smoke-tested. Revisit this when the
SDK publishes declaration-compatible types.

Phase 2B boundary runtime slice:

- [x] Node gRPC runtime loads the provider-neutral proto contract
- [x] bounded client-streaming upload forwarding
- [x] bounded server-streaming download forwarding
- [x] unary delete/stat/usage/health handler wiring
- [x] provider-neutral error to gRPC status translation
- [x] in-process gRPC wire test with fake backend
- [ ] Proton SDK-backed storage backend

Spike exit criteria:

- [ ] confirm the immutable upstream commit/package relationship
- [ ] document the supported auth/session handoff into the SDK
- [ ] define encrypted credential/session persistence through `credential_ref`
- [ ] prove streaming upload and download through the adapter
- [ ] restart adapter and recover the session without re-authentication
- [ ] prove delete, stat, usage, health, and provider-neutral errors
- [ ] pass one-account checksum and remote cleanup tests

Primary risk: the SDK is evolving toward a cryptographic migration while
third-party production support and standalone integration documentation are not
yet available. Keep the Proton adapter isolated so this risk cannot alter the
LocalProvider core.

Implementation:
- [x] storage.proto provider-neutral contract
- [x] provider-neutral streaming Upload boundary
- [x] provider-neutral streaming Download boundary
- [x] provider-neutral Delete/Stat/Usage/Health boundary
- [x] TypeScript adapter runtime scaffold
- [ ] Proton session manager
- [x] encrypted session/credentials vault foundation
- [x] provider-neutral error translation
- [ ] Go ProtonProvider

Acceptance:
- [ ] repeated upload/download checksum tests
- [ ] adapter restart/session recovery
- [ ] delete
- [ ] quota/usage

# Phase 3 — Multi-Account Proton

Status: FUTURE — only after the one-account Proton spike passes.

- [ ] runtime account management
- [ ] 5 accounts
- [ ] test up to 10
- [ ] health
- [ ] quota refresh
- [ ] per-account semaphores
- [ ] global semaphore
- [ ] allocator across Proton
- [ ] alternate-account retry
- [ ] rate-limit exclusion
- [ ] auth-failure exclusion

# Phase 4 — Reliability

Status: FUTURE.

- [ ] worker
- [ ] PostgreSQL jobs
- [ ] DELETE_FILE
- [ ] expired upload cleanup
- [ ] orphan cleanup
- [ ] health jobs
- [ ] quota jobs
- [ ] structured logs
- [ ] Prometheus
- [ ] Grafana
- [ ] DB backup
- [ ] encrypted manifests
- [ ] disaster recovery test

# Phase 5 — Advanced

Status: FUTURE.

- [ ] bounded download prefetch
- [ ] HTTP Range
- [ ] rebalance
- [ ] replication
- [ ] chunk_objects schema
- [ ] Reed-Solomon
- [ ] self-healing
- [ ] additional providers

## Current Blockers

None. Phase 0.1 and 0.2 acceptance passed with Docker Engine 29.7.2, Docker
Compose v5.5.0, and the official PostgreSQL 18 Alpine image.

Compose runtime acceptance was rerun after an environment-resolution issue was
found: this environment requires the explicit `--env-file .env` flag when
using `-f deploy/docker-compose.yml`. With it, API, worker, and frontend were
recreated successfully; health, login, `/auth/me`, `/api/v1/accounts` (five
ACTIVE local accounts), and frontend HTTP 200 all passed.

Phase 0.3 acceptance passed against the running PostgreSQL 18 development
container. Eight concurrent attempts to store the same `(file_id, chunk_index)`
produced one authoritative chunk mapping and advanced upload progress once.

Phase 0.4 introduced the provider-neutral streaming contract and an injected,
concurrency-safe provider registry. No core package depends on provider-specific
SDK or protocol details.

LocalProvider filesystem operations pass unit, race, quota, isolation, and
repeat-run tests. Runtime provisioning idempotently creates `local-1` through
`local-N` only for a configured, existing Shardrive user; it never creates a
placeholder user or password.

Placement V1 passes filtering, component scoring, deterministic ordering,
retry exclusion, reserve-overflow, and sequential diversity tests. Ineligible
account states are never selected.

Upload API creation/status foundation passes unit, HTTP, and PostgreSQL tests.
File plus upload-session creation is atomic, sessions start in `UPLOADING`,
status queries are user-scoped, and resume responses expose ordered completed
indexes without relying on process memory.

Streaming chunk upload passes HTTP, race, LocalProvider, and live PostgreSQL
integration tests. It validates index and exact size, bounds global and
per-account work, streams SHA-256 without staging, retries an alternate account
only before request-body consumption, persists mapping/progress/account usage
atomically, and returns committed duplicates without consuming the body.

Upload completion passes HTTP, full race, and live PostgreSQL integration tests.
It rejects incomplete metadata, reconstructs chunks sequentially from their
recorded accounts, validates exact size and per-chunk SHA-256, calculates the
whole-file SHA-256, and atomically commits `AVAILABLE` plus `COMPLETED`.
Concurrent and repeated completion calls are idempotent. Same-size remote
content corruption marks the chunk `CORRUPT` and its file/session `FAILED`;
zero-byte completion stores the standard SHA-256 of empty input.

Sequential download passes handler, full race, and live PostgreSQL integration
tests. It rejects non-`AVAILABLE` and cross-user files, validates all database
metadata plus provider `Stat` results before HTTP streaming, reconstructs chunks
in order, and verifies per-chunk plus whole-file SHA-256. A recreated provider,
registry, repositories, and service download the original bytes using only the
same PostgreSQL database and storage root. Missing objects fail preparation;
same-size content corruption fails before the corrupt chunk is written.

The dedicated Phase 0 HTTP gate drives public create, chunk PUT, complete, and
download routes against a temporary PostgreSQL database. It closes the first
HTTP application server, recreates provider/registry/repositories/services and
a second HTTP server from the same database and storage root, then downloads
the same file. It also verifies chunks span multiple physical accounts.

Future risk: Proton Drive SDK/auth/session behavior can change. Re-check official sources before Phase 2.

No active frontend toolchain blocker. Node.js 24.20.0 LTS and npm 11.19.0 are
installed through NVM. npm audit currently reports four low-severity findings;
no forced upgrade was applied.

## Tests Actually Run

Successful on 2026-09-02:

```text
GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod make check
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./...
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test -race ./...
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go build -o /tmp/shardrive-api ./cmd/api
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go vet ./...
cd apps/backend && gofmt -l ./cmd ./internal
git diff --check
sg docker -c 'docker compose -f deploy/docker-compose.yml config -q'
sg docker -c 'docker compose -f deploy/docker-compose.yml up --build -d'
sg docker -c 'docker compose -f deploy/docker-compose.yml exec -T postgres pg_isready -U shardrive -d shardrive'
sg docker -c 'docker compose -f deploy/docker-compose.yml exec -T postgres psql -U shardrive -d shardrive -tAc "SELECT 1"'
curl --fail --silent --show-error http://127.0.0.1:8080/health/live
curl --fail --silent --show-error http://127.0.0.1:8080/health/ready
cd apps/backend && SHARDRIVE_TEST_DATABASE_URL=postgres://shardrive:shardrive-dev-only@127.0.0.1:5432/shardrive?sslmode=disable GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/db -run TestMigrateFreshDatabase -count=1 -v
```

Test packages passed:

```text
ok github.com/shardrive/shardrive/apps/backend/internal/config
ok github.com/shardrive/shardrive/apps/backend/internal/health
```

Runtime verification results:

```text
PostgreSQL: healthy, accepting connections, SELECT 1 returned 1
API: healthy
GET /health/live:  {"status":"ok"}
GET /health/ready: {"status":"ok"}
Migration 000001_initial: applied
Schema tables: 8 (schema_migrations plus 7 domain tables)
Fresh-database migration integration test: PASS
Temporary integration-test databases remaining: 0
```

Never mark tests as passing unless they were genuinely executed.

Successful on 2026-09-05:

```text
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./...
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go vet ./...
cd apps/backend && test -z "$(gofmt -l ./cmd ./internal ./migrations)"
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test -race ./...
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test -race ./internal/storage ./internal/account ./internal/chunk ./internal/domain ./internal/file ./internal/upload
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go build -o /tmp/shardrive-api ./cmd/api
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/storage/... ./...
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test -race ./internal/storage/... ./internal/account ./internal/chunk ./internal/domain ./internal/file ./internal/upload
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/storage/local -count=50
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test -race ./internal/account ./internal/config ./internal/repositorytest ./internal/storage/...
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/placement -count=1 -v
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test -race ./internal/placement ./internal/account ./internal/storage/...
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/placement -count=100
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test -race ./internal/upload ./internal/domain ./internal/storage/... ./internal/repositorytest
cd apps/backend && SHARDRIVE_TEST_DATABASE_URL=postgres://shardrive:shardrive-dev-only@127.0.0.1:5432/shardrive?sslmode=disable GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/repositorytest -run TestPostgresRepositoriesAndDuplicateChunkRace -count=1 -v
cd apps/backend && gofmt -w internal/upload/handler_test.go internal/repositorytest/repository_integration_test.go
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./...
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test -race ./internal/upload ./internal/chunk ./internal/placement ./internal/storage/...
cd apps/backend && SHARDRIVE_TEST_DATABASE_URL=postgres://shardrive:shardrive-dev-only@127.0.0.1:5432/shardrive?sslmode=disable GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/repositorytest -run TestPostgresRepositoriesAndDuplicateChunkRace -count=1 -v
cd apps/backend && gofmt -w internal/upload/service_test.go
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./... && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test -race ./internal/upload ./internal/chunk ./internal/placement ./internal/storage/... && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go vet ./... && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go build -o /tmp/shardrive-api ./cmd/api && test -z "$(gofmt -l ./cmd ./internal ./migrations)"
sg docker -c 'docker compose -f deploy/docker-compose.yml config -q && docker compose -f deploy/docker-compose.yml up --build -d && docker compose -f deploy/docker-compose.yml ps && curl --fail --silent --show-error http://127.0.0.1:8080/health/ready'
sg docker -c 'docker compose -f deploy/docker-compose.yml ps && curl --fail --silent --show-error http://127.0.0.1:8080/health/ready'
cd apps/backend && gofmt -w internal/upload internal/httpapi internal/repositorytest/repository_integration_test.go
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/upload ./internal/httpapi ./internal/repositorytest
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./...
cd apps/backend && SHARDRIVE_TEST_DATABASE_URL=postgres://shardrive:shardrive-dev-only@127.0.0.1:5432/shardrive?sslmode=disable GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/repositorytest -run TestPostgresRepositoriesAndDuplicateChunkRace -count=1 -v
cd apps/backend && gofmt -w internal/upload/service.go internal/upload/service_test.go internal/repositorytest/repository_integration_test.go
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./... && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test -race ./internal/upload ./internal/repositorytest ./internal/chunk ./internal/storage/...
cd apps/backend && SHARDRIVE_TEST_DATABASE_URL=postgres://shardrive:shardrive-dev-only@127.0.0.1:5432/shardrive?sslmode=disable GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/repositorytest -run TestPostgresRepositoriesAndDuplicateChunkRace -count=1 -v
cd apps/backend && SHARDRIVE_TEST_DATABASE_URL=postgres://shardrive:shardrive-dev-only@127.0.0.1:5432/shardrive?sslmode=disable GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test -race ./internal/repositorytest -run TestPostgresRepositoriesAndDuplicateChunkRace -count=1 -v
cd apps/backend && gofmt -w internal/repositorytest/phase0_e2e_integration_test.go && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/repositorytest -run TestPhase0HTTPRoundTripAfterApplicationRecreation -count=1 -v
cd apps/backend && SHARDRIVE_TEST_DATABASE_URL=postgres://shardrive:shardrive-dev-only@127.0.0.1:5432/shardrive?sslmode=disable GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/repositorytest -run TestPhase0HTTPRoundTripAfterApplicationRecreation -count=1 -v
cd apps/backend && SHARDRIVE_TEST_DATABASE_URL=postgres://shardrive:shardrive-dev-only@127.0.0.1:5432/shardrive?sslmode=disable GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test -race ./internal/repositorytest -run TestPhase0HTTPRoundTripAfterApplicationRecreation -count=1 -v
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./... && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test -race ./... && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go vet ./... && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go build -o /tmp/shardrive-api ./cmd/api && test -z "$(gofmt -l ./cmd ./internal ./migrations)"
cd apps/frontend && bash -lc 'nvm use --lts >/dev/null && node --version && npm --version && npm install'
cd apps/frontend && bash -lc 'nvm use --lts >/dev/null && npm run check && npm run build'
python3 -m json.tool apps/frontend/package.json
python3 -m json.tool apps/frontend/tsconfig.json
git diff --check
python3 -m json.tool apps/frontend/package.json
python3 -m json.tool apps/frontend/tsconfig.json
git diff --check
git diff --check
cd apps/backend && test -z "$(gofmt -l ./cmd ./internal ./migrations)"
sg docker -c 'docker compose -f deploy/docker-compose.yml up --build -d'
sg docker -c 'docker compose -f deploy/docker-compose.yml ps && curl --fail --silent --show-error http://127.0.0.1:8080/health/ready && curl --silent --show-error --include http://127.0.0.1:8080/api/v1/files/11111111-1111-4111-8111-111111111111/download'
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./... && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test -race ./... && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go vet ./... && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go build -o /tmp/shardrive-api ./cmd/api && test -z "$(gofmt -l ./cmd ./internal ./migrations)"
git diff --check
sg docker -c 'docker compose -f deploy/docker-compose.yml config -q && docker compose -f deploy/docker-compose.yml up --build -d && docker compose -f deploy/docker-compose.yml ps && curl --fail --silent --show-error http://127.0.0.1:8080/health/ready'
sg docker -c 'docker compose -f deploy/docker-compose.yml ps && curl --fail --silent --show-error http://127.0.0.1:8080/health/ready'
sg docker -c 'docker compose -f deploy/docker-compose.yml up --build -d'
sg docker -c 'docker image inspect shardrive-api:latest --format "latest={{.Id}} created={{.Created}}" && docker inspect shardrive-api-1 --format "container_image={{.Image}} started={{.State.StartedAt}}" && docker compose -f deploy/docker-compose.yml ps && curl --fail --silent --show-error http://127.0.0.1:8080/health/ready'
cd apps/backend && gofmt -w cmd internal && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./...
cd apps/backend && gofmt -w internal/repositorytest/repository_integration_test.go && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./...
cd apps/backend && SHARDRIVE_TEST_DATABASE_URL=postgres://shardrive:shardrive-dev-only@127.0.0.1:5432/shardrive?sslmode=disable GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/repositorytest -run TestPostgresRepositoriesAndDuplicateChunkRace -count=1 -v
cd apps/backend && gofmt -w internal/download internal/repositorytest/repository_integration_test.go && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./... && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test -race ./internal/download ./internal/file ./internal/repositorytest ./internal/storage/...
cd apps/backend && SHARDRIVE_TEST_DATABASE_URL=postgres://shardrive:shardrive-dev-only@127.0.0.1:5432/shardrive?sslmode=disable GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/repositorytest -run TestPostgresRepositoriesAndDuplicateChunkRace -count=1 -v
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./...
cd apps/backend && SHARDRIVE_TEST_DATABASE_URL=postgres://shardrive:shardrive-dev-only@127.0.0.1:5432/shardrive?sslmode=disable GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/repositorytest -run TestPostgresRepositoriesAndDuplicateChunkRace -count=1 -v
cd apps/backend && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./... && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test -race ./... && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go vet ./... && GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go build -o /tmp/shardrive-api ./cmd/api && test -z "$(gofmt -l ./cmd ./internal ./migrations)"
cd apps/backend && SHARDRIVE_TEST_DATABASE_URL=postgres://shardrive:shardrive-dev-only@127.0.0.1:5432/shardrive?sslmode=disable GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test -race ./internal/repositorytest -run TestPostgresRepositoriesAndDuplicateChunkRace -count=1 -v
sg docker -c 'docker compose -f deploy/docker-compose.yml config -q'
sg docker -c 'docker compose -f deploy/docker-compose.yml up --build -d && docker compose -f deploy/docker-compose.yml ps && curl --fail --silent --show-error http://127.0.0.1:8080/health/ready'
sg docker -c 'docker compose -f deploy/docker-compose.yml config -q && docker compose -f deploy/docker-compose.yml up --build -d && docker compose -f deploy/docker-compose.yml ps && docker compose -f deploy/docker-compose.yml exec -T api sh -c "id && stat -c '\''%U:%G %a %n'\'' /data/storage" && curl --fail --silent --show-error http://127.0.0.1:8080/health/ready'
sg docker -c 'docker compose -f deploy/docker-compose.yml exec -T api sh -c "grep '\''^Uid:\|^Gid:'\'' /proc/1/status && stat -c '\''%U:%G %a %n'\'' /data/storage"'
sg docker -c 'docker compose -f deploy/docker-compose.yml ps'
sg docker -c 'docker compose -f deploy/docker-compose.yml up --build -d && docker compose -f deploy/docker-compose.yml ps && curl --fail --silent --show-error http://127.0.0.1:8080/health/ready && test "$(curl --silent --output /tmp/shardrive-upload-response --write-out "%{http_code}" -X POST -H "Content-Type: application/json" --data "{\"name\":\"smoke.bin\",\"size\":1}" http://127.0.0.1:8080/api/v1/uploads)" = 503 && grep -q not_configured /tmp/shardrive-upload-response'
sh -n apps/backend/docker-entrypoint.sh
git diff --check
```

Latest repository integration results:

```text
=== RUN   TestPostgresRepositoriesAndDuplicateChunkRace
--- PASS: TestPostgresRepositoriesAndDuplicateChunkRace (0.36s)
PASS

Race detector: PASS (test 0.48s, package 1.501s)
Latest upload-complete-download integration: PASS (test 0.47s, package 0.481s)
Latest upload-complete-download race integration: PASS (test 0.57s, package 1.595s)
Phase 0 HTTP gate: PASS (test 0.24s, package 0.247s).
ORIGINAL_SHA256=afa570bfc21d952abff9435233ffa17aa02b81846a6ea1358d1a0aac15ccd4a7
DOWNLOADED_SHA256=afa570bfc21d952abff9435233ffa17aa02b81846a6ea1358d1a0aac15ccd4a7
RECREATED_ORIGINAL_SHA256=afa570bfc21d952abff9435233ffa17aa02b81846a6ea1358d1a0aac15ccd4a7
RECREATED_DOWNLOADED_SHA256=afa570bfc21d952abff9435233ffa17aa02b81846a6ea1358d1a0aac15ccd4a7
Phase 0 HTTP gate race: PASS (test 0.29s, package 1.309s).
```

Local account/runtime verification results:

```text
ProvisionLocal created local-1 through local-5 for an existing test user.
Second provisioning preserved account UUIDs and safely updated quota/workers.
Each account received a distinct LocalProvider object directory.
Compose API and PostgreSQL containers: healthy.
API process UID/GID: 100/101 (shardrive user/group).
/data/storage ownership/mode: shardrive:shardrive 0750.
GET /health/ready: {"status":"ok"}
POST /api/v1/uploads without configured user: 503 not_configured
Persisted STORED chunk resume result: completedIndexes [0]
Latest rebuilt API and PostgreSQL containers: healthy.
Latest GET /health/ready: {"status":"ok"}
Completion-enabled API container recreated and started at
2026-09-05T06:47:49Z; subsequent Compose status reported healthy.
Download-enabled API image built successfully and the API container was
recreated. API/PostgreSQL status: healthy. Download route without a configured
Phase 0 user: HTTP 503 `not_configured`. Latest readiness: {"status":"ok"}.
Frontend verification: Node.js v24.20.0, npm 11.19.0; `npm run check` passed
with 0 errors/0 warnings and `npm run build` passed. npm install reported
four low-severity audit findings; no forced upgrade was applied.
The same check/build command was rerun after adding the bounded upload queue and
again passed with 0 errors/0 warnings.
```

The first combined completion-image rebuild output ended at the image compile
step before it printed container confirmation. It was not treated as proof of
deployment; `docker compose up --build -d` was rerun, then image/container IDs,
the new start timestamp, Compose health, and readiness were checked explicitly.

One verification command was initially invoked from the repository root and
failed before running tests because the Go module is under `apps/backend`:

```text
GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test -race ./internal/storage ./internal/account ./internal/chunk ./internal/domain ./internal/file ./internal/upload
go: cannot find main module
```

The same command was rerun successfully from `apps/backend`, as recorded above.

A read-only router search was also invoked with a redundant `apps/backend`
prefix while already inside that directory; `rg` reported the path missing.
No files were changed by that search, and the following formatter/tests in the
same command completed successfully.

## Architecture Decisions Added in Phases 0.3–0.8

- Domain IDs cross repository boundaries as canonical UUID strings; PostgreSQL
  remains responsible for generating and validating UUID values.
- State transitions are explicit and validated before conditional database
  updates. A stale expected state returns a typed conflict.
- Chunk mapping insertion and upload progress advancement share one PostgreSQL
  transaction. `UNIQUE(file_id, chunk_index)` is the concurrency authority.
- An identical duplicate chunk returns the stored mapping without incrementing
  progress; a duplicate with different size or checksum returns a typed conflict.
- Provider selection uses an injected registry keyed by the database provider
  name. There is no package-global mutable registry.
- Provider errors expose stable categories for not-found, conflict, quota,
  availability, and invalid requests while allowing implementations to wrap
  their underlying causes.
- LocalProvider derives account directories from validated account UUIDs and
  stores objects under prefix-sharded opaque UUID paths. Logical filenames are
  never used as physical identities.
- Local uploads stream into a same-directory temporary file, validate the exact
  declared size, sync it, then publish through a no-overwrite hard link. Failed
  and cancelled uploads do not expose partial objects.
- Per-account locks serialize quota-sensitive mutations while allowing usage
  reads to proceed independently across accounts. Delete is idempotent.
- Local development account provisioning requires an existing user UUID,
  creates `local-1` through configurable `local-N` transactionally, and safely
  reconciles quota and worker limits without deleting accounts when N shrinks.
- The API registers LocalProvider at startup and initializes configured account
  directories. Compose persists them in a dedicated `local-storage` volume.
- The container entrypoint repairs only the storage-root ownership as root and
  then replaces itself with the API under the unprivileged `shardrive` user.
- Placement uses `0.60 × free-capacity ratio + 0.20 × normalized priority
  − 0.15 × upload-load ratio − 0.05 × recent-error rate`. Equal priorities get
  a neutral midpoint contribution, and stable free-space/priority/UUID
  tie-breakers make ranking deterministic.
- The specification leaves diversity-penalty magnitude open. V1 uses a
  configurable `0.10` penalty for the previous account: this spreads equal
  accounts while still allowing a materially healthier account to win. It
  requires no schema migration and can be tuned when production metrics exist.
- Account reserve is configurable through the placement engine and defaults to
  `max(256 MiB, 4 × chunkSize)`. Overflow is rejected rather than wrapped.
- Phase 0 HTTP requests use the configured existing-user UUID as a temporary
  single-user identity. If it is absent, upload routes fail closed with 503.
  Phase 1 authentication will replace this resolver; clients never send a user
  ID or provider credential in request bodies.
- File and upload-session rows are created in one PostgreSQL transaction.
  Optional directories must belong to the same user, preventing cross-user
  virtual-filesystem references without a schema migration.
- Chunk count uses division/remainder instead of `size + chunkSize - 1`, avoiding
  integer overflow. Counts exceeding PostgreSQL `integer` range are rejected.
- Upload concurrency is bounded by one configurable process-wide semaphore and
  one semaphore per storage account using its persisted worker limit. Waiting
  for either honors request cancellation.
- Alternate-account retry is safe only when a retryable provider error occurs
  before any request-body byte is consumed. A consumed streaming body is never
  replayed implicitly.
- The remote object UUID is generated before upload. Remote success followed by
  a database failure deliberately leaves recoverable orphan data; it does not
  pretend that provider and PostgreSQL operations are one transaction.
- Chunk mapping, upload progress, and metadata-account usage advance in the same
  PostgreSQL transaction. The unique `(file_id, chunk_index)` constraint remains
  the duplicate authority, and a losing duplicate upload is deleted best-effort.
- Completion verification reads providers sequentially and hashes each chunk
  into both its per-chunk SHA-256 and one continuous whole-file SHA-256. It never
  stages the complete file or derives a fake whole-file hash from chunk hashes.
- Provider verification happens before metadata state mutation. The final
  transaction revalidates counts, bytes, indexes, upload/file ownership, and
  states, then passes file/session through `VERIFYING` to
  `AVAILABLE`/`COMPLETED` atomically.
  A crash or transient provider failure before that transaction leaves the
  upload retryable in `UPLOADING`.
- Missing or checksum/size-mismatched remote objects are integrity failures.
  Their chunk becomes `CORRUPT` in the same transaction that marks the file and
  upload session `FAILED`. Provider availability errors do not falsely mark
  immutable metadata as corrupt.
- Download preparation is user-scoped and requires an `AVAILABLE` file with a
  whole-file checksum. It loads ordered chunks and user accounts, validates the
  full sequence/layout, resolves providers, and stats every remote object before
  the handler commits HTTP headers.
- Download provider access is sequential per file and bounded across requests
  by each account's persisted `max_download_workers` value. Semaphore waits
  honor request cancellation.
- Download buffers at most one configured chunk, not the complete file. This
  lets exact size and per-chunk SHA-256 be validated before that chunk reaches
  the client, while one continuous hasher validates the reconstructed file.
  If a later chunk fails, `Content-Length` makes the truncated response visible.
- `Content-Type` is parsed and normalized with an octet-stream fallback.
  `Content-Disposition` is generated through the MIME formatter rather than
  interpolating filenames, preventing header injection.
- The Phase 0 acceptance gate uses public HTTP handlers with a temporary
  PostgreSQL database, then recreates the application server, provider registry,
  repositories, and services against the same persistent state. It records the
  original and downloaded SHA-256 values and requires exact equality.

## Important Notes

1. Do not start with Proton.
2. Prove LocalProvider storage engine first.
3. Reconstruction must survive process restart using PostgreSQL.
4. Whole-file backend staging is not the normal architecture.
5. Go core remains provider-neutral.
6. V1 accepts that losing one account can make a file unavailable; resilience coding comes later.
7. Use opaque remote object IDs.
8. Remote upload success + DB failure creates an orphan and must be recoverable.

## Next 3 Tasks

1. Add provider-neutral account health/quota refresh actions and durable status updates.
2. Add frontend component/route tests for account status rendering and degraded states.
3. Revisit the Proton adapter only after selecting a reproducible official-runtime dependency strategy.

Current sequencing note: retry status is implemented; auth rate limiting now
uses durable PostgreSQL state so security state never exists only in one API
process.

Durable login-attempt storage is now migrated (`auth_login_attempts`) with
atomic window counting, lock expiry recording, normalization, and reset. The
handler now enforces five failures per 15-minute window followed by a 15-minute
lock, resets counters on successful login, and preserves generic responses.

Integration coverage now proves lockout enforcement: five wrong passwords return
generic unauthorized responses and a correct password remains rejected while
the PostgreSQL-backed lock is active.

Durable job repository groundwork is now present for cancellation cleanup:
`Enqueue` persists JSON payloads and `Claim` uses `FOR UPDATE SKIP LOCKED`,
increments attempts, and records worker ownership. The cleanup worker and
cancel endpoint remain next so remote deletion is never falsely reported done.

Chunk cleanup now has an atomic `MarkDeleted` repository operation that updates
chunk state and releases the exact account quota in one PostgreSQL transaction;
the remote provider delete must succeed before this operation is called.

Upload service now exposes a user-scoped `Cancel` operation that transitions an
active session to `CANCELLED` and its file to `DELETING` when configured. It
does not claim cleanup completion; durable enqueue and remote deletion remain
separate.

`DELETE /api/v1/uploads/{id}` now validates the session, cancels it, and enqueues
`CLEANUP_UPLOAD` with the authoritative file ID, returning `202 Accepted`.

The cleanup worker now also exposes a context-aware polling `Run` loop with a
bounded ticker interval and clean shutdown behavior; empty queues/errors do not
crash the process.

The worker is now built as `cmd/worker`, included in the backend image, and
defined as a Compose `worker` service sharing PostgreSQL and LocalProvider
storage. Compose configuration validation passes.

The worker image was built and the Compose service started successfully; its
logs report `cleanup worker started` and it remains running with an empty queue.

`internal/job` now includes a provider-neutral cleanup worker that claims
`CLEANUP_UPLOAD`, deletes mapped remote objects, calls `MarkDeleted`, completes
only after all chunks succeed, and retries/fails jobs on errors. File/session
terminal-state transitions remain the next hardening step.

PostgreSQL integration coverage now validates job enqueue, JSON payload storage,
`SKIP LOCKED` claim metadata/attempt increment, completion, and exclusion of
completed jobs from subsequent claims.

Cleanup worker integration coverage now proves the full deletion path against
PostgreSQL and LocalProvider: a claimed `CLEANUP_UPLOAD` job deletes the remote
object, marks the chunk `DELETED`, releases the account's used quota, and
transitions a `DELETING` file to `DELETED`. The worker command now injects the
file repository so this terminal transition is durable.

HTTP/PostgreSQL integration coverage now proves `DELETE /api/v1/uploads/{id}`
transitions the session to `CANCELLED`, the file to `DELETING`, and persists a
pending `CLEANUP_UPLOAD` job whose payload contains the authoritative file ID.
Repeated cancellation returns `409 CONFLICT` and does not enqueue a duplicate
cleanup job. Credentialed CORS now also allows the `DELETE` method required by
the browser cancel action.

The frontend API client and bounded upload queue now support cancellation. A
cancel request marks the queue `CANCELLED`, stops scheduling new chunks while
allowing in-flight requests to finish, calls the durable backend cancellation
endpoint, and removes the cancelled session from IndexedDB. The Svelte page now
exposes a Cancel upload control alongside Pause/Resume.

Persisted upload sessions are reconciled against the backend on page load:
terminal sessions (`COMPLETED`, `CANCELLED`, `FAILED`, `EXPIRED`) are removed,
while active sessions refresh their completed chunk indexes before being saved
again. This prevents stale IndexedDB metadata from presenting unusable resumes.

The upload form now accepts drag-and-drop files through an explicit drop zone in
addition to the file picker, while preserving the existing bounded chunk queue.

Upload progress now reports measured completed-byte throughput in MiB/s and a
remaining-seconds ETA, derived from the queue's actual completion timestamps.

The storage dashboard slice is now wired end-to-end: `GET /api/v1/accounts`
returns user-scoped account quota/status summaries, and the frontend displays
aggregate used versus total bytes without exposing credentials.

The storage dashboard now also renders each configured account with provider,
state, per-account usage, and a clear warning when the account is not `ACTIVE`.
This keeps the product's operational state visible without exposing credential
references or coupling the frontend to Proton-specific details.

Storage account refresh is now provider-neutral and durable. `POST
/api/v1/accounts/{accountId}/refresh` verifies ownership, calls the registered
provider health/quota methods, maps provider failures to stable account states,
and persists the observation in PostgreSQL. The dashboard exposes a per-account
Refresh action; provider-specific errors are reduced to safe error codes and
never persisted as raw provider messages.

Storage API handlers now resolve the authenticated user ID from the validated
PostgreSQL session context (with the configured ID retained only for isolated
Phase 0/test routers). Production routing requires a valid session for upload,
file, directory, account, and download endpoints, preventing cross-user data
access through a static request identity.

File deletion is now available from the browser and API. `DELETE
/api/v1/files/{id}` transitions an available/degraded file to `DELETING` and
enqueues durable `DELETE_FILE` cleanup; the existing worker removes remote
chunks before transitioning the file to `DELETED`.

Cleanup retry exhaustion now transitions a still-deleting file to `FAILED`
after the durable job reaches its final attempt; transient failures remain
`DELETING` while the job is rescheduled. This keeps terminal metadata state
durable and prevents files from appearing indefinitely active after unrecoverable
remote deletion errors.

Frontend verification commands run:

```text
npm run check
npm run build
GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/job ./internal/repositorytest
```

Both completed successfully with zero Svelte diagnostics; the build uses
`@sveltejs/adapter-node`.

Exact verification commands run in this session:

```text
gofmt -w internal/job/cleanup.go cmd/worker/main.go internal/repositorytest/worker_integration_test.go
SHARDRIVE_TEST_DATABASE_URL=postgres://shardrive:shardrive-dev-only@127.0.0.1:5432/shardrive?sslmode=disable GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/repositorytest -run TestPostgresCleanupWorkerDeletesRemoteChunk -count=1 -v
SHARDRIVE_TEST_DATABASE_URL=postgres://shardrive:shardrive-dev-only@127.0.0.1:5432/shardrive?sslmode=disable GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/repositorytest -run TestPostgresUploadCancellationEnqueuesCleanup -count=1 -v
GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./...
GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go vet ./...
git diff --check
```

Frontend account dashboard verification in this session:

```text
nvm use --lts >/dev/null && npm run check && npm run build
```

Both frontend commands completed successfully; `svelte-check` reported zero
errors and zero warnings, and the production adapter-node build completed.

Account refresh verification in this session:

```text
gofmt -w internal/account/account.go internal/account/account_test.go internal/account/handler.go internal/account/repository.go internal/httpapi/router.go internal/storage/errors.go cmd/api/account_refresh.go cmd/api/main.go
GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./internal/account ./internal/httpapi ./internal/storage ./cmd/api
GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go test ./...
GOCACHE=/tmp/shardrive-go-build GOMODCACHE=/tmp/shardrive-go-mod go vet ./...
git diff --check
```

All completed successfully. No live Proton transfer was added; the official
CLI's unpublished account runtime remains an explicit Phase 2 integration
blocker rather than being replaced with a non-streaming CLI shim.

All commands completed successfully; no blockers remain for this milestone.

At the end of every AI coding session:
- update this file
- list exact commands/tests run
- record blockers
- record architectural decisions
- set the next three concrete tasks
