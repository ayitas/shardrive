# Protocol definitions

`storage.proto` defines the provider-neutral gRPC boundary used by the Phase 2
one-account Proton adapter and extended by the Phase 3 multi-account work. It
is intentionally independent of Proton names, SDK types, and credential
formats.

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

The TypeScript adapter loads this contract dynamically with `@grpc/proto-loader`.
The Go core uses a small transport-local protobuf client that mirrors only these
provider-neutral messages; Proton SDK types and credentials never cross the
boundary. Generated bindings can replace that client later without changing the
`storage.Provider` interface.
