package repository

import (
	"fmt"
	"io"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
)

// Blob represents a file in the repository at a specific revision.
type Blob struct {
	name      string
	size      int64
	modTime   time.Time
	newReader func() (io.ReadCloser, error)
	hash      string
}

// Name returns the file name of the blob.
func (b *Blob) Name() string {
	return b.name
}

// Size returns the size of the blob in bytes.
func (b *Blob) Size() int64 {
	return b.size
}

// ModTime returns the last modification time of the blob, which is typically the commit time of the commit that introduced or last modified the file.
func (b *Blob) ModTime() (t time.Time) {
	return b.modTime
}

// NewReader returns a new reader for the blob's content.
func (b *Blob) NewReader() (io.ReadCloser, error) {
	return b.newReader()
}

// Hash returns the Git object hash of the blob content.
func (b *Blob) Hash() string {
	return b.hash
}

// serveBlob serves the content of the blob over HTTP, handling LFS pointers if necessary.
func (r *Repository) Blob(rev string, path string) (b *Blob, err error) {
	if rev == "" {
		rev = r.DefaultBranch()
	}

	hash, err := r.repo.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve revision: %w", err)
	}

	commit, err := r.repo.CommitObject(*hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit object: %w", err)
	}

	file, err := commit.File(path)
	if err != nil {
		return nil, fmt.Errorf("file not found in tree: %w", err)
	}

	return &Blob{
		name:      file.Name,
		size:      file.Size,
		modTime:   commit.Committer.When,
		newReader: file.Reader,
		hash:      file.Hash.String(),
	}, nil
}
