package gitio

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectFastSeparatesCodeAndGitSizes(t *testing.T) {
	repo := t.TempDir()
	gitDir := filepath.Join(repo, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repo, "main.go"), "12345")
	writeTestFile(t, filepath.Join(repo, "README.md"), "123")
	writeTestFile(t, filepath.Join(gitDir, "config"), "1234567")
	writeTestFile(t, filepath.Join(gitDir, "objects", "pack"), "12")

	stat := CollectFast(repo)
	if stat.CodeSize != 8 {
		t.Fatalf("code size = %d, want 8", stat.CodeSize)
	}
	if stat.DotGitSize != 9 {
		t.Fatalf(".git size = %d, want 9", stat.DotGitSize)
	}
	if stat.CodeSize+stat.DotGitSize != 17 {
		t.Fatalf("combined size = %d, want 17", stat.CodeSize+stat.DotGitSize)
	}
}

func TestCollectFastExcludesNestedGitDirectoriesFromCodeSize(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "nested", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repo, "source.txt"), "code")
	writeTestFile(t, filepath.Join(repo, "nested", ".git", "index"), "metadata")

	stat := CollectFast(repo)
	if stat.CodeSize != 4 {
		t.Fatalf("code size = %d, want 4", stat.CodeSize)
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
