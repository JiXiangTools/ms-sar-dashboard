package cache

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/kely-jian/ms-sar-dashboard/internal/config"
)

type Client struct {
	Client redis.UniversalClient
	cfg    config.RedisConfig
}

const (
	ModeStandalone = "standalone"
	ModeCluster    = "cluster"
	ModeSentinel   = "sentinel"
)

func New(cfg config.RedisConfig) (*Client, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	client := &Client{cfg: cfg}
	client.Client = redis.NewUniversalClient(buildUniversalOptions(cfg))
	return client, nil
}

func buildUniversalOptions(cfg config.RedisConfig) *redis.UniversalOptions {
	mode := normalizeMode(cfg.Mode)
	db := cfg.DB
	if mode == ModeCluster {
		db = 0
	}
	options := &redis.UniversalOptions{
		Addrs:        cfg.Addrs,
		Username:     cfg.Username,
		Password:     cfg.Password,
		DB:           db,
		MasterName:   strings.TrimSpace(cfg.MasterName),
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		MaxRetries:   1,
	}
	if mode == ModeCluster {
		options.IsClusterMode = true
	}
	if mode == ModeSentinel {
		options.SentinelUsername = strings.TrimSpace(cfg.SentinelUsername)
		options.SentinelPassword = cfg.SentinelPassword
		return options
	}
	options.MasterName = ""
	return options
}

func (c *Client) Name() string {
	return "redis"
}

func (c *Client) Check(ctx context.Context) (string, error) {
	if c.Client == nil {
		return "down", errors.New("redis client is nil")
	}
	if err := c.ping(ctx); err != nil {
		return "down", err
	}
	return "up", nil
}

func (c *Client) Close() error {
	if c == nil || c.Client == nil {
		return nil
	}
	return c.Client.Close()
}

func (c *Client) ping(ctx context.Context) error {
	if c.Client == nil {
		return errors.New("redis client is nil")
	}
	if c.cfg.HealthCheckTimeout <= 0 {
		return c.Client.Ping(ctx).Err()
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, c.cfg.HealthCheckTimeout)
	defer cancel()
	return c.Client.Ping(timeoutCtx).Err()
}

func validateConfig(cfg config.RedisConfig) error {
	mode := normalizeMode(cfg.Mode)
	if len(cfg.Addrs) == 0 {
		return errors.New("redis addrs must not be empty")
	}
	switch mode {
	case ModeStandalone:
		if len(cfg.Addrs) != 1 {
			return errors.New("redis standalone mode requires exactly one addr")
		}
	case ModeCluster:
		if cfg.DB != 0 {
			return errors.New("redis cluster mode requires db 0")
		}
		return nil
	case ModeSentinel:
		if strings.TrimSpace(cfg.MasterName) == "" {
			return errors.New("redis sentinel mode requires master_name")
		}
	default:
		return fmt.Errorf("unsupported redis mode %q", cfg.Mode)
	}
	return nil
}

func normalizeMode(mode string) string {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	if normalized == "" {
		return ModeStandalone
	}
	return normalized
}
