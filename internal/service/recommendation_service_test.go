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
	"sync"
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
	source, err := s.SaveSource(ctx, 1, domain.RecallSource{SourceID: "icf", Name: "Item CF", Kind: "item", Producer: "icf", KeyTemplate: "icf_{appid}_{itemid}", ValueFormat: "compact_score"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveSource(ctx, 1, domain.RecallSource{SourceID: "unused", Name: "Unrelated source", Kind: "item", Producer: "ir", KeyTemplate: "ir_{appid}_{itemid}", ValueFormat: "compact_score"}); err != nil {
		t.Fatal(err)
	}
	strategy, err := s.SaveStrategy(ctx, 1, domain.RecommendationStrategy{AppID: "100001", StrategyID: "a", Name: "策略 A", Rank: "recall_weight", Recall: []domain.StrategyRecall{{SourceID: "icf", Weight: 1, RecallRate: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	experiment := domain.RecommendationExperiment{AppID: "100001", ExperimentID: "exp_one", Name: "首页实验", BucketBy: "user_id", Assignments: []domain.ExperimentAssignment{{StrategyID: "a", BucketStart: 1, BucketEnd: 29}}}
	first, err := s.SaveExperiment(ctx, 1, experiment)
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || first.BucketAlgorithm != "sha256" || first.Snapshot.Strategies[0].Version != strategy.Version || len(first.Snapshot.Sources) != 1 || first.Snapshot.Sources[0].SourceID != "icf" {
		t.Fatal("experiment did not pin strategy version")
	}
	pub, err := s.Publish(ctx, 1, "100001", "exp_one", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if pub.Revision != 1 || pub.PublishedRevision != 1 {
		t.Fatalf("publication not committed %+v", pub)
	}
	if ttl := redisClient.Client.TTL(ctx, key).Val(); ttl != -1 {
		t.Fatalf("configuration must not expire: %v", ttl)
	}
	source.KeyTemplate = "icf_next_{appid}_{itemid}"
	if _, err = s.SaveSource(ctx, 1, source); err != nil {
		t.Fatal(err)
	}
	strategy.Recall[0].Weight = 3
	if _, err = s.SaveStrategy(ctx, 1, strategy); err != nil {
		t.Fatal(err)
	}
	experiment.Version = 1
	second, err := s.SaveExperiment(ctx, 1, experiment)
	if err != nil {
		t.Fatal(err)
	}
	if second.Version != 2 || second.Snapshot.Sources[0].KeyTemplate == first.Snapshot.Sources[0].KeyTemplate || first.Snapshot.Strategies[0].Recall[0].Weight != 1 {
		t.Fatal("snapshots were mutated")
	}
	if _, err = s.Publish(ctx, 1, "100001", "exp_one", 2, 1); err != nil {
		t.Fatal(err)
	}
	w, err := s.Workspace(ctx, "100001")
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Experiments) != 2 || w.Experiments[0].Status != "running" || w.Experiments[1].Status != "ended" || w.Experiments[1].StartedAt == nil || w.Experiments[1].EndedAt == nil || w.StrategyHistory[1].EndedAt == nil {
		t.Fatalf("missing lifecycle timestamps %+v", w.Experiments)
	}
	if w.Experiments[1].Snapshot.Sources[0].KeyTemplate != "icf_{appid}_{itemid}" {
		t.Fatal("history changed with source edit")
	}
	if _, err = s.Publish(ctx, 1, "100001", "exp_one", 1, 2); err == nil {
		t.Fatal("ended version cannot be reopened")
	}
	if _, err = s.Publish(ctx, 1, "100002", "exp_one", 2, 0); err == nil {
		t.Fatal("cross-app experiment publication succeeded")
	}
	if _, err = s.Publish(ctx, 1, "100001", "", 0, 2); err != nil {
		t.Fatal(err)
	}
	raw, err := redisClient.Client.Get(ctx, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	var current domain.RecommendationConfig
	if err = json.Unmarshal([]byte(raw), &current); err != nil {
		t.Fatal(err)
	}
	if current.Experiment != nil || current.Revision != 3 {
		t.Fatal("stop did not atomically replace config")
	}
	// Two editors holding the same version cannot both append an experiment.
	experiment.Version = 2
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.SaveExperiment(ctx, 1, experiment); results <- err }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("concurrent edits succeeded %d times", success)
	}
	// A queued publication survives a lost acknowledgement. A subsequent edit
	// cannot overwrite it before reconciliation completes.
	if err := repo.QueueRecommendationPublication(ctx, "100001", "exp_one", 3, 3); err != nil {
		t.Fatal(err)
	}
	if err := repo.QueueRecommendationPublication(ctx, "100001", "", 0, 4); err != repository.ErrConflict {
		t.Fatalf("pending publication overwritten: %v", err)
	}
	if err := s.Reconcile(ctx, false); err != nil {
		t.Fatal(err)
	}
	// Failed startup recovery must remain eligible for the background worker,
	// even though this revision was already acknowledged in the database.
	beforeRecovery, err := s.Workspace(ctx, "100001")
	if err != nil {
		t.Fatal(err)
	}
	var original domain.RecommendationConfig
	if err := json.Unmarshal([]byte(redisClient.Client.Get(ctx, key).Val()), &original); err != nil {
		t.Fatal(err)
	}
	if err := redisClient.Client.Del(ctx, key).Err(); err != nil {
		t.Fatal(err)
	}
	failedStartup, cancelStartup := context.WithCancel(ctx)
	cancelStartup()
	if err := s.Reconcile(failedStartup, true); err == nil {
		t.Fatal("canceled startup recovery unexpectedly succeeded")
	}
	workerCtx, stopWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); s.Run(workerCtx) }()
	defer func() { stopWorker(); <-workerDone }()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(20 * time.Millisecond)
	defer poll.Stop()
	var restored domain.RecommendationConfig
waitForRecovery:
	for {
		select {
		case <-deadline.C:
			t.Fatal("background retry did not restore the acknowledged configuration")
		case <-poll.C:
			value, err := redisClient.Client.Get(ctx, key).Result()
			if err != nil {
				continue
			}
			if err := json.Unmarshal([]byte(value), &restored); err != nil {
				t.Fatal(err)
			}
			break waitForRecovery
		}
	}
	if restored.PublishedAt != original.PublishedAt || restored.Revision != original.Revision {
		t.Fatal("recovery changed publication metadata")
	}
	afterRecovery, err := s.Workspace(ctx, "100001")
	if err != nil {
		t.Fatal(err)
	}
	if !afterRecovery.Experiments[0].StartedAt.Equal(*beforeRecovery.Experiments[0].StartedAt) || afterRecovery.Experiments[0].Status != "running" {
		t.Fatal("configuration recovery changed the experiment lifecycle")
	}
	var logs int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM t_admin_log WHERE cate='RECOMMENDATION'").Scan(&logs); err != nil || logs < 10 {
		t.Fatalf("mutations not audited: %d %v", logs, err)
	}
}
