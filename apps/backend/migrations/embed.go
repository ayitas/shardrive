package migrations

import "embed"

// Files contains the versioned PostgreSQL migrations shipped with the API.
//
//go:embed *.sql
var Files embed.FS
