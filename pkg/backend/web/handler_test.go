package web_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	backendweb "github.com/wzshiming/gitd/pkg/backend/web"
	"github.com/wzshiming/gitd/pkg/storage"
)

func newTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dataDir, err := os.MkdirTemp("", "web-test-data")
	if err != nil {
		t.Fatalf("Failed to create temp data dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })

	stor := storage.NewStorage(storage.WithRootDir(dataDir))
	handler := backendweb.NewHandler(backendweb.WithStorage(stor))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, dataDir
}

func TestHandleCreateAndGetRepository(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create a repository
	resp, err := http.Post(srv.URL+"/api/repositories/myrepo.git", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/repositories/myrepo.git: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Expected 201 Created, got %d", resp.StatusCode)
	}

	// Get the repository
	resp2, err := http.Get(srv.URL + "/api/repositories/myrepo.git")
	if err != nil {
		t.Fatalf("GET /api/repositories/myrepo.git: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", resp2.StatusCode)
	}

	var repo struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&repo); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if repo.Name != "myrepo" {
		t.Errorf("Expected repo name 'myrepo', got %q", repo.Name)
	}
}

func TestHandleCreateRepositoryConflict(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create a repository
	resp, err := http.Post(srv.URL+"/api/repositories/conflict-repo.git", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/repositories/conflict-repo.git: %v", err)
	}
	resp.Body.Close()

	// Try to create it again
	resp2, err := http.Post(srv.URL+"/api/repositories/conflict-repo.git", "application/json", nil)
	if err != nil {
		t.Fatalf("Second POST /api/repositories/conflict-repo.git: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Errorf("Expected 409 Conflict, got %d", resp2.StatusCode)
	}
}

func TestHandleGetRepositoryNotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	resp, err := http.Get(srv.URL + "/api/repositories/nonexistent.git")
	if err != nil {
		t.Fatalf("GET /api/repositories/nonexistent.git: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 Not Found, got %d", resp.StatusCode)
	}
}

func TestHandleListRepositories(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create two repositories
	for _, name := range []string{"repo-a.git", "repo-b.git"} {
		resp, err := http.Post(srv.URL+"/api/repositories/"+name, "application/json", nil)
		if err != nil {
			t.Fatalf("POST /api/repositories/%s: %v", name, err)
		}
		resp.Body.Close()
	}

	// List repositories
	resp, err := http.Get(srv.URL + "/api/repositories")
	if err != nil {
		t.Fatalf("GET /api/repositories: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
	}

	var repos []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if len(repos) != 2 {
		t.Errorf("Expected 2 repositories, got %d", len(repos))
	}
}

func TestHandleDeleteRepository(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create a repository
	resp, err := http.Post(srv.URL+"/api/repositories/todelete.git", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/repositories/todelete.git: %v", err)
	}
	resp.Body.Close()

	// Delete the repository
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/repositories/todelete.git", nil)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/repositories/todelete.git: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNoContent {
		t.Errorf("Expected 204 No Content, got %d", resp2.StatusCode)
	}

	// Verify it no longer exists
	resp3, err := http.Get(srv.URL + "/api/repositories/todelete.git")
	if err != nil {
		t.Fatalf("GET /api/repositories/todelete.git: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 after deletion, got %d", resp3.StatusCode)
	}
}

func TestHandleDeleteRepositoryNotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/repositories/ghost.git", bytes.NewReader(nil))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/repositories/ghost.git: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 Not Found, got %d", resp.StatusCode)
	}
}
