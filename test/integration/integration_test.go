package integration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wzshiming/gitd/internal/utils"
	backendhttp "github.com/wzshiming/gitd/pkg/backend/http"
	backendlfs "github.com/wzshiming/gitd/pkg/backend/lfs"
	backendweb "github.com/wzshiming/gitd/pkg/backend/web"
	"github.com/wzshiming/gitd/pkg/storage"
)

// runGitCmd runs a git command in the specified directory with optional extra env.
func runGitCmd(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := utils.Command(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), append(env, "GIT_TERMINAL_PROMPT=0")...)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Git command failed: git %s\nError: %v\nOutput: %s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

// newTestStack creates a full HTTP server stack (web API + LFS + git HTTP)
// sharing the same storage and returns the test server.
func newTestStack(t *testing.T) (*httptest.Server, *storage.Storage) {
	t.Helper()

	dataDir, err := os.MkdirTemp("", "integration-test-data")
	if err != nil {
		t.Fatalf("Failed to create temp data dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })

	stor := storage.NewStorage(storage.WithRootDir(dataDir))

	// Build handler chain matching the production setup (innermost first):
	//   web -> lfs -> git-http
	var handler http.Handler
	handler = backendweb.NewHandler(backendweb.WithStorage(stor))
	handler = backendlfs.NewHandler(backendlfs.WithStorage(stor), backendlfs.WithNext(handler))
	handler = backendhttp.NewHandler(backendhttp.WithStorage(stor), backendhttp.WithNext(handler))

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, stor
}

// TestIntegrationHTTPWorkflow exercises the full lifecycle:
// create repo → clone (empty) → push → clone (with content) → pull.
func TestIntegrationHTTPWorkflow(t *testing.T) {
	srv, _ := newTestStack(t)

	clientDir, err := os.MkdirTemp("", "integration-test-client")
	if err != nil {
		t.Fatalf("Failed to create temp client dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(clientDir) }()

	// --- Step 1: Create repository via web API ---
	createResp, err := http.Post(srv.URL+"/api/repositories/myrepo.git", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/repositories/myrepo.git: %v", err)
	}
	createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201 Created, got %d", createResp.StatusCode)
	}

	repoURL := srv.URL + "/myrepo.git"

	// --- Step 2: Clone the empty repository ---
	cloneEmptyDir := filepath.Join(clientDir, "clone-empty")
	runGitCmd(t, "", nil, "clone", repoURL, cloneEmptyDir)

	if _, err := os.Stat(filepath.Join(cloneEmptyDir, ".git")); os.IsNotExist(err) {
		t.Fatal(".git directory not found in cloned repository")
	}

	// --- Step 3: Push content ---
	runGitCmd(t, cloneEmptyDir, nil, "config", "user.email", "test@test.com")
	runGitCmd(t, cloneEmptyDir, nil, "config", "user.name", "Test User")

	if err := os.WriteFile(filepath.Join(cloneEmptyDir, "README.md"), []byte("# Integration Test\n"), 0644); err != nil {
		t.Fatalf("Failed to create README.md: %v", err)
	}
	runGitCmd(t, cloneEmptyDir, nil, "add", "README.md")
	runGitCmd(t, cloneEmptyDir, nil, "commit", "-m", "Initial commit")
	runGitCmd(t, cloneEmptyDir, nil, "push", "-u", "origin", "master")

	// --- Step 4: Clone again and verify content ---
	cloneWithContentDir := filepath.Join(clientDir, "clone-with-content")
	runGitCmd(t, "", nil, "clone", repoURL, cloneWithContentDir)

	readmePath := filepath.Join(cloneWithContentDir, "README.md")
	content, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("Failed to read README.md from second clone: %v", err)
	}
	if string(content) != "# Integration Test\n" {
		t.Errorf("Unexpected README.md content: %q", content)
	}

	// --- Step 5: Push another commit and pull ---
	runGitCmd(t, cloneWithContentDir, nil, "config", "user.email", "test@test.com")
	runGitCmd(t, cloneWithContentDir, nil, "config", "user.name", "Test User")

	if err := os.WriteFile(filepath.Join(cloneWithContentDir, "data.txt"), []byte("some data\n"), 0644); err != nil {
		t.Fatalf("Failed to create data.txt: %v", err)
	}
	runGitCmd(t, cloneWithContentDir, nil, "add", "data.txt")
	runGitCmd(t, cloneWithContentDir, nil, "commit", "-m", "Add data file")
	runGitCmd(t, cloneWithContentDir, nil, "push")

	runGitCmd(t, cloneEmptyDir, nil, "pull")

	if _, err := os.Stat(filepath.Join(cloneEmptyDir, "data.txt")); os.IsNotExist(err) {
		t.Error("data.txt not found in first clone after pull")
	}

	// --- Step 6: Verify via web API ---
	getResp, err := http.Get(srv.URL + "/api/repositories/myrepo.git")
	if err != nil {
		t.Fatalf("GET /api/repositories/myrepo.git: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK from repo API, got %d", getResp.StatusCode)
	}

	var repoInfo struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&repoInfo); err != nil {
		t.Fatalf("Failed to decode repo info: %v", err)
	}
	if repoInfo.Name != "myrepo" {
		t.Errorf("Expected repo name 'myrepo', got %q", repoInfo.Name)
	}
}

// TestIntegrationRepositoryLifecycle tests create, list, get, delete via the web API.
func TestIntegrationRepositoryLifecycle(t *testing.T) {
	srv, _ := newTestStack(t)

	// Create two repositories
	for _, name := range []string{"repo-alpha.git", "repo-beta.git"} {
		resp, err := http.Post(srv.URL+"/api/repositories/"+name, "application/json", nil)
		if err != nil {
			t.Fatalf("POST /api/repositories/%s: %v", name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected 201 for %s, got %d", name, resp.StatusCode)
		}
	}

	// List repositories
	listResp, err := http.Get(srv.URL + "/api/repositories")
	if err != nil {
		t.Fatalf("GET /api/repositories: %v", err)
	}
	defer listResp.Body.Close()

	var repos []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&repos); err != nil {
		t.Fatalf("Failed to decode repositories list: %v", err)
	}
	if len(repos) != 2 {
		t.Errorf("Expected 2 repositories, got %d", len(repos))
	}

	// Delete one
	delReq, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/repositories/repo-alpha.git", nil)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("DELETE /api/repositories/repo-alpha.git: %v", err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Errorf("Expected 204 No Content, got %d", delResp.StatusCode)
	}

	// Verify only one remains
	listResp2, err := http.Get(srv.URL + "/api/repositories")
	if err != nil {
		t.Fatalf("GET /api/repositories: %v", err)
	}
	defer listResp2.Body.Close()

	var repos2 []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(listResp2.Body).Decode(&repos2); err != nil {
		t.Fatalf("Failed to decode repositories list: %v", err)
	}
	if len(repos2) != 1 {
		t.Errorf("Expected 1 repository after deletion, got %d", len(repos2))
	}
	if repos2[0].Name != "repo-beta" {
		t.Errorf("Expected remaining repo to be 'repo-beta', got %q", repos2[0].Name)
	}
}

// TestIntegrationLFSObjectUploadDownload tests LFS content store via HTTP.
func TestIntegrationLFSObjectUploadDownload(t *testing.T) {
	srv, _ := newTestStack(t)

	content := []byte("integration lfs test content")
	oid := sha256HexStr(content)

	// Upload
	putReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/objects/"+oid, bytes.NewReader(content))
	putReq.ContentLength = int64(len(content))
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatalf("PUT /objects/%s failed: %v", oid, err)
	}
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for PUT, got %d", putResp.StatusCode)
	}

	// Download
	getResp, err := http.Get(srv.URL + "/objects/" + oid)
	if err != nil {
		t.Fatalf("GET /objects/%s failed: %v", oid, err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for GET, got %d", getResp.StatusCode)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(getResp.Body); err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), content) {
		t.Errorf("Content mismatch: got %q, want %q", buf.Bytes(), content)
	}
}

// sha256HexStr returns the hex SHA-256 of data.
func sha256HexStr(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
