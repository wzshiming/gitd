package gitd

import (
	"context"
	"encoding/json"
	"fmt"
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

func (h *Handler) registryRepositoriesImport(r *mux.Router) {
	r.HandleFunc("/api/repositories/{repo:.+}.git/import", h.requireAuth(h.handleImportRepository)).Methods(http.MethodPost)
	r.HandleFunc("/api/repositories/{repo:.+}.git/import/status", h.requireAuth(h.handleImportStatus)).Methods(http.MethodGet)
	r.HandleFunc("/api/repositories/{repo:.+}.git/sync", h.requireAuth(h.handleSyncRepository)).Methods(http.MethodPost)
	r.HandleFunc("/api/repositories/{repo:.+}.git/mirror", h.requireAuth(h.handleGetMirrorInfo)).Methods(http.MethodGet)
}

// handleImportRepository handles the import of a repository from a source URL.
// The import process follows these steps for fast imports and intermittent transfers:
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

	ctx := context.Background()

	if err := h.createBareRepo(ctx, repoPath); err != nil {
		http.Error(w, "Failed to create repository", http.StatusInternalServerError)
		return
	}

	err = h.saveMirrorConfig(ctx, repoPath, req.SourceURL)
	if err != nil {
		http.Error(w, "Failed to save mirror config", http.StatusInternalServerError)
		return
	}

	defaultBranch, err := h.getRemoteDefaultBranch(ctx, req.SourceURL)
	if err != nil {
		http.Error(w, "Failed to get default branch from source", http.StatusInternalServerError)
		return
	}
	err = h.setLocalDefaultBranch(ctx, repoPath, defaultBranch)
	if err != nil {
		http.Error(w, "Failed to set default branch", http.StatusInternalServerError)
		return
	}

	// Initialize import status
	h.setImportStatus(repoName, &ImportStatus{
		Status: "in_progress",
		Step:   "starting",
	})

	// Run import in background
	go func() {
		err := h.doImportWithStatus(context.Background(), repoPath, repoName, defaultBranch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Import failed for %s: %v\n", repoName, err)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "accepted",
		"message": "Import started",
	})
}

// doImportWithStatus performs the actual import operation in steps with status tracking.
func (h *Handler) doImportWithStatus(ctx context.Context, repoPath, repoName, branch string) error {
	h.setImportStatus(repoName, &ImportStatus{
		Status: "in_progress",
		Step:   "fetching default branch",
	})

	err := h.fetchWithOptions(ctx, repoPath, branch, 1, true)
	if err != nil {
		h.setImportStatus(repoName, &ImportStatus{
			Status: "failed",
			Step:   "fetching default branch",
			Error:  err.Error(),
		})
		return fmt.Errorf("failed to import history: %w", err)
	}

	h.setImportStatus(repoName, &ImportStatus{
		Status: "in_progress",
		Step:   "fetching all branches (shallow)",
	})

	err = h.fetchWithOptions(ctx, repoPath, "", 1, true)
	if err != nil {
		h.setImportStatus(repoName, &ImportStatus{
			Status: "failed",
			Step:   "fetching all branches (shallow)",
			Error:  err.Error(),
		})
		return fmt.Errorf("failed to import history: %w", err)
	}

	h.setImportStatus(repoName, &ImportStatus{
		Status: "in_progress",
		Step:   "fetching more history",
	})

	err = h.fetchWithOptions(ctx, repoPath, "", 10, true)
	if err != nil {
		h.setImportStatus(repoName, &ImportStatus{
			Status: "failed",
			Step:   "fetching more history",
			Error:  err.Error(),
		})
		return fmt.Errorf("failed to import history: %w", err)
	}

	h.setImportStatus(repoName, &ImportStatus{
		Status: "in_progress",
		Step:   "fetching full history",
	})

	err = h.fetchFull(ctx, repoPath)
	if err != nil {
		h.setImportStatus(repoName, &ImportStatus{
			Status: "failed",
			Step:   "fetching full history",
			Error:  err.Error(),
		})
		return fmt.Errorf("failed to import full history: %w", err)
	}

	h.setImportStatus(repoName, &ImportStatus{
		Status: "completed",
		Step:   "done",
	})

	return nil
}

// setImportStatus updates the import status for a repository
func (h *Handler) setImportStatus(repoName string, status *ImportStatus) {
	h.importStatusMu.Lock()
	defer h.importStatusMu.Unlock()
	h.importStatus[repoName] = status
}

// getImportStatus returns the import status for a repository
func (h *Handler) getImportStatus(repoName string) *ImportStatus {
	h.importStatusMu.RLock()
	defer h.importStatusMu.RUnlock()
	return h.importStatus[repoName]
}

// handleImportStatus returns the status of an import operation
func (h *Handler) handleImportStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	repoName := vars["repo"]

	repoPath := h.resolveRepoPath(repoName)
	if repoPath == "" {
		http.NotFound(w, r)
		return
	}

	status := h.getImportStatus(repoName)
	if status == nil {
		// Check if repository is a mirror - if so, assume completed
		_, isMirror, _ := h.getMirrorInfo(repoPath)
		if isMirror {
			status = &ImportStatus{
				Status: "completed",
				Step:   "done",
			}
		} else {
			status = &ImportStatus{
				Status: "unknown",
				Step:   "no import in progress",
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleGetMirrorInfo returns the mirror configuration for a repository
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
		http.Error(w, "Failed to get mirror info", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"is_mirror":  isMirror,
		"source_url": sourceURL,
	})
}

// fetchWithOptions fetches from remote with specified options
func (h *Handler) fetchWithOptions(ctx context.Context, repoPath, branch string, depth int, noBlob bool) error {
	args := []string{"fetch"}

	if depth > 0 {
		args = append(args, fmt.Sprintf("--depth=%d", depth))
	}

	if noBlob {
		args = append(args, "--filter=blob:none")
	}

	if branch != "" {
		args = append(args, "origin", fmt.Sprintf("+refs/heads/%s:refs/heads/%s", branch, branch))
	} else {
		args = append(args, "origin", "+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*")
	}

	cmd := command(ctx, "git", args...)
	cmd.Dir = repoPath
	return cmd.Run()
}

// fetchFull fetches the complete history from the source.
func (h *Handler) fetchFull(ctx context.Context, repoPath string) error {
	// Check if the repository is shallow
	shallowFile := filepath.Join(repoPath, "shallow")
	isShallow := false
	if _, err := os.Stat(shallowFile); err == nil {
		isShallow = true
	}

	args := []string{"fetch", "--prune"}
	if isShallow {
		args = append(args, "--unshallow")
	}
	args = append(args, "origin", "+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*")

	cmd := command(ctx, "git", args...)
	cmd.Dir = repoPath
	return cmd.Run()
}

// saveMirrorConfig marks the repository as a mirror in git config.
// This sets remote.origin.mirror = true in the repository's config.
func (h *Handler) saveMirrorConfig(ctx context.Context, repoPath string, sourceURL string) error {
	cmd := command(ctx, "git", "config", "remote.origin.mirror", "true")
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		return err
	}

	cmd = command(ctx, "git", "remote", "set-url", "origin", sourceURL)
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		// If that fails, try to add the remote
		cmd = command(ctx, "git", "remote", "add", "origin", sourceURL)
		cmd.Dir = repoPath
		return cmd.Run()
	}
	return nil
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

	if !isMirror || sourceURL == "" {
		http.Error(w, "Repository is not a mirror", http.StatusBadRequest)
		return
	}

	// Initialize import status
	h.setImportStatus(repoName, &ImportStatus{
		Status: "in_progress",
		Step:   "syncing",
	})

	// Run sync in background
	go func() {
		err := h.doImportWithStatus(context.Background(), repoPath, repoName, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Sync failed for %s: %v\n", repoName, err)
		}
	}()

	w.WriteHeader(http.StatusAccepted)
}

// getRemoteDefaultBranch discovers the default branch of a remote repository
func (h *Handler) getRemoteDefaultBranch(ctx context.Context, sourceURL string) (string, error) {
	cmd := command(ctx, "git", "ls-remote", "--symref", sourceURL, "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// Parse output to find the default branch
	// Format: ref: refs/heads/main	HEAD
	lines := string(output)
	for _, line := range splitLines(lines) {
		if len(line) > 5 && line[:5] == "ref: " {
			// Extract the ref part after "ref: "
			remaining := line[5:]
			// Find the end of the ref (before whitespace/tab)
			refEnd := len(remaining)
			for i := 0; i < len(remaining); i++ {
				if remaining[i] == ' ' || remaining[i] == '\t' {
					refEnd = i
					break
				}
			}
			ref := remaining[:refEnd]
			// Extract branch name from refs/heads/xxx
			if len(ref) > 11 && ref[:11] == "refs/heads/" {
				return ref[11:], nil
			}
		}
	}

	return "", fmt.Errorf("could not determine default branch")
}

func (h *Handler) setLocalDefaultBranch(ctx context.Context, repoPath, branch string) error {
	// Set HEAD to the specified branch
	cmd := command(ctx, "git", "symbolic-ref", "HEAD", fmt.Sprintf("refs/heads/%s", branch))
	cmd.Dir = repoPath
	return cmd.Run()
}

// splitLines splits a string into lines
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
