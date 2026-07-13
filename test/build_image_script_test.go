package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildImageWritesIgnoredComposeEnvWithoutEditingCompose(t *testing.T) {
	composePath := repoPath("docker-compose.yml")
	before, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("read compose before build: %v", err)
	}
	envFile := filepath.Join(t.TempDir(), ".compose-image.env")
	repoRoot, err := filepath.Abs(repoPath())
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	cmd := exec.Command("sh", filepath.Join(repoRoot, "admin", "build-image.sh"))
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"DOCKER_BIN=true",
		"COMMIT_ID=test",
		"IMAGE_NAME=registry.example.test/ms/sar-dashboard:test",
		"COMPOSE_ENV_FILE="+envFile,
		"SYNC_COMPOSE_IMAGE=true",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run build-image.sh: %v\n%s", err, output)
	}

	content, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read compose image env: %v", err)
	}
	if got, want := string(content), "MS_SAR_DASHBOARD_IMAGE=registry.example.test/ms/sar-dashboard:test\n"; got != want {
		t.Fatalf("compose image env = %q, want %q", got, want)
	}
	after, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("read compose after build: %v", err)
	}
	if string(after) != string(before) {
		t.Fatal("build-image.sh must not edit tracked docker-compose.yml")
	}
}

func TestComposeUsesOneImageForApplicationAndMigrator(t *testing.T) {
	compose := readRepoFile(t, "docker-compose.yml")
	if !strings.Contains(compose, `${MS_SAR_DASHBOARD_IMAGE:-ms-sar-dashboard:local}`) {
		t.Fatal("compose must use the local-safe MS_SAR_DASHBOARD_IMAGE default")
	}
	if count := strings.Count(compose, "image: *ms-sar-dashboard-image"); count != 2 {
		t.Fatalf("application and migrator must share the same image anchor, got %d references", count)
	}
	ignore := readRepoFile(t, ".gitignore")
	if !strings.Contains(ignore, ".compose-image.env") {
		t.Fatal("local compose image env must be gitignored")
	}
}
