package backend_test

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wzshiming/gitd/internal/utils"
	backendhttp "github.com/wzshiming/gitd/pkg/backend/http"
	"github.com/wzshiming/gitd/pkg/storage"
)

// runGitCmd runs a git command in the specified directory.
func runGitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := utils.Command(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Git command failed: git %s\nError: %v\nOutput: %s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func TestHTTPGitProtocol(t *testing.T) {
	// Create a temporary data directory
	dataDir, err := os.MkdirTemp("", "http-test-data")
	if err != nil {
		t.Fatalf("Failed to create temp data dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(dataDir) }()

	// Create a temporary directory for client operations
	clientDir, err := os.MkdirTemp("", "http-test-client")
	if err != nil {
		t.Fatalf("Failed to create temp client dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(clientDir) }()

	stor := storage.NewStorage(storage.WithRootDir(dataDir))
	repositoriesDir := stor.RepositoriesDir()
	if err := os.MkdirAll(repositoriesDir, 0755); err != nil {
		t.Fatalf("Failed to create repositories dir: %v", err)
	}

	// Create a bare repository
	repoName := "test-repo"
	repoPath := filepath.Join(repositoriesDir, repoName+".git")
	runGitCmd(t, "", "init", "--bare", repoPath)

	handler := backendhttp.NewHandler(backendhttp.WithStorage(stor))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	repoURL := srv.URL + "/" + repoName + ".git"

	t.Run("CloneEmptyRepository", func(t *testing.T) {
		cloneDir := filepath.Join(clientDir, "clone-empty")
		runGitCmd(t, "", "clone", repoURL, cloneDir)

		gitDir := filepath.Join(cloneDir, ".git")
		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			t.Errorf(".git directory not found in cloned repository")
		}
	})

	t.Run("PushToRepository", func(t *testing.T) {
		workDir := filepath.Join(clientDir, "clone-empty")

		runGitCmd(t, workDir, "config", "user.email", "test@test.com")
		runGitCmd(t, workDir, "config", "user.name", "Test User")

		testFile := filepath.Join(workDir, "README.md")
		if err := os.WriteFile(testFile, []byte("# HTTP Test Repository\n"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		runGitCmd(t, workDir, "add", "README.md")
		runGitCmd(t, workDir, "commit", "-m", "Initial commit")
		runGitCmd(t, workDir, "push", "-u", "origin", "master")
	})

	t.Run("CloneWithContent", func(t *testing.T) {
		cloneDir := filepath.Join(clientDir, "clone-with-content")
		runGitCmd(t, "", "clone", repoURL, cloneDir)

		readmePath := filepath.Join(cloneDir, "README.md")
		content, err := os.ReadFile(readmePath)
		if err != nil {
			t.Fatalf("Failed to read README.md: %v", err)
		}
		if string(content) != "# HTTP Test Repository\n" {
			t.Errorf("Unexpected content: %s", content)
		}
	})

	t.Run("PushMoreCommitsAndPull", func(t *testing.T) {
		workDir := filepath.Join(clientDir, "clone-with-content")

		runGitCmd(t, workDir, "config", "user.email", "test@test.com")
		runGitCmd(t, workDir, "config", "user.name", "Test User")

		testFile := filepath.Join(workDir, "file2.txt")
		if err := os.WriteFile(testFile, []byte("Second file\n"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		runGitCmd(t, workDir, "add", "file2.txt")
		runGitCmd(t, workDir, "commit", "-m", "Add second file")
		runGitCmd(t, workDir, "push")

		// Pull the new commit in the first clone
		pullDir := filepath.Join(clientDir, "clone-empty")
		runGitCmd(t, pullDir, "config", "user.email", "test@test.com")
		runGitCmd(t, pullDir, "config", "user.name", "Test User")
		runGitCmd(t, pullDir, "pull")

		file2Path := filepath.Join(pullDir, "file2.txt")
		if _, err := os.Stat(file2Path); os.IsNotExist(err) {
			t.Errorf("file2.txt not found after pull")
		}
	})
}
