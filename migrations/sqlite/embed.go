// Package sqlitemigrations embeds the SQLite migration scripts so the
// desktop installer can materialize them next to the per-user data dir;
// internal/database/migration.go reads them from the relative disk path
// migrations/sqlite at startup.
package sqlitemigrations

import "embed"

//go:embed *.sql
var FS embed.FS
