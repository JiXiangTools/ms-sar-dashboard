// Package testutil contains fixtures shared by integration tests.
package testutil

import (
	"context"
	"database/sql"
	"fmt"
)

// PreparePostgresExtensions keeps database-wide extensions outside temporary
// test schemas. The transaction lock also protects parallel test packages from
// racing to install an extension in a freshly provisioned database.
func PreparePostgresExtensions(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('ms-sar-dashboard-test-extensions'))`); err != nil {
		return fmt.Errorf("lock test extensions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public`); err != nil {
		return fmt.Errorf("prepare test extensions: %w", err)
	}
	return tx.Commit()
}
