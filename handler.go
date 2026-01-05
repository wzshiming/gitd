// Package gitd provides an HTTP handler for serving git repositories.
package gitd

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Handler handles HTTP requests for git operations.
type Handler struct {
	// RepoDir is the base directory containing git repositories.
	RepoDir string
	// GitBinPath is the path to the git binary. If empty, uses "git" from PATH.
	GitBinPath string
}

// NewHandler creates a new Handler with the given repository directory.
func NewHandler(repoDir string) *Handler {
	return &Handler{
		RepoDir: repoDir,
	}
}

// gitPath returns the path to the git binary.
func (h *Handler) gitPath() string {
	if h.GitBinPath != "" {
		return h.GitBinPath
	}
	return "git"
}

// pathPatterns defines URL patterns for git operations.
var pathPatterns = struct {
	infoRefs   *regexp.Regexp
	uploadPack *regexp.Regexp
	recvPack   *regexp.Regexp
}{
	infoRefs:   regexp.MustCompile(`^(.*)/info/refs$`),
	uploadPack: regexp.MustCompile(`^(.*)/git-upload-pack$`),
	recvPack:   regexp.MustCompile(`^(.*)/git-receive-pack$`),
}

// ServeHTTP implements the http.Handler interface.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case pathPatterns.infoRefs.MatchString(path):
		h.handleInfoRefs(w, r)
	case pathPatterns.uploadPack.MatchString(path):
		h.handleUploadPack(w, r)
	case pathPatterns.recvPack.MatchString(path):
		h.handleReceivePack(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleInfoRefs handles the /info/refs endpoint for git service discovery.
func (h *Handler) handleInfoRefs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	matches := pathPatterns.infoRefs.FindStringSubmatch(r.URL.Path)
	if len(matches) < 2 {
		http.NotFound(w, r)
		return
	}

	repoPath := h.resolveRepoPath(matches[1])
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

	// Execute git command
	cmd := exec.Command(h.gitPath(), strings.TrimPrefix(service, "git-"), "--stateless-rpc", "--advertise-refs", repoPath)
	cmd.Stdout = w
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// Already started writing, can't change status
		return
	}
}

// handleUploadPack handles the git-upload-pack endpoint (fetch/clone).
func (h *Handler) handleUploadPack(w http.ResponseWriter, r *http.Request) {
	h.handleService(w, r, "upload-pack", pathPatterns.uploadPack)
}

// handleReceivePack handles the git-receive-pack endpoint (push).
func (h *Handler) handleReceivePack(w http.ResponseWriter, r *http.Request) {
	h.handleService(w, r, "receive-pack", pathPatterns.recvPack)
}

// handleService handles a git service request.
func (h *Handler) handleService(w http.ResponseWriter, r *http.Request, service string, pattern *regexp.Regexp) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	matches := pattern.FindStringSubmatch(r.URL.Path)
	if len(matches) < 2 {
		http.NotFound(w, r)
		return
	}

	repoPath := h.resolveRepoPath(matches[1])
	if repoPath == "" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", fmt.Sprintf("application/x-git-%s-result", service))
	w.Header().Set("Cache-Control", "no-cache")

	var body io.Reader = r.Body
	if r.Header.Get("Content-Encoding") == "gzip" {
		var err error
		body, err = gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, "Failed to decompress request body", http.StatusBadRequest)
			return
		}
	}

	cmd := exec.Command(h.gitPath(), service, "--stateless-rpc", repoPath)
	cmd.Stdin = body
	cmd.Stdout = w
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// Already started writing, can't change status
		return
	}
}

// resolveRepoPath resolves and validates a repository path.
func (h *Handler) resolveRepoPath(urlPath string) string {
	// Clean the path
	urlPath = strings.TrimPrefix(urlPath, "/")
	if urlPath == "" {
		return ""
	}

	// Construct the full path
	fullPath := filepath.Join(h.RepoDir, urlPath)

	// Clean and verify the path is within RepoDir
	fullPath = filepath.Clean(fullPath)
	absRepoDir, err := filepath.Abs(h.RepoDir)
	if err != nil {
		return ""
	}
	absFullPath, err := filepath.Abs(fullPath)
	if err != nil {
		return ""
	}
	if !strings.HasPrefix(absFullPath, absRepoDir) {
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

	return ""
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
