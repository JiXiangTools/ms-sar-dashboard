package test

import (
	"strings"
	"testing"
)

func TestStartDebugScriptBootstrapsServicesDeploy(t *testing.T) {
	script := readRepoFile(t, "admin", "start-debug.sh")
	helper := readRepoFile(t, "admin", "services-deploy-env.sh")

	required := []string{
		`SERVICES_DEPLOY_DIR="${MSSAR_SERVICES_DEPLOY_DIR}"`,
		"init-local-pg.sh",
		"services-deploy-postgres",
		"services-deploy-redis",
		"services-deploy-elasticsearch",
		"services-deploy-env.sh",
		"services_compose_up postgres redis elasticsearch",
		"KEYS 'app_auth_*'",
		"MSSAR_REDIS_PASSWORD",
		"--no-auth-warning",
	}
	for _, value := range required {
		if !strings.Contains(script, value) {
			t.Fatalf("start-debug.sh must contain %q", value)
		}
	}

	for _, value := range []string{
		"docker daemon is not running",
		"POSTGRES_IMAGE",
		"REDIS_PASSWORD",
		"ELASTICSEARCH_IMAGE",
		"KAFKA_IMAGE",
		"SERVICE_DEPLOY_NET_SUBNET",
	} {
		if !strings.Contains(helper, value) {
			t.Fatalf("services-deploy-env.sh must contain %q", value)
		}
	}

	for _, value := range []string{"DEFAULT_APP_ID", "DEFAULT_APP_NAME", "DEFAULT_APP_SECRET", "HSET"} {
		if strings.Contains(script, value) {
			t.Fatalf("start-debug.sh must not contain %q", value)
		}
	}
}

func TestInitDataSeedsAdminOnly(t *testing.T) {
	sql := readRepoFile(t, "upgrade", "sql", "data.sql")

	for _, value := range []string{"admin_name", "admin_password_hash"} {
		if !strings.Contains(sql, value) {
			t.Fatalf("data.sql must contain %q", value)
		}
	}

	for _, value := range []string{"default_app_id", "default_app_name", "default_app_secret", "t_app_id_seq"} {
		if strings.Contains(sql, value) {
			t.Fatalf("data.sql must not contain %q", value)
		}
	}
}
