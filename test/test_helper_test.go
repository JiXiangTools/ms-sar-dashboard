package test

import (
	"os"
	"path/filepath"
	"testing"
)

func repoPath(parts ...string) string {
	values := append([]string{".."}, parts...)
	return filepath.Join(values...)
}

func readRepoFile(t *testing.T, parts ...string) string {
	t.Helper()

	raw, err := os.ReadFile(repoPath(parts...))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(parts...), err)
	}
	return string(raw)
}
