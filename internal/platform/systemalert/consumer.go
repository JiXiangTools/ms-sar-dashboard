package systemalert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const ConsumerGroup = "dashboard-system-alert-v1"
const PollInterval = 5 * time.Second
const maxAttempts = 10

var alertTimezone = time.FixedZone("Asia/Shanghai", 8*60*60)

type Consumer struct {
	client             redis.UniversalClient
	key, webhook, name string
	claimCursor        string
	http               *http.Client
	local              *log.Logger
	now                func() time.Time
}

func NewConsumer(client redis.UniversalClient, prefix, webhook string) *Consumer {
	return &Consumer{client: client, key: prefix + StreamName, webhook: webhook, name: newID(), claimCursor: "0-0", now: time.Now,
		http:  &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		local: log.New(os.Stderr, "system-alert-consumer ", log.LstdFlags|log.LUTC)}
}

// Run uses its own Redis pool and never calls the application's alert logger.
func (c *Consumer) Run(ctx context.Context) {
	if c.webhook == "" {
		return
	}
	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.Poll(ctx); err != nil {
				c.local.Printf("level=ERROR event=alert_delivery_failed reason=%s", clean(err.Error(), 512))
			}
		}
	}
}

type retryState struct {
	Attempts int   `json:"attempts"`
	Next     int64 `json:"next"`
}
type delivery struct {
	message redis.XMessage
	alert   Alert
}

func (c *Consumer) Poll(ctx context.Context) error {
	if c.webhook == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if err := c.client.XGroupCreateMkStream(ctx, c.key, ConsumerGroup, "0").Err(); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	var messages []redis.XMessage
	// Read own pending, recover abandoned work, and still read new messages.
	// A bad/retrying old record must not prevent fresh alerts from progressing.
	for _, cursor := range []string{"0", ">"} {
		streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: ConsumerGroup, Consumer: c.name, Streams: []string{c.key, cursor}, Count: 100, Block: -1}).Result()
		if err != nil && err != redis.Nil {
			return err
		}
		for _, stream := range streams {
			messages = append(messages, stream.Messages...)
		}
	}
	claimed, nextCursor, err := c.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{Stream: c.key, Group: ConsumerGroup, Consumer: c.name, MinIdle: time.Minute, Start: c.claimCursor, Count: 100}).Result()
	if err != nil && err != redis.Nil {
		return err
	}
	c.claimCursor = nextCursor
	messages = append(messages, claimed...)
	seen := map[string]bool{}
	groups := map[string][]delivery{}
	var failures []error
	for _, message := range messages {
		if seen[message.ID] {
			continue
		}
		seen[message.ID] = true
		var a Alert
		raw, ok := message.Values["alert"].(string)
		if !ok || len(raw) > MaxPayloadBytes || json.Unmarshal([]byte(raw), &a) != nil || !validAlert(a) {
			if err := c.isolate(ctx, message, "invalid or unsupported alert payload"); err != nil {
				failures = append(failures, err)
			}
			continue
		}
		if !IsError(a.Level) {
			if err := c.ack(ctx, []string{message.ID}); err != nil {
				return err
			}
			continue
		}
		state, err := c.retry(ctx, message.ID)
		if err != nil {
			return err
		}
		if state.Next > c.now().Unix() {
			continue
		}
		key := a.Env + "\x00" + a.Module + "\x00" + a.Level + "\x00" + a.Event
		groups[key] = append(groups[key], delivery{message: message, alert: a})
	}
	if len(groups) == 0 {
		return errors.Join(failures...)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := groups[keys[i]][0], groups[keys[j]][0]
		if a.alert.CreatedAt != b.alert.CreatedAt {
			return a.alert.CreatedAt < b.alert.CreatedAt
		}
		return a.message.ID < b.message.ID
	})
	if len(keys) > 10 {
		keys = keys[:10]
	}
	var selected []delivery
	for _, key := range keys {
		selected = append(selected, groups[key]...)
	}
	// One shared rate slot: at most 60 sends/minute even with many Dashboards.
	allowed, err := c.client.SetNX(ctx, c.key+":send-slot", c.name, time.Second).Result()
	if err != nil {
		return err
	}
	if !allowed {
		return errors.Join(failures...)
	}
	text := render(groups, keys)
	if err = c.send(ctx, text); err != nil {
		for _, item := range selected {
			state, stateErr := c.retry(ctx, item.message.ID)
			if stateErr != nil {
				return stateErr
			}
			state.Attempts++
			if state.Attempts >= maxAttempts {
				if e := c.isolate(ctx, item.message, "delivery attempts exhausted: "+clean(err.Error(), 256)); e != nil {
					return e
				}
				continue
			}
			delay := 5 * time.Second << uint(state.Attempts-1)
			if delay > 5*time.Minute {
				delay = 5 * time.Minute
			}
			state.Next = c.now().Add(delay).Unix()
			raw, _ := json.Marshal(state)
			if e := c.client.HSet(ctx, c.key+":retry", item.message.ID, string(raw)).Err(); e != nil {
				return e
			}
		}
		return err
	}
	ids := make([]string, 0, len(selected))
	for _, item := range selected {
		ids = append(ids, item.message.ID)
	}
	failures = append(failures, c.ack(ctx, ids))
	return errors.Join(failures...)
}

func validAlert(a Alert) bool {
	return a.SchemaVersion == 1 && a.AlertID != "" && len(a.AlertID) <= 128 && a.Module != "" && len(a.Module) <= 128 &&
		a.Event != "" && len(a.Event) <= 128 && a.Reason != "" && len(a.Reason) <= 4096 && a.CreatedAt > 0 &&
		len(a.Location) <= 1024 && len(a.RequestID) <= 128 && len(a.Env) <= 128 && len(a.InstanceID) <= 256 && len(a.Params) <= 32 &&
		(a.Level == "DEBUG" || a.Level == "INFO" || a.Level == "WARN" || a.Level == "WARNING" || IsError(a.Level))
}

func (c *Consumer) retry(ctx context.Context, id string) (retryState, error) {
	var state retryState
	raw, err := c.client.HGet(ctx, c.key+":retry", id).Result()
	if err == redis.Nil {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err = json.Unmarshal([]byte(raw), &state); err != nil {
		return state, fmt.Errorf("invalid alert retry state")
	}
	return state, nil
}

var ackScript = redis.NewScript(`redis.call('XACK',KEYS[1],ARGV[1],unpack(ARGV,2)); return redis.call('XDEL',KEYS[1],unpack(ARGV,2))`)

func (c *Consumer) ack(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := []any{ConsumerGroup}
	for _, id := range ids {
		args = append(args, id)
	}
	if err := ackScript.Run(ctx, c.client, []string{c.key}, args...).Err(); err != nil {
		return err
	}
	return c.client.HDel(ctx, c.key+":retry", ids...).Err()
}

func (c *Consumer) isolate(ctx context.Context, message redis.XMessage, reason string) error {
	raw, err := json.Marshal(message.Values)
	if err != nil {
		return err
	}
	// Quarantine succeeds before ACK. No multi-key Lua, so Cluster slots may differ.
	accepted, err := c.client.Eval(ctx, enqueueScript, []string{c.key + ":quarantine"}, MaxStreamEntries, "stream_id", message.ID, "fields", string(raw), "error", reason, "created_at", c.now().Unix()).Int()
	if err != nil {
		return err
	}
	if accepted == 0 {
		return fmt.Errorf("alert quarantine is full; message remains pending")
	}
	return c.ack(ctx, []string{message.ID})
}

func (c *Consumer) send(ctx context.Context, text string) error {
	body, _ := json.Marshal(map[string]any{"msg_type": "text", "content": map[string]string{"text": text}})
	if len(body) > 19000 {
		return fmt.Errorf("alert notification exceeds size limit")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.webhook, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("invalid alert webhook")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Feishu request failed (%T)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Feishu HTTP status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if err != nil || len(raw) > 4096 {
		return fmt.Errorf("invalid Feishu response")
	}
	var result struct {
		Code       *int `json:"code"`
		LegacyCode *int `json:"StatusCode"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return fmt.Errorf("invalid Feishu response")
	}
	if result.Code != nil && *result.Code == 0 {
		return nil
	}
	if result.Code == nil && result.LegacyCode != nil && *result.LegacyCode == 0 {
		return nil
	}
	if result.Code != nil {
		return fmt.Errorf("Feishu rejected alert (code=%d)", *result.Code)
	}
	if result.LegacyCode != nil {
		return fmt.Errorf("Feishu rejected alert (code=%d)", *result.LegacyCode)
	}
	return fmt.Errorf("Feishu response missing business code")
}

func render(groups map[string][]delivery, keys []string) string {
	var b strings.Builder
	b.WriteString("搜广推系统告警\n")
	for _, key := range keys {
		items := groups[key]
		sample := items[len(items)-1].alert
		first, last := sample.CreatedAt, sample.CreatedAt
		unique := map[string]bool{}
		var requestIDs []string
		for _, item := range items {
			a := item.alert
			unique[a.AlertID] = true
			if a.CreatedAt < first {
				first = a.CreatedAt
			}
			if a.CreatedAt > last {
				last = a.CreatedAt
			}
			if a.RequestID != "" && len(requestIDs) < 3 {
				requestIDs = append(requestIDs, a.RequestID)
			}
		}
		params, _ := json.Marshal(safeParams(sample.Params))
		fmt.Fprintf(&b, "\n环境：%s\n模块：%s\n级别：%s\n事件：%s\n原因：%s\n位置：%s\n次数：%d\n时间：%s ～ %s\nrequest_id：%s\n参数：%s\n告警ID：%s\n",
			clean(sample.Env, 64), clean(sample.Module, 64), sample.Level, clean(sample.Event, 96), clean(sample.Reason, 160), clean(sample.Location, 128), len(unique),
			time.Unix(first, 0).In(alertTimezone).Format(time.RFC3339), time.Unix(last, 0).In(alertTimezone).Format(time.RFC3339), clean(strings.Join(requestIDs, ", "), 128), clean(string(params), 160), clean(sample.AlertID, 64))
	}
	return b.String()
}
