package test

import (
	"os"
	"testing"
)

func TestProjectLayoutMatchesMsUserCenterConvention(t *testing.T) {
	requiredFiles := []string{
		".dockerignore",
		"Dockerfile",
		"docker-compose.yml",
		"admin/build-image.sh",
		"admin/start-debug.sh",
		"admin/init-local-pg.sh",
		"admin/docker-postgres-init.sh",
		"deploy/.env",
		"deploy/docker-compose.yml",
		"test/shell/curl.sh",
	}

	for _, path := range requiredFiles {
		if _, err := os.Stat(repoPath(path)); err != nil {
			t.Fatalf("required project file %s is missing: %v", path, err)
		}
	}
}

func TestAdminScriptsAreExecutable(t *testing.T) {
	scripts := []string{
		"admin/build-image.sh",
		"admin/start-debug.sh",
		"admin/init-local-pg.sh",
		"admin/docker-postgres-init.sh",
		"test/shell/curl.sh",
	}

	for _, path := range scripts {
		info, err := os.Stat(repoPath(path))
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("%s must be executable", path)
		}
	}
}
