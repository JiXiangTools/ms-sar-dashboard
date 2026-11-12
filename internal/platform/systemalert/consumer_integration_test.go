//go:build integration

package systemalert

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestSystemAlertDelivery(t *testing.T) {
	for _, mode := range []string{"single", "cluster"} {
		t.Run(mode, func(t *testing.T) {
			addresses := os.Getenv("SAR_TEST_REDIS_SINGLE_ADDR")
			if mode == "cluster" {
				addresses = os.Getenv("SAR_TEST_REDIS_CLUSTER_ADDRS")
			}
			if addresses == "" {
				t.Skip("requires real Redis fixture")
			}
			options := RedisConfig{Mode: mode, Addrs: strings.Split(addresses, ","), Password: os.Getenv("SAR_TEST_REDIS_PASSWORD")}
			client, err := Connect(options)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			ctx := context.Background()
			prefix := fmt.Sprintf("alert_test_%d:", time.Now().UnixNano())
			key := prefix + StreamName
			defer client.Del(ctx, key, key+":retry", key+":send-slot", key+":quarantine")
			var status atomic.Int64
			status.Store(0)
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				raw, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(raw), "ms-rec-online") {
					t.Error("notification omitted module")
				}
				fmt.Fprintf(w, `{"code":%d}`, status.Load())
			}))
			defer server.Close()
			consumer := NewConsumer(client, prefix, server.URL)
			makeAlert := func(id string) Alert {
				return Alert{SchemaVersion: 1, AlertID: id, Level: "ERROR", Reason: "Redis unavailable", RequestID: "rq_12", Location: "recommend.publish", Params: map[string]any{"appid": "100001"}, CreatedAt: time.Now().Unix(), Module: "ms-rec-online", Event: "decision_publish_failed", Env: "test", InstanceID: "online-one"}
			}
			add := func(a Alert) string {
				t.Helper()
				raw, e := json.Marshal(a)
				if e != nil {
					t.Fatal(e)
				}
				id, e := client.XAdd(ctx, &redis.XAddArgs{Stream: key, Values: map[string]any{"alert": string(raw)}}).Result()
				if e != nil {
					t.Fatal(e)
				}
				return id
			}
			pending := func() int64 {
				t.Helper()
				v, e := client.XPending(ctx, key, ConsumerGroup).Result()
				if e != nil {
					t.Fatal(e)
				}
				return v.Count
			}
			unlock := func() {
				t.Helper()
				if e := client.Del(ctx, key+":send-slot").Err(); e != nil {
					t.Fatal(e)
				}
			}

			// A live producer drains before closing; the caller only enqueues locally.
			producerClient, e := Connect(options)
			if e != nil {
				t.Fatal(e)
			}
			publisher := NewPublisher(producerClient, prefix, "ms-rec-online", "test")
			publisher.Emit("INFO", "ignored", "not sent", "", "", nil)
			publisher.Emit("ERROR", "decision_publish_failed", "Redis unavailable", "rq_12", "recommend.publish", nil)
			if e := publisher.Close(); e != nil {
				t.Fatal(e)
			}
			if n := client.XLen(ctx, key).Val(); n != 1 {
				t.Fatalf("publisher close lost queued alert: %d", n)
			}
			// Unsupported versions are isolated, warnings removed, valid alerts sent.
			poison := makeAlert("poison")
			poison.SchemaVersion = 99
			add(poison)
			warning := makeAlert("warning")
			warning.Level = "WARN"
			add(warning)
			add(makeAlert("valid"))
			if e := consumer.Poll(ctx); e != nil {
				t.Fatal(e)
			}
			if client.XLen(ctx, key).Val() != 0 || pending() != 0 || client.XLen(ctx, key+":quarantine").Val() != 1 || calls.Load() != 1 {
				t.Fatal("invalid record blocked valid alert or ACK was lost")
			}

			// HTTP 200 with a business error remains pending and has a retry schedule.
			unlock()
			id := add(makeAlert("retry"))
			status.Store(19001)
			if e := consumer.Poll(ctx); e == nil {
				t.Fatal("business rejection accepted")
			}
			if client.XLen(ctx, key).Val() != 1 || pending() != 1 {
				t.Fatal("failed delivery was acknowledged")
			}
			before := calls.Load()
			if e := consumer.Poll(ctx); e != nil {
				t.Fatal(e)
			}
			if calls.Load() != before {
				t.Fatal("backoff ignored")
			}
			// A fresh valid event must still progress while the earlier event waits.
			unlock()
			add(makeAlert("fresh"))
			status.Store(0)
			if e := consumer.Poll(ctx); e != nil {
				t.Fatal(e)
			}
			if pending() != 1 || calls.Load() != before+1 {
				t.Fatal("old retry blocked new alert")
			}
			unlock()
			consumer.now = func() time.Time { return time.Now().Add(6 * time.Second) }
			if e := consumer.Poll(ctx); e != nil {
				t.Fatal(e)
			}
			if pending() != 0 || client.HExists(ctx, key+":retry", id).Val() {
				t.Fatal("successful retry was not cleaned")
			}
			consumer.now = time.Now

			// Another Dashboard takes ownership after the previous consumer disappears.
			unlock()
			abandoned := add(makeAlert("abandoned"))
			if e := client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: ConsumerGroup, Consumer: "dead-instance", Streams: []string{key, ">"}, Count: 1, Block: -1}).Err(); e != nil {
				t.Fatal(e)
			}
			if e := client.Do(ctx, "XCLAIM", key, ConsumerGroup, "dead-instance", 0, abandoned, "IDLE", 120000).Err(); e != nil {
				t.Fatal(e)
			}
			replacement := NewConsumer(client, prefix, server.URL)
			if e := replacement.Poll(ctx); e != nil {
				t.Fatal(e)
			}
			if pending() != 0 {
				t.Fatal("abandoned pending alert was not recovered")
			}

			// Persistent malformed messages do not stop good messages if quarantine fails.
			unlock()
			if e := client.Del(ctx, key+":quarantine").Err(); e != nil {
				t.Fatal(e)
			}
			if e := client.Set(ctx, key+":quarantine", "wrong type", 0).Err(); e != nil {
				t.Fatal(e)
			}
			badID := add(poison)
			add(makeAlert("good-after-bad"))
			before = calls.Load()
			if e := consumer.Poll(ctx); e == nil {
				t.Fatal("failed quarantine was hidden")
			}
			if pending() != 1 || calls.Load() != before+1 {
				t.Fatal("failed quarantine blocked good alert or acknowledged poison")
			}
			if e := client.Del(ctx, key+":quarantine").Err(); e != nil {
				t.Fatal(e)
			}
			if e := consumer.Poll(ctx); e != nil {
				t.Fatal(e)
			}
			if pending() != 0 {
				t.Fatal("poison could not be isolated after recovery")
			}
			rows := client.XRange(ctx, key+":quarantine", "-", "+").Val()
			if len(rows) != 1 || rows[0].Values["stream_id"] != badID {
				t.Fatal("quarantine lost original identity")
			}

			// Shared rate limit works across independent Dashboard instances.
			unlock()
			add(makeAlert("rate-one"))
			before = calls.Load()
			if e := consumer.Poll(ctx); e != nil {
				t.Fatal(e)
			}
			add(makeAlert("rate-two"))
			other := NewConsumer(client, prefix, server.URL)
			if e := other.Poll(ctx); e != nil {
				t.Fatal(e)
			}
			if calls.Load() != before+1 || pending() != 1 {
				t.Fatal("multi-instance rate limit failed")
			}
			unlock()
			if e := other.Poll(ctx); e != nil {
				t.Fatal(e)
			}
			if pending() != 0 {
				t.Fatal("rate-limited alert was lost")
			}
			capacityKey := prefix + "capacity"
			defer client.Del(ctx, capacityKey)
			if n, e := client.Eval(ctx, enqueueScript, []string{capacityKey}, 1, "alert", "first").Int(); e != nil || n != 1 {
				t.Fatal("capacity enqueue failed")
			}
			if n, e := client.Eval(ctx, enqueueScript, []string{capacityKey}, 1, "alert", "second").Int(); e != nil || n != 0 {
				t.Fatal("queue capacity was not enforced")
			}
			if entries := client.XRange(ctx, capacityKey, "-", "+").Val(); len(entries) != 1 || entries[0].Values["alert"] != "first" {
				t.Fatal("capacity limit trimmed existing pending data")
			}
			// Retry exhaustion persists the original event before releasing the queue.
			unlock()
			exhausted := add(makeAlert("exhausted"))
			state, _ := json.Marshal(retryState{Attempts: 9})
			if e := client.HSet(ctx, key+":retry", exhausted, string(state)).Err(); e != nil {
				t.Fatal(e)
			}
			status.Store(19001)
			if e := consumer.Poll(ctx); e == nil {
				t.Fatal("delivery exhaustion hidden")
			}
			if pending() != 0 || client.XLen(ctx, key+":quarantine").Val() != 2 {
				t.Fatal("retry exhaustion lost durable quarantine")
			}

			// Claim scans must progress beyond a page of recently delivered pending.
			unlock()
			status.Store(0)
			pipe := client.Pipeline()
			for i := 0; i < 1000; i++ {
				raw, _ := json.Marshal(makeAlert(fmt.Sprintf("recent-%d", i)))
				pipe.XAdd(ctx, &redis.XAddArgs{Stream: key, Values: map[string]any{"alert": string(raw)}})
			}
			if _, e := pipe.Exec(ctx); e != nil {
				t.Fatal(e)
			}
			tail := add(makeAlert("abandoned-tail"))
			if e := client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: ConsumerGroup, Consumer: "recent-owner", Streams: []string{key, ">"}, Count: 1001, Block: -1}).Err(); e != nil {
				t.Fatal(e)
			}
			if e := client.Do(ctx, "XCLAIM", key, ConsumerGroup, "recent-owner", 0, tail, "IDLE", 120000).Err(); e != nil {
				t.Fatal(e)
			}
			scanner := NewConsumer(client, prefix, server.URL)
			before = calls.Load()
			for i := 0; i < 2; i++ {
				if e := scanner.Poll(ctx); e != nil {
					t.Fatal(e)
				}
			}
			if calls.Load() != before+1 || pending() != 1000 {
				t.Fatal("claim scan starved abandoned messages beyond the first page")
			}
		})
	}
}
