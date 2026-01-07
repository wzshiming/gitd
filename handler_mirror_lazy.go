package gitd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// lazyMirrorSync manages lazy synchronization of mirror repositories.
// It tracks in-flight syncs to avoid duplicate fetches and respects cooldown periods.
type lazyMirrorSync struct {
	mu       sync.Mutex
	syncing  map[string]chan struct{} // tracks in-flight syncs
	lastSync map[string]time.Time     // tracks last sync time for cooldown
	cooldown time.Duration            // minimum time between syncs
}

func newLazyMirrorSync(cooldown time.Duration) *lazyMirrorSync {
	return &lazyMirrorSync{
		syncing:  make(map[string]chan struct{}),
		lastSync: make(map[string]time.Time),
		cooldown: cooldown,
	}
}

// syncMirror synchronizes a mirror repository with its remote.
// It returns immediately if a sync is already in progress (waiting for it to complete),
// or if the cooldown period hasn't elapsed since the last sync.
func (l *lazyMirrorSync) syncMirror(ctx context.Context, repoPath string, sourceURL string) error {
	l.mu.Lock()

	// Check cooldown
	if lastSync, ok := l.lastSync[repoPath]; ok {
		if time.Since(lastSync) < l.cooldown {
			l.mu.Unlock()
			return nil // Within cooldown period, skip sync
		}
	}

	// Check if sync is already in progress
	if ch, ok := l.syncing[repoPath]; ok {
		l.mu.Unlock()
		// Wait for the existing sync to complete
		select {
		case <-ch:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Start a new sync
	ch := make(chan struct{})
	l.syncing[repoPath] = ch
	l.mu.Unlock()

	// Perform the sync
	err := l.doSync(ctx, repoPath, sourceURL)

	// Mark sync as complete
	l.mu.Lock()
	delete(l.syncing, repoPath)
	l.lastSync[repoPath] = time.Now()
	l.mu.Unlock()

	close(ch)
	return err
}

// doSync performs the actual fetch from the remote.
func (l *lazyMirrorSync) doSync(ctx context.Context, repoPath string, sourceURL string) error {
	// First, check if local refs differ from remote refs
	needsSync, err := l.checkNeedsSync(ctx, repoPath, sourceURL)
	if err != nil {
		// If we can't determine, try to sync anyway
		needsSync = true
	}

	if !needsSync {
		return nil
	}

	// Fetch from remote, updating all refs
	cmd := command(ctx, "git", "fetch", "--prune", "origin", "+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*")
	cmd.Dir = repoPath
	return cmd.Run()
}

// checkNeedsSync checks if the local repository needs to sync with remote.
// Returns true if refs differ or if we can't determine.
func (l *lazyMirrorSync) checkNeedsSync(ctx context.Context, repoPath string, sourceURL string) (bool, error) {
	// Get local refs
	localRefs, err := l.getLocalRefs(ctx, repoPath)
	if err != nil {
		return true, err
	}

	// Get remote refs
	remoteRefs, err := l.getRemoteRefs(ctx, sourceURL)
	if err != nil {
		return true, err
	}

	// Compare refs
	return !refsEqual(localRefs, remoteRefs), nil
}

// getLocalRefs returns a map of ref name to commit SHA for local refs.
func (l *lazyMirrorSync) getLocalRefs(ctx context.Context, repoPath string) (map[string]string, error) {
	cmd := exec.CommandContext(ctx, "git", "show-ref")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		// show-ref returns error if no refs exist
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return make(map[string]string), nil
		}
		return nil, err
	}

	return parseRefs(string(output)), nil
}

// getRemoteRefs returns a map of ref name to commit SHA for remote refs.
func (l *lazyMirrorSync) getRemoteRefs(ctx context.Context, sourceURL string) (map[string]string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-remote", sourceURL)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list remote refs: %w", err)
	}

	return parseRemoteRefs(string(output)), nil
}

// parseRefs parses output of `git show-ref`.
// Format: <sha> <ref>
func parseRefs(output string) map[string]string {
	refs := make(map[string]string)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			sha := parts[0]
			ref := parts[1]
			// Normalize ref name: remove refs/heads/ or refs/tags/ prefix for comparison
			refs[ref] = sha
		}
	}
	return refs
}

// parseRemoteRefs parses output of `git ls-remote`.
// Format: <sha>\t<ref>
func parseRemoteRefs(output string) map[string]string {
	refs := make(map[string]string)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 {
			sha := parts[0]
			ref := parts[1]
			// Skip HEAD ref as it's symbolic
			if ref == "HEAD" {
				continue
			}
			refs[ref] = sha
		}
	}
	return refs
}

// refsEqual checks if two ref maps are equal.
func refsEqual(local, remote map[string]string) bool {
	// Check if all remote refs exist in local with same SHA
	for ref, remoteSha := range remote {
		localSha, ok := local[ref]
		if !ok || localSha != remoteSha {
			return false
		}
	}
	// Check if local has any refs that don't exist in remote
	for ref := range local {
		if _, ok := remote[ref]; !ok {
			return false
		}
	}
	return true
}

// isLazyMirror checks if a repository is configured for lazy mirroring.
// This is determined by checking if gitd.lazy is set to "true" in git config.
func isLazyMirror(repoPath string) bool {
	cmd := exec.Command("git", "config", "--get", "gitd.lazy")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "true"
}

// setLazyMirror configures a repository for lazy mirroring.
func setLazyMirror(ctx context.Context, repoPath string, enabled bool) error {
	value := "false"
	if enabled {
		value = "true"
	}
	cmd := command(ctx, "git", "config", "gitd.lazy", value)
	cmd.Dir = repoPath
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
