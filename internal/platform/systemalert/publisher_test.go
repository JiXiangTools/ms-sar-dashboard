package systemalert

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestObserverPreservesLogsAndFiltersSeverity(t *testing.T) {
	for _, tc := range []struct {
		name, raw, fallback string
		want                bool
	}{
		{"json error", `{"level":"ERROR","event":"recommend.failed","msg":"failed","request_id":"rq_12","error":"Redis timeout","appid":"100001"}`, "INFO", true},
		{"json info", `{"level":"INFO","event":"error.example","msg":"ERROR in documentation"}`, "INFO", false},
		{"json warning", `{"level":"WARN","event":"request.error","msg":"retry"}`, "INFO", false},
		{"dashboard error", `2026/11/12 12:00:00 level=error event=config.restore request_id=rq_12 msg="restore failed" error="Redis timeout"`, "INFO", true},
		{"search error", `2026-11-12 level=ERROR event=search.failed msg="query failed" request_id=rq_12`, "INFO", true},
		{"receiver error", `error 2026/11/12 event=projection.failed request_id=rq_12 error="Redis timeout"`, "ERROR", true},
		{"explicit warning", `level=WARN event=client_auth_stale_used`, "ERROR", false},
		{"critical", `level=CRITICAL event=archive.failed msg="disk unavailable"`, "INFO", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &Publisher{queue: make(chan Alert, 8), module: "test-module", env: "test", instance: "instance-one"}
			var output bytes.Buffer
			n, err := p.Wrap(&output, tc.fallback).Write([]byte(tc.raw))
			if err != nil || n != len(tc.raw) || output.String() != tc.raw {
				t.Fatalf("original log changed: %q %v", output.String(), err)
			}
			if (len(p.queue) == 1) != tc.want {
				t.Fatalf("queued=%d, want=%v", len(p.queue), tc.want)
			}
			if tc.want {
				a := <-p.queue
				if a.SchemaVersion != 1 || a.Module != "test-module" || a.Env != "test" || a.AlertID == "" || a.CreatedAt < time.Now().Add(-time.Second).Unix() || a.Location == "" {
					t.Fatalf("missing alert fields: %+v", a)
				}
			}
		})
	}
}

func TestEmitDoesNotWaitForRedisOrQueueCapacity(t *testing.T) {
	// No Redis client or worker: enqueue must never try network I/O or wait.
	p := &Publisher{queue: make(chan Alert, 1), module: "test", env: "test"}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			p.Emit("ERROR", "test.failed", "unavailable", "rq", "test.location", nil)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("caller blocked on full alert queue")
	}
	if p.Dropped() != 999 || len(p.queue) != 1 {
		t.Fatalf("dropped=%d queued=%d", p.Dropped(), len(p.queue))
	}
	p.closed.Store(true)
	if p.Emit("ERROR", "test.failed", "closed", "", "", nil) {
		t.Fatal("closed publisher accepted event")
	}
}

func TestAlertParametersAreBoundedAndRedacted(t *testing.T) {
	p := &Publisher{queue: make(chan Alert, 1), module: "test", env: "test"}
	p.Emit("ERROR", "test.failed", "password=private secret=hidden redis://account:credential@localhost error", "rq_12", "test.location", map[string]any{
		"appid": "100001", "error": "token=private-token " + strings.Repeat("中", 2000), "user_id": "web-private", "authorization": "private-auth", "query": "private-query",
	})
	a := <-p.queue
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private", "hidden", "credential", "web-private", "private-query"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("sensitive data leaked: %s", raw)
		}
	}
	if len(a.Reason) > 1030 || len(a.Params["error"].(string)) > 260 || a.Params["appid"] != "100001" || a.RequestID != "rq_12" {
		t.Fatalf("bounds or context changed: %+v", a)
	}
}

func TestConnectValidatesModeAndBoundsPool(t *testing.T) {
	for _, mode := range []string{"single", "cluster"} {
		t.Run(mode, func(t *testing.T) {
			client, err := Connect(RedisConfig{Mode: mode, Addrs: []string{"127.0.0.1:1"}})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			switch c := client.(type) {
			case *redis.Client:
				if c.Options().PoolSize != 2 || !c.Options().ContextTimeoutEnabled {
					t.Fatal("unbounded publisher pool")
				}
			case *redis.ClusterClient:
				if c.Options().PoolSize != 2 || !c.Options().ContextTimeoutEnabled {
					t.Fatal("unbounded cluster pool")
				}
			default:
				t.Fatalf("unexpected client %T", client)
			}
		})
	}
	if _, err := Connect(RedisConfig{Mode: "cluster", Addrs: []string{"127.0.0.1:1"}, DB: 1}); err == nil {
		t.Fatal("Cluster DB accepted")
	}
}

func TestRedactsQuotedSecretsAndBearerCredentials(t *testing.T) {
	for _, value := range []string{`authorization=Bearer hidden-auth`, `secret="a private value"`, `{"password":"private-value"}`} {
		got := clean(value, 1024)
		if strings.Contains(got, "hidden-auth") || strings.Contains(got, "private") {
			t.Fatalf("redaction missed credential: %s", got)
		}
	}
}

func TestTruncatedFieldsStillFitTheAlertContract(t *testing.T) {
	p := &Publisher{queue: make(chan Alert, 1), module: "test", env: "test"}
	p.Emit("ERROR", strings.Repeat("中", 100), "failure", strings.Repeat("r", 200), "location", nil)
	a := <-p.queue
	if len(a.Event) > 128 || len(a.RequestID) > 128 {
		t.Fatal("truncated fields exceed consumer schema limits")
	}
}
