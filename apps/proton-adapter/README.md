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

No password-over-gRPC flow is permitted. The session recovery path must be
proven with a real one-account test before this adapter is connected to the
placement engine.

`src/proton-client-factory.ts` now codifies this handoff: a session provider
loads account-scoped SDK constructor parameters, the factory returns
`AUTH_REQUIRED`-equivalent `ProtonAuthRequiredError` when no session exists,
and the SDK constructor is injected so the bundled runtime is explicit. The
factory test uses no Proton credentials or network calls.

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
