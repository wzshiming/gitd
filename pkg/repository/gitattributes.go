package repository

import (
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitattributes"
)

// GitAttributes represents parsed .gitattributes content and provides
// methods to check if a file path matches LFS filter patterns.
type GitAttributes struct {
	attrs []gitattributes.MatchAttribute
}

// IsLFS returns true if the given file path matches an LFS filter pattern
// defined in the .gitattributes file.
func (g *GitAttributes) IsLFS(filePath string) bool {
	if g == nil || len(g.attrs) == 0 {
		return false
	}
	path := strings.Split(filePath, "/")
	// Iterate in reverse so that the last matching pattern wins (highest priority).
	for i := len(g.attrs) - 1; i >= 0; i-- {
		a := g.attrs[i]
		if a.Pattern == nil || !a.Pattern.Match(path) {
			continue
		}
		for _, attr := range a.Attributes {
			if attr.Name() == "filter" {
				return attr.IsValueSet() && attr.Value() == "lfs"
			}
		}
	}
	return false
}

// GitAttributes reads and parses the .gitattributes file from the repository
// at the given revision. Returns nil (not an error) if the file does not exist.
func (r *Repository) GitAttributes(ref string) (*GitAttributes, error) {
	blob, err := r.Blob(ref, ".gitattributes")
	if err != nil {
		return nil, nil
	}
	reader, err := blob.NewReader()
	if err != nil {
		return nil, nil
	}
	defer reader.Close()

	attrs, err := gitattributes.ReadAttributes(reader, nil, true)
	if err != nil {
		return nil, nil
	}
	return &GitAttributes{attrs: attrs}, nil
}
