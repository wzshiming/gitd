package gitd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

// mirrorSyncState tracks the synchronization state for lazy mirrors
type mirrorSyncState struct {
	mu          sync.Mutex
	lastSync    time.Time
	syncing     bool
	syncErr     error
	remoteRefs  map[string]string // ref name -> commit SHA
	localRefs   map[string]string // ref name -> commit SHA
}

// mirrorSyncCache stores sync state for each repository
var mirrorSyncCache = struct {
	mu    sync.RWMutex
	cache map[string]*mirrorSyncState
}{
	cache: make(map[string]*mirrorSyncState),
}

// getMirrorSyncState gets or creates the sync state for a repository
func getMirrorSyncState(repoPath string) *mirrorSyncState {
	mirrorSyncCache.mu.Lock()
	defer mirrorSyncCache.mu.Unlock()
	
	if state, ok := mirrorSyncCache.cache[repoPath]; ok {
		return state
	}
	
	state := &mirrorSyncState{
		remoteRefs: make(map[string]string),
		localRefs:  make(map[string]string),
	}
	mirrorSyncCache.cache[repoPath] = state
	return state
}

// getLazyMirrorInfo reads lazy mirror configuration from git config.
// Returns whether the repository is a lazy mirror.
func (h *Handler) getLazyMirrorInfo(repoPath string) (bool, error) {
	cmd := exec.Command("git", "config", "--get", "remote.origin.lazymirror")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		// If the config key doesn't exist, it's not a lazy mirror
		return false, nil
	}

	return strings.TrimSpace(string(output)) == "true", nil
}

// setLazyMirrorConfig sets the lazy mirror configuration for a repository
func (h *Handler) setLazyMirrorConfig(ctx context.Context, repoPath string, enabled bool) error {
	value := "false"
	if enabled {
		value = "true"
	}
	cmd := command(ctx, "git", "config", "remote.origin.lazymirror", value)
	cmd.Dir = repoPath
	return cmd.Run()
}

// getRemoteRefs fetches the refs from the remote repository
func (h *Handler) getRemoteRefs(ctx context.Context, sourceURL string) (map[string]string, error) {
	cmd := command(ctx, "git", "ls-remote", sourceURL)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get remote refs: %w", err)
	}

	refs := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			refs[parts[1]] = parts[0]
		}
	}
	return refs, nil
}

// getLocalRefs fetches the refs from the local repository
func (h *Handler) getLocalRefs(ctx context.Context, repoPath string) (map[string]string, error) {
	cmd := command(ctx, "git", "show-ref")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		// Empty repository has no refs
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return make(map[string]string), nil
		}
		return nil, fmt.Errorf("failed to get local refs: %w", err)
	}

	refs := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			refs[parts[1]] = parts[0]
		}
	}
	return refs, nil
}

// refsNeedSync checks if local refs are different from remote refs
func refsNeedSync(localRefs, remoteRefs map[string]string) bool {
	// Check if any remote ref is missing or different in local
	for ref, remoteSha := range remoteRefs {
		if localSha, ok := localRefs[ref]; !ok || localSha != remoteSha {
			return true
		}
	}
	return false
}

// syncLazyMirror synchronizes the lazy mirror with its remote
func (h *Handler) syncLazyMirror(ctx context.Context, repoPath, sourceURL string) error {
	state := getMirrorSyncState(repoPath)
	
	state.mu.Lock()
	// If already syncing, wait for it to complete
	if state.syncing {
		state.mu.Unlock()
		// Wait a bit and return - the other goroutine is handling sync
		return nil
	}
	
	// Check if we synced recently (within last 5 seconds) to avoid hammering remote
	if time.Since(state.lastSync) < 5*time.Second && state.syncErr == nil {
		state.mu.Unlock()
		return nil
	}
	
	state.syncing = true
	state.mu.Unlock()
	
	defer func() {
		state.mu.Lock()
		state.syncing = false
		state.lastSync = time.Now()
		state.mu.Unlock()
	}()

	// Get remote refs
	remoteRefs, err := h.getRemoteRefs(ctx, sourceURL)
	if err != nil {
		state.mu.Lock()
		state.syncErr = err
		state.mu.Unlock()
		return err
	}

	// Get local refs
	localRefs, err := h.getLocalRefs(ctx, repoPath)
	if err != nil {
		state.mu.Lock()
		state.syncErr = err
		state.mu.Unlock()
		return err
	}

	// Update cached refs
	state.mu.Lock()
	state.remoteRefs = remoteRefs
	state.localRefs = localRefs
	state.mu.Unlock()

	// Check if sync is needed
	if !refsNeedSync(localRefs, remoteRefs) {
		state.mu.Lock()
		state.syncErr = nil
		state.mu.Unlock()
		return nil
	}

	// Fetch refs from remote
	cmd := command(ctx, "git", "fetch", "--prune", "origin", "+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*")
	cmd.Dir = repoPath
	err = cmd.Run()
	if err != nil {
		state.mu.Lock()
		state.syncErr = fmt.Errorf("failed to fetch: %w", err)
		state.mu.Unlock()
		return state.syncErr
	}

	state.mu.Lock()
	state.syncErr = nil
	state.mu.Unlock()
	return nil
}

// ensureLazyMirrorSynced ensures a lazy mirror is synced before serving requests
func (h *Handler) ensureLazyMirrorSynced(ctx context.Context, repoPath string) error {
	// Check if this is a lazy mirror
	isLazy, err := h.getLazyMirrorInfo(repoPath)
	if err != nil || !isLazy {
		return nil // Not a lazy mirror, nothing to do
	}

	// Get source URL
	sourceURL, isMirror, err := h.getMirrorInfo(repoPath)
	if err != nil || !isMirror || sourceURL == "" {
		return nil // Not a proper mirror
	}

	// Sync with remote
	return h.syncLazyMirror(ctx, repoPath, sourceURL)
}

// MirrorInfo represents mirror configuration for API responses
type MirrorInfo struct {
	IsMirror   bool   `json:"is_mirror"`
	SourceURL  string `json:"source_url,omitempty"`
	LazyMirror bool   `json:"lazy_mirror"`
}

// LazyMirrorRequest represents a request to configure lazy mirror mode
type LazyMirrorRequest struct {
	Enabled bool `json:"enabled"`
}

func (h *Handler) registryMirror(r *mux.Router) {
	r.HandleFunc("/api/repositories/{repo:.+}.git/mirror", h.requireAuth(h.handleGetMirrorInfo)).Methods(http.MethodGet)
	r.HandleFunc("/api/repositories/{repo:.+}.git/mirror/lazy", h.requireAuth(h.handleSetLazyMirror)).Methods(http.MethodPost)
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

	isLazy, err := h.getLazyMirrorInfo(repoPath)
	if err != nil {
		http.Error(w, "Failed to get lazy mirror info", http.StatusInternalServerError)
		return
	}

	info := MirrorInfo{
		IsMirror:   isMirror,
		SourceURL:  sourceURL,
		LazyMirror: isLazy,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// handleSetLazyMirror enables or disables lazy mirror mode for a repository
func (h *Handler) handleSetLazyMirror(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	repoName := vars["repo"]

	repoPath := h.resolveRepoPath(repoName)
	if repoPath == "" {
		http.NotFound(w, r)
		return
	}

	// Check if this is a mirror repository
	_, isMirror, err := h.getMirrorInfo(repoPath)
	if err != nil {
		http.Error(w, "Failed to get mirror info", http.StatusInternalServerError)
		return
	}

	if !isMirror {
		http.Error(w, "Repository is not a mirror", http.StatusBadRequest)
		return
	}

	var req LazyMirrorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	err = h.setLazyMirrorConfig(r.Context(), repoPath, req.Enabled)
	if err != nil {
		http.Error(w, "Failed to set lazy mirror config", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"lazy_mirror": req.Enabled})
}
