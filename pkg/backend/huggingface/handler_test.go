package huggingface_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wzshiming/gitd/internal/utils"
	"github.com/wzshiming/gitd/pkg/backend/huggingface"
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

// setupTestRepo creates a bare repository pre-populated with a file commit.
// Returns the repositories directory and the bare repo path.
func setupTestRepo(t *testing.T, repoName string) (dataDir string) {
	t.Helper()

	dataDir, err := os.MkdirTemp("", "hf-test-data")
	if err != nil {
		t.Fatalf("Failed to create temp data dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })

	repositoriesDir := filepath.Join(dataDir, "repositories")
	if err := os.MkdirAll(repositoriesDir, 0755); err != nil {
		t.Fatalf("Failed to create repositories dir: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(filepath.Join(repositoriesDir, repoName+".git")), 0755); err != nil {
		t.Fatalf("Failed to create repo parent dir: %v", err)
	}

	bareRepoPath := filepath.Join(repositoriesDir, repoName+".git")
	runGitCmd(t, "", "init", "--bare", "-b", "main", bareRepoPath)

	// Create a working directory to make a commit
	workDir, err := os.MkdirTemp("", "hf-test-work")
	if err != nil {
		t.Fatalf("Failed to create temp work dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	runGitCmd(t, workDir, "init", "-b", "main", workDir)
	runGitCmd(t, workDir, "config", "user.email", "test@test.com")
	runGitCmd(t, workDir, "config", "user.name", "Test User")

	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("# Test Model\n"), 0644); err != nil {
		t.Fatalf("Failed to create README.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "config.json"), []byte(`{"model": "test"}`), 0644); err != nil {
		t.Fatalf("Failed to create config.json: %v", err)
	}

	runGitCmd(t, workDir, "add", ".")
	runGitCmd(t, workDir, "commit", "-m", "Initial commit")
	runGitCmd(t, workDir, "remote", "add", "origin", bareRepoPath)
	runGitCmd(t, workDir, "push", "origin", "main")

	return dataDir
}

func newHFTestServer(t *testing.T, dataDir string) *httptest.Server {
	t.Helper()
	stor := storage.NewStorage(storage.WithRootDir(dataDir))
	handler := huggingface.NewHandler(huggingface.WithStorage(stor))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestHFModelInfo(t *testing.T) {
	dataDir := setupTestRepo(t, "myorg/mymodel")
	srv := newHFTestServer(t, dataDir)

	resp, err := http.Get(srv.URL + "/api/models/myorg/mymodel")
	if err != nil {
		t.Fatalf("GET /api/models/myorg/mymodel: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
	}

	var info struct {
		ID            string `json:"id"`
		ModelID       string `json:"modelId"`
		DefaultBranch string `json:"defaultBranch"`
		Siblings      []struct {
			RFilename string `json:"rfilename"`
		} `json:"siblings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("Failed to decode model info: %v", err)
	}

	if info.ID != "myorg/mymodel" {
		t.Errorf("Expected id 'myorg/mymodel', got %q", info.ID)
	}
	if info.DefaultBranch != "main" {
		t.Errorf("Expected default branch 'main', got %q", info.DefaultBranch)
	}
	if len(info.Siblings) == 0 {
		t.Error("Expected at least one sibling (file) in model info")
	}
}

func TestHFModelInfoNotFound(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "hf-notfound-data")
	if err != nil {
		t.Fatalf("Failed to create temp data dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(dataDir) }()

	srv := newHFTestServer(t, dataDir)

	resp, err := http.Get(srv.URL + "/api/models/nonexistent/model")
	if err != nil {
		t.Fatalf("GET /api/models/nonexistent/model: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 Not Found, got %d", resp.StatusCode)
	}
}

func TestHFModelInfoRevision(t *testing.T) {
	dataDir := setupTestRepo(t, "myorg/mymodel")
	srv := newHFTestServer(t, dataDir)

	resp, err := http.Get(srv.URL + "/api/models/myorg/mymodel/revision/main")
	if err != nil {
		t.Fatalf("GET /api/models/myorg/mymodel/revision/main: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
	}

	var info struct {
		ID       string `json:"id"`
		SHA      string `json:"sha"`
		Siblings []struct {
			RFilename string `json:"rfilename"`
		} `json:"siblings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("Failed to decode model info revision: %v", err)
	}

	if info.ID != "myorg/mymodel" {
		t.Errorf("Expected id 'myorg/mymodel', got %q", info.ID)
	}
	if info.SHA == "" {
		t.Error("Expected non-empty SHA in model info revision")
	}
	if len(info.Siblings) == 0 {
		t.Error("Expected at least one sibling in model info revision")
	}
}

func TestHFTree(t *testing.T) {
	dataDir := setupTestRepo(t, "myorg/mymodel")
	srv := newHFTestServer(t, dataDir)

	resp, err := http.Get(srv.URL + "/api/models/myorg/mymodel/tree/main")
	if err != nil {
		t.Fatalf("GET /api/models/myorg/mymodel/tree/main: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
	}

	var entries []struct {
		Type string `json:"type"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		t.Fatalf("Failed to decode tree entries: %v", err)
	}

	if len(entries) == 0 {
		t.Error("Expected at least one tree entry")
	}
}

func TestHFResolve(t *testing.T) {
	dataDir := setupTestRepo(t, "myorg/mymodel")
	srv := newHFTestServer(t, dataDir)

	resp, err := http.Get(srv.URL + "/myorg/mymodel/resolve/main/README.md")
	if err != nil {
		t.Fatalf("GET /myorg/mymodel/resolve/main/README.md: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	if string(body) != "# Test Model\n" {
		t.Errorf("Unexpected content: %q", body)
	}

	// Check HuggingFace-required headers
	if resp.Header.Get("X-Repo-Commit") == "" {
		t.Error("Expected X-Repo-Commit header to be set")
	}
	if resp.Header.Get("ETag") == "" {
		t.Error("Expected ETag header to be set")
	}
}

func TestHFResolveNotFound(t *testing.T) {
	dataDir := setupTestRepo(t, "myorg/mymodel")
	srv := newHFTestServer(t, dataDir)

	resp, err := http.Get(srv.URL + "/myorg/mymodel/resolve/main/nonexistent.txt")
	if err != nil {
		t.Fatalf("GET /myorg/mymodel/resolve/main/nonexistent.txt: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 Not Found, got %d", resp.StatusCode)
	}
}

func TestHFResolveHead(t *testing.T) {
	dataDir := setupTestRepo(t, "myorg/mymodel")
	srv := newHFTestServer(t, dataDir)

	req, _ := http.NewRequest(http.MethodHead, srv.URL+"/myorg/mymodel/resolve/main/config.json", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HEAD /myorg/mymodel/resolve/main/config.json: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK for HEAD request, got %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Repo-Commit") == "" {
		t.Error("Expected X-Repo-Commit header to be set for HEAD request")
	}
}
