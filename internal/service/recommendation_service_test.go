//go:build integration

package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/audit"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/cache"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/database"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/repository"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/testutil"
)

func TestRecommendationPublicationLifecycle(t *testing.T) {
	dsn, addrs := os.Getenv("MSSAR_RECOMMENDATION_TEST_DSN"), os.Getenv("MSSAR_RECOMMENDATION_TEST_REDIS")
	if dsn == "" || addrs == "" {
		t.Skip("requires MSSAR_RECOMMENDATION_TEST_DSN and MSSAR_RECOMMENDATION_TEST_REDIS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if err := testutil.PreparePostgresExtensions(ctx, admin); err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("rec_test_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	db, err := database.New(config.DatabaseConfig{DSN: u.String(), MaxOpenConns: 8})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := database.Migrate(ctx, db.DB); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.ExecContext(ctx, "INSERT INTO t_app(id,name,secret) VALUES (100001,'first','test'),(100002,'second','test')"); err != nil {
		t.Fatal(err)
	}
	redisClient, err := cache.New(config.RedisConfig{Mode: "cluster", Addrs: strings.Split(addrs, ","), Password: os.Getenv("MSSAR_RECOMMENDATION_TEST_REDIS_PASSWORD"), DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer redisClient.Close()
	prefix := schema + ":"
	key := prefix + "rec:appconf:{100001}"
	defer redisClient.Client.Del(context.Background(), key)
	repo := repository.New(db.ORM)
	s := NewRecommendationService(repo, redisClient.Client, prefix, audit.NewService(repo), nil)
	recall := domain.RecallSource{RecallID: "icf", Name: "Item CF", Kind: "item", Producer: "icf", KeyTemplate: "icf_{appid}_{itemid}", ValueFormat: "compact_score"}
	if _, err := s.SaveSource(ctx, 1, recall); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveSource(ctx, 1, recall); err == nil {
		t.Fatal("existing ID edited")
	}
	strategy := domain.RecommendationStrategy{AppID: "100001", RecommendationType: "personalized", StrategyID: "a", Name: "策略", Recall: []domain.StrategyRecall{{RecallID: "icf", Weight: 1, RecallRate: 1}}, Fallback: []domain.StrategyRecall{}, Rank: "recall_weight"}
	if _, err := s.SaveStrategy(ctx, 1, strategy); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, 1, "recall", "", "icf"); err == nil {
		t.Fatal("referenced recall deleted")
	}
	experiment := domain.RecommendationExperiment{AppID: "100001", ExperimentID: "exp_a", Name: "实验", BucketBy: "user_id", Assignments: []domain.ExperimentAssignment{{StrategyID: "a", BucketStart: 0, BucketEnd: 29}}}
	if _, err := s.SaveExperiment(ctx, 1, experiment); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, 1, "strategy", "100001", "a"); err == nil {
		t.Fatal("referenced strategy deleted")
	}
	if _, err := s.Publish(ctx, 1, "100001", "exp_a"); err != nil {
		t.Fatal(err)
	}
	raw, err := redisClient.Client.Get(ctx, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	var config domain.RecommendationConfig
	if json.Unmarshal([]byte(raw), &config) != nil || config.Experiment.ExperimentID != "exp_a" || config.SchemaVersion != 3 || config.PublishedAt == 0 {
		t.Fatalf("incorrect snapshot %s", raw)
	}
	if strings.Contains(raw, "revision") || strings.Contains(raw, "experiment_version") || strings.Contains(raw, "controls") {
		t.Fatal("obsolete publication fields")
	}
	if err := s.Delete(ctx, 1, "experiment", "100001", "exp_a"); err == nil {
		t.Fatal("active experiment deleted")
	}
	if _, err := s.Publish(ctx, 1, "100001", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, 1, "experiment", "100001", "exp_a"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, 1, "strategy", "100001", "a"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, 1, "recall", "", "icf"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveSource(ctx, 1, recall); err == nil {
		t.Fatal("deleted ID reused")
	}
	redisClient.Client.Del(ctx, key)
	if err := s.Reconcile(ctx, true); err != nil {
		t.Fatal(err)
	}
	raw, err = redisClient.Client.Get(ctx, key).Result()
	if err != nil || json.Unmarshal([]byte(raw), &config) != nil || config.Experiment != nil {
		t.Fatalf("stop snapshot not replayed: %s %v", raw, err)
	}
}
