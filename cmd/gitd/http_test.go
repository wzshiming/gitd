package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/osfs"
	githttp "github.com/go-git/go-git/v6/backend/http"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

// TestHTTPServer tests the HTTP server by creating a bare git repository
// and cloning it via HTTP using the binary git command.
func TestHTTPServer(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir, err := os.MkdirTemp("", "gitd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a bare git repository
	repoPath := filepath.Join(tmpDir, "test.git")
	cmd := exec.Command("git", "init", "--bare", repoPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init bare repo: %v, output: %s", err, out)
	}

	// Start HTTP server
	loader := transport.NewFilesystemLoader(osfs.New(tmpDir, osfs.WithBoundOS()), false)
	gitmw := githttp.NewBackend(loader)

	// Start server in background on dynamic port
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	server := &http.Server{
		Handler: gitmw,
	}

	go server.Serve(ln)
	defer server.Shutdown(context.Background())

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Get the dynamically assigned port
	port := ln.Addr().(*net.TCPAddr).Port

	// Create another temp dir for clone
	cloneDir, err := os.MkdirTemp("", "gitd-clone-*")
	if err != nil {
		t.Fatalf("failed to create clone dir: %v", err)
	}
	defer os.RemoveAll(cloneDir)

	// Clone the repo using binary git
	repoURL := fmt.Sprintf("http://localhost:%d/test.git", port)
	cmd = exec.Command("git", "clone", repoURL, filepath.Join(cloneDir, "test"))
	if out, err := cmd.CombinedOutput(); err != nil {
		// Cloning an empty repo gives a warning but should succeed
		t.Logf("git clone output: %s", out)
	}

	// Verify the clone directory exists
	if _, err := os.Stat(filepath.Join(cloneDir, "test", ".git")); os.IsNotExist(err) {
		t.Errorf("cloned repo does not exist")
	}
}
