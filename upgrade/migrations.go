package upgrade

import "embed"

// Migrations contains the immutable, versioned PostgreSQL migrations shipped
// with this build. Migration files are executed by ms-sar-dbtool; applications
// only verify that the database is at the version expected by the binary.
//
//go:embed sql/migrations/*.sql
var Migrations embed.FS
