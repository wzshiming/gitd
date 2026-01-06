package gitd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

// ImportRequest represents the request body for importing a repository
type ImportRequest struct {
	SourceURL string `json:"source_url"`
}

// ImportStatus represents the current status of an import operation
type ImportStatus struct {
	State       string  `json:"state"`        // "pending", "in_progress", "completed", "failed"
	Phase       string  `json:"phase"`        // Current phase of import
	Progress    float64 `json:"progress"`     // Progress percentage (0-100)
	Message     string  `json:"message"`      // Human-readable status message
	StartedAt   string  `json:"started_at"`   // When the import started
	CompletedAt string  `json:"completed_at"` // When the import completed (if applicable)
	Error       string  `json:"error"`        // Error message if failed
}

// importStore stores the status of import operations
type importStore struct {
	mu     sync.RWMutex
	status map[string]*ImportStatus
}

var imports = &importStore{
	status: make(map[string]*ImportStatus),
}

func (s *importStore) get(repo string) *ImportStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if status, ok := s.status[repo]; ok {
		return status
	}
	return nil
}

func (s *importStore) set(repo string, status *ImportStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status[repo] = status
}

func (s *importStore) delete(repo string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.status, repo)
}

func (h *Handler) registryImport(r *mux.Router) {
	r.HandleFunc("/api/repositories/{repo:.+}/import", h.requireAuth(h.handleImportRepository)).Methods(http.MethodPost)
	r.HandleFunc("/api/repositories/{repo:.+}/import/status", h.requireAuth(h.handleImportStatus)).Methods(http.MethodGet)
}

// handleImportRepository handles the import repository request
func (h *Handler) handleImportRepository(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	repoName := vars["repo"]

	// Parse request body
	var req ImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.SourceURL == "" {
		http.Error(w, "source_url is required", http.StatusBadRequest)
		return
	}

	// Check if repository already exists
	repoPath := h.resolveRepoPath(repoName)
	if repoPath != "" {
		http.Error(w, "Repository already exists", http.StatusConflict)
		return
	}

	// Check if import is already in progress
	if status := imports.get(repoName); status != nil && status.State == "in_progress" {
		http.Error(w, "Import already in progress", http.StatusConflict)
		return
	}

	// Create the repository path
	repoPath = filepath.Join(h.rootDir, repoName)

	// Initialize import status
	status := &ImportStatus{
		State:     "in_progress",
		Phase:     "initializing",
		Progress:  0,
		Message:   "Starting import...",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	imports.set(repoName, status)

	// Start import in background
	go h.performImport(repoName, repoPath, req.SourceURL)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(status)
}

// handleImportStatus returns the status of an import operation
func (h *Handler) handleImportStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	repoName := vars["repo"]

	status := imports.get(repoName)
	if status == nil {
		// Check if repository exists and is complete
		repoPath := h.resolveRepoPath(repoName)
		if repoPath != "" {
			status = &ImportStatus{
				State:    "completed",
				Phase:    "done",
				Progress: 100,
				Message:  "Repository is ready",
			}
		} else {
			http.NotFound(w, r)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// performImport performs the progressive import of a repository
func (h *Handler) performImport(repoName, repoPath, sourceURL string) {
	updateStatus := func(phase string, progress float64, message string) {
		status := imports.get(repoName)
		if status != nil {
			status.Phase = phase
			status.Progress = progress
			status.Message = message
			imports.set(repoName, status)
		}
	}

	setFailed := func(err error) {
		status := imports.get(repoName)
		if status != nil {
			status.State = "failed"
			status.Error = err.Error()
			status.CompletedAt = time.Now().UTC().Format(time.RFC3339)
			imports.set(repoName, status)
		}
	}

	setCompleted := func() {
		status := imports.get(repoName)
		if status != nil {
			status.State = "completed"
			status.Phase = "done"
			status.Progress = 100
			status.Message = "Import completed successfully"
			status.CompletedAt = time.Now().UTC().Format(time.RFC3339)
			imports.set(repoName, status)
		}
	}

	// Phase 1: Create the bare repository
	updateStatus("creating_repository", 5, "Creating bare repository...")
	base, dir := filepath.Split(repoPath)
	if err := os.MkdirAll(base, 0755); err != nil {
		setFailed(fmt.Errorf("failed to create directory: %w", err))
		return
	}

	cmd := exec.Command("git", "init", "--bare", dir)
	cmd.Dir = base
	if err := cmd.Run(); err != nil {
		setFailed(fmt.Errorf("failed to initialize repository: %w", err))
		return
	}

	// Phase 2: Fetch the default branch name from remote
	updateStatus("discovering_remote", 10, "Discovering remote repository...")
	defaultBranch, err := h.getRemoteDefaultBranch(sourceURL)
	if err != nil {
		// Fall back to common branch names
		defaultBranch = "main"
	}

	// Phase 3: Clone with tree-only depth 1 (fastest initial access)
	updateStatus("fetching_tree", 15, "Fetching tree structure with depth 1...")
	if err := h.fetchWithOptions(repoPath, sourceURL, defaultBranch, 1, true); err != nil {
		// Non-fatal: continue with other phases
		updateStatus("fetching_tree", 20, "Tree fetch skipped, continuing...")
	} else {
		updateStatus("fetching_tree", 25, "Tree structure fetched")
	}

	// Phase 4: Clone with depth 1 (latest branch content)
	updateStatus("fetching_shallow", 30, "Fetching latest commits with depth 1...")
	if err := h.fetchWithOptions(repoPath, sourceURL, defaultBranch, 1, false); err != nil {
		setFailed(fmt.Errorf("failed to fetch shallow clone: %w", err))
		return
	}
	updateStatus("fetching_shallow", 45, "Shallow fetch completed")

	// Phase 5: Fetch LFS files for the latest branch
	updateStatus("fetching_lfs", 50, "Fetching LFS files...")
	if err := h.fetchLFSFiles(repoPath, sourceURL); err != nil {
		// LFS fetch is non-fatal
		updateStatus("fetching_lfs", 55, "LFS fetch skipped or no LFS files found")
	} else {
		updateStatus("fetching_lfs", 60, "LFS files fetched")
	}

	// Phase 6: Clone with depth 100 (more history)
	updateStatus("fetching_history", 65, "Fetching more history with depth 100...")
	if err := h.fetchWithOptions(repoPath, sourceURL, "", 100, false); err != nil {
		// Non-fatal: continue with full fetch
		updateStatus("fetching_history", 70, "Partial history fetch skipped, continuing...")
	} else {
		updateStatus("fetching_history", 75, "Partial history fetched")
	}

	// Phase 7: Fetch full repository
	updateStatus("fetching_full", 80, "Fetching full repository...")
	if err := h.fetchFull(repoPath, sourceURL); err != nil {
		setFailed(fmt.Errorf("failed to fetch full repository: %w", err))
		return
	}
	updateStatus("fetching_full", 95, "Full repository fetched")

	// Phase 8: Final cleanup and verification
	updateStatus("finalizing", 98, "Finalizing import...")
	if err := h.finalizeImport(repoPath, defaultBranch); err != nil {
		setFailed(fmt.Errorf("failed to finalize import: %w", err))
		return
	}

	setCompleted()
}

// getRemoteDefaultBranch discovers the default branch of a remote repository
func (h *Handler) getRemoteDefaultBranch(sourceURL string) (string, error) {
	cmd := exec.Command("git", "ls-remote", "--symref", sourceURL, "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// Parse output to find the default branch
	// Format: ref: refs/heads/main	HEAD
	for _, line := range strings.Split(string(output), "\n") {
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

// fetchWithOptions fetches from remote with specified options
func (h *Handler) fetchWithOptions(repoPath, sourceURL, branch string, depth int, filterTree bool) error {
	args := []string{"fetch", "--progress"}

	if depth > 0 {
		args = append(args, fmt.Sprintf("--depth=%d", depth))
	}

	if filterTree {
		args = append(args, "--filter=tree:0")
	}

	args = append(args, sourceURL)

	if branch != "" {
		args = append(args, fmt.Sprintf("+refs/heads/%s:refs/heads/%s", branch, branch))
	} else {
		args = append(args, "+refs/heads/*:refs/heads/*")
		args = append(args, "+refs/tags/*:refs/tags/*")
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	cmd.Stderr = io.Discard
	return cmd.Run()
}

// fetchLFSFiles fetches LFS files from the remote
func (h *Handler) fetchLFSFiles(repoPath, sourceURL string) error {
	// First check if LFS is configured
	cmd := exec.Command("git", "lfs", "version")
	if err := cmd.Run(); err != nil {
		// LFS not installed, skip
		return nil
	}

	// Configure LFS remote
	cmd = exec.Command("git", "config", "lfs.url", sourceURL+"/info/lfs")
	cmd.Dir = repoPath
	cmd.Run() // Ignore errors

	// Fetch LFS objects
	cmd = exec.Command("git", "lfs", "fetch", "--all", sourceURL)
	cmd.Dir = repoPath
	cmd.Stderr = io.Discard
	return cmd.Run()
}

// fetchFull fetches the complete repository history
func (h *Handler) fetchFull(repoPath, sourceURL string) error {
	// Unshallow if necessary
	cmd := exec.Command("git", "fetch", "--unshallow", sourceURL, "+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*")
	cmd.Dir = repoPath
	cmd.Stderr = io.Discard
	cmd.Run() // Ignore errors from unshallow if already full

	// Fetch all refs
	cmd = exec.Command("git", "fetch", "--progress", sourceURL, "+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*")
	cmd.Dir = repoPath
	cmd.Stderr = io.Discard
	return cmd.Run()
}

// finalizeImport performs final cleanup after import
func (h *Handler) finalizeImport(repoPath, defaultBranch string) error {
	// Set HEAD to the default branch
	cmd := exec.Command("git", "symbolic-ref", "HEAD", fmt.Sprintf("refs/heads/%s", defaultBranch))
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		// Try to set HEAD to main or master
		for _, branch := range []string{"main", "master"} {
			cmd = exec.Command("git", "symbolic-ref", "HEAD", fmt.Sprintf("refs/heads/%s", branch))
			cmd.Dir = repoPath
			if err := cmd.Run(); err == nil {
				break
			}
		}
	}

	// Update server info for dumb HTTP protocol support
	cmd = exec.Command("git", "update-server-info")
	cmd.Dir = repoPath
	return cmd.Run()
}
