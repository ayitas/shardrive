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

The initial gRPC boundary is defined in [`proto/storage.proto`](../../proto/storage.proto):
one metadata frame plus bounded data frames for upload, bounded server frames
for download, and unary delete/stat/usage/health calls. The adapter receives
only Shardrive's internal `account_id`; credential and session lookup remain
inside the adapter.
