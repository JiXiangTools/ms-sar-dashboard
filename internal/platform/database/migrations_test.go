package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestEmbeddedMigrationsAreVersionedAndNonDestructive(t *testing.T) {
	items, err := loadMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if len(items) != 1 || items[0].Version != 1 || items[0].Name != "initial" {
		t.Fatalf("unexpected migrations: %+v", items)
	}
	if len(items[0].Checksum) != 64 {
		t.Fatalf("unexpected migration checksum: %q", items[0].Checksum)
	}
	upperSQL := strings.ToUpper(items[0].SQL)
	for _, destructive := range []string{"DROP TABLE", "DROP DATABASE", "TRUNCATE TABLE"} {
		if strings.Contains(upperSQL, destructive) {
			t.Fatalf("migration must not contain %q", destructive)
		}
	}
}

func TestValidateAppliedMigrationsRejectsDriftAndGaps(t *testing.T) {
	expected := []migration{
		{Version: 1, Name: "initial", Checksum: strings.Repeat("a", 64)},
		{Version: 2, Name: "next", Checksum: strings.Repeat("b", 64)},
	}

	if err := validateAppliedMigrations(expected, map[int64]appliedMigration{
		1: {Name: "initial", Checksum: strings.Repeat("a", 64)},
	}, false); err != nil {
		t.Fatalf("valid migration prefix rejected: %v", err)
	}
	if err := validateAppliedMigrations(expected, map[int64]appliedMigration{
		1: {Name: "initial", Checksum: "changed"},
	}, false); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("expected checksum drift rejection, got %v", err)
	}
	if err := validateAppliedMigrations(expected, map[int64]appliedMigration{
		2: {Name: "next", Checksum: strings.Repeat("b", 64)},
	}, false); err == nil || !strings.Contains(err.Error(), "not contiguous") {
		t.Fatalf("expected history gap rejection, got %v", err)
	}
	if err := validateAppliedMigrations(expected, map[int64]appliedMigration{
		3: {Name: "future", Checksum: strings.Repeat("c", 64)},
	}, false); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("expected future migration rejection, got %v", err)
	}
	if err := validateAppliedMigrations(expected, map[int64]appliedMigration{
		1: {Name: "initial", Checksum: strings.Repeat("a", 64)},
	}, true); err == nil || !strings.Contains(err.Error(), "pending") {
		t.Fatalf("expected pending migration rejection, got %v", err)
	}
}

func TestMigrateIntegrationConcurrentLegacyAdoption(t *testing.T) {
	baseDSN := strings.TrimSpace(os.Getenv("MSSAR_MIGRATION_TEST_DSN"))
	if baseDSN == "" {
		t.Skip("MSSAR_MIGRATION_TEST_DSN is not set")
	}

	parsed, err := url.Parse(baseDSN)
	if err != nil || parsed.Scheme == "" {
		t.Fatalf("MSSAR_MIGRATION_TEST_DSN must be a PostgreSQL URL: %v", err)
	}
	schema := fmt.Sprintf("migration_test_%d", time.Now().UnixNano())

	adminDB, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	defer adminDB.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := adminDB.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create integration schema: %v", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = adminDB.ExecContext(cleanupCtx, `DROP SCHEMA `+schema+` CASCADE`)
	}()

	query := parsed.Query()
	query.Set("search_path", schema+",public")
	parsed.RawQuery = query.Encode()
	testDB, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatalf("open schema-scoped database: %v", err)
	}
	defer testDB.Close()
	testDB.SetMaxOpenConns(8)

	items, err := loadMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if _, err := testDB.ExecContext(ctx, items[0].SQL); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	if _, err := testDB.ExecContext(ctx, `INSERT INTO t_app (id, name, secret) VALUES (700001, 'legacy', 'preserve-me')`); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	const workers = 4
	results := make(chan MigrationResult, workers)
	errorsCh := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := Migrate(ctx, testDB)
			if err != nil {
				errorsCh <- err
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		t.Errorf("concurrent migration failed: %v", err)
	}
	if t.Failed() {
		return
	}

	appliedCount := 0
	for result := range results {
		appliedCount += len(result.AppliedVersions)
		if result.CurrentVersion != 1 {
			t.Errorf("unexpected current version: %+v", result)
		}
	}
	if appliedCount != 1 {
		t.Fatalf("migration must be applied exactly once, got %d applications", appliedCount)
	}

	var migrationRows int
	if err := testDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationRows); err != nil {
		t.Fatalf("count schema migrations: %v", err)
	}
	if migrationRows != 1 {
		t.Fatalf("expected one migration row, got %d", migrationRows)
	}
	var legacySecret string
	if err := testDB.QueryRowContext(ctx, `SELECT secret FROM t_app WHERE id = 700001`).Scan(&legacySecret); err != nil {
		t.Fatalf("read preserved legacy row: %v", err)
	}
	if legacySecret != "preserve-me" {
		t.Fatalf("legacy data changed: %q", legacySecret)
	}
	if err := VerifyMigrations(ctx, testDB); err != nil {
		t.Fatalf("verify migrated schema: %v", err)
	}

	created, err := SeedAdmin(ctx, testDB, "migration-admin", "hash-one", false)
	if err != nil || !created {
		t.Fatalf("seed initial admin: created=%v err=%v", created, err)
	}
	created, err = SeedAdmin(ctx, testDB, "migration-admin", "hash-two", false)
	if err != nil || created {
		t.Fatalf("existing admin must be preserved: created=%v err=%v", created, err)
	}
	var passwordHash string
	if err := testDB.QueryRowContext(ctx, `SELECT password FROM t_admin WHERE name = 'migration-admin'`).Scan(&passwordHash); err != nil {
		t.Fatalf("read seeded admin: %v", err)
	}
	if passwordHash != "hash-one" {
		t.Fatalf("admin password was unexpectedly replaced: %q", passwordHash)
	}
	updated, err := SeedAdmin(ctx, testDB, "migration-admin", "hash-three", true)
	if err != nil || !updated {
		t.Fatalf("explicit admin rotation: updated=%v err=%v", updated, err)
	}

	if _, err := testDB.ExecContext(ctx, `UPDATE schema_migrations SET checksum = repeat('0', 64) WHERE version = 1`); err != nil {
		t.Fatalf("tamper migration metadata: %v", err)
	}
	if err := VerifyMigrations(ctx, testDB); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("expected checksum drift rejection, got %v", err)
	}
}
