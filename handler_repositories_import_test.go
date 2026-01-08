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

// TestImportRepositoryWithLFS tests importing a repository that contains LFS objects
func TestImportRepositoryWithLFS(t *testing.T) {
	// Create a temporary directory for the source repository with LFS
	sourceDir, err := os.MkdirTemp("", "gitd-import-source")
	if err != nil {
		t.Fatalf("Failed to create temp source dir: %v", err)
	}
	defer os.RemoveAll(sourceDir)

	// Create a temporary directory for gitd repositories
	repoDir, err := os.MkdirTemp("", "gitd-import-repos")
	if err != nil {
		t.Fatalf("Failed to create temp repo dir: %v", err)
	}
	defer os.RemoveAll(repoDir)

	// Create a source repository with LFS content
	sourceRepo := filepath.Join(sourceDir, "source.git")
	if err := createSourceRepoWithLFS(sourceRepo); err != nil {
		t.Fatalf("Failed to create source repo: %v", err)
	}

	// Create gitd handler and test server
	handler := handlers.LoggingHandler(os.Stderr, gitd.NewHandler(gitd.WithRootDir(repoDir)))
	server := httptest.NewServer(handler)
	defer server.Close()

	targetRepoName := "imported-repo.git"

	// Test: Import repository with LFS
	t.Run("ImportRepositoryWithLFS", func(t *testing.T) {
		// Create import request
		importReq := map[string]string{
			"source_url": sourceRepo,
		}
		body, err := json.Marshal(importReq)
		if err != nil {
			t.Fatalf("Failed to marshal import request: %v", err)
		}

		req, err := http.NewRequest(http.MethodPost, server.URL+"/api/repositories/"+targetRepoName+"/import", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("Failed to create import request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send import request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("Expected status %d, got %d", http.StatusAccepted, resp.StatusCode)
		}

		// Wait for import to complete
		time.Sleep(5 * time.Second)

		// Check import status
		statusReq, err := http.NewRequest(http.MethodGet, server.URL+"/api/repositories/"+targetRepoName+"/import/status", nil)
		if err != nil {
			t.Fatalf("Failed to create status request: %v", err)
		}

		statusResp, err := http.DefaultClient.Do(statusReq)
		if err != nil {
			t.Fatalf("Failed to send status request: %v", err)
		}
		defer statusResp.Body.Close()

		var status map[string]interface{}
		if err := json.NewDecoder(statusResp.Body).Decode(&status); err != nil {
			t.Fatalf("Failed to decode status response: %v", err)
		}

		t.Logf("Import status: %v", status)

		// Verify the repository was imported
		targetRepoPath := filepath.Join(repoDir, "repos", targetRepoName)
		if _, err := os.Stat(targetRepoPath); os.IsNotExist(err) {
			t.Logf("Trying alternative path without 'repos' subdirectory")
			targetRepoPath = filepath.Join(repoDir, targetRepoName)
			if _, err := os.Stat(targetRepoPath); os.IsNotExist(err) {
				t.Errorf("Target repository was not created at %s", targetRepoPath)
			}
		}

		// Verify LFS objects were imported to gitd's LFS storage
		lfsStoragePath := filepath.Join(repoDir, "lfs")
		if _, err := os.Stat(lfsStoragePath); os.IsNotExist(err) {
			t.Errorf("LFS storage directory was not created")
		}

		// Check if LFS objects exist in storage
		// The actual content OID (not the pointer file OID)
		oid := "be81f51559b40b4a444a792ca7aa18d23ff4a23d293d9cc03e0c4520c802a5c7"
		lfsObjectPath := filepath.Join(lfsStoragePath, oid[0:2], oid[2:4], oid[4:])
		if _, err := os.Stat(lfsObjectPath); os.IsNotExist(err) {
			t.Errorf("LFS object was not imported to storage, expected at: %s", lfsObjectPath)
			// List what files are actually there
			filepath.Walk(lfsStoragePath, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					t.Logf("Found LFS file: %s", path)
				}
				return nil
			})
		} else {
			t.Logf("LFS object successfully imported to: %s", lfsObjectPath)
		}
	})
}

// createSourceRepoWithLFS creates a bare git repository with LFS content
func createSourceRepoWithLFS(repoPath string) error {
	// Create a temporary working directory
	workDir := repoPath + "-work"
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	// Initialize git repository
	cmd := exec.Command("git", "init")
	cmd.Dir = workDir
	if _, err := cmd.CombinedOutput(); err != nil {
		return err
	}

	// Configure git user
	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = workDir
	if _, err := cmd.CombinedOutput(); err != nil {
		return err
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = workDir
	if _, err := cmd.CombinedOutput(); err != nil {
		return err
	}

	// Initialize LFS
	cmd = exec.Command("git", "lfs", "install", "--local")
	cmd.Dir = workDir
	if _, err := cmd.CombinedOutput(); err != nil {
		return err
	}

	// Track binary files with LFS
	cmd = exec.Command("git", "lfs", "track", "*.bin")
	cmd.Dir = workDir
	if _, err := cmd.CombinedOutput(); err != nil {
		return err
	}

	// Create an LFS file
	lfsContent := []byte("large file content for testing LFS")
	if err := os.WriteFile(filepath.Join(workDir, "test.bin"), lfsContent, 0644); err != nil {
		return err
	}

	// Add and commit files
	cmd = exec.Command("git", "add", ".gitattributes", "test.bin")
	cmd.Dir = workDir
	if _, err := cmd.CombinedOutput(); err != nil {
		return err
	}

	cmd = exec.Command("git", "commit", "-m", "Add LFS file")
	cmd.Dir = workDir
	if _, err := cmd.CombinedOutput(); err != nil {
		return err
	}

	// Create bare repository and push
	cmd = exec.Command("git", "init", "--bare", repoPath)
	if _, err := cmd.CombinedOutput(); err != nil {
		return err
	}

	cmd = exec.Command("git", "remote", "add", "origin", repoPath)
	cmd.Dir = workDir
	if _, err := cmd.CombinedOutput(); err != nil {
		return err
	}

	cmd = exec.Command("git", "push", "origin", "master")
	cmd.Dir = workDir
	if _, err := cmd.CombinedOutput(); err != nil {
		return err
	}

	return nil
}
