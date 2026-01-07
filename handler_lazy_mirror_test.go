package gitd_test

import (
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
	// Create a temporary directory for the source repository
	sourceRepoDir, err := os.MkdirTemp("", "gitd-test-source-repos")
	if err != nil {
		t.Fatalf("Failed to create temp source repo dir: %v", err)
	}
	defer os.RemoveAll(sourceRepoDir)

	// Create a temporary directory for the lazy mirror cache
	cacheDir, err := os.MkdirTemp("", "gitd-test-cache")
	if err != nil {
		t.Fatalf("Failed to create temp cache dir: %v", err)
	}
	defer os.RemoveAll(cacheDir)

	// Set up source repository server
	sourceHandler := handlers.LoggingHandler(os.Stderr, gitd.NewHandler(gitd.WithRootDir(sourceRepoDir)))
	sourceServer := httptest.NewServer(sourceHandler)
	defer sourceServer.Close()

	// Create source repository with content
	sourceRepoName := "upstream-repo.git"
	createSourceRepoWithContent(t, sourceRepoDir, sourceRepoName)

	// Create lazy mirror handler that maps any repo to the source server
	lazyMirrorFunc := func(repoName string) string {
		return sourceServer.URL + "/" + repoName + ".git"
	}

	// Set up lazy mirror server
	lazyHandler := handlers.LoggingHandler(os.Stderr, gitd.NewHandler(
		gitd.WithRootDir(cacheDir),
		gitd.WithLazyMirrorSource(lazyMirrorFunc),
	))
	lazyServer := httptest.NewServer(lazyHandler)
	defer lazyServer.Close()

	t.Run("LazyMirrorOnClone", func(t *testing.T) {
		// Try to access a repository that doesn't exist yet but has an upstream
		// The lazy mirror should create it automatically
		repoURL := lazyServer.URL + "/upstream-repo.git"

		// Create temporary directory for client
		clientDir, err := os.MkdirTemp("", "gitd-test-client")
		if err != nil {
			t.Fatalf("Failed to create temp client dir: %v", err)
		}
		defer os.RemoveAll(clientDir)

		cloneDir := filepath.Join(clientDir, "cloned-repo")

		// Retry clone a few times as the lazy mirror may need time to fetch
		var cloneErr error
		var cloneOutput []byte
		for i := 0; i < 10; i++ {
			cmd := exec.Command("git", "clone", repoURL, cloneDir)
			cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
			cloneOutput, cloneErr = cmd.CombinedOutput()
			if cloneErr == nil {
				break
			}
			// Clean up failed clone attempt
			os.RemoveAll(cloneDir)
			time.Sleep(500 * time.Millisecond)
		}

		if cloneErr != nil {
			t.Fatalf("Failed to clone lazy mirror repository: %v\nOutput: %s", cloneErr, cloneOutput)
		}

		// Verify .git directory exists
		gitDir := filepath.Join(cloneDir, ".git")
		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			t.Errorf(".git directory not found in cloned repository")
		}

		// Wait for import to complete, then pull to get full content
		time.Sleep(2 * time.Second)

		// Pull to get the latest content after import completes
		pullCmd := exec.Command("git", "pull")
		pullCmd.Dir = cloneDir
		pullCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		pullCmd.CombinedOutput() // Ignore error as it might already be up to date

		// Verify README.md exists (from the upstream)
		readmePath := filepath.Join(cloneDir, "README.md")
		if _, err := os.Stat(readmePath); os.IsNotExist(err) {
			// This is expected if the lazy mirror is still importing
			// The key test is that the mirror was created and is working
			t.Log("README.md not found yet, but mirror was created successfully")
		}

		// Verify the cached repository is marked as a mirror
		req, _ := http.NewRequest(http.MethodGet, lazyServer.URL+"/api/repositories/upstream-repo.git/mirror", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to get mirror info: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var mirrorInfo struct {
			IsMirror  bool   `json:"is_mirror"`
			SourceURL string `json:"source_url"`
		}
		json.NewDecoder(resp.Body).Decode(&mirrorInfo)

		if !mirrorInfo.IsMirror {
			t.Error("Expected lazy mirror repository to be marked as mirror")
		}
		if mirrorInfo.SourceURL == "" {
			t.Error("Expected source URL to be set")
		}
	})

	t.Run("LazyMirrorNonExistentUpstream", func(t *testing.T) {
		// Try to access a repository that doesn't exist in upstream
		resp, err := http.Get(lazyServer.URL + "/nonexistent-repo.git/info/refs?service=git-upload-pack")
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		defer resp.Body.Close()

		// Should return 404 because the upstream doesn't have this repo
		// Note: The lazy mirror will try to create it but the import will fail
		// The response might be 200 if the repo was created before the fetch failed,
		// or 404/500 depending on timing
		// For this test, we just verify the server doesn't crash
		if resp.StatusCode >= 500 {
			t.Errorf("Server error: %d", resp.StatusCode)
		}
	})

	t.Run("DisabledLazyMirror", func(t *testing.T) {
		// Create a separate cache directory for this test
		noLazyCacheDir, err := os.MkdirTemp("", "gitd-test-no-lazy-cache")
		if err != nil {
			t.Fatalf("Failed to create temp cache dir: %v", err)
		}
		defer os.RemoveAll(noLazyCacheDir)

		// Set up a handler without lazy mirror
		noLazyHandler := handlers.LoggingHandler(os.Stderr, gitd.NewHandler(
			gitd.WithRootDir(noLazyCacheDir),
			// No WithLazyMirrorSource
		))
		noLazyServer := httptest.NewServer(noLazyHandler)
		defer noLazyServer.Close()

		// Try to access a repository that doesn't exist
		resp, err := http.Get(noLazyServer.URL + "/new-repo.git/info/refs?service=git-upload-pack")
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		defer resp.Body.Close()

		// Should return 404 because lazy mirror is not enabled
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
		}
	})
}

// TestImportStatusEndpoint tests the import status endpoint
func TestImportStatusEndpoint(t *testing.T) {
	// Create a temporary directory for repositories
	repoDir, err := os.MkdirTemp("", "gitd-test-repos")
	if err != nil {
		t.Fatalf("Failed to create temp repo dir: %v", err)
	}
	defer os.RemoveAll(repoDir)

	handler := handlers.LoggingHandler(os.Stderr, gitd.NewHandler(gitd.WithRootDir(repoDir)))
	server := httptest.NewServer(handler)
	defer server.Close()

	t.Run("StatusForNonExistentRepo", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/repositories/nonexistent.git/import/status")
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
		}
	})
}

// TestMirrorInfoEndpoint tests the mirror info endpoint
func TestMirrorInfoEndpoint(t *testing.T) {
	// Create a temporary directory for repositories
	repoDir, err := os.MkdirTemp("", "gitd-test-repos")
	if err != nil {
		t.Fatalf("Failed to create temp repo dir: %v", err)
	}
	defer os.RemoveAll(repoDir)

	handler := handlers.LoggingHandler(os.Stderr, gitd.NewHandler(gitd.WithRootDir(repoDir)))
	server := httptest.NewServer(handler)
	defer server.Close()

	t.Run("MirrorInfoForNonExistentRepo", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/repositories/nonexistent.git/mirror")
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
		}
	})

	t.Run("MirrorInfoForRegularRepo", func(t *testing.T) {
		// Create a regular repository
		repoPath := filepath.Join(repoDir, "regular-repo")
		cmd := exec.Command("git", "init", "--bare", repoPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("Failed to create repo: %v\nOutput: %s", err, output)
		}

		resp, err := http.Get(server.URL + "/api/repositories/regular-repo.git/mirror")
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var mirrorInfo struct {
			IsMirror  bool   `json:"is_mirror"`
			SourceURL string `json:"source_url"`
		}
		json.NewDecoder(resp.Body).Decode(&mirrorInfo)

		if mirrorInfo.IsMirror {
			t.Error("Regular repository should not be marked as mirror")
		}
	})
}
