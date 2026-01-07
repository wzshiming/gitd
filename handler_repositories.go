package gitd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
)

func (h *Handler) registryManagement(r *mux.Router) {
	r.HandleFunc("/api/repositories/{repo:.+}", h.requireAuth(h.handleCreateRepository)).Methods(http.MethodPost)
	r.HandleFunc("/api/repositories/{repo:.+}", h.requireAuth(h.handleDeleteRepository)).Methods(http.MethodDelete)
}

// validateRepoPath validates and constructs a repository path, ensuring it's within the root directory.
func (h *Handler) validateRepoPath(urlPath string) (string, error) {
	// Clean the path
	urlPath = strings.TrimPrefix(urlPath, "/")
	if urlPath == "" {
		return "", fmt.Errorf("empty path")
	}

	// Construct the full path
	fullPath := filepath.Join(h.rootDir, urlPath)

	// Clean and verify the path is within RepoDir using filepath.Rel
	fullPath = filepath.Clean(fullPath)
	absRepoDir, err := filepath.Abs(h.rootDir)
	if err != nil {
		return "", err
	}
	absFullPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", err
	}
	// Use filepath.Rel to safely check if absFullPath is within absRepoDir
	relPath, err := filepath.Rel(absRepoDir, absFullPath)
	if err != nil {
		return "", err
	}
	// Reject if the relative path starts with ".." (meaning it's outside RepoDir)
	if strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("path outside repository directory")
	}

	return fullPath, nil
}

// createBareRepo creates a bare git repository at the given path.
func (h *Handler) createBareRepo(ctx context.Context, repoPath string) error {
	if _, err := os.Stat(filepath.Join(repoPath, "HEAD")); err == nil {
		return fmt.Errorf("repository already exists")
	}

	base, dir := filepath.Split(repoPath)
	if err := os.MkdirAll(base, 0755); err != nil {
		return err
	}

	cmd := command(ctx, "git", "init", "--bare", dir)
	cmd.Dir = base
	return cmd.Run()
}

func (h *Handler) handleCreateRepository(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	repoName := vars["repo"]

	repoPath := h.resolveRepoPath(repoName)
	if repoPath != "" {
		http.Error(w, "Repository already exists", http.StatusConflict)
		return
	}

	repoPath = filepath.Join(h.rootDir, repoName)

	err := h.createBareRepo(r.Context(), repoPath)
	if err != nil {
		http.Error(w, "Failed to create repository", http.StatusInternalServerError)
		return
	}
}

func (h *Handler) handleDeleteRepository(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	repoName := vars["repo"]

	repoPath := h.resolveRepoPath(repoName)
	if repoPath == "" {
		http.NotFound(w, r)
		return
	}

	err := os.RemoveAll(repoPath)
	if err != nil {
		http.Error(w, "Failed to delete repository", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
