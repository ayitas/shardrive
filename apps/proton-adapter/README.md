# Proton adapter

Phase 0 and the Phase 1 browser acceptance gates have passed. The Phase 2
one-account Proton product gate is complete, including live transfer,
restart/session recovery, Compose deployment, account lifecycle, and the
Firefox browser gate. Phase 3 multi-account support has not started yet.

This adapter is intentionally isolated and should not be treated as a
production-certified Proton client: the official SDK and its cryptographic
model are still evolving. The next product slice is two independently
configured accounts while preserving this boundary.

The official TypeScript SDK currently exposes Drive operations such as upload
and download, while authentication and session management remain outside the
SDK. The SDK public interface and cryptographic model are still evolving, so
all Proton-specific code must remain isolated here. Do not put Proton
cryptography or SDK types in the Go core.

The exact SDK commit/package snapshot and session handoff design are recorded
below. The completed one-account acceptance gate covers adapter restart,
checksum, delete, quota, health, and provider-neutral error translation.

## Phase 2A decision record

The adapter now pins the published npm package `@protontech/drive-sdk@0.21.0`
in `package.json` and `package-lock.json`. The current official repository
`main` reference was recorded as commit
`c8d03244938a6b4d107c755df8904d7d971ed1c2`; this commit/package relationship
must still be confirmed before calling the live spike reproducible.

The package's direct Node ESM entrypoint currently fails to resolve its own
extensionless internal imports. The adapter therefore treats bundling as a
required runtime build step for SDK code; `npm run sdk:smoke` bundles only the
public exports and executes the result successfully. Do not import the SDK
directly from an unbundled Node entrypoint.

The SDK does not provide authentication, login, session management, or a user
address provider. The intended Shardrive handoff is therefore:

1. An explicit adapter-owned setup flow authenticates the Proton account.
2. The adapter persists the resulting session and SDK cache encrypted at rest.
3. PostgreSQL stores only an opaque `credential_ref`; no password, token, or
   Proton node ID crosses the Go/gRPC boundary.
4. On restart, the adapter resolves `account_id` to `credential_ref`, decrypts
   the session in memory, reconstructs the SDK client, and reports an explicit
   `AUTH_REQUIRED`/`AUTH_FAILED` state if recovery is not possible.
5. The Go core sees only provider-neutral upload, download, delete, stat,
   usage, and health results.

No password-over-gRPC flow is permitted. The session recovery path was proven
with the real one-account gate before any future placement expansion.
Multi-account must add independent session selection and failure isolation
without moving credentials into Go.

`src/proton-client-factory.ts` now codifies this handoff: a session provider
loads account-scoped SDK constructor parameters, the factory returns
`AUTH_REQUIRED`-equivalent `ProtonAuthRequiredError` when no session exists,
and the SDK constructor is injected so the bundled runtime is explicit. The
factory test uses no Proton credentials or network calls.

`src/proton-cli-session.ts` accepts the official CLI's documented session
snapshot shape and writes only validated fields through the encrypted store
boundary. This importer is intended for a local test bootstrap using the CLI's
opt-in `unsafe_file` store; it must not become the default production
credential store. The default production path remains an OS secret store or
the adapter-owned encrypted store.

The local test machine has completed the official CLI browser login. The CLI's
OS-secret-store snapshot was imported into the adapter vault under an internal
test account reference and recovered in a separate process. Only metadata was
printed; token values remain encrypted outside the repository. The later live
one-account gate proves SDK client construction and remote transfer as well.

The factory intentionally uses a structural generic contract instead of
importing the SDK's declaration graph into the adapter compiler. SDK 0.21.0's
public declaration entry currently pulls TypeScript source from its crypto peer,
which violates this package's strict compiler settings with errors inside
`node_modules`. The bundled runtime remains pinned and smoke-tested; this seam
can adopt official SDK types once that declaration compatibility is fixed.

The initial gRPC boundary is defined in [`proto/storage.proto`](../../proto/storage.proto):
one metadata frame plus bounded data frames for upload, bounded server frames
for download, and unary delete/stat/usage/health calls. The adapter receives
only Shardrive's internal `account_id`; credential and session lookup remain
inside the adapter.

The provider-neutral Node runtime now loads this contract dynamically with
`@grpc/proto-loader`. `src/grpc-server.ts` provides bounded client-streaming
upload, server-streaming download, unary operations, cancellation-safe async
chunk forwarding, and translation from adapter errors to gRPC status codes.
Its `StorageBackend` interface is deliberately separate from Proton SDK types;
the current integration test uses an in-memory fake backend and proves the
wire contract without claiming a Proton connection.

## Runtime decision after official-source verification

The official repository source at commit
`c8d03244938a6b4d107c755df8904d7d971ed1c2` was installed in a temporary
checkout with Bun. Its internal `client/js` and
`incubating/account/js` workspaces were installed, and the official CLI bundle
completed successfully (`1067 modules`, `12.92 MB`). This confirms that the
SDK, account runtime, and crypto dependencies can be bundled together from the
same pinned source tree.

The CLI remains unsuitable as Shardrive's provider implementation: its upload
and download commands require local filesystem paths and do not expose the
provider-neutral streaming object contract. The live adapter therefore reuses
the pinned official source/runtime pieces in a dedicated entry point rather
than invoking the CLI as a file-transfer shim.

The reproducible read-only runtime probe is available as
`npm run proton:runtime:probe`. Set `PROTON_SDK_SOURCE_DIR` to the checked-out
official source tree at the pinned commit. The probe bundles the SDK,
account, and crypto runtime, reads the existing OS-secret-store session,
constructs a `ProtonDriveClient`, and reads only the My Files root metadata.
It prints no credential, token, filename, or node ID.

The probe also contains an opt-in live transfer gate. Only run it when a test
object may be created and permanently deleted in the logged-in Proton account:

```text
PROTON_SDK_SOURCE_DIR=/path/to/proton-sdk \
SHARDRIVE_PROTON_LIVE_TRANSFER=1 \
npm run proton:runtime:probe
```

The gate uploads a small generated payload, downloads it through the backend,
compares SHA-256, and deletes the resulting Proton node in a `finally` block.
It is disabled by default and has not been run as part of ordinary adapter
validation. The probe installs empty SDK log handlers so transfer tokens and
other request details are not written to stdout.

`src/proton-storage-backend.ts` now provides the provider-neutral stream bridge
for the official SDK shape. It uses the SDK's uploader/downloader streams,
performs Proton trash-then-permanent-delete, maps node metadata to Shardrive
stat results, and delegates account health/quota to the runtime factory. Its
10-test adapter suite uses a fake SDK runtime; the source-pinned runtime
factory is now used by the adapter process entrypoint below.

## Adapter process entrypoint

### Local session setup

Before connecting the account in the dashboard, import the already-authenticated
official CLI session into the adapter vault from the OS keychain:

```text
export SHARDRIVE_PROTON_MASTER_KEY_B64="$(openssl rand -base64 32)"
export SHARDRIVE_PROTON_ACCOUNT_REF=account-1
export SHARDRIVE_PROTON_SESSION_ROOT=/var/lib/shardrive/proton-sessions
npm run proton:session:import
```

The command reads only the official CLI OS-secret-store entry, validates its
shape, encrypts it with AES-256-GCM, writes a mode `0600` account file, and
prints only the opaque reference and vault directory. Persist the master key
through a Docker secret or equivalent protected environment; never put it in
the repository or send it through the HTTP/gRPC API. Use the same
`account_ref` as the dashboard's `credentialRef`.

`npm run proton:adapter` builds and starts the real gRPC adapter process. It
requires the pinned SDK source, an encrypted session directory, and a 32-byte
master key supplied as base64:

```text
PROTON_SDK_SOURCE_DIR=/path/to/proton-sdk \
SHARDRIVE_PROTON_MASTER_KEY_B64=<base64-32-byte-key> \
SHARDRIVE_PROTON_SESSION_ROOT=/var/lib/shardrive/proton-sessions \
SHARDRIVE_PROTON_GRPC_ADDRESS=0.0.0.0:50051 \
npm run proton:adapter
```

The entrypoint bundles the official SDK from the pinned source tree, keeps the
gRPC/protobuf packages external to the SDK bundle, loads sessions through
`FileSessionStore`, and starts `StorageAdapter`. It does not print session
contents or credentials. The current session payload is the validated CLI
snapshot imported into the encrypted vault; an account without a session fails
with an authentication error at the provider boundary.

### Docker Compose

The Compose adapter is opt-in. Create a base64 master-key file outside the
repository and import the authenticated CLI session into the adapter's session
volume before starting the Proton profile:

```text
mkdir -p ../secrets
openssl rand -base64 32 > ../secrets/proton-master-key.b64
export SHARDRIVE_PROTON_MASTER_KEY_B64="$(cat ../secrets/proton-master-key.b64)"
export SHARDRIVE_PROTON_ACCOUNT_REF=account-1
export SHARDRIVE_PROTON_SESSION_ROOT="$PWD/data/proton-sessions"
npm run proton:session:import
```

Set these values in the root `.env`:

```text
SHARDRIVE_PROTON_ADAPTER_ADDRESS=proton-adapter:50051
SHARDRIVE_PROTON_MASTER_KEY_FILE=../secrets/proton-master-key.b64
```

Then start the profile from the repository root:

```text
docker compose --env-file .env -f deploy/docker-compose.yml --profile proton up --build
```

The image clones and verifies the pinned official SDK commit during its build.
The default Compose profile remains LocalProvider-only.
