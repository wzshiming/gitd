package e2e_test

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wzshiming/hfd/internal/utils"
)

// createRepo creates a model repository via the HuggingFace API.
func createRepo(t *testing.T, endpoint, org, name string) {
	t.Helper()
	body := strings.NewReader(fmt.Sprintf(`{"type":"model","name":%q,"organization":%q}`, name, org))
	resp, err := http.Post(endpoint+"/api/repos/create", "application/json", body)
	if err != nil {
		t.Fatalf("Failed to create repo %s/%s: %v", org, name, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 creating repo %s/%s, got %d", org, name, resp.StatusCode)
	}
}

// runGitWithEnv runs a git command in dir with the provided extra env vars appended to os.Environ().
func runGitWithEnv(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := utils.Command(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if output, err := cmd.Output(); err != nil {
		t.Fatalf("git %s failed: %v\nOutput: %s", strings.Join(args, " "), err, output)
	}
}

// sshNoAuthGitEnv returns git env vars that connect via SSH without a key file
// (suitable when the server has NoClientAuth enabled).
func sshNoAuthGitEnv(port string) []string {
	sshCmd := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -p %s", port)
	return []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=" + sshCmd,
	}
}

// TestHTTPGitProtocolVersions verifies that the server accepts both protocol
// version=1 and version=2 via the Git-Protocol HTTP header and that normal git
// operations (clone, push, fetch) complete successfully under each version.
func TestHTTPGitProtocolVersions(t *testing.T) {
	server, _ := setupTestServer(t)
	endpoint := server.URL

	for _, version := range []string{"version=1", "version=2"} {
		version := version
		t.Run(version, func(t *testing.T) {
			clientDir, err := os.MkdirTemp("", "http-proto-e2e-client")
			if err != nil {
				t.Fatalf("Failed to create temp client dir: %v", err)
			}
			defer os.RemoveAll(clientDir)

			// Encode the version string into a safe directory/repo name.
			suffix := strings.ReplaceAll(version, "=", "-") // "version-1" / "version-2"
			org := "proto-user"
			repoName := "proto-" + suffix + "-model"
			createRepo(t, endpoint, org, repoName)

			gitURL := endpoint + "/" + org + "/" + repoName + ".git"
			env := []string{
				"GIT_TERMINAL_PROMPT=0",
				"GIT_PROTOCOL=" + version,
			}

			t.Run("Clone", func(t *testing.T) {
				cloneDir := filepath.Join(clientDir, "clone")
				runGitWithEnv(t, "", env, "clone", gitURL, cloneDir)
				if _, err := os.Stat(filepath.Join(cloneDir, ".git")); os.IsNotExist(err) {
					t.Error(".git directory not found after clone")
				}
			})

			t.Run("Push", func(t *testing.T) {
				workDir := filepath.Join(clientDir, "clone")
				runGitWithEnv(t, workDir, env, "config", "user.email", "test@test.com")
				runGitWithEnv(t, workDir, env, "config", "user.name", "Test User")

				if err := os.WriteFile(filepath.Join(workDir, "README.md"),
					[]byte("# "+version+"\n"), 0644); err != nil {
					t.Fatalf("Failed to create README: %v", err)
				}

				runGitWithEnv(t, workDir, env, "add", "README.md")
				runGitWithEnv(t, workDir, env, "commit", "-m", "initial commit")
				runGitWithEnv(t, workDir, env, "push", "origin", "main")
			})

			t.Run("Fetch", func(t *testing.T) {
				workDir := filepath.Join(clientDir, "clone")
				runGitWithEnv(t, workDir, env, "fetch", "origin")
			})

			t.Run("CloneWithContent", func(t *testing.T) {
				cloneDir := filepath.Join(clientDir, "clone2")
				runGitWithEnv(t, "", env, "clone", gitURL, cloneDir)

				content, err := os.ReadFile(filepath.Join(cloneDir, "README.md"))
				if err != nil {
					t.Fatalf("Failed to read README.md: %v", err)
				}
				if string(content) != "# "+version+"\n" {
					t.Errorf("Unexpected content: %q", content)
				}
			})
		})
	}
}

// TestHTTPGitProtocolInvalidRejected verifies that the server returns HTTP 400
// when an unrecognised Git-Protocol value is sent.
func TestHTTPGitProtocolInvalidRejected(t *testing.T) {
	server, _ := setupTestServer(t)
	endpoint := server.URL

	// Create a repo so the path is valid (we want to reach protocol validation).
	createRepo(t, endpoint, "invalid-proto-user", "invalid-proto-model")

	infoRefsURL := endpoint + "/invalid-proto-user/invalid-proto-model.git/info/refs?service=git-upload-pack"
	req, err := http.NewRequest(http.MethodGet, infoRefsURL, nil)
	if err != nil {
		t.Fatalf("Failed to build request: %v", err)
	}
	req.Header.Set("Git-Protocol", "version=99")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for unknown Git-Protocol, got %d", resp.StatusCode)
	}
}

// TestSSHGitProtocolVersions verifies that the server accepts both protocol
// version=1 and version=2 via the SSH GIT_PROTOCOL environment variable and
// that normal git operations complete successfully under each version.
func TestSSHGitProtocolVersions(t *testing.T) {
	clientDir, err := os.MkdirTemp("", "ssh-proto-e2e-client")
	if err != nil {
		t.Fatalf("Failed to create temp client dir: %v", err)
	}
	defer os.RemoveAll(clientDir)

	// No authorised keys → server runs with NoClientAuth = true.
	httpServer, sshListener, _ := setupSSHTestServer(t, nil)

	addr := sshListener.Addr().(*net.TCPAddr)
	port := strings.Split(addr.String(), ":")[1]
	sshURL := "ssh://git@" + addr.String() + "/"
	endpoint := httpServer.URL

	for _, version := range []string{"version=1", "version=2"} {
		version := version
		t.Run(version, func(t *testing.T) {
			suffix := strings.ReplaceAll(version, "=", "-")
			org := "ssh-proto-user"
			repoName := "ssh-proto-" + suffix + "-model"
			createRepo(t, endpoint, org, repoName)

			baseEnv := sshNoAuthGitEnv(port)
			env := append(baseEnv, "GIT_PROTOCOL="+version)

			t.Run("Clone", func(t *testing.T) {
				cloneDir := filepath.Join(clientDir, "clone-"+suffix)
				runGitWithEnv(t, "", env, "clone", sshURL+org+"/"+repoName+".git", cloneDir)
				if _, err := os.Stat(filepath.Join(cloneDir, ".git")); os.IsNotExist(err) {
					t.Error(".git directory not found after clone")
				}
			})

			t.Run("Push", func(t *testing.T) {
				workDir := filepath.Join(clientDir, "clone-"+suffix)
				runGitWithEnv(t, workDir, env, "config", "user.email", "test@test.com")
				runGitWithEnv(t, workDir, env, "config", "user.name", "Test User")

				if err := os.WriteFile(filepath.Join(workDir, "README.md"),
					[]byte("# "+version+"\n"), 0644); err != nil {
					t.Fatalf("Failed to create README: %v", err)
				}

				runGitWithEnv(t, workDir, env, "add", "README.md")
				runGitWithEnv(t, workDir, env, "commit", "-m", "initial commit")
				runGitWithEnv(t, workDir, env, "push", "origin", "main")
			})

			t.Run("Fetch", func(t *testing.T) {
				workDir := filepath.Join(clientDir, "clone-"+suffix)
				runGitWithEnv(t, workDir, env, "fetch", "origin")
			})

			t.Run("CloneWithContent", func(t *testing.T) {
				cloneDir := filepath.Join(clientDir, "clone2-"+suffix)
				runGitWithEnv(t, "", env, "clone", sshURL+org+"/"+repoName+".git", cloneDir)

				content, err := os.ReadFile(filepath.Join(cloneDir, "README.md"))
				if err != nil {
					t.Fatalf("Failed to read README.md: %v", err)
				}
				if string(content) != "# "+version+"\n" {
					t.Errorf("Unexpected content: %q", content)
				}
			})
		})
	}
}
