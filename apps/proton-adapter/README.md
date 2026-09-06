# Proton adapter

Phase 0 and the Phase 1 browser acceptance gates have passed. The current
official Proton Drive SDK has now been evaluated, but this adapter remains a
time-boxed one-account spike rather than a production integration.

The official TypeScript SDK currently exposes Drive operations such as upload
and download, while authentication and session management remain outside the
SDK. The SDK public interface and cryptographic model are still evolving, so
all Proton-specific code must remain isolated here. Do not put Proton
cryptography or SDK types in the Go core.

Before implementation, record the exact SDK commit/package snapshot and the
session handoff design. Production readiness requires an adapter restart,
checksum, delete, quota, health, and provider-neutral error acceptance gate.

## Phase 2A decision record

The current investigation uses the official `ProtonDriveApps/sdk` `main`
branch as the source reference. Its JavaScript package manifest currently
identifies `@protontech/drive-sdk` as version `0.0.1`; the JavaScript changelog
currently documents the `js/v0.20.0` release. These are investigation
references, not yet a production dependency pin. Before the first live spike,
record the exact immutable commit and the resolved package tarball in this
document and in the adapter lockfile.

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

No password-over-gRPC flow is permitted. The session recovery path must be
proven with a real one-account test before this adapter is connected to the
placement engine.

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
