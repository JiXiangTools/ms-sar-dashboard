package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
)

type Client struct {
	cfg    config.ElasticsearchConfig
	base   *url.URL
	client *http.Client
}

func New(cfg config.ElasticsearchConfig) (*Client, error) {
	var base *url.URL
	if len(cfg.Addrs) > 0 {
		parsed, err := url.Parse(strings.TrimSpace(cfg.Addrs[0]))
		if err != nil {
			return nil, fmt.Errorf("parse elasticsearch addr: %w", err)
		}
		if parsed.Scheme == "" {
			parsed.Scheme = "http"
		}
		if parsed.Host == "" {
			return nil, fmt.Errorf("invalid elasticsearch addr %q", cfg.Addrs[0])
		}
		base = parsed
	}

	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	return &Client{
		cfg:  cfg,
		base: base,
		client: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func (c *Client) Name() string {
	return "elasticsearch"
}

func (c *Client) Check(ctx context.Context) (string, error) {
	if c == nil || c.base == nil {
		return "down", errors.New("elasticsearch client is not configured")
	}
	_, err := c.do(ctx, http.MethodGet, "/_cluster/health", nil)
	if err != nil {
		return "down", err
	}
	return "up", nil
}

func (c *Client) IndexExists(ctx context.Context, index string) (bool, error) {
	if err := c.ensureConfigured(); err != nil {
		return false, err
	}
	req, err := c.newRequest(ctx, http.MethodHead, "/"+url.PathEscape(index), nil)
	if err != nil {
		return false, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxReadLimit(c.cfg.MaxResponseBytes)))
		return false, fmt.Errorf("elasticsearch head index %s: status %d: %s", index, resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

func (c *Client) IndexInfo(ctx context.Context, index string) (map[string]any, error) {
	exists, err := c.IndexExists(ctx, index)
	if err != nil {
		return nil, err
	}
	if !exists {
		return map[string]any{
			"index":  index,
			"exists": false,
		}, nil
	}

	mapping, err := c.getJSON(ctx, http.MethodGet, "/"+url.PathEscape(index)+"/_mapping")
	if err != nil {
		return nil, err
	}
	settings, err := c.getJSON(ctx, http.MethodGet, "/"+url.PathEscape(index)+"/_settings")
	if err != nil {
		return nil, err
	}
	count, err := c.getJSON(ctx, http.MethodGet, "/"+url.PathEscape(index)+"/_count")
	if err != nil {
		return nil, err
	}
	health, err := c.getJSON(ctx, http.MethodGet, "/_cluster/health")
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"index":    index,
		"exists":   true,
		"mapping":  mapping,
		"settings": settings,
		"count":    count,
		"health":   health,
	}, nil
}

func (c *Client) Document(ctx context.Context, index string, id string) (map[string]any, error) {
	if err := c.ensureConfigured(); err != nil {
		return nil, err
	}
	return c.getJSON(ctx, http.MethodGet, "/"+url.PathEscape(index)+"/_doc/"+url.PathEscape(id))
}

func (c *Client) Search(ctx context.Context, index string, body []byte) (map[string]any, error) {
	if err := c.ensureConfigured(); err != nil {
		return nil, err
	}
	return c.postJSON(ctx, "/"+url.PathEscape(index)+"/_search", body)
}

func (c *Client) Raw(ctx context.Context, method string, path string, body []byte) (any, error) {
	if err := c.ensureConfigured(); err != nil {
		return nil, err
	}
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReadLimit(c.cfg.MaxResponseBytes)))
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]any{"status_code": resp.StatusCode}, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return map[string]any{
			"status_code": resp.StatusCode,
			"body":        strings.TrimSpace(string(raw)),
		}, nil
	}
	return value, nil
}

func (c *Client) getJSON(ctx context.Context, method string, path string) (map[string]any, error) {
	raw, err := c.do(ctx, method, path, nil)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func (c *Client) postJSON(ctx context.Context, path string, body []byte) (map[string]any, error) {
	raw, err := c.do(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func (c *Client) do(ctx context.Context, method string, path string, body []byte) ([]byte, error) {
	if err := c.ensureConfigured(); err != nil {
		return nil, err
	}
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReadLimit(c.cfg.MaxResponseBytes)))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("elasticsearch %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

func (c *Client) newRequest(ctx context.Context, method string, path string, body []byte) (*http.Request, error) {
	if err := c.ensureConfigured(); err != nil {
		return nil, err
	}
	endpoint := *c.base
	requestPath, rawQuery, _ := strings.Cut(path, "?")
	endpoint.Path = joinPath(endpoint.Path, requestPath)
	endpoint.RawQuery = rawQuery
	reader := bytes.NewReader(body)
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if username := strings.TrimSpace(c.cfg.Username); username != "" {
		req.SetBasicAuth(username, c.cfg.Password)
	}
	return req, nil
}

func (c *Client) ensureConfigured() error {
	if c == nil || c.base == nil {
		return errors.New("elasticsearch client is not configured")
	}
	if c.client == nil {
		return errors.New("elasticsearch http client is nil")
	}
	return nil
}

func joinPath(base string, p string) string {
	if base == "" {
		return p
	}
	if strings.HasSuffix(base, "/") {
		base = strings.TrimSuffix(base, "/")
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return base + p
}

func maxReadLimit(limit int64) int64 {
	if limit <= 0 {
		return 4 * 1024 * 1024
	}
	return limit
}
