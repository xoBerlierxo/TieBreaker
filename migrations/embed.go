// Package migrations embeds the plain .sql migration files into the binary, so
// `tiebreaker migrate up` works inside a container without the files on disk.
// Files follow golang-migrate naming: NNNN_name.up.sql / NNNN_name.down.sql.
package migrations

import "embed"

// FS holds every *.sql file in this directory.
//
//go:embed *.sql
var FS embed.FS
