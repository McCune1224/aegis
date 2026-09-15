// Package db carries the schema into the binary so a deployment needs no
// migration files on disk.
package db

import "embed"

// Migrations holds every migration, applied at startup by internal/store.
//
//go:embed migrations/*.sql
var Migrations embed.FS
