package config

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/version"
)

const envPrefix = "MSSAR"

type Config struct {
	App            AppConfig            `mapstructure:"app"`
	Security       SecurityConfig       `mapstructure:"security"`
	Auth           AuthConfig           `mapstructure:"auth"`
	SSO            SSOConfig            `mapstructure:"sso"`
	Log            LogConfig            `mapstructure:"log"`
	Database       DatabaseConfig       `mapstructure:"database"`
	Redis          RedisConfig          `mapstructure:"redis"`
	Elasticsearch  ElasticsearchConfig  `mapstructure:"elasticsearch"`
	RecommendDebug RecommendDebugConfig `mapstructure:"recommend_debug"`
}

const DefaultMaxRequestBodyBytes int64 = 1024 * 1024

type AppConfig struct {
	Name              string        `mapstructure:"name"`
	Env               string        `mapstructure:"env"`
	Host              string        `mapstructure:"host"`
	Port              int           `mapstructure:"port"`
	ReadHeaderTimeout time.Duration `mapstructure:"read_header_timeout"`
	ReadTimeout       time.Duration `mapstructure:"read_timeout"`
	WriteTimeout      time.Duration `mapstructure:"write_timeout"`
	IdleTimeout       time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout   time.Duration `mapstructure:"shutdown_timeout"`
	Version           string        `mapstructure:"version"`
}

type SecurityConfig struct {
	MaxRequestBodyBytes   int64         `mapstructure:"max_request_body_bytes"`
	TrustedProxyCIDRs     []string      `mapstructure:"trusted_proxy_cidrs"`
	RateLimitStoreTimeout time.Duration `mapstructure:"rate_limit_store_timeout"`
}

type LogConfig struct {
	Level string `mapstructure:"level"`
}

type AuthConfig struct {
	JWTSecret      string        `mapstructure:"jwt_secret"`
	AccessTokenTTL time.Duration `mapstructure:"access_token_ttl"`
	Issuer         string        `mapstructure:"issuer"`
}

type SSOConfig struct {
	Enabled        bool          `mapstructure:"enabled"`
	AdminUIURL     string        `mapstructure:"admin_ui_url"`
	APIBaseURL     string        `mapstructure:"api_base_url"`
	AppID          string        `mapstructure:"app_id"`
	AppSecret      string        `mapstructure:"app_secret"`
	RedirectURL    string        `mapstructure:"redirect_url"`
	RequestTimeout time.Duration `mapstructure:"request_timeout"`
}

type DatabaseConfig struct {
	DSN                string        `mapstructure:"dsn"`
	MaxOpenConns       int           `mapstructure:"max_open_conns"`
	MaxIdleConns       int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime    time.Duration `mapstructure:"conn_max_lifetime"`
	HealthCheckTimeout time.Duration `mapstructure:"health_check_timeout"`
	MigrationTimeout   time.Duration `mapstructure:"migration_timeout"`
}

type RedisConfig struct {
	Mode               string        `mapstructure:"mode"`
	Addrs              []string      `mapstructure:"addrs"`
	KeyPrefix          string        `mapstructure:"key_prefix"`
	Username           string        `mapstructure:"username"`
	Password           string        `mapstructure:"password"`
	MasterName         string        `mapstructure:"master_name"`
	SentinelUsername   string        `mapstructure:"sentinel_username"`
	SentinelPassword   string        `mapstructure:"sentinel_password"`
	DB                 int           `mapstructure:"db"`
	DialTimeout        time.Duration `mapstructure:"dial_timeout"`
	ReadTimeout        time.Duration `mapstructure:"read_timeout"`
	WriteTimeout       time.Duration `mapstructure:"write_timeout"`
	HealthCheckTimeout time.Duration `mapstructure:"health_check_timeout"`
}

type ElasticsearchConfig struct {
	Addrs            []string      `mapstructure:"addrs"`
	Username         string        `mapstructure:"username"`
	Password         string        `mapstructure:"password"`
	ItemIndexPrefix  string        `mapstructure:"item_index_prefix"`
	RequestTimeout   time.Duration `mapstructure:"request_timeout"`
	MaxResponseBytes int64         `mapstructure:"max_response_bytes"`
	DebugEnabled     bool          `mapstructure:"debug_enabled"`
}

type RecommendDebugConfig struct {
	MaxCandidateLimit int           `mapstructure:"max_candidate_limit"`
	DebugEnabled      bool          `mapstructure:"debug_enabled"`
	OnlineBaseURL     string        `mapstructure:"online_base_url"`
	RequestTimeout    time.Duration `mapstructure:"request_timeout"`
}

func Load(path string, environment string) (Config, error) {
	v := viper.New()
	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	setDefaults(v)

	resolvedEnv := detectEnvironment(environment)
	resolvedPath, err := resolveConfigPath(path, resolvedEnv)
	if err != nil {
		return Config{}, err
	}

	v.SetConfigFile(resolvedPath)
	if err := v.ReadInConfig(); err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", resolvedPath, err)
	}

	if v.GetString("app.env") == "" {
		v.Set("app.env", resolvedEnv)
	}
	if v.GetString("app.version") == "" {
		v.Set("app.version", version.Version)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) {
		dc.WeaklyTypedInput = true
		dc.DecodeHook = mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			stringToSliceHookFunc(),
		)
	}); err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}
	if err := validate(cfg); err != nil {
		return Config{}, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("app.name", version.Name)
	v.SetDefault("app.env", "dev")
	v.SetDefault("app.host", "127.0.0.1")
	v.SetDefault("app.port", 8081)
	v.SetDefault("app.read_header_timeout", "3s")
	v.SetDefault("app.read_timeout", "5s")
	v.SetDefault("app.write_timeout", "10s")
	v.SetDefault("app.idle_timeout", "60s")
	v.SetDefault("app.shutdown_timeout", "10s")
	v.SetDefault("app.version", version.Version)

	v.SetDefault("security.max_request_body_bytes", DefaultMaxRequestBodyBytes)
	v.SetDefault("security.trusted_proxy_cidrs", []string{})
	v.SetDefault("security.rate_limit_store_timeout", "100ms")

	v.SetDefault("auth.jwt_secret", "")
	v.SetDefault("auth.access_token_ttl", "2h")
	v.SetDefault("auth.issuer", version.Name)

	v.SetDefault("sso.enabled", false)
	v.SetDefault("sso.admin_ui_url", "")
	v.SetDefault("sso.api_base_url", "")
	v.SetDefault("sso.app_id", "")
	v.SetDefault("sso.app_secret", "")
	v.SetDefault("sso.redirect_url", "")
	v.SetDefault("sso.request_timeout", "3s")

	v.SetDefault("log.level", "info")

	v.SetDefault("database.dsn", "postgres://postgres:postgres@127.0.0.1:5432/ms_sar_dashboard?sslmode=disable")
	v.SetDefault("database.max_open_conns", 20)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("database.conn_max_lifetime", "30m")
	v.SetDefault("database.health_check_timeout", "2s")
	v.SetDefault("database.migration_timeout", "60s")

	v.SetDefault("redis.mode", "standalone")
	v.SetDefault("redis.addrs", []string{"127.0.0.1:6379"})
	v.SetDefault("redis.key_prefix", "")
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.dial_timeout", "2s")
	v.SetDefault("redis.read_timeout", "2s")
	v.SetDefault("redis.write_timeout", "2s")
	v.SetDefault("redis.health_check_timeout", "2s")

	v.SetDefault("elasticsearch.addrs", []string{"http://127.0.0.1:9200"})
	v.SetDefault("elasticsearch.item_index_prefix", "ms_search_item")
	v.SetDefault("elasticsearch.request_timeout", "5s")
	v.SetDefault("elasticsearch.max_response_bytes", int64(4*1024*1024))
	v.SetDefault("elasticsearch.debug_enabled", true)

	v.SetDefault("recommend_debug.max_candidate_limit", 1000)
	v.SetDefault("recommend_debug.debug_enabled", true)
	v.SetDefault("recommend_debug.online_base_url", "http://127.0.0.1:18082")
	v.SetDefault("recommend_debug.request_timeout", "5s")
}

func validate(cfg Config) error {
	if cfg.App.Port <= 0 || cfg.App.Port > 65535 {
		return errors.New("app.port must be between 1 and 65535")
	}
	if cfg.App.ReadHeaderTimeout <= 0 || cfg.App.ReadTimeout <= 0 || cfg.App.WriteTimeout <= 0 || cfg.App.IdleTimeout <= 0 || cfg.App.ShutdownTimeout <= 0 {
		return errors.New("app timeouts must be positive")
	}
	if cfg.Security.MaxRequestBodyBytes <= 0 {
		return errors.New("security.max_request_body_bytes must be positive")
	}
	if cfg.Security.RateLimitStoreTimeout <= 0 {
		return errors.New("security.rate_limit_store_timeout must be positive")
	}
	for _, rawPrefix := range cfg.Security.TrustedProxyCIDRs {
		if _, err := netip.ParsePrefix(strings.TrimSpace(rawPrefix)); err != nil {
			return fmt.Errorf("security.trusted_proxy_cidrs contains invalid CIDR %q", rawPrefix)
		}
	}
	if cfg.Auth.AccessTokenTTL <= 0 {
		return errors.New("auth.access_token_ttl must be positive")
	}
	if strings.TrimSpace(cfg.Database.DSN) == "" || cfg.Database.MaxOpenConns <= 0 || cfg.Database.MaxIdleConns < 0 || cfg.Database.MaxIdleConns > cfg.Database.MaxOpenConns || cfg.Database.ConnMaxLifetime <= 0 || cfg.Database.HealthCheckTimeout <= 0 || cfg.Database.MigrationTimeout <= 0 {
		return errors.New("database pool and timeout settings are invalid")
	}
	if len(cfg.Redis.Addrs) == 0 || cfg.Redis.DB < 0 || cfg.Redis.DialTimeout <= 0 || cfg.Redis.ReadTimeout <= 0 || cfg.Redis.WriteTimeout <= 0 || cfg.Redis.HealthCheckTimeout <= 0 {
		return errors.New("redis addresses, db, and timeouts must be valid")
	}
	if cfg.SSO.RequestTimeout <= 0 || cfg.Elasticsearch.RequestTimeout <= 0 || cfg.Elasticsearch.MaxResponseBytes <= 0 || cfg.RecommendDebug.RequestTimeout <= 0 || cfg.RecommendDebug.MaxCandidateLimit <= 0 {
		return errors.New("external request timeouts and limits must be positive")
	}
	environment := strings.ToLower(strings.TrimSpace(cfg.App.Env))
	jwtSecret := strings.TrimSpace(cfg.Auth.JWTSecret)
	if jwtSecret != "" && isKnownInsecureSecret(jwtSecret) {
		return errors.New("auth.jwt_secret uses a known insecure default")
	}
	if environment == "prod" || environment == "production" {
		if len(jwtSecret) < 32 {
			return errors.New("auth.jwt_secret must be set to at least 32 characters in production")
		}
	}
	if cfg.SSO.Enabled {
		for name, value := range map[string]string{
			"sso.admin_ui_url": cfg.SSO.AdminUIURL,
			"sso.api_base_url": cfg.SSO.APIBaseURL,
			"sso.app_id":       cfg.SSO.AppID,
			"sso.app_secret":   cfg.SSO.AppSecret,
			"sso.redirect_url": cfg.SSO.RedirectURL,
		} {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("%s is required when SSO is enabled", name)
			}
		}
	}
	return nil
}

func isKnownInsecureSecret(secret string) bool {
	switch strings.TrimSpace(secret) {
	case "ms-sar-dashboard-dev-secret", "ms-sar-dashboard-compose-secret":
		return true
	default:
		return false
	}
}

func detectEnvironment(value string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	if envValue := strings.TrimSpace(os.Getenv(envPrefix + "_APP_ENV")); envValue != "" {
		return envValue
	}
	return "dev"
}

func resolveConfigPath(path string, environment string) (string, error) {
	target := path
	if strings.TrimSpace(target) == "" {
		target = filepath.Join("configs", environment+".yaml")
	}

	if filepath.IsAbs(target) {
		if _, err := os.Stat(target); err != nil {
			return "", fmt.Errorf("stat config path %s: %w", target, err)
		}
		return target, nil
	}

	resolved, err := searchInParentDirs(target)
	if err != nil {
		return "", fmt.Errorf("locate config path %s: %w", target, err)
	}
	return resolved, nil
}

func searchInParentDirs(target string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	current := cwd
	for {
		candidate := filepath.Clean(filepath.Join(current, target))
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return "", os.ErrNotExist
}

func stringToSliceHookFunc() mapstructure.DecodeHookFuncType {
	return func(from reflect.Type, to reflect.Type, data interface{}) (interface{}, error) {
		if from.Kind() != reflect.String || to != reflect.TypeOf([]string{}) {
			return data, nil
		}

		raw := strings.TrimSpace(data.(string))
		if raw == "" {
			return []string{}, nil
		}

		parts := strings.Split(raw, ",")
		values := make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				values = append(values, trimmed)
			}
		}
		return values, nil
	}
}
