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

func (h *Handler) registryGit(r *mux.Router) {
	r.HandleFunc("/{repo:.+}/info/refs", h.requireAuth(h.handleInfoRefs)).Methods(http.MethodGet)
	r.HandleFunc("/{repo:.+}/git-upload-pack", h.requireAuth(h.handleUploadPack)).Methods(http.MethodPost)
	r.HandleFunc("/{repo:.+}/git-receive-pack", h.requireAuth(h.handleReceivePack)).Methods(http.MethodPost)
}

// handleInfoRefs handles the /info/refs endpoint for git service discovery.
func (h *Handler) handleInfoRefs(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	repoName := vars["repo"]

	repoPath := h.resolveRepoPath(repoName)
	if repoPath == "" {
		http.NotFound(w, r)
		return
	}

	service := r.URL.Query().Get("service")
	if service == "" {
		http.Error(w, "service parameter is required", http.StatusBadRequest)
		return
	}

	if service != "git-upload-pack" && service != "git-receive-pack" {
		http.Error(w, "unsupported service", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", fmt.Sprintf("application/x-%s-advertisement", service))
	w.Header().Set("Cache-Control", "no-cache")

	// Write packet-line formatted header
	if _, err := w.Write(packetLine(fmt.Sprintf("# service=%s\n", service))); err != nil {
		return
	}
	if _, err := w.Write([]byte("0000")); err != nil {
		return
	}

	base, dir := filepath.Split(repoPath)

	cmd := command(r.Context(), service, "--stateless-rpc", "--advertise-refs", dir)
	// Execute git command
	cmd.Dir = base
	cmd.Stdout = w
	err := cmd.Run()
	if err != nil {
		http.Error(w, "Failed to execute git command", http.StatusInternalServerError)
		return
	}
}

// handleUploadPack handles the git-upload-pack endpoint (fetch/clone).
func (h *Handler) handleUploadPack(w http.ResponseWriter, r *http.Request) {
	h.handleService(w, r, "git-upload-pack")
}

// handleReceivePack handles the git-receive-pack endpoint (push).
func (h *Handler) handleReceivePack(w http.ResponseWriter, r *http.Request) {
	h.handleService(w, r, "git-receive-pack")
}

// handleService handles a git service request.
func (h *Handler) handleService(w http.ResponseWriter, r *http.Request, service string) {
	vars := mux.Vars(r)
	repoName := vars["repo"]

	repoPath := h.resolveRepoPath(repoName)
	if repoPath == "" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", fmt.Sprintf("application/x-git-%s-result", service))
	w.Header().Set("Cache-Control", "no-cache")

	base, dir := filepath.Split(repoPath)

	cmd := command(r.Context(), service, "--stateless-rpc", dir)
	cmd.Dir = base
	cmd.Stdin = r.Body
	cmd.Stdout = w
	if err := cmd.Run(); err != nil {
		http.Error(w, "Failed to execute git command", http.StatusInternalServerError)
		return
	}
}

// resolveRepoPath resolves and validates a repository path.
// If lazy mirroring is enabled and the repository doesn't exist locally,
// it will attempt to create it as a mirror from the upstream source.
func (h *Handler) resolveRepoPath(urlPath string) string {
	// Clean the path
	urlPath = strings.TrimPrefix(urlPath, "/")
	if urlPath == "" {
		return ""
	}

	// Construct the full path
	fullPath := filepath.Join(h.rootDir, urlPath)

	// Clean and verify the path is within RepoDir using filepath.Rel
	fullPath = filepath.Clean(fullPath)
	absRepoDir, err := filepath.Abs(h.rootDir)
	if err != nil {
		return ""
	}
	absFullPath, err := filepath.Abs(fullPath)
	if err != nil {
		return ""
	}
	// Use filepath.Rel to safely check if absFullPath is within absRepoDir
	relPath, err := filepath.Rel(absRepoDir, absFullPath)
	if err != nil {
		return ""
	}
	// Reject if the relative path starts with ".." (meaning it's outside RepoDir)
	if strings.HasPrefix(relPath, "..") {
		return ""
	}

	// Check if the path is a git repository
	if isGitRepository(fullPath) {
		return fullPath
	}

	// Try with .git extension
	gitPath := fullPath + ".git"
	if isGitRepository(gitPath) {
		return gitPath
	}

	// If lazy mirror is enabled, try to create the repository on-demand
	if h.lazyMirrorSource != nil {
		repoPath := h.tryLazyMirror(urlPath)
		if repoPath != "" {
			return repoPath
		}
	}

	return ""
}

// tryLazyMirror attempts to create a lazy mirror repository for the given path.
// It returns the repository path if successful, or empty string if the mirror cannot be created.
func (h *Handler) tryLazyMirror(urlPath string) string {
	// Determine the repository name (strip .git suffix if present)
	repoName := strings.TrimSuffix(urlPath, ".git")

	// Ask the lazy mirror source function for the upstream URL
	upstreamURL := h.lazyMirrorSource(repoName)
	if upstreamURL == "" {
		return ""
	}

	// Validate and construct the repository path
	repoPath, err := h.validateRepoPath(repoName)
	if err != nil {
		return ""
	}

	ctx := context.Background()

	// Create the bare repository
	if err := h.createBareRepo(ctx, repoPath); err != nil {
		// Repository might already be being created by another request
		if isGitRepository(repoPath) {
			return repoPath
		}
		return ""
	}

	// Configure it as a mirror
	if err := h.saveMirrorConfig(ctx, repoPath, upstreamURL); err != nil {
		return ""
	}

	// Try to get the default branch and set it
	defaultBranch, err := h.getRemoteDefaultBranch(ctx, upstreamURL)
	if err != nil {
		defaultBranch = "main" // Fallback to main
	}
	_ = h.setLocalDefaultBranch(ctx, repoPath, defaultBranch)

	// Initialize import status
	h.setImportStatus(repoName, &ImportStatus{
		Status: "in_progress",
		Step:   "starting lazy mirror",
	})

	// Start the import in the background
	go func() {
		err := h.doImportWithStatus(context.Background(), repoPath, repoName, defaultBranch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Lazy mirror import failed for %s: %v\n", repoName, err)
		}
	}()

	return repoPath
}

// isGitRepository checks if the given path is a git repository.
func isGitRepository(path string) bool {
	// Check for bare repository (has HEAD file directly)
	if _, err := os.Stat(filepath.Join(path, "HEAD")); err == nil {
		return true
	}
	// Check for non-bare repository (has .git directory)
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return true
	}
	return false
}

// packetLine formats a string as a git packet-line.
func packetLine(s string) []byte {
	return []byte(fmt.Sprintf("%04x%s", len(s)+4, s))
}
