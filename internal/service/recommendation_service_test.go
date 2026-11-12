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

	"github.com/JiXiangTools/ms-sar-dashboard/internal/apperror"
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
	if strategy.RecommendationType != domain.RecommendationPersonalized {
		t.Fatal("untyped strategy was not canonicalized to personalized")
	}
	controls := []domain.ExperimentControl{}
	for _, kind := range []string{domain.RecommendationPersonalized, domain.RecommendationRelated} {
		id := "base_p"
		if kind == domain.RecommendationRelated {
			id = "base_r"
		}
		baseline, err := s.SaveStrategy(ctx, 1, domain.RecommendationStrategy{AppID: "100001", StrategyID: id, Name: "固定对照", RecommendationType: kind, Rank: "recall_weight", Recall: []domain.StrategyRecall{{SourceID: "icf", Weight: 1, RecallRate: 1}}})
		if err != nil {
			t.Fatal(err)
		}
		controls = append(controls, domain.ExperimentControl{StrategyID: baseline.StrategyID, StrategyVersion: baseline.Version})
	}
	experiment := domain.RecommendationExperiment{AppID: "100001", ExperimentID: "exp_one", Name: "首页实验", BucketBy: "user_id", Assignments: []domain.ExperimentAssignment{{StrategyID: "a", BucketStart: 1, BucketEnd: 29}}, Controls: controls[:1]}
	missingControl := experiment
	missingControl.Controls = nil
	if _, err := s.SaveExperiment(ctx, 1, missingControl); err == nil {
		t.Fatal("new experiment accepted a mutable local control baseline")
	} else if apiError, ok := err.(*apperror.Error); !ok || apiError.Status != 400 {
		t.Fatalf("missing control must be a client error: %v", err)
	}
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
	if len(w.Experiments) != 2 || w.Experiments[0].Status != "running" || w.Experiments[1].Status != "ended" || w.Experiments[1].StartedAt == nil || w.Experiments[1].EndedAt == nil {
		t.Fatalf("missing lifecycle timestamps %+v", w.Experiments)
	}
	for _, history := range w.StrategyHistory {
		if history.ExperimentVersion == 1 && (history.EndedAt == nil || history.StartedAt == nil) {
			t.Fatalf("strategy usage interval was not closed: %+v", history)
		}
		if history.StrategyID == "base_p" && (history.GroupID != "default" || history.StrategyVersion != 1) {
			t.Fatalf("control strategy version or group was not frozen: %+v", history)
		}
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
	go func() { defer close(workerDone); s.run(workerCtx, 20*time.Millisecond, 80*time.Millisecond) }()
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
	// Acknowledged projections must also heal during ordinary background work,
	// without a pending publication or a Dashboard restart.
	for _, corruption := range []string{"missing", "invalid JSON", "wrong Redis type", "same revision drift"} {
		t.Run(corruption, func(t *testing.T) {
			var err error
			switch corruption {
			case "missing":
				err = redisClient.Client.Del(ctx, key).Err()
			case "invalid JSON":
				err = redisClient.Client.Set(ctx, key, "{invalid", 0).Err()
			case "wrong Redis type":
				err = redisClient.Client.Eval(ctx, "redis.call('DEL',KEYS[1]); return redis.call('LPUSH',KEYS[1],'invalid')", []string{key}).Err()
			case "same revision drift":
				drift := original
				drift.PublishedAt++
				drift.Strategies = nil
				raw, marshalErr := json.Marshal(drift)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				err = redisClient.Client.Set(ctx, key, raw, 0).Err()
			}
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.NewTimer(2 * time.Second)
			defer deadline.Stop()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-deadline.C:
					t.Fatal("background reconciliation did not restore canonical publication")
				case <-ticker.C:
					value, err := redisClient.Client.Get(ctx, key).Result()
					var repaired domain.RecommendationConfig
					if err == nil && json.Unmarshal([]byte(value), &repaired) == nil && repaired.Revision == original.Revision && repaired.PublishedAt == original.PublishedAt && len(repaired.Strategies) == len(original.Strategies) {
						return
					}
				}
			}
		})
	}
	stopWorker()
	<-workerDone
	// Preserve an unconfirmed first Redis write after a lost DB acknowledgement,
	// but use the durable database time once publication has been acknowledged.
	probeKey := prefix + "rec:appconf:{timestamp_probe}"
	defer redisClient.Client.Del(context.Background(), probeKey)
	if err := redisClient.Client.Set(ctx, probeKey, `{"appid":"100001","revision":1,"published_at":100}`, 0).Err(); err != nil {
		t.Fatal(err)
	}
	value, err := publishRecommendationScript.Run(ctx, redisClient.Client, []string{probeKey}, 1, `{"appid":"100001","revision":1,"published_at":200}`, 200, "100001", 1).Text()
	if err != nil || value != "100" {
		t.Fatalf("lost acknowledgement changed first publication time: %s %v", value, err)
	}
	value, err = publishRecommendationScript.Run(ctx, redisClient.Client, []string{probeKey}, 1, `{"appid":"100001","revision":1,"published_at":200}`, 200, "100001", 0).Text()
	if err != nil || value != "200" {
		t.Fatalf("acknowledged publication did not restore durable time: %s %v", value, err)
	}
	if _, err := publishRecommendationScript.Run(ctx, redisClient.Client, []string{probeKey}, 0, `{}`, 200, "100001", 0).Result(); err == nil {
		t.Fatal("older revision overwrote a newer publication")
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
	// Resolve types from the pinned versions, even after a strategy changes type.
	related, err := s.SaveStrategy(ctx, 1, domain.RecommendationStrategy{AppID: "100001", StrategyID: "related_a", RecommendationType: domain.RecommendationRelated, Name: "相关推荐 A", Rank: "recall_weight", Recall: []domain.StrategyRecall{{SourceID: "icf", Weight: 1, RecallRate: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	strategy.Version = 2
	strategy.RecommendationType = domain.RecommendationRelated
	changedType, err := s.SaveStrategy(ctx, 1, strategy)
	if err != nil || changedType.Version != 3 {
		t.Fatalf("strategy type change must create a new version: %+v %v", changedType, err)
	}
	typed := domain.RecommendationExperiment{AppID: "100001", ExperimentID: "exp_types", Name: "两类推荐实验", BucketBy: "user_id", Assignments: []domain.ExperimentAssignment{
		{StrategyID: "a", StrategyVersion: 2, BucketStart: 1, BucketEnd: 29},
		{StrategyID: related.StrategyID, StrategyVersion: related.Version, BucketStart: 1, BucketEnd: 29},
	}, Controls: controls}
	typedVersion, err := s.SaveExperiment(ctx, 1, typed)
	if err != nil || typedVersion.Version != 1 || typedVersion.Snapshot.Strategies[0].EffectiveType() != domain.RecommendationPersonalized || typedVersion.Snapshot.Strategies[1].EffectiveType() != domain.RecommendationRelated {
		t.Fatalf("different types must allow the same bucket range using pinned versions: %+v %v", typedVersion, err)
	}
	invalid := typed
	invalid.ExperimentID = "exp_overlap"
	invalid.Assignments = append([]domain.ExperimentAssignment(nil), typed.Assignments...)
	invalid.Assignments[0].StrategyVersion = changedType.Version
	if _, err := s.SaveExperiment(ctx, 1, invalid); err == nil {
		t.Fatal("same-type overlapping buckets accepted")
	} else if apiError, ok := err.(*apperror.Error); !ok || apiError.Status != 400 {
		t.Fatalf("invalid bucket allocation must be a client error: %v", err)
	}
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM t_rec_experiment_version WHERE experiment_id='exp_overlap'").Scan(&logs); err != nil || logs != 0 {
		t.Fatalf("rejected experiment left a snapshot: %d %v", logs, err)
	}
	publication, err := s.Publish(ctx, 1, "100001", typed.ExperimentID, typedVersion.Version, afterRecovery.Publication.Revision)
	if err != nil || publication.Config.Experiment.ExperimentID != typed.ExperimentID {
		t.Fatalf("could not publish mixed recommendation types: %+v %v", publication, err)
	}
	var published domain.RecommendationConfig
	if err := json.Unmarshal([]byte(redisClient.Client.Get(ctx, key).Val()), &published); err != nil || len(published.Strategies) != 4 || published.Strategies[0].EffectiveType() != domain.RecommendationPersonalized || published.Strategies[1].EffectiveType() != domain.RecommendationRelated {
		t.Fatalf("typed snapshot was not published intact: %+v %v", published, err)
	}
	if first.Snapshot.Strategies[0].EffectiveType() != domain.RecommendationPersonalized {
		t.Fatal("changing strategy type rewrote the historical snapshot")
	}
	workspace, err := s.Workspace(ctx, "100001")
	if err != nil {
		t.Fatal(err)
	}
	running, typedHistory := 0, map[string]string{}
	for _, version := range workspace.Experiments {
		if version.Status == "running" {
			running++
			if version.ExperimentID != typed.ExperimentID {
				t.Fatal("previous experiment remained active")
			}
		}
	}
	for _, history := range workspace.StrategyHistory {
		if history.ExperimentID == typed.ExperimentID {
			typedHistory[history.StrategyID] = history.RecommendationType
		}
	}
	if running != 1 || typedHistory["a"] != domain.RecommendationPersonalized || typedHistory["related_a"] != domain.RecommendationRelated {
		t.Fatalf("incorrect active experiment or typed history: %d %+v", running, typedHistory)
	}
	latestSource, err := s.SaveSource(ctx, 1, domain.RecallSource{SourceID: "latest", Name: "最新上架", Kind: "global", Producer: "hot", HotPeriod: "newest", KeyTemplate: "hot_{appid}_newest", ValueFormat: "compact_score"})
	if err != nil {
		t.Fatal(err)
	}
	newestExperiment := domain.RecommendationExperiment{AppID: "100001", ExperimentID: "exp_newest", Name: "统一最新召回", BucketBy: "user_id", Controls: controls}
	for _, kind := range []string{domain.RecommendationPersonalized, domain.RecommendationRelated} {
		id := "fresh_p"
		if kind == domain.RecommendationRelated {
			id = "fresh_r"
		}
		strategy := domain.RecommendationStrategy{AppID: "100001", StrategyID: id, Name: "最新召回", RecommendationType: kind, Rank: "recall_weight", Recall: []domain.StrategyRecall{{SourceID: "icf", Weight: 3, RecallRate: 1}, {SourceID: latestSource.SourceID, Weight: 2, RecallRate: .25}}}
		legacy := strategy
		legacy.Newest = domain.StrategyNewest{SourceID: latestSource.SourceID, SharePercent: 25, Scenes: []string{"discover"}}
		if _, err := s.SaveStrategy(ctx, 1, legacy); err == nil {
			t.Fatal("new strategy accepted a separate newest stage")
		} else if apiError, ok := err.(*apperror.Error); !ok || apiError.Status != 400 {
			t.Fatalf("legacy newest stage must produce a client error: %v", err)
		}
		created, err := s.SaveStrategy(ctx, 1, strategy)
		if err != nil {
			t.Fatal(err)
		}
		newestExperiment.Assignments = append(newestExperiment.Assignments, domain.ExperimentAssignment{StrategyID: id, StrategyVersion: created.Version, BucketStart: 1, BucketEnd: 29})
	}
	newestVersion, err := s.SaveExperiment(ctx, 1, newestExperiment)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish(ctx, 1, "100001", newestExperiment.ExperimentID, newestVersion.Version, publication.Revision); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Strategies []map[string]json.RawMessage `json:"strategies"`
	}
	if err := json.Unmarshal([]byte(redisClient.Client.Get(ctx, key).Val()), &payload); err != nil || len(payload.Strategies) != 4 {
		t.Fatalf("newest recalls were not published for both types: %+v %v", payload, err)
	}
	for _, strategy := range payload.Strategies {
		if _, ok := strategy["newest"]; ok {
			t.Fatal("new snapshots emitted a separate newest stage")
		}
		var recall []domain.StrategyRecall
		if err := json.Unmarshal(strategy["recall"], &recall); err != nil {
			t.Fatal(err)
		}
		var id string
		if err := json.Unmarshal(strategy["strategy_id"], &id); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(id, "base_") {
			if len(recall) != 1 || recall[0].SourceID != "icf" {
				t.Fatalf("control recall changed: %+v", recall)
			}
			continue
		}
		if len(recall) != 2 || recall[1].SourceID != latestSource.SourceID || recall[1].Weight != 2 || recall[1].RecallRate != .25 {
			t.Fatalf("newest recall settings were not frozen intact: %+v %v", recall, err)
		}
	}
}
