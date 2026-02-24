package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/wzshiming/gitd/internal/utils"
	"github.com/wzshiming/gitd/pkg/repository"
)

// Signer is an alias for ssh.Signer to avoid requiring callers to import golang.org/x/crypto/ssh.
type Signer = ssh.Signer

// Server implements the SSH protocol (ssh://) server for git operations.
type Server struct {
	repositoriesDir string
	config          *ssh.ServerConfig
}

// NewServer creates a new SSH protocol server.
func NewServer(repositoriesDir string, hostKey ssh.Signer) *Server {
	config := &ssh.ServerConfig{
		NoClientAuth: true,
	}
	config.AddHostKey(hostKey)

	return &Server{
		repositoriesDir: repositoriesDir,
		config:          config,
	}
}

// GenerateHostKey generates an ED25519 host key for the SSH server.
func GenerateHostKey() (ssh.Signer, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating host key: %w", err)
	}
	return ssh.NewSignerFromKey(priv)
}

// GenerateAndSaveHostKey generates an ED25519 host key and saves it to the given file path
// with 0600 permissions.
func GenerateAndSaveHostKey(path string) (ssh.Signer, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating host key: %w", err)
	}

	derBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshaling host key: %w", err)
	}

	pemBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: derBytes,
	}

	if err := os.WriteFile(path, pem.EncodeToMemory(pemBlock), 0600); err != nil {
		return nil, fmt.Errorf("writing host key to %s: %w", path, err)
	}

	return ssh.NewSignerFromKey(priv)
}

// Serve accepts connections on the listener and handles them.
func (s *Server) Serve(listener net.Listener) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go s.handleConnection(conn)
	}
}

// ListenAndServe listens on the given address and serves SSH protocol requests.
func (s *Server) ListenAndServe(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	return s.Serve(listener)
}

// handleConnection handles a single SSH connection.
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	serverConn, chans, reqs, err := ssh.NewServerConn(conn, s.config)
	if err != nil {
		log.Printf("ssh protocol: handshake failed: %v\n", err)
		return
	}
	defer serverConn.Close()

	// Discard global requests
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			log.Printf("ssh protocol: could not accept channel: %v\n", err)
			return
		}

		go s.handleSession(channel, requests)
	}
}

// handleSession handles an SSH session channel.
func (s *Server) handleSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()

	for req := range requests {
		switch req.Type {
		case "exec":
			if len(req.Payload) < 4 {
				_ = req.Reply(false, nil)
				continue
			}

			// Payload format: uint32 length + string
			cmdLen := int(req.Payload[0])<<24 | int(req.Payload[1])<<16 | int(req.Payload[2])<<8 | int(req.Payload[3])
			if cmdLen+4 > len(req.Payload) {
				_ = req.Reply(false, nil)
				continue
			}
			cmdLine := string(req.Payload[4 : 4+cmdLen])

			service, repoPath, err := parseCommand(cmdLine)
			if err != nil {
				log.Printf("ssh protocol: invalid command: %v\n", err)
				_ = req.Reply(false, nil)
				continue
			}

			_ = req.Reply(true, nil)
			s.executeCommand(channel, service, repoPath)
			return

		default:
			// Reject unknown request types
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}
}

// executeCommand runs a git service command and pipes I/O through the SSH channel.
func (s *Server) executeCommand(channel ssh.Channel, service string, repoPath string) {
	defer channel.Close()

	fullPath := s.resolveRepoPath(repoPath)
	if fullPath == "" || !repository.IsRepository(fullPath) {
		log.Printf("ssh protocol: repository not found: %s\n", repoPath)
		sendExitStatus(channel, 1)
		return
	}

	if service == "git-receive-pack" {
		repo, err := repository.Open(fullPath)
		if err != nil {
			log.Printf("ssh protocol: failed to open repository: %v\n", err)
			sendExitStatus(channel, 1)
			return
		}
		isMirror, _, err := repo.IsMirror()
		if err != nil {
			log.Printf("ssh protocol: failed to check repository type: %v\n", err)
			sendExitStatus(channel, 1)
			return
		}
		if isMirror {
			log.Printf("ssh protocol: push to mirror repository %q is not allowed\n", repoPath)
			sendExitStatus(channel, 1)
			return
		}
	}

	ctx := context.Background()
	cmd := utils.Command(ctx, service, fullPath)
	cmd.Stdin = channel
	cmd.Stdout = channel
	cmd.Stderr = channel.Stderr()

	if err := cmd.Run(); err != nil {
		log.Printf("ssh protocol: command %s failed: %v\n", service, err)
		sendExitStatus(channel, 1)
		return
	}

	sendExitStatus(channel, 0)
}

// parseCommand parses an SSH exec command like "git-upload-pack '/repo.git'" or
// "git-upload-pack /repo.git".
func parseCommand(cmdLine string) (service string, repoPath string, err error) {
	parts := strings.SplitN(strings.TrimSpace(cmdLine), " ", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid command format: %q", cmdLine)
	}

	service = parts[0]
	if service != "git-upload-pack" && service != "git-receive-pack" {
		return "", "", fmt.Errorf("unsupported service: %s", service)
	}

	repoPath = parts[1]
	// Remove surrounding single quotes if present (git client sends 'path')
	repoPath = strings.Trim(repoPath, "'")

	return service, repoPath, nil
}

// sendExitStatus sends the exit status to the SSH client.
func sendExitStatus(channel ssh.Channel, status uint32) {
	payload := []byte{
		byte(status >> 24),
		byte(status >> 16),
		byte(status >> 8),
		byte(status),
	}
	_, _ = channel.SendRequest("exit-status", false, payload)
}

func (s *Server) resolveRepoPath(urlPath string) string {
	urlPath = strings.TrimPrefix(urlPath, "/")
	if urlPath == "" {
		return ""
	}

	if !strings.HasSuffix(urlPath, ".git") {
		urlPath += ".git"
	}

	fullPath := filepath.Join(s.repositoriesDir, urlPath)
	fullPath = filepath.Clean(fullPath)

	// Prevent path traversal outside the repositories directory
	rel, err := filepath.Rel(s.repositoriesDir, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}

	return fullPath
}

// ParseHostKeyFile reads a PEM-encoded private key file and returns an SSH signer.
func ParseHostKeyFile(data []byte) (ssh.Signer, error) {
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("parsing host key: %w", err)
	}
	return signer, nil
}
