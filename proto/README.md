# Protocol definitions

`storage.proto` defines the provider-neutral gRPC boundary for the Phase 2
adapter. It is intentionally independent of Proton names, SDK types, and
credential formats.

Boundary decisions:

- Go sends Shardrive's internal `account_id`; the adapter resolves credentials
  and provider sessions locally.
- Upload is client-streaming: one metadata frame followed by bounded data
  frames.
- Download is server-streaming and ends at EOF; the Go side reconstructs an
  `io.ReadCloser` around the received frames.
- Delete, Stat, Usage, and Health are unary calls.
- Provider-specific failures must be translated to gRPC status codes by the
  adapter and then to Shardrive's provider-neutral errors by Go.
- Credentials, access tokens, Proton node IDs, and SDK-specific objects never
  appear in this protocol.

Generated Go/TypeScript bindings are intentionally deferred until the
one-account SDK spike confirms the exact adapter runtime and build toolchain.
