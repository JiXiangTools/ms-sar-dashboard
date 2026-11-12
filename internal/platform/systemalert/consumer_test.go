package systemalert

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSendRequiresBothHTTPAndBusinessSuccess(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"success", 200, `{"code":0}`, true},
		{"legacy success", 200, `{"StatusCode":0}`, true},
		{"business failure", 200, `{"code":19001,"msg":"failed"}`, false},
		{"new code wins", 200, `{"code":19001,"StatusCode":0}`, false},
		{"legacy failure", 200, `{"StatusCode":19001}`, false},
		{"HTTP failure", 503, `{"code":0}`, false},
		{"redirect", 302, `{"code":0}`, false},
		{"missing code", 200, `{}`, false},
		{"invalid response", 200, `not JSON`, false},
		{"oversized response", 200, strings.Repeat("x", 4097), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Authorization") != "" {
					t.Error("unexpected webhook request")
				}
				var body struct {
					Type    string            `json:"msg_type"`
					Content map[string]string `json:"content"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Type != "text" || body.Content["text"] != "test notification" {
					t.Error("invalid Feishu message")
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			c := NewConsumer(nil, "", server.URL)
			err := c.send(context.Background(), "test notification")
			if (err == nil) != tc.want {
				t.Fatalf("send error=%v, want success=%v", err, tc.want)
			}
		})
	}
}

func TestSendTimeoutDoesNotExposeWebhookOrRetryRecursively(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); <-r.Context().Done() }))
	defer server.Close()
	c := NewConsumer(nil, "", server.URL+"/private-hook")
	c.http.Timeout = 20 * time.Millisecond
	err := c.send(context.Background(), "test notification")
	if err == nil || strings.Contains(err.Error(), "private-hook") || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("unsafe timeout error: %v", err)
	}
}

func TestRenderAggregatesAndBoundsNotification(t *testing.T) {
	groups := map[string][]delivery{}
	var keys []string
	for i := 0; i < 10; i++ {
		key := fmt.Sprint(i)
		keys = append(keys, key)
		for j := 0; j < 30; j++ {
			a := Alert{SchemaVersion: 1, AlertID: fmt.Sprintf("a_%d", j), Level: "ERROR", Reason: strings.Repeat("中", 2000), Module: "ms-rec-online", Event: strings.Repeat("e", 128), Location: strings.Repeat("位", 1000), CreatedAt: int64(100 + j), Env: "test", RequestID: "rq_12", Params: map[string]any{"error": strings.Repeat("错", 1000), "secret": "private"}}
			groups[key] = append(groups[key], delivery{alert: a})
		}
	}
	text := render(groups, keys)
	raw, _ := json.Marshal(map[string]any{"msg_type": "text", "content": map[string]string{"text": text}})
	if len(raw) > 19000 || !strings.Contains(text, "次数：30") || !strings.Contains(text, "request_id：rq_12") || strings.Contains(text, "private") {
		t.Fatalf("bad notification size=%d: %s", len(raw), text)
	}
}

func TestDisabledConsumerDoesNoIO(t *testing.T) {
	c := NewConsumer(nil, "", "")
	c.local.SetOutput(io.Discard)
	if err := c.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.Run(context.Background())
}
