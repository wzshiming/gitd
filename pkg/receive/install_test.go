package receive

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallHooks(t *testing.T) {
	repoPath := t.TempDir()

	err := InstallHooks(repoPath)
	if err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}

	// Check pre-receive hook exists and is executable
	preReceivePath := filepath.Join(repoPath, "hooks", "pre-receive")
	info, err := os.Stat(preReceivePath)
	if err != nil {
		t.Fatalf("pre-receive hook not found: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("pre-receive hook is not executable, mode = %v", info.Mode())
	}

	// Check post-receive hook exists and is executable
	postReceivePath := filepath.Join(repoPath, "hooks", "post-receive")
	info, err = os.Stat(postReceivePath)
	if err != nil {
		t.Fatalf("post-receive hook not found: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("post-receive hook is not executable, mode = %v", info.Mode())
	}

	// Verify content
	data, err := os.ReadFile(preReceivePath)
	if err != nil {
		t.Fatalf("failed to read pre-receive hook: %v", err)
	}
	if string(data) != preReceiveScript {
		t.Errorf("pre-receive hook content mismatch")
	}

	data, err = os.ReadFile(postReceivePath)
	if err != nil {
		t.Fatalf("failed to read post-receive hook: %v", err)
	}
	if string(data) != postReceiveScript {
		t.Errorf("post-receive hook content mismatch")
	}
}

func TestInstallHooksDoesNotOverwrite(t *testing.T) {
	repoPath := t.TempDir()

	hooksDir := filepath.Join(repoPath, "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}

	customContent := "#!/bin/sh\necho custom\n"
	if err := os.WriteFile(filepath.Join(hooksDir, "pre-receive"), []byte(customContent), 0755); err != nil {
		t.Fatal(err)
	}

	err := InstallHooks(repoPath)
	if err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}

	// pre-receive should not be overwritten
	data, err := os.ReadFile(filepath.Join(hooksDir, "pre-receive"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != customContent {
		t.Errorf("pre-receive was overwritten, got %q", string(data))
	}

	// post-receive should be created (didn't exist)
	data, err = os.ReadFile(filepath.Join(hooksDir, "post-receive"))
	if err != nil {
		t.Fatalf("post-receive not found: %v", err)
	}
	if string(data) != postReceiveScript {
		t.Errorf("post-receive content mismatch")
	}
}

func TestInstallHooksCreatesDirectory(t *testing.T) {
	repoPath := t.TempDir()

	// hooks/ dir should not exist yet
	hooksDir := filepath.Join(repoPath, "hooks")
	if _, err := os.Stat(hooksDir); !os.IsNotExist(err) {
		t.Fatal("hooks dir should not exist before InstallHooks")
	}

	err := InstallHooks(repoPath)
	if err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}

	info, err := os.Stat(hooksDir)
	if err != nil {
		t.Fatalf("hooks dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("hooks is not a directory")
	}
}

func TestPostReceiveScriptCapturesRefUpdates(t *testing.T) {
	repoPath := t.TempDir()

	err := InstallHooks(repoPath)
	if err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}

	// Create a temp file for hook output
	outputFile := filepath.Join(t.TempDir(), "hook-output")

	// Simulate running the post-receive hook with ref updates on stdin
	refUpdates := "abc123 def456 refs/heads/main\n" +
		ZeroHash + " aaa111 refs/tags/v1.0\n"

	hookScript := filepath.Join(repoPath, "hooks", "post-receive")
	cmd := exec.Command("/bin/sh", hookScript)
	cmd.Stdin = strings.NewReader(refUpdates)
	cmd.Env = append(os.Environ(),
		"HFD_REPO_NAME=test/repo",
		"HFD_HOOK_OUTPUT="+outputFile,
	)

	if err := cmd.Run(); err != nil {
		t.Fatalf("post-receive hook failed: %v", err)
	}

	// Verify the output file contains the ref updates
	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read hook output: %v", err)
	}

	updates, err := ParseRefUpdates(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("failed to parse hook output: %v", err)
	}

	if len(updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(updates))
	}

	if updates[0].RefName != "refs/heads/main" || updates[0].OldRev != "abc123" || updates[0].NewRev != "def456" {
		t.Errorf("unexpected first update: %+v", updates[0])
	}
	if updates[1].RefName != "refs/tags/v1.0" || updates[1].OldRev != ZeroHash || updates[1].NewRev != "aaa111" {
		t.Errorf("unexpected second update: %+v", updates[1])
	}
}

func TestPostReceiveScriptNoOutputWithoutEnv(t *testing.T) {
	repoPath := t.TempDir()

	err := InstallHooks(repoPath)
	if err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}

	// Run the post-receive hook without HFD_HOOK_OUTPUT set
	refUpdates := "abc123 def456 refs/heads/main\n"

	hookScript := filepath.Join(repoPath, "hooks", "post-receive")
	cmd := exec.Command("/bin/sh", hookScript)
	cmd.Stdin = strings.NewReader(refUpdates)
	// Don't set HFD_HOOK_OUTPUT

	if err := cmd.Run(); err != nil {
		t.Fatalf("post-receive hook failed: %v", err)
	}
	// Script should exit 0 without errors when HFD_HOOK_OUTPUT is not set
}
