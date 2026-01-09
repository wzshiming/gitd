package repository

import (
	"bufio"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

var (
	ErrObjectNotFound = plumbing.ErrObjectNotFound
)

// TreeEntry represents a file or directory in the repository
type TreeEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"` // "blob" or "tree"
	Mode       string `json:"mode"`
	SHA        string `json:"sha"`
	IsLFS      bool   `json:"isLfs,omitempty"`
	BlobSha256 string `json:"blobSha256,omitempty"`
}

func (r *Repository) Tree(ref string, path string) ([]TreeEntry, error) {
	refObj, err := r.repo.Reference(plumbing.ReferenceName("refs/heads/"+ref), true)
	if err != nil {
		if err == plumbing.ErrReferenceNotFound {
			return []TreeEntry{}, nil
		}
		return nil, err
	}

	commit, err := r.repo.CommitObject(refObj.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get commit object: %w", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("failed to get tree object: %w", err)
	}

	if path != "" {
		entry, err := tree.FindEntry(path)
		if err != nil {
			return nil, fmt.Errorf("path not found: %w", err)
		}

		if entry.Mode.IsFile() {
			return nil, fmt.Errorf("path is not a directory")
		}

		tree, err = r.repo.TreeObject(entry.Hash)
		if err != nil {
			return nil, fmt.Errorf("failed to get subtree object: %w", err)
		}
	}

	var entries []TreeEntry
	for _, entry := range tree.Entries {
		if entry.Mode.IsFile() {
			entry := TreeEntry{
				Name: entry.Name,
				Path: filepath.Join(path, entry.Name),
				Type: "blob",
				Mode: formatMode(entry.Mode),
				SHA:  entry.Hash.String(),
			}

			hash := plumbing.NewHash(entry.SHA)
			blob, err := r.repo.BlobObject(hash)
			if err == nil {
				isLFS, sha256 := detectLFSPointer(blob)
				if isLFS {
					entry.IsLFS = true
					entry.BlobSha256 = sha256
				}
			}
			entries = append(entries, entry)
		} else {
			entries = append(entries, TreeEntry{
				Name: entry.Name,
				Path: filepath.Join(path, entry.Name),
				Type: "tree",
				Mode: formatMode(entry.Mode),
				SHA:  entry.Hash.String(),
			})
		}
	}
	return entries, nil
}

func formatMode(mode filemode.FileMode) string {
	switch mode {
	case filemode.Dir:
		return "dir"
	case filemode.Regular:
		return "regular"
	case filemode.Executable:
		return "executable"
	case filemode.Symlink:
		return "symlink"
	case filemode.Submodule:
		return "submodule"
	default:
		return fmt.Sprintf("unknown(%07o)", uint32(mode))
	}
}

const maxLFSPointerSize = 1024 // LFS pointers are typically < 200 bytes

// detectLFSPointer checks if a blob is a git-lfs pointer file and returns the SHA256 if it is
// LFS pointer files have a specific format:
// version https://git-lfs.github.com/spec/v1
// oid sha256:<hash>
// size <bytes>
func detectLFSPointer(blob *object.Blob) (bool, string) {
	// LFS pointers are small (typically < 200 bytes)
	if blob.Size > maxLFSPointerSize {
		return false, ""
	}

	reader, err := blob.Reader()
	if err != nil {
		return false, ""
	}
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	var isLFS bool
	var sha256 string

	for scanner.Scan() {
		line := scanner.Text()

		// Check for LFS version header
		if strings.HasPrefix(line, "version https://git-lfs.github.com/spec/v") {
			isLFS = true
		}

		// Extract SHA256 from oid line
		if strings.HasPrefix(line, "oid sha256:") {
			sha256 = strings.TrimPrefix(line, "oid sha256:")
		}
	}

	if err := scanner.Err(); err != nil {
		return false, ""
	}

	// Only return SHA256 if both version and oid are present
	if isLFS && sha256 != "" {
		return true, sha256
	}
	return false, ""
}
