package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wzshiming/hfd/internal/utils"
)

// InitMirror initializes a new bare git repository as a mirror of the remote repository at sourceURL.
func InitMirror(ctx context.Context, repoPath string, sourceURL string) (*Repository, error) {
	sourceURL = strings.TrimSuffix(sourceURL, "/")
	sourceURL = strings.TrimSuffix(sourceURL, ".git") + ".git"

	cmd := utils.Command(ctx, "git", "clone", "--mirror", "--bare", sourceURL, repoPath)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to initialize git repository: %w", err)
	}

	repo, err := Open(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open git repository: %w", err)
	}

	return repo, nil
}

func (r *Repository) SyncMirror(ctx context.Context) error {

	args := []string{
		"fetch",
		"--prune",
		"origin",
		"--progress",
	}

	if fi, err := os.Stat(filepath.Join(r.repoPath, "shallow")); err == nil && !fi.IsDir() {
		args = append(args, "--unshallow")
	}

	cmd := utils.Command(ctx, "git", args...)
	cmd.Dir = r.repoPath
	return cmd.Run()
}
