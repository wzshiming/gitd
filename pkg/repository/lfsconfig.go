package repository

import (
	"context"
	"fmt"
	"io"
)

// lfsConfigContent returns the expected .lfsconfig file content for the given LFS href.
func lfsConfigContent(lfsHref string) string {
	return fmt.Sprintf("[lfs]\n\turl = %s\n", lfsHref)
}

// EnsureLFSConfig ensures the repository has a .lfsconfig file with the given
// lfs.url value. If .lfsconfig already exists with the correct URL, this is a
// no-op. If the repository has no commits, this is a no-op.
// This is needed for git:// protocol clones where git-lfs cannot discover the
// LFS server URL automatically from the remote URL.
func (r *Repository) EnsureLFSConfig(ctx context.Context, lfsHref string) error {
	// Check if the repository has any commits
	_, err := r.repo.Head()
	if err != nil {
		return nil // No commits yet, nothing to do
	}

	expected := lfsConfigContent(lfsHref)

	// Check if .lfsconfig already exists with the correct content
	blob, err := r.Blob("", ".lfsconfig")
	if err == nil {
		reader, err := blob.NewReader()
		if err == nil {
			content, readErr := io.ReadAll(reader)
			_ = reader.Close()
			if readErr == nil && string(content) == expected {
				return nil // Already configured correctly
			}
		}
	}

	// Create commit with .lfsconfig
	_, err = r.CreateCommit(ctx, "", "Configure LFS URL", "hfd", "hfd@local",
		[]CommitOperation{{Type: CommitOperationAdd, Path: ".lfsconfig", Content: []byte(expected)}}, "")
	return err
}
