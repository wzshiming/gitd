package gitd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
)

// importRequest represents a request to import a repository from a source URL.
type importRequest struct {
	SourceURL string `json:"source_url"`
}

// importStatusResponse represents the status of an import operation.
type importStatusResponse struct {
	Status     string `json:"status"`
	Step       string `json:"step"`
	Progress   int    `json:"progress"`
	TotalSteps int    `json:"total_steps"`
	Error      string `json:"error,omitempty"`
}

// importState tracks the state of ongoing imports.
type importState struct {
	Status     string
	Step       string
	Progress   int
	TotalSteps int
	Error      string
}

// mirrorInfoResponse represents the mirror info returned to clients.
type mirrorInfoResponse struct {
	IsMirror  bool   `json:"is_mirror"`
	SourceURL string `json:"source_url,omitempty"`
}

func (h *Handler) registryImport(r *mux.Router) {
	r.HandleFunc("/api/repositories/{repo:.+}/import", h.requireAuth(h.handleImportRepository)).Methods(http.MethodPost)
	r.HandleFunc("/api/repositories/{repo:.+}/import/status", h.requireAuth(h.handleImportStatus)).Methods(http.MethodGet)
	r.HandleFunc("/api/repositories/{repo:.+}/mirror", h.requireAuth(h.handleGetMirrorInfo)).Methods(http.MethodGet)
	r.HandleFunc("/api/repositories/{repo:.+}/sync", h.requireAuth(h.handleSyncRepository)).Methods(http.MethodPost)
}

// handleImportRepository handles the import of a repository from a source URL.
// The import process follows these steps for fast imports and intermittent transfers:
// 1. Create the bare repository if it doesn't exist
// 2. Import the latest branch with depth 1 (shallow clone for quick initial data)
// 3. Import LFS files for the latest branch
// 4. Import the latest branch with depth 100 (more history)
// 5. Import the full repository (complete history)
func (h *Handler) handleImportRepository(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	repoName := vars["repo"]

	var req importRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.SourceURL == "" {
		http.Error(w, "source_url is required", http.StatusBadRequest)
		return
	}

	// Validate and construct the repository path using the same logic as resolveRepoPath
	repoPath, err := h.validateRepoPath(repoName)
	if err != nil {
		http.Error(w, "Invalid repository path", http.StatusBadRequest)
		return
	}

	// Initialize import state
	h.importStatesMu.Lock()
	h.importStates[repoPath] = &importState{
		Status:     "in_progress",
		Step:       "initializing",
		Progress:   0,
		TotalSteps: 6,
	}
	h.importStatesMu.Unlock()

	// Run import in background
	go h.doImport(context.Background(), repoPath, req.SourceURL)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "accepted",
		"message": "Import started",
	})
}

// handleImportStatus returns the status of an ongoing or completed import.
func (h *Handler) handleImportStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	repoName := vars["repo"]

	// Validate and construct the repository path
	repoPath, err := h.validateRepoPath(repoName)
	if err != nil {
		http.Error(w, "Invalid repository path", http.StatusBadRequest)
		return
	}

	h.importStatesMu.RLock()
	state, exists := h.importStates[repoPath]
	h.importStatesMu.RUnlock()

	if !exists {
		// Check if repository exists
		if h.resolveRepoPath(repoName) != "" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(importStatusResponse{
				Status:     "completed",
				Step:       "done",
				Progress:   6,
				TotalSteps: 6,
			})
			return
		}
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(importStatusResponse{
		Status:     state.Status,
		Step:       state.Step,
		Progress:   state.Progress,
		TotalSteps: state.TotalSteps,
		Error:      state.Error,
	})
}

// doImport performs the actual import operation in steps.
func (h *Handler) doImport(ctx context.Context, repoPath, sourceURL string) {
	updateState := func(step string, progress int) {
		h.importStatesMu.Lock()
		if state, exists := h.importStates[repoPath]; exists {
			state.Step = step
			state.Progress = progress
		}
		h.importStatesMu.Unlock()
	}

	setError := func(err error) {
		h.importStatesMu.Lock()
		if state, exists := h.importStates[repoPath]; exists {
			state.Status = "failed"
			state.Error = err.Error()
		}
		h.importStatesMu.Unlock()
	}

	setCompleted := func() {
		h.importStatesMu.Lock()
		if state, exists := h.importStates[repoPath]; exists {
			state.Status = "completed"
			state.Step = "done"
			state.Progress = 6
		}
		h.importStatesMu.Unlock()
	}

	// Step 1: Create bare repository and configure mirror
	updateState("creating_repository", 1)
	if err := h.createBareRepo(ctx, repoPath); err != nil {
		setError(fmt.Errorf("failed to create repository: %w", err))
		return
	}
	if err := h.ensureMirrorConfig(ctx, repoPath, sourceURL); err != nil {
		setError(fmt.Errorf("failed to save mirror config: %w", err))
		return
	}
	if err := h.ensureRemote(ctx, repoPath, sourceURL); err != nil {
		setError(fmt.Errorf("failed to configure remote: %w", err))
		return
	}
	if err := h.ensureDefaultBranch(ctx, repoPath); err != nil {
		setError(fmt.Errorf("failed to set default branch: %w", err))
		return
	}

	// Step 2: Fetch tree structure only (no blobs) - allows immediate file listing
	updateState("importing_tree", 2)
	if err := h.fetchWithOnlyTree(ctx, repoPath, 1); err != nil {
		setError(fmt.Errorf("failed to import tree structure: %w", err))
		return
	}

	// Step 3: Fetch latest commit with blobs - allows viewing file content
	updateState("importing_content", 3)
	if err := h.fetchWithDepth(ctx, repoPath, 1); err != nil {
		setError(fmt.Errorf("failed to import content: %w", err))
		return
	}

	// Step 4: Import LFS files - needed for large file content
	updateState("importing_lfs", 4)
	if err := h.fetchLFS(ctx, repoPath); err != nil {
		// LFS fetch failure is not fatal - repository might not have LFS files
		// Continue with the import
	}

	// Step 5: Import more history (depth 100)
	updateState("importing_history", 5)
	if err := h.fetchWithDepth(ctx, repoPath, 100); err != nil {
		setError(fmt.Errorf("failed to import history: %w", err))
		return
	}

	// Step 6: Import full repository history
	updateState("importing_full", 6)
	if err := h.fetchFull(ctx, repoPath, sourceURL); err != nil {
		setError(fmt.Errorf("failed to import full repository: %w", err))
		return
	}

	setCompleted()
}

// create	BareRepo creates a bare git repository at the given path.
func (h *Handler) createBareRepo(ctx context.Context, repoPath string) error {
	// Check if repository already exists
	if _, err := os.Stat(filepath.Join(repoPath, "HEAD")); err == nil {
		return nil // Repository already exists
	}

	base, dir := filepath.Split(repoPath)
	if err := os.MkdirAll(base, 0755); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "git", "init", "--bare", dir)
	cmd.Dir = base
	return cmd.Run()
}

func (h *Handler) fetchWithOnlyTree(ctx context.Context, repoPath string, depth int) error {
	cmd := exec.CommandContext(ctx, "git", "fetch", "origin", "--depth", fmt.Sprintf("%d", depth), "--filter=blob:none")
	cmd.Dir = repoPath
	cmd.Stderr = io.Discard
	return cmd.Run()
}

// fetchWithDepth fetches from the source with a specified depth.
func (h *Handler) fetchWithDepth(ctx context.Context, repoPath string, depth int) error {
	// Fetch with depth
	cmd := exec.CommandContext(ctx, "git", "fetch", "origin", "--depth", fmt.Sprintf("%d", depth))
	cmd.Dir = repoPath
	cmd.Stderr = io.Discard
	return cmd.Run()
}

// fetchLFS fetches LFS objects from the source.
func (h *Handler) fetchLFS(ctx context.Context, repoPath string) error {
	// Fetch LFS objects
	cmd := exec.CommandContext(ctx, "git", "lfs", "fetch", "--all", "origin")
	cmd.Dir = repoPath
	cmd.Stderr = io.Discard
	return cmd.Run()
}

// fetchFull fetches the complete history from the source.
func (h *Handler) fetchFull(ctx context.Context, repoPath, sourceURL string) error {
	// Unshallow the repository
	cmd := exec.CommandContext(ctx, "git", "fetch", "--unshallow", "origin")
	cmd.Dir = repoPath
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		// If unshallow fails (repo might already be full), do a regular fetch
		cmd = exec.CommandContext(ctx, "git", "fetch", "origin")
		cmd.Dir = repoPath
		cmd.Stderr = io.Discard
		return cmd.Run()
	}
	return nil
}

func (h *Handler) ensureDefaultBranch(ctx context.Context, repoPath string) error {
	// Try to determine the remote's default branch using ls-remote --symref
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--symref", "origin", "HEAD")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		// Can't determine remote default branch — do not fail import
		return nil
	}

	var branch string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ref: refs/heads/") {
			// line looks like: "ref: refs/heads/main\tHEAD"
			fields := strings.Fields(line)
			if len(fields) > 0 {
				branch = strings.TrimSpace(strings.TrimPrefix(fields[0], "ref: refs/heads/"))
			}
			break
		}
	}
	if branch == "" {
		return nil
	}

	// Set HEAD to point to the branch
	cmd = exec.CommandContext(ctx, "git", "symbolic-ref", "HEAD", "refs/heads/"+branch)
	cmd.Dir = repoPath
	_ = cmd.Run() // non-fatal if this fails

	return nil
}

// ensureRemote ensures the origin remote is set to the source URL.
func (h *Handler) ensureRemote(ctx context.Context, repoPath, sourceURL string) error {
	// Try to set-url first (in case remote already exists)
	cmd := exec.CommandContext(ctx, "git", "remote", "set-url", "origin", sourceURL)
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		// If that fails, try to add the remote
		cmd = exec.CommandContext(ctx, "git", "remote", "add", "origin", sourceURL)
		cmd.Dir = repoPath
		return cmd.Run()
	}
	return nil
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

// ensureMirrorConfig marks the repository as a mirror in git config.
// This sets remote.origin.mirror = true in the repository's config.
func (h *Handler) ensureMirrorConfig(ctx context.Context, repoPath string, sourceURL string) error {
	cmd := exec.CommandContext(ctx, "git", "config", "remote.origin.mirror", "true")
	cmd.Dir = repoPath
	return cmd.Run()
}

// getMirrorInfo reads mirror configuration from git config.
// Returns the source URL and whether the repository is a mirror.
func (h *Handler) getMirrorInfo(repoPath string) (sourceURL string, isMirror bool, err error) {
	// Check if remote.origin.mirror is set to true
	cmd := exec.Command("git", "config", "--get", "remote.origin.mirror")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		// If the config key doesn't exist, it's not a mirror
		return "", false, nil
	}

	mirrorValue := strings.TrimSpace(string(output))
	if mirrorValue != "true" {
		return "", false, nil
	}

	// Get the remote origin URL
	cmd = exec.Command("git", "config", "--get", "remote.origin.url")
	cmd.Dir = repoPath
	output, err = cmd.Output()
	if err != nil {
		return "", false, nil
	}

	sourceURL = strings.TrimSpace(string(output))
	return sourceURL, true, nil
}

// handleGetMirrorInfo returns mirror information for a repository.
func (h *Handler) handleGetMirrorInfo(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	repoName := vars["repo"]

	repoPath := h.resolveRepoPath(repoName)
	if repoPath == "" {
		http.NotFound(w, r)
		return
	}

	sourceURL, isMirror, err := h.getMirrorInfo(repoPath)
	if err != nil {
		http.Error(w, "Failed to get mirror config", http.StatusInternalServerError)
		return
	}

	response := mirrorInfoResponse{
		IsMirror:  isMirror,
		SourceURL: sourceURL,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleSyncRepository synchronizes a mirror repository with its source.
func (h *Handler) handleSyncRepository(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	repoName := vars["repo"]

	repoPath := h.resolveRepoPath(repoName)
	if repoPath == "" {
		http.NotFound(w, r)
		return
	}

	sourceURL, isMirror, err := h.getMirrorInfo(repoPath)
	if err != nil {
		http.Error(w, "Failed to get mirror config", http.StatusInternalServerError)
		return
	}

	if !isMirror {
		http.Error(w, "Repository is not a mirror", http.StatusBadRequest)
		return
	}

	// Initialize sync state (reusing import state)
	h.importStatesMu.Lock()
	h.importStates[repoPath] = &importState{
		Status: "in_progress",
		Step:   "syncing",
	}
	h.importStatesMu.Unlock()

	// Run sync in background
	go h.doImport(context.Background(), repoPath, sourceURL)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "accepted",
		"message": "Sync started",
	})
}
