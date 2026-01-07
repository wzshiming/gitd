package gitd_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/handlers"
	"github.com/wzshiming/gitd"
)

// TestLazyMirror tests the lazy mirror functionality.
func TestLazyMirror(t *testing.T) {
	// Create a temporary directory for the destination repositories
	destRepoDir, err := os.MkdirTemp("", "gitd-test-lazy-dest-repos")
	if err != nil {
		t.Fatalf("Failed to create temp dest repo dir: %v", err)
	}
	defer os.RemoveAll(destRepoDir)

	// Create a temporary source repository with content to import
	sourceRepoDir, err := os.MkdirTemp("", "gitd-test-lazy-source-repos")
	if err != nil {
		t.Fatalf("Failed to create temp source repo dir: %v", err)
	}
	defer os.RemoveAll(sourceRepoDir)

	// Set up source repository server
	sourceHandler := handlers.LoggingHandler(os.Stderr, gitd.NewHandler(gitd.WithRootDir(sourceRepoDir)))
	sourceServer := httptest.NewServer(sourceHandler)
	defer sourceServer.Close()

	// Create source repository with content
	sourceRepoName := "source-repo.git"
	createSourceRepoWithContentForLazy(t, sourceRepoDir, sourceRepoName)

	// Set up destination server with a short cooldown for testing
	destHandler := handlers.LoggingHandler(os.Stderr, gitd.NewHandler(
		gitd.WithRootDir(destRepoDir),
		gitd.WithLazySyncCooldown(100*time.Millisecond),
	))
	destServer := httptest.NewServer(destHandler)
	defer destServer.Close()

	destRepoName := "lazy-mirror-repo.git"
	sourceURL := sourceServer.URL + "/" + sourceRepoName

	t.Run("ImportWithLazyMode", func(t *testing.T) {
		// Start import with lazy mode enabled
		importReq := map[string]interface{}{
			"source_url": sourceURL,
			"lazy":       true,
		}
		reqBody, _ := json.Marshal(importReq)

		req, _ := http.NewRequest(http.MethodPost, destServer.URL+"/api/repositories/"+destRepoName+"/import", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send import request: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("Expected status 202, got %d", resp.StatusCode)
		}

		// Wait for import to complete
		time.Sleep(2 * time.Second)
	})

	t.Run("VerifyMirrorInfoWithLazy", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, destServer.URL+"/api/repositories/"+destRepoName+"/mirror", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.StatusCode)
		}

		var mirrorInfo struct {
			IsMirror  bool   `json:"is_mirror"`
			SourceURL string `json:"source_url"`
			IsLazy    bool   `json:"is_lazy"`
		}
		json.NewDecoder(resp.Body).Decode(&mirrorInfo)

		if !mirrorInfo.IsMirror {
			t.Error("Expected repository to be marked as mirror")
		}
		if mirrorInfo.SourceURL == "" {
			t.Error("Expected source URL to be set")
		}
		if !mirrorInfo.IsLazy {
			t.Error("Expected repository to have lazy mode enabled")
		}
	})

	t.Run("ToggleLazyMode", func(t *testing.T) {
		// Disable lazy mode
		lazyReq := map[string]bool{"enabled": false}
		reqBody, _ := json.Marshal(lazyReq)

		req, _ := http.NewRequest(http.MethodPut, destServer.URL+"/api/repositories/"+destRepoName+"/mirror/lazy", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// Verify lazy mode is disabled
		req, _ = http.NewRequest(http.MethodGet, destServer.URL+"/api/repositories/"+destRepoName+"/mirror", nil)
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}

		var mirrorInfo struct {
			IsLazy bool `json:"is_lazy"`
		}
		json.NewDecoder(resp.Body).Decode(&mirrorInfo)
		resp.Body.Close()

		if mirrorInfo.IsLazy {
			t.Error("Expected lazy mode to be disabled")
		}

		// Re-enable lazy mode
		lazyReq = map[string]bool{"enabled": true}
		reqBody, _ = json.Marshal(lazyReq)

		req, _ = http.NewRequest(http.MethodPut, destServer.URL+"/api/repositories/"+destRepoName+"/mirror/lazy", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})

	t.Run("CloneLazyMirror", func(t *testing.T) {
		// Create a temporary directory for clone
		cloneDir, err := os.MkdirTemp("", "gitd-lazy-clone")
		if err != nil {
			t.Fatalf("Failed to create temp clone dir: %v", err)
		}
		defer os.RemoveAll(cloneDir)

		// Clone the lazy mirror repository
		cmd := exec.Command("git", "clone", destServer.URL+"/"+destRepoName, filepath.Join(cloneDir, "repo"))
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to clone repository: %v\nOutput: %s", err, output)
		}

		// Verify README.md exists
		readmePath := filepath.Join(cloneDir, "repo", "README.md")
		if _, err := os.Stat(readmePath); os.IsNotExist(err) {
			t.Errorf("README.md not found in cloned repository")
		}
	})

	t.Run("LazyModeOnNonMirrorFails", func(t *testing.T) {
		// Create a regular repository
		createReq, _ := http.NewRequest(http.MethodPost, destServer.URL+"/api/repositories/regular-repo.git", nil)
		createResp, _ := http.DefaultClient.Do(createReq)
		createResp.Body.Close()

		// Try to enable lazy mode on non-mirror - should fail
		lazyReq := map[string]bool{"enabled": true}
		reqBody, _ := json.Marshal(lazyReq)

		req, _ := http.NewRequest(http.MethodPut, destServer.URL+"/api/repositories/regular-repo.git/mirror/lazy", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})
}

// createSourceRepoWithContentForLazy creates a source repository with some content for lazy mirror testing.
func createSourceRepoWithContentForLazy(t *testing.T, repoDir, repoName string) {
	t.Helper()

	repoPath := filepath.Join(repoDir, repoName)

	// Create temporary work directory for initial commit
	workDir, err := os.MkdirTemp("", "gitd-lazy-source-work")
	if err != nil {
		t.Fatalf("Failed to create work directory: %v", err)
	}
	defer os.RemoveAll(workDir)

	// Initialize a non-bare repository
	runCmdLazy(t, workDir, "git", "init")
	runCmdLazy(t, workDir, "git", "config", "user.email", "test@test.com")
	runCmdLazy(t, workDir, "git", "config", "user.name", "Test User")

	// Create some files and commits
	os.WriteFile(filepath.Join(workDir, "README.md"), []byte("# Source Repository for Lazy Mirror\n"), 0644)
	runCmdLazy(t, workDir, "git", "add", "README.md")
	runCmdLazy(t, workDir, "git", "commit", "-m", "Initial commit")

	os.WriteFile(filepath.Join(workDir, "file1.txt"), []byte("File 1 content\n"), 0644)
	runCmdLazy(t, workDir, "git", "add", "file1.txt")
	runCmdLazy(t, workDir, "git", "commit", "-m", "Add file1")

	// Create the bare repository
	runCmdLazy(t, repoDir, "git", "clone", "--bare", workDir, repoPath)
}

// runCmdLazy runs a command and fails the test if it errors.
func runCmdLazy(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Command failed: %s %v\nError: %v\nOutput: %s", name, args, err, output)
	}
}
