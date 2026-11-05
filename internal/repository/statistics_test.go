package repository_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/database"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/repository"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/testutil"
)

func TestStatisticsPostgresSnapshots(t *testing.T) {
	dsn := os.Getenv("MSSAR_STATISTICS_TEST_DSN")
	if dsn == "" {
		t.Skip("MSSAR_STATISTICS_TEST_DSN is not set")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if err := testutil.PreparePostgresExtensions(ctx, admin); err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("statistics_test_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	client, err := database.New(config.DatabaseConfig{DSN: u.String()})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := database.Migrate(ctx, client.DB); err != nil {
		t.Fatal(err)
	}
	if _, err := client.DB.ExecContext(ctx, "INSERT INTO t_app(id,name,secret) VALUES (100001,'first','test'),(100002,'second','test')"); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(client.ORM)
	day := domain.StatisticsDay{AppID: "100001", Date: "2026-01-01", Revision: 1, Counts: domain.StatisticsCounts{Clicks: 1}, Snapshot: domain.StatisticsSnapshot{ItemTotal: 2}}
	if changed, err := repo.PutStatistics(ctx, day); err != nil || !changed {
		t.Fatalf("initial changed=%v err=%v", changed, err)
	}
	if changed, err := repo.PutStatistics(ctx, day); err != nil || changed {
		t.Fatalf("retry changed=%v err=%v", changed, err)
	}
	conflict := day
	conflict.Counts.Clicks = 20
	if _, err := repo.PutStatistics(ctx, conflict); err != repository.ErrConflict {
		t.Fatalf("conflicting retry: %v", err)
	}
	next := day
	next.Revision = 2
	next.Counts.Clicks = 3
	next.Snapshot.ItemTotal = 4
	if _, err := repo.PutStatistics(ctx, next); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PutStatistics(ctx, day); err != repository.ErrConflict {
		t.Fatalf("stale retry: %v", err)
	}
	rows, snapshot, err := repo.Statistics(ctx, 100001, "2026-01-01", "2026-01-01")
	if err != nil || len(rows) != 1 || rows[0].Counts.Clicks != 3 || snapshot.ItemTotal != 4 {
		t.Fatalf("replacement failed rows=%+v snapshot=%+v err=%v", rows, snapshot, err)
	}
	rows, snapshot, err = repo.Statistics(ctx, 100002, "2026-01-01", "2026-01-01")
	if err != nil || len(rows) != 0 || snapshot != nil {
		t.Fatal("cross-app statistics leaked")
	}
	absent := day
	absent.AppID = "100003"
	if _, err := repo.PutStatistics(ctx, absent); err != repository.ErrNotFound {
		t.Fatalf("missing app: %v", err)
	}
}
