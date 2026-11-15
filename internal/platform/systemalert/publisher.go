// Package systemalert publishes bounded, asynchronous operational alerts.
package systemalert

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"
)

const StreamName = "system-alert"
const MaxPayloadBytes = 8192
const MaxStreamEntries = 10000

// Reject new work at capacity; never trim messages that have not been ACKed.
const enqueueScript = `if redis.call('XLEN',KEYS[1]) >= tonumber(ARGV[1]) then return 0 end
redis.call('XADD',KEYS[1],'*',unpack(ARGV,2)); return 1`

type Alert struct {
	SchemaVersion int            `json:"schema_version"`
	AlertID       string         `json:"alert_id"`
	Level         string         `json:"level"`
	Reason        string         `json:"reason"`
	RequestID     string         `json:"request_id"`
	Location      string         `json:"location"`
	Params        map[string]any `json:"params"`
	CreatedAt     int64          `json:"created_at"`
	Module        string         `json:"module"`
	Event         string         `json:"event"`
	Env           string         `json:"env"`
	InstanceID    string         `json:"instance_id"`
}

// RedisConfig points at the same Redis deployment and DB as the shared queue.
type RedisConfig struct {
	Mode             string   `mapstructure:"mode"`
	Addrs            []string `mapstructure:"addrs"`
	Username         string   `mapstructure:"username"`
	Password         string   `mapstructure:"password"`
	DB               int      `mapstructure:"db"`
	MasterName       string   `mapstructure:"master_name"`
	SentinelUsername string   `mapstructure:"sentinel_username"`
	SentinelPassword string   `mapstructure:"sentinel_password"`
	KeyPrefix        string   `mapstructure:"key_prefix"`
}

func (c RedisConfig) Validate() error {
	if len(c.Addrs) == 0 || c.DB < 0 {
		return fmt.Errorf("alert Redis requires addresses and a nonnegative DB")
	}
	switch strings.ToLower(strings.TrimSpace(c.Mode)) {
	case "", "single", "standalone":
		if len(c.Addrs) != 1 {
			return fmt.Errorf("alert Redis single mode requires one address")
		}
	case "cluster":
		if c.DB != 0 {
			return fmt.Errorf("alert Redis Cluster requires DB 0")
		}
	case "sentinel":
		if c.MasterName == "" {
			return fmt.Errorf("alert Redis Sentinel requires master_name")
		}
	default:
		return fmt.Errorf("invalid alert Redis mode")
	}
	return nil
}

// A separate, small pool prevents alert writes from using business connections.
func Connect(c RedisConfig) (redis.UniversalClient, error) {
	c.Mode = strings.ToLower(strings.TrimSpace(c.Mode))
	if err := c.Validate(); err != nil {
		return nil, err
	}
	o := &redis.UniversalOptions{Addrs: c.Addrs, Username: c.Username, Password: c.Password, DB: c.DB,
		DialTimeout: 300 * time.Millisecond, ReadTimeout: 300 * time.Millisecond, WriteTimeout: 300 * time.Millisecond,
		PoolTimeout: 300 * time.Millisecond, PoolSize: 2, MaxRetries: -1, ContextTimeoutEnabled: true}
	if c.Mode == "cluster" {
		o.IsClusterMode = true
	}
	if c.Mode == "sentinel" {
		o.MasterName = c.MasterName
		o.SentinelUsername = c.SentinelUsername
		o.SentinelPassword = c.SentinelPassword
	}
	return redis.NewUniversalClient(o), nil
}

type Publisher struct {
	client                     redis.UniversalClient
	key, module, env, instance string
	queue                      chan Alert
	stop, done                 chan struct{}
	ctx                        context.Context
	cancel                     context.CancelFunc
	once                       sync.Once
	closed                     atomic.Bool
	dropped                    atomic.Uint64
	local                      *log.Logger
}

// NewPublisher owns client; callers must Close it during graceful shutdown.
func NewPublisher(client redis.UniversalClient, prefix, module, env string) *Publisher {
	ctx, cancel := context.WithCancel(context.Background())
	host, _ := os.Hostname()
	p := &Publisher{client: client, key: prefix + StreamName, module: clean(module, 128), env: clean(env, 128), instance: clean(host, 256),
		queue: make(chan Alert, 1024), stop: make(chan struct{}), done: make(chan struct{}), ctx: ctx, cancel: cancel,
		local: log.New(os.Stderr, "system-alert ", log.LstdFlags|log.LUTC)}
	go p.run()
	return p
}

func IsError(level string) bool {
	switch strings.ToUpper(level) {
	case "ERROR", "CRITICAL", "FATAL", "PANIC":
		return true
	}
	return false
}

// Emit never performs network I/O and never waits for queue capacity.
func (p *Publisher) Emit(level, event, reason, requestID, location string, params map[string]any) bool {
	if p == nil || !IsError(level) || p.closed.Load() {
		return false
	}
	if len(p.queue) == cap(p.queue) {
		p.dropped.Add(1)
		return false
	}
	a := Alert{SchemaVersion: 1, AlertID: newID(), Level: strings.ToUpper(level), Reason: clean(reason, 1024),
		RequestID: clean(requestID, 128), Location: clean(location, 256), Params: safeParams(params), CreatedAt: time.Now().Unix(),
		Module: p.module, Event: clean(event, 128), Env: p.env, InstanceID: p.instance}
	select {
	case p.queue <- a:
		return true
	default:
		p.dropped.Add(1)
		return false
	}
}

func (p *Publisher) Dropped() uint64 {
	if p == nil {
		return 0
	}
	return p.dropped.Load()
}

func (p *Publisher) Close() error {
	if p == nil {
		return nil
	}
	p.once.Do(func() { p.closed.Store(true); close(p.stop) })
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		p.cancel()
		<-p.done
	}
	p.cancel()
	if count := p.Dropped(); count > 0 {
		p.local.Printf("level=ERROR event=alerts_dropped total=%d", count)
	}
	return p.client.Close()
}

func (p *Publisher) run() {
	defer close(p.done)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var reported uint64
	for {
		select {
		case a := <-p.queue:
			p.flush(a)
		case <-ticker.C:
			if count := p.Dropped(); count != reported {
				p.local.Printf("level=ERROR event=alerts_dropped total=%d", count)
				reported = count
			}
		case <-p.stop:
			for {
				select {
				case a := <-p.queue:
					p.flush(a)
				default:
					return
				}
			}
		case <-p.ctx.Done():
			return
		}
	}
}

func (p *Publisher) flush(first Alert) {
	batch := []Alert{first}
	for len(batch) < 100 {
		select {
		case a := <-p.queue:
			batch = append(batch, a)
		default:
			goto send
		}
	}
send:
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		ctx, cancel := context.WithTimeout(p.ctx, 300*time.Millisecond)
		pipe := p.client.Pipeline()
		var writes []*redis.Cmd
		for _, a := range batch {
			raw, marshalErr := json.Marshal(a)
			if len(raw) > MaxPayloadBytes {
				a.Params = map[string]any{}
				raw, marshalErr = json.Marshal(a)
			}
			if marshalErr != nil || len(raw) > MaxPayloadBytes {
				p.dropped.Add(1)
				continue
			}
			writes = append(writes, pipe.Eval(ctx, enqueueScript, []string{p.key}, MaxStreamEntries, "alert", string(raw)))
		}
		_, err = pipe.Exec(ctx)
		cancel()
		if err == nil {
			for _, write := range writes {
				if accepted, _ := write.Int(); accepted == 0 {
					p.dropped.Add(1)
				}
			}
			return
		}
		select {
		case <-p.ctx.Done():
			attempt = 3
		case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
		}
	}
	p.dropped.Add(uint64(len(batch)))
	p.local.Printf("level=ERROR event=alert_enqueue_failed dropped=%d reason=%s", len(batch), clean(err.Error(), 512))
}

type observer struct {
	output    io.Writer
	publisher *Publisher
	fallback  string
}

func (p *Publisher) Wrap(output io.Writer, fallbackLevel string) io.Writer {
	if p == nil {
		return output
	}
	return &observer{output: output, publisher: p, fallback: fallbackLevel}
}

func (w *observer) Write(raw []byte) (int, error) {
	n, err := w.output.Write(raw)
	// Skip the overwhelmingly common non-error records before parsing fields.
	text := string(raw)
	if !IsError(w.fallback) && !strings.Contains(text, "ERROR") && !strings.Contains(text, "error") && !strings.Contains(text, "FATAL") && !strings.Contains(text, "CRITICAL") && !strings.Contains(text, "PANIC") {
		return n, err
	}
	fields := map[string]any{}
	if json.Unmarshal(raw, &fields) != nil {
		fields = parseFields(text)
	}
	level, _ := fields["level"].(string)
	if level == "" {
		level = w.fallback
	}
	if !IsError(level) {
		return n, err
	}
	get := func(k string) string {
		if v, ok := fields[k]; ok {
			return fmt.Sprint(v)
		}
		return ""
	}
	event := get("event")
	if event == "" {
		event = "application.error"
	}
	reason := get("msg")
	if reason == "" {
		reason = get("error")
	}
	if reason == "" {
		reason = text
	}
	if detail := get("error"); detail != "" && detail != reason {
		reason += ": " + detail
	}
	location := get("location")
	if location == "" {
		location = callerLocation(event)
	}
	w.publisher.Emit(level, event, reason, get("request_id"), location, fields)
	return n, err
}

func callerLocation(fallback string) string {
	var stack [24]uintptr
	frames := runtime.CallersFrames(stack[:runtime.Callers(2, stack[:])])
	for {
		frame, more := frames.Next()
		file := frame.File
		if (strings.Contains(file, "/internal/") || strings.Contains(file, "/cmd/")) && !strings.Contains(file, "/systemalert/") && !strings.Contains(file, "/logx/") && !strings.Contains(file, "/logging/") {
			start := strings.LastIndex(file, "/internal/")
			if start < 0 {
				start = strings.LastIndex(file, "/cmd/")
			}
			return fmt.Sprintf("%s:%d", file[start+1:], frame.Line)
		}
		if !more {
			break
		}
	}
	return fallback
}

// Existing services emit JSON or logfmt; quoted values retain spaces/escapes.
func parseFields(text string) map[string]any {
	out := map[string]any{}
	for len(text) > 0 && len(out) < 64 {
		text = strings.TrimLeft(text, " \t\r\n")
		end := strings.IndexAny(text, " \t\r\n=")
		if end < 0 {
			break
		}
		if text[end] != '=' {
			text = text[end+1:]
			continue
		}
		key := text[:end]
		text = text[end+1:]
		if strings.HasPrefix(text, "\"") {
			i := 1
			for i < len(text) {
				if text[i] == '\\' {
					i += 2
					continue
				}
				if text[i] == '"' {
					break
				}
				i++
			}
			if i >= len(text) {
				break
			}
			value, e := strconv.Unquote(text[:i+1])
			if e == nil {
				out[key] = value
			}
			text = text[i+1:]
		} else {
			i := strings.IndexAny(text, " \t\r\n")
			if i < 0 {
				i = len(text)
			}
			out[key] = text[:i]
			text = text[i:]
		}
	}
	return out
}

var sensitive = regexp.MustCompile(`(?i)(password|secret|token|authorization|cookie|user_id|anonymous_id|session_id|client_ip|user_agent)[\s"']*[:=]\s*(?:"(?:\\.|[^"\\])*"|'[^']*'|(?:(?:Bearer|Basic)\s+)?[^\s,;]+)`)
var credentials = regexp.MustCompile(`(https?://|rediss?://)[^\s/@]+:[^\s/@]+@`)

func clean(s string, limit int) string {
	s = credentials.ReplaceAllString(s, "${1}[redacted]@")
	s = sensitive.ReplaceAllString(s, "${1}=[redacted]")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if len(s) > limit {
		s = s[:max(0, limit-len("…"))]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
		if limit >= len("…") {
			s += "…"
		}
	}
	return s
}

func safeParams(fields map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"appid", "status", "operation", "op", "kind", "recall_id", "strategy_id", "experiment_id", "stream", "stream_id", "code", "count", "retry", "error", "cost_ms", "method", "route"} {
		if v, ok := fields[key]; ok {
			switch x := v.(type) {
			case float64:
				if !math.IsNaN(x) && !math.IsInf(x, 0) {
					out[key] = x
				}
			case bool, int, int64:
				out[key] = x
			case string:
				out[key] = clean(x, 256)
			}
		}
	}
	return out
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("a_%d", time.Now().UnixNano())
	}
	return "a_" + hex.EncodeToString(b[:])
}
