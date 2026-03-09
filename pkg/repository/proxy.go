package repository

import (
	"context"
	"os"
	"strings"
)

// ProxyFunc is a callback that initializes a new repository by creating a mirror
// from a proxy source. It is called when a repository is not found locally.
// If the proxy source is unavailable or the repository does not exist there,
// it should return ErrRepositoryNotExists.
type ProxyFunc func(ctx context.Context, repoPath, repoName string) (*Repository, error)

// NewProxyFunc creates a ProxyFunc that mirrors repositories from the given base URL.
// The source URL for each repository is computed as baseURL + "/" + repoName.
func NewProxyFunc(baseURL string) ProxyFunc {
	return func(ctx context.Context, repoPath, repoName string) (*Repository, error) {
		sourceURL := strings.TrimSuffix(baseURL, "/") + "/" + repoName

		repo, err := InitMirror(ctx, repoPath, sourceURL)
		if err != nil {
			_ = os.RemoveAll(repoPath)
			return nil, ErrRepositoryNotExists
		}

		if err := repo.SyncMirror(ctx); err != nil {
			_ = os.RemoveAll(repoPath)
			return nil, ErrRepositoryNotExists
		}

		return repo, nil
	}
}
