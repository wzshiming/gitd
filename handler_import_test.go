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

// TestImportRepository tests the import repository functionality.
func TestImportRepository(t *testing.T) {
	// Create a temporary directory for repositories (destination)
	repoDir, err := os.MkdirTemp("", "gitd-test-import-dest")
	if err != nil {
		t.Fatalf("Failed to create temp repo dir: %v", err)
	}
	defer os.RemoveAll(repoDir)

	// Create a temporary directory for the source repository
	sourceDir, err := os.MkdirTemp("", "gitd-test-import-source")
	if err != nil {
		t.Fatalf("Failed to create temp source dir: %v", err)
	}
	defer os.RemoveAll(sourceDir)

	// Create a source repository with some content
	sourceRepoPath := filepath.Join(sourceDir, "source-repo.git")
	workDir := filepath.Join(sourceDir, "work")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("Failed to create work dir: %v", err)
	}

	// Initialize source repo
	cmd := exec.Command("git", "init")
	cmd.Dir = workDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to init source repo: %v\nOutput: %s", err, output)
	}

	// Configure git user
	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = workDir
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = workDir
	cmd.Run()

	// Add some content
	testFile := filepath.Join(workDir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Source Repository\n\nThis is a test repository for import.\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = workDir
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = workDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to commit: %v\nOutput: %s", err, output)
	}

	// Clone to bare repository (simulating a remote)
	cmd = exec.Command("git", "clone", "--bare", workDir, sourceRepoPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to create bare repo: %v\nOutput: %s", err, output)
	}

	// Create handler and test server
	handler := handlers.LoggingHandler(os.Stderr, gitd.NewHandler(gitd.WithRootDir(repoDir)))
	server := httptest.NewServer(handler)
	defer server.Close()

	t.Run("ImportRepository", func(t *testing.T) {
		repoName := "imported-repo.git"
		importURL := server.URL + "/api/repositories/" + repoName + "/import"

		// Start import
		reqBody, _ := json.Marshal(map[string]string{
			"source_url": sourceRepoPath,
		})

		req, _ := http.NewRequest(http.MethodPost, importURL, bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to start import: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("Expected status 202, got %d", resp.StatusCode)
		}

		// Poll for completion
		statusURL := server.URL + "/api/repositories/" + repoName + "/import/status"
		var status struct {
			State   string `json:"state"`
			Phase   string `json:"phase"`
			Message string `json:"message"`
			Error   string `json:"error"`
		}

		timeout := time.After(30 * time.Second)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-timeout:
				t.Fatalf("Import timed out. Last status: %+v", status)
			case <-ticker.C:
				resp, err := http.Get(statusURL)
				if err != nil {
					t.Fatalf("Failed to get status: %v", err)
				}
				json.NewDecoder(resp.Body).Decode(&status)
				resp.Body.Close()

				if status.State == "completed" {
					t.Logf("Import completed: %s", status.Message)
					goto done
				} else if status.State == "failed" {
					t.Fatalf("Import failed: %s", status.Error)
				}
				t.Logf("Import status: %s - %s (%s)", status.State, status.Phase, status.Message)
			}
		}
	done:

		// Verify repository was imported
		repoPath := filepath.Join(repoDir, repoName)
		headPath := filepath.Join(repoPath, "HEAD")
		if _, err := os.Stat(headPath); os.IsNotExist(err) {
			t.Errorf("HEAD file not found, not a valid git repository")
		}

		// Verify we can clone from the imported repository
		cloneDir, _ := os.MkdirTemp("", "gitd-test-import-clone")
		defer os.RemoveAll(cloneDir)

		cmd := exec.Command("git", "clone", server.URL+"/"+repoName, filepath.Join(cloneDir, "clone"))
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to clone imported repository: %v\nOutput: %s", err, output)
		}

		// Verify content
		readmePath := filepath.Join(cloneDir, "clone", "README.md")
		if _, err := os.Stat(readmePath); os.IsNotExist(err) {
			t.Errorf("README.md not found in cloned repository")
		}
	})

	t.Run("ImportMissingSourceURL", func(t *testing.T) {
		importURL := server.URL + "/api/repositories/test-missing-url.git/import"

		reqBody, _ := json.Marshal(map[string]string{})
		req, _ := http.NewRequest(http.MethodPost, importURL, bytes.NewBuffer(reqBody))
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

	t.Run("ImportDuplicateRepository", func(t *testing.T) {
		// First create a repository
		repoName := "existing-repo.git"
		repoPath := filepath.Join(repoDir, repoName)

		cmd := exec.Command("git", "init", "--bare", repoPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("Failed to create repo: %v\nOutput: %s", err, output)
		}

		// Try to import over existing
		importURL := server.URL + "/api/repositories/" + repoName + "/import"
		reqBody, _ := json.Marshal(map[string]string{
			"source_url": sourceRepoPath,
		})

		req, _ := http.NewRequest(http.MethodPost, importURL, bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusConflict {
			t.Errorf("Expected status 409, got %d", resp.StatusCode)
		}
	})

	t.Run("ImportStatusNotFound", func(t *testing.T) {
		statusURL := server.URL + "/api/repositories/nonexistent-import.git/import/status"
		resp, err := http.Get(statusURL)
		if err != nil {
			t.Fatalf("Failed to get status: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
		}
	})
}
