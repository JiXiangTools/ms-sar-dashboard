package test

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestDatabaseMigrationDeploymentContract(t *testing.T) {
	migration := readRepoFile(t, "upgrade", "sql", "migrations", "000001_initial.sql")
	bootstrap := readRepoFile(t, "upgrade", "sql", "schema.sql")
	localInit := readRepoFile(t, "admin", "init-local-pg.sh")
	compose := readRepoFile(t, "docker-compose.yml")
	dockerfile := readRepoFile(t, "Dockerfile")
	application := readRepoFile(t, "internal", "app", "app.go")

	checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(migration)))
	if !strings.Contains(bootstrap, checksum) {
		t.Fatalf("schema.sql compatibility bootstrap checksum is stale; expected %s", checksum)
	}

	for path, content := range map[string]string{
		"upgrade/sql/migrations/000001_initial.sql": migration,
		"upgrade/sql/schema.sql":                    bootstrap,
		"admin/init-local-pg.sh":                    localInit,
	} {
		upper := strings.ToUpper(content)
		for _, destructive := range []string{"DROP TABLE", "DROP DATABASE", "TRUNCATE TABLE"} {
			if strings.Contains(upper, destructive) {
				t.Fatalf("%s must not contain destructive statement %q", path, destructive)
			}
		}
	}

	for _, expected := range []string{
		"database-migrate:",
		"/app/ms-sar-dbtool",
		"condition: service_completed_successfully",
		"MSSAR_DATABASE_MIGRATION_TIMEOUT",
		"MSSAR_DATABASE_MIGRATION_DSN",
	} {
		if !strings.Contains(compose, expected) {
			t.Fatalf("docker-compose.yml must contain %q", expected)
		}
	}
	if !strings.Contains(dockerfile, "./cmd/ms-sar-dbtool") {
		t.Fatal("Dockerfile must build ms-sar-dbtool")
	}
	if !strings.Contains(application, "database.VerifyMigrations") {
		t.Fatal("application startup must verify the database migration version")
	}
}
