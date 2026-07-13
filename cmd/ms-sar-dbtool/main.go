package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	platformdatabase "github.com/JiXiangTools/ms-sar-dashboard/internal/platform/database"
)

func main() {
	dsn := flag.String("dsn", strings.TrimSpace(os.Getenv("MSSAR_DATABASE_DSN")), "PostgreSQL DSN (defaults to MSSAR_DATABASE_DSN)")
	adminName := flag.String("admin-name", strings.TrimSpace(os.Getenv("MSSAR_INIT_ADMIN_NAME")), "optional initial admin name")
	adminPasswordHash := flag.String("admin-password-hash", strings.TrimSpace(os.Getenv("MSSAR_INIT_ADMIN_PASSWORD_HASH")), "optional initial admin bcrypt hash")
	updateAdmin := flag.Bool("update-admin", false, "explicitly replace an existing initial admin password and re-enable the account")
	timeout := flag.Duration("timeout", migrationTimeoutFromEnv(), "total migration and seed timeout")
	flag.Parse()

	if strings.TrimSpace(*dsn) == "" {
		exitf("database DSN is required through -dsn or MSSAR_DATABASE_DSN")
	}
	if *timeout <= 0 {
		exitf("timeout must be positive")
	}
	if (*adminName == "") != (*adminPasswordHash == "") {
		exitf("admin name and password hash must be provided together")
	}
	if *updateAdmin && *adminName == "" {
		exitf("-update-admin requires admin seed credentials")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	db, err := sql.Open("pgx", *dsn)
	if err != nil {
		exitf("open database: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		exitf("connect database: %v", err)
	}

	result, err := platformdatabase.Migrate(ctx, db)
	if err != nil {
		exitf("migrate database: %v", err)
	}
	fmt.Printf("database schema is at version %06d", result.CurrentVersion)
	if len(result.AppliedVersions) > 0 {
		fmt.Printf("; applied %v", result.AppliedVersions)
	}
	fmt.Println()

	if *adminName != "" {
		changed, err := platformdatabase.SeedAdmin(ctx, db, *adminName, *adminPasswordHash, *updateAdmin)
		if err != nil {
			exitf("seed admin: %v", err)
		}
		if changed {
			fmt.Printf("admin %q initialized\n", *adminName)
		} else {
			fmt.Printf("admin %q already exists; credentials were preserved\n", *adminName)
		}
	}
}

func migrationTimeoutFromEnv() time.Duration {
	value := strings.TrimSpace(os.Getenv("MSSAR_DATABASE_MIGRATION_TIMEOUT"))
	if value == "" {
		return 60 * time.Second
	}
	timeout, err := time.ParseDuration(value)
	if err != nil {
		exitf("parse MSSAR_DATABASE_MIGRATION_TIMEOUT: %v", err)
	}
	return timeout
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
