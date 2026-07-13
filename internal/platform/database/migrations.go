package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/JiXiangTools/ms-sar-dashboard/upgrade"
)

const (
	migrationDirectory = "sql/migrations"
	migrationLockKey   = int64(21765190250021954)
)

var migrationFilenamePattern = regexp.MustCompile(`^(\d{6})_([a-z0-9][a-z0-9_-]*)\.sql$`)

const migrationTableDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version BIGINT PRIMARY KEY,
    name TEXT NOT NULL,
    checksum CHAR(64) NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`

type migration struct {
	Version  int64
	Name     string
	SQL      string
	Checksum string
}

// MigrationResult describes the schema transition performed by Migrate.
type MigrationResult struct {
	PreviousVersion int64
	CurrentVersion  int64
	AppliedVersions []int64
}

type appliedMigration struct {
	Name     string
	Checksum string
}

// Migrate applies every pending embedded migration in version order. A
// session-level PostgreSQL advisory lock serializes concurrent migrators, and
// each migration plus its version record is committed in one transaction.
func Migrate(ctx context.Context, db *sql.DB) (result MigrationResult, err error) {
	if db == nil {
		return result, errors.New("database is required")
	}

	migrations, err := loadMigrations()
	if err != nil {
		return result, err
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return result, fmt.Errorf("reserve migration connection: %w", err)
	}
	defer conn.Close()

	if _, err = conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return result, fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var unlocked bool
		unlockErr := conn.QueryRowContext(unlockCtx, `SELECT pg_advisory_unlock($1)`, migrationLockKey).Scan(&unlocked)
		if unlockErr != nil {
			err = errors.Join(err, fmt.Errorf("release migration lock: %w", unlockErr))
		} else if !unlocked {
			err = errors.Join(err, errors.New("release migration lock: lock was not held"))
		}
	}()

	if _, err = conn.ExecContext(ctx, migrationTableDDL); err != nil {
		return result, fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := readAppliedMigrations(ctx, conn)
	if err != nil {
		return result, err
	}
	if err := validateAppliedMigrations(migrations, applied, false); err != nil {
		return result, err
	}
	for version := range applied {
		if version > result.PreviousVersion {
			result.PreviousVersion = version
		}
	}
	result.CurrentVersion = result.PreviousVersion

	for _, item := range migrations {
		if _, ok := applied[item.Version]; ok {
			continue
		}

		tx, txErr := conn.BeginTx(ctx, nil)
		if txErr != nil {
			return result, fmt.Errorf("begin migration %06d: %w", item.Version, txErr)
		}
		if _, txErr = tx.ExecContext(ctx, item.SQL); txErr == nil {
			_, txErr = tx.ExecContext(ctx,
				`INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)`,
				item.Version, item.Name, item.Checksum,
			)
		}
		if txErr != nil {
			_ = tx.Rollback()
			return result, fmt.Errorf("apply migration %06d_%s: %w", item.Version, item.Name, txErr)
		}
		if txErr = tx.Commit(); txErr != nil {
			return result, fmt.Errorf("commit migration %06d_%s: %w", item.Version, item.Name, txErr)
		}

		result.AppliedVersions = append(result.AppliedVersions, item.Version)
		result.CurrentVersion = item.Version
	}

	return result, nil
}

// VerifyMigrations waits for an active migrator, then verifies that every
// migration expected by this binary is present and has the original checksum.
// It never performs DDL and is safe for the restricted application DB role.
func VerifyMigrations(ctx context.Context, db *sql.DB) (err error) {
	if db == nil {
		return errors.New("database is required")
	}
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserve schema verification connection: %w", err)
	}
	defer conn.Close()

	if _, err = conn.ExecContext(ctx, `SELECT pg_advisory_lock_shared($1)`, migrationLockKey); err != nil {
		return fmt.Errorf("acquire schema verification lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var unlocked bool
		unlockErr := conn.QueryRowContext(unlockCtx, `SELECT pg_advisory_unlock_shared($1)`, migrationLockKey).Scan(&unlocked)
		if unlockErr != nil {
			err = errors.Join(err, fmt.Errorf("release schema verification lock: %w", unlockErr))
		} else if !unlocked {
			err = errors.Join(err, errors.New("release schema verification lock: lock was not held"))
		}
	}()

	var tableExists bool
	if err := conn.QueryRowContext(ctx, `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&tableExists); err != nil {
		return fmt.Errorf("inspect schema_migrations: %w", err)
	}
	if !tableExists {
		return errors.New("database schema is not initialized; run ms-sar-dbtool before starting the service")
	}

	applied, err := readAppliedMigrations(ctx, conn)
	if err != nil {
		return err
	}
	return validateAppliedMigrations(migrations, applied, true)
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(upgrade.Migrations, migrationDirectory)
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	items := make([]migration, 0, len(entries))
	seen := make(map[int64]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := migrationFilenamePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		version, err := strconv.ParseInt(matches[1], 10, 64)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("invalid migration version in %q", entry.Name())
		}
		if previous, ok := seen[version]; ok {
			return nil, fmt.Errorf("duplicate migration version %06d in %q and %q", version, previous, entry.Name())
		}
		body, err := fs.ReadFile(upgrade.Migrations, migrationDirectory+"/"+entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		if strings.TrimSpace(string(body)) == "" {
			return nil, fmt.Errorf("migration %q is empty", entry.Name())
		}
		digest := sha256.Sum256(body)
		items = append(items, migration{
			Version:  version,
			Name:     matches[2],
			SQL:      string(body),
			Checksum: fmt.Sprintf("%x", digest),
		})
		seen[version] = entry.Name()
	}
	if len(items) == 0 {
		return nil, errors.New("no embedded database migrations found")
	}
	for index, item := range items {
		expectedVersion := int64(index + 1)
		if item.Version != expectedVersion {
			return nil, fmt.Errorf("embedded migration history must be contiguous: expected version %06d, found %06d", expectedVersion, item.Version)
		}
	}
	return items, nil
}

func readAppliedMigrations(ctx context.Context, conn *sql.Conn) (map[int64]appliedMigration, error) {
	rows, err := conn.QueryContext(ctx, `SELECT version, name, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int64]appliedMigration)
	for rows.Next() {
		var version int64
		var item appliedMigration
		if err := rows.Scan(&version, &item.Name, &item.Checksum); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[version] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations: %w", err)
	}
	return applied, nil
}

func validateAppliedMigrations(expected []migration, applied map[int64]appliedMigration, requireAll bool) error {
	known := make(map[int64]migration, len(expected))
	for _, item := range expected {
		known[item.Version] = item
	}
	for version, actual := range applied {
		item, ok := known[version]
		if !ok {
			return fmt.Errorf("database migration %06d is newer than or unknown to this binary", version)
		}
		if actual.Name != item.Name || actual.Checksum != item.Checksum {
			return fmt.Errorf("database migration %06d metadata mismatch: migration files are immutable", version)
		}
	}
	foundPending := false
	for _, item := range expected {
		_, ok := applied[item.Version]
		if !ok {
			foundPending = true
			continue
		}
		if foundPending {
			return fmt.Errorf("database migration history is not contiguous before version %06d", item.Version)
		}
	}
	if requireAll {
		for _, item := range expected {
			if _, ok := applied[item.Version]; !ok {
				return fmt.Errorf("database migration %06d_%s is pending; run ms-sar-dbtool before starting the service", item.Version, item.Name)
			}
		}
	}
	return nil
}

// SeedAdmin inserts an initial administrator. Existing accounts are preserved
// unless updateExisting is explicitly requested for a controlled rotation.
func SeedAdmin(ctx context.Context, db *sql.DB, name string, passwordHash string, updateExisting bool) (bool, error) {
	if db == nil {
		return false, errors.New("database is required")
	}
	name = strings.TrimSpace(name)
	passwordHash = strings.TrimSpace(passwordHash)
	if name == "" || passwordHash == "" {
		return false, errors.New("admin name and password hash are required")
	}

	query := `
INSERT INTO t_admin (name, nickname, password, disabled)
VALUES ($1, 'System Admin', $2, FALSE)
ON CONFLICT (name) DO NOTHING`
	if updateExisting {
		query = `
INSERT INTO t_admin (name, nickname, password, disabled)
VALUES ($1, 'System Admin', $2, FALSE)
ON CONFLICT (name) DO UPDATE SET
    nickname = EXCLUDED.nickname,
    password = EXCLUDED.password,
    disabled = FALSE,
    last_update_time = NOW()`
	}

	result, err := db.ExecContext(ctx, query, name, passwordHash)
	if err != nil {
		return false, fmt.Errorf("seed initial admin: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read seed admin result: %w", err)
	}
	return rows > 0, nil
}
