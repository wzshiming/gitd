package gitd

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// createTestRepo creates a bare git repository for testing.
func createTestRepo(t *testing.T, dir string) string {
	t.Helper()
	repoPath := filepath.Join(dir, "test.git")
	cmd := exec.Command("git", "init", "--bare", repoPath)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to create test repository: %v", err)
	}
	return repoPath
}

func TestNewHandler(t *testing.T) {
	h := NewHandler("/tmp/repos")
	if h.RepoDir != "/tmp/repos" {
		t.Errorf("Expected RepoDir to be /tmp/repos, got %s", h.RepoDir)
	}
	if h.GitBinPath != "" {
		t.Errorf("Expected GitBinPath to be empty, got %s", h.GitBinPath)
	}
}

func TestGitPath(t *testing.T) {
	h := NewHandler("/tmp/repos")
	if h.gitPath() != "git" {
		t.Errorf("Expected default git path to be 'git', got %s", h.gitPath())
	}

	h.GitBinPath = "/usr/bin/git"
	if h.gitPath() != "/usr/bin/git" {
		t.Errorf("Expected git path to be '/usr/bin/git', got %s", h.gitPath())
	}
}

func TestPacketLine(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"# service=git-upload-pack\n", "001e# service=git-upload-pack\n"},
		{"# service=git-receive-pack\n", "001f# service=git-receive-pack\n"},
	}

	for _, tt := range tests {
		result := string(packetLine(tt.input))
		if result != tt.expected {
			t.Errorf("packetLine(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

func TestIsGitRepository(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gitd-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with bare repository
	bareRepo := createTestRepo(t, tmpDir)
	if !isGitRepository(bareRepo) {
		t.Errorf("Expected %s to be recognized as a git repository", bareRepo)
	}

	// Test with non-repository
	nonRepo := filepath.Join(tmpDir, "not-a-repo")
	if err := os.MkdirAll(nonRepo, 0755); err != nil {
		t.Fatalf("Failed to create non-repo dir: %v", err)
	}
	if isGitRepository(nonRepo) {
		t.Errorf("Expected %s to not be recognized as a git repository", nonRepo)
	}
}

func TestResolveRepoPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gitd-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	createTestRepo(t, tmpDir)
	h := NewHandler(tmpDir)

	// Test with valid path
	resolved := h.resolveRepoPath("/test.git")
	if resolved == "" {
		t.Error("Expected valid repo path to be resolved")
	}

	// Test with path traversal attack
	resolved = h.resolveRepoPath("/../../../etc/passwd")
	if resolved != "" {
		t.Error("Expected path traversal to be rejected")
	}

	// Test with empty path
	resolved = h.resolveRepoPath("")
	if resolved != "" {
		t.Error("Expected empty path to be rejected")
	}
}

func TestServeHTTPNotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gitd-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	h := NewHandler(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent/path", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestInfoRefsRequiresService(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gitd-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	createTestRepo(t, tmpDir)
	h := NewHandler(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/test.git/info/refs", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for missing service, got %d", w.Code)
	}
}

func TestInfoRefsInvalidService(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gitd-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	createTestRepo(t, tmpDir)
	h := NewHandler(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/test.git/info/refs?service=invalid", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403 for invalid service, got %d", w.Code)
	}
}

func TestInfoRefsUploadPack(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gitd-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	createTestRepo(t, tmpDir)
	h := NewHandler(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/test.git/info/refs?service=git-upload-pack", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/x-git-upload-pack-advertisement" {
		t.Errorf("Expected Content-Type application/x-git-upload-pack-advertisement, got %s", contentType)
	}

	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte("# service=git-upload-pack")) {
		t.Errorf("Expected response to contain service header")
	}
}

func TestInfoRefsReceivePack(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gitd-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	createTestRepo(t, tmpDir)
	h := NewHandler(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/test.git/info/refs?service=git-receive-pack", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/x-git-receive-pack-advertisement" {
		t.Errorf("Expected Content-Type application/x-git-receive-pack-advertisement, got %s", contentType)
	}
}

func TestUploadPackRequiresPost(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gitd-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	createTestRepo(t, tmpDir)
	h := NewHandler(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/test.git/git-upload-pack", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestReceivePackRequiresPost(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gitd-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	createTestRepo(t, tmpDir)
	h := NewHandler(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/test.git/git-receive-pack", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestUploadPackPost(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gitd-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	createTestRepo(t, tmpDir)
	h := NewHandler(tmpDir)

	// Send empty body, git will return an error but we should still get proper content-type
	req := httptest.NewRequest(http.MethodPost, "/test.git/git-upload-pack", bytes.NewReader([]byte("0000")))
	req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/x-git-upload-pack-result" {
		t.Errorf("Expected Content-Type application/x-git-upload-pack-result, got %s", contentType)
	}
}

func TestReceivePackPost(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gitd-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	createTestRepo(t, tmpDir)
	h := NewHandler(tmpDir)

	// Send empty body
	req := httptest.NewRequest(http.MethodPost, "/test.git/git-receive-pack", bytes.NewReader([]byte("0000")))
	req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/x-git-receive-pack-result" {
		t.Errorf("Expected Content-Type application/x-git-receive-pack-result, got %s", contentType)
	}
}

func TestInfoRefsMethodNotAllowed(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gitd-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	createTestRepo(t, tmpDir)
	h := NewHandler(tmpDir)

	req := httptest.NewRequest(http.MethodPost, "/test.git/info/refs?service=git-upload-pack", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestNonexistentRepo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gitd-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	h := NewHandler(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent.git/info/refs?service=git-upload-pack", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

// TestIntegration performs an integration test with actual git operations.
func TestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "gitd-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a bare repository
	repoPath := createTestRepo(t, tmpDir)

	h := NewHandler(tmpDir)
	server := httptest.NewServer(h)
	defer server.Close()

	// Clone the repository
	cloneDir := filepath.Join(tmpDir, "clone")
	cmd := exec.Command("git", "clone", server.URL+"/test.git", cloneDir)
	cmd.Stderr = io.Discard
	cmd.Stdout = io.Discard
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to clone repository: %v", err)
	}

	// Create a file and commit
	testFile := filepath.Join(cloneDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("Hello, World!"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cmd = exec.Command("git", "-C", cloneDir, "config", "user.email", "test@example.com")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to configure git email: %v", err)
	}

	cmd = exec.Command("git", "-C", cloneDir, "config", "user.name", "Test User")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to configure git name: %v", err)
	}

	cmd = exec.Command("git", "-C", cloneDir, "add", ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to add files: %v", err)
	}

	cmd = exec.Command("git", "-C", cloneDir, "commit", "-m", "Initial commit")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	// Push to the server
	cmd = exec.Command("git", "-C", cloneDir, "push", "origin", "master")
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	// Verify the commit is in the bare repository
	cmd = exec.Command("git", "-C", repoPath, "log", "--oneline")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to get log from bare repository: %v", err)
	}

	if !bytes.Contains(output, []byte("Initial commit")) {
		t.Errorf("Expected bare repository to contain 'Initial commit', got: %s", output)
	}
}
