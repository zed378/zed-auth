// Package migrations embeds the SQL migration files into the binary.
//
// Embedding rather than reading from disk means the migration binary can never
// drift from the migrations it was built with: there is no way to deploy an
// image whose /migrations directory was left at an older revision, and no
// filesystem path to get wrong across operating systems.
package migrations

import "embed"

// FS holds every .sql migration file in this directory.
//
//go:embed *.sql
var FS embed.FS
