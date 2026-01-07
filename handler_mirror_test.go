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
	sourceRepoName := "lazy-source-repo.git"
	createSourceRepoWithContent(t, sourceRepoDir, sourceRepoName)

	// Set up destination server
	destHandler := handlers.LoggingHandler(os.Stderr, gitd.NewHandler(gitd.WithRootDir(destRepoDir)))
	destServer := httptest.NewServer(destHandler)
	defer destServer.Close()

	destRepoName := "lazy-mirror-repo.git"
	sourceURL := sourceServer.URL + "/" + sourceRepoName

	t.Run("ImportWithLazyMirror", func(t *testing.T) {
		// Start import with lazy_mirror enabled
		importReq := map[string]interface{}{
			"source_url":  sourceURL,
			"lazy_mirror": true,
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

		// Wait for initial import to complete (at least the shallow fetch)
		time.Sleep(3 * time.Second)

		// Verify the repository was created
		importedRepoPath := filepath.Join(destRepoDir, destRepoName)
		if _, err := os.Stat(importedRepoPath); os.IsNotExist(err) {
			// Repository should exist even if full import fails
			t.Log("Note: Full import may have failed but repository should still be created")
		}
	})

	t.Run("VerifyLazyMirrorConfig", func(t *testing.T) {
		// Check mirror info
		req, _ := http.NewRequest(http.MethodGet, destServer.URL+"/api/repositories/"+destRepoName+"/mirror", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var mirrorInfo struct {
			IsMirror   bool   `json:"is_mirror"`
			SourceURL  string `json:"source_url"`
			LazyMirror bool   `json:"lazy_mirror"`
		}
		json.NewDecoder(resp.Body).Decode(&mirrorInfo)

		if !mirrorInfo.IsMirror {
			t.Error("Expected repository to be marked as mirror")
		}
		if !mirrorInfo.LazyMirror {
			t.Error("Expected repository to be marked as lazy mirror")
		}
		if mirrorInfo.SourceURL == "" {
			t.Error("Expected source URL to be set")
		}
	})

	t.Run("ToggleLazyMirror", func(t *testing.T) {
		// Disable lazy mirror
		toggleReq := map[string]bool{"enabled": false}
		reqBody, _ := json.Marshal(toggleReq)

		req, _ := http.NewRequest(http.MethodPost, destServer.URL+"/api/repositories/"+destRepoName+"/mirror/lazy", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// Verify it's disabled
		req2, _ := http.NewRequest(http.MethodGet, destServer.URL+"/api/repositories/"+destRepoName+"/mirror", nil)
		resp2, _ := http.DefaultClient.Do(req2)
		defer resp2.Body.Close()

		var mirrorInfo struct {
			LazyMirror bool `json:"lazy_mirror"`
		}
		json.NewDecoder(resp2.Body).Decode(&mirrorInfo)

		if mirrorInfo.LazyMirror {
			t.Error("Expected lazy mirror to be disabled")
		}

		// Re-enable lazy mirror
		toggleReq = map[string]bool{"enabled": true}
		reqBody, _ = json.Marshal(toggleReq)

		req3, _ := http.NewRequest(http.MethodPost, destServer.URL+"/api/repositories/"+destRepoName+"/mirror/lazy", bytes.NewReader(reqBody))
		req3.Header.Set("Content-Type", "application/json")

		resp3, _ := http.DefaultClient.Do(req3)
		resp3.Body.Close()

		// Verify it's enabled again
		req4, _ := http.NewRequest(http.MethodGet, destServer.URL+"/api/repositories/"+destRepoName+"/mirror", nil)
		resp4, _ := http.DefaultClient.Do(req4)
		defer resp4.Body.Close()

		json.NewDecoder(resp4.Body).Decode(&mirrorInfo)

		if !mirrorInfo.LazyMirror {
			t.Error("Expected lazy mirror to be enabled")
		}
	})

	t.Run("CloneLazyMirror", func(t *testing.T) {
		// Create a temporary directory for cloning
		cloneDir, err := os.MkdirTemp("", "gitd-lazy-clone")
		if err != nil {
			t.Fatalf("Failed to create temp clone dir: %v", err)
		}
		defer os.RemoveAll(cloneDir)

		// Clone from the lazy mirror
		repoURL := destServer.URL + "/" + destRepoName
		cmd := exec.Command("git", "clone", repoURL, filepath.Join(cloneDir, "cloned"))
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to clone from lazy mirror: %v\nOutput: %s", err, output)
		}

		// Verify the cloned repo has content
		readmePath := filepath.Join(cloneDir, "cloned", "README.md")
		if _, err := os.Stat(readmePath); os.IsNotExist(err) {
			t.Error("README.md not found in cloned repository")
		}
	})

	t.Run("LazyMirrorSyncOnFetch", func(t *testing.T) {
		// Add a new file to the source repository
		sourceRepoPath := filepath.Join(sourceRepoDir, sourceRepoName)
		workDir, err := os.MkdirTemp("", "gitd-source-work")
		if err != nil {
			t.Fatalf("Failed to create work directory: %v", err)
		}
		defer os.RemoveAll(workDir)

		// Clone the source
		cmd := exec.Command("git", "clone", filepath.Join(sourceRepoDir, sourceRepoName), workDir)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to clone source: %v\nOutput: %s", err, output)
		}

		// Add a new file
		newFilePath := filepath.Join(workDir, "newfile.txt")
		if err := os.WriteFile(newFilePath, []byte("New content\n"), 0644); err != nil {
			t.Fatalf("Failed to create new file: %v", err)
		}

		runCmd(t, workDir, "git", "config", "user.email", "test@test.com")
		runCmd(t, workDir, "git", "config", "user.name", "Test User")
		runCmd(t, workDir, "git", "add", "newfile.txt")
		runCmd(t, workDir, "git", "commit", "-m", "Add new file")
		runCmd(t, workDir, "git", "push", "origin", "HEAD")

		// Verify the source repo has the new file
		cmd = exec.Command("git", "log", "--oneline", "-1")
		cmd.Dir = sourceRepoPath
		output, _ = cmd.CombinedOutput()
		if !bytes.Contains(output, []byte("Add new file")) {
			t.Log("Source repo log:", string(output))
		}

		// Wait a bit for cache to expire (sync cooldown)
		time.Sleep(6 * time.Second)

		// Clone from the lazy mirror again - it should sync and get the new file
		cloneDir, err := os.MkdirTemp("", "gitd-lazy-clone-2")
		if err != nil {
			t.Fatalf("Failed to create temp clone dir: %v", err)
		}
		defer os.RemoveAll(cloneDir)

		repoURL := destServer.URL + "/" + destRepoName
		cmd = exec.Command("git", "clone", repoURL, filepath.Join(cloneDir, "cloned"))
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		output, err = cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to clone from lazy mirror: %v\nOutput: %s", err, output)
		}

		// Verify the new file exists
		newFilePath = filepath.Join(cloneDir, "cloned", "newfile.txt")
		if _, err := os.Stat(newFilePath); os.IsNotExist(err) {
			t.Error("newfile.txt not found in cloned repository - lazy sync may not be working")
		}
	})

	t.Run("LazyMirrorNonMirrorRepo", func(t *testing.T) {
		// Create a regular (non-mirror) repository
		createReq, _ := http.NewRequest(http.MethodPost, destServer.URL+"/api/repositories/regular-repo.git", nil)
		createResp, _ := http.DefaultClient.Do(createReq)
		createResp.Body.Close()

		// Try to set lazy mirror on it - should fail
		toggleReq := map[string]bool{"enabled": true}
		reqBody, _ := json.Marshal(toggleReq)

		req, _ := http.NewRequest(http.MethodPost, destServer.URL+"/api/repositories/regular-repo.git/mirror/lazy", bytes.NewReader(reqBody))
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
