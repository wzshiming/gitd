package repository

import (
	"io"
	"path"
	"strings"
)

// GitAttributes represents parsed .gitattributes content and provides
// methods to check if a file path matches LFS filter patterns.
type GitAttributes struct {
	patterns []gitAttributePattern
}

type gitAttributePattern struct {
	pattern string
	isLFS   bool
}

// ParseGitAttributes parses .gitattributes content and extracts LFS-related patterns.
func ParseGitAttributes(content string) *GitAttributes {
	var patterns []gitAttributePattern
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		pattern := fields[0]

		isLFS := false
		unsetLFS := false
		for _, attr := range fields[1:] {
			switch attr {
			case "filter=lfs":
				isLFS = true
			case "-filter", "!filter", "filter":
				unsetLFS = true
			}
		}

		if isLFS || unsetLFS {
			patterns = append(patterns, gitAttributePattern{
				pattern: pattern,
				isLFS:   isLFS && !unsetLFS,
			})
		}
	}
	return &GitAttributes{patterns: patterns}
}

// IsLFS returns true if the given file path matches an LFS filter pattern
// defined in the .gitattributes file.
func (g *GitAttributes) IsLFS(filePath string) bool {
	if g == nil || len(g.patterns) == 0 {
		return false
	}
	isLFS := false
	for _, p := range g.patterns {
		if matchGitPattern(p.pattern, filePath) {
			isLFS = p.isLFS
		}
	}
	return isLFS
}

// matchGitPattern matches a .gitattributes pattern against a file path.
// Patterns without '/' are matched against the filename only.
// Patterns with '/' are matched against the full path.
// The '**/' prefix matches any directory level.
func matchGitPattern(pattern, filePath string) bool {
	if strings.HasPrefix(pattern, "**/") {
		// Match against any directory level
		subPattern := pattern[3:]
		if matchSimple(subPattern, filePath) {
			return true
		}
		for i := 0; i < len(filePath); i++ {
			if filePath[i] == '/' {
				if matchSimple(subPattern, filePath[i+1:]) {
					return true
				}
			}
		}
		return false
	}

	if !strings.Contains(pattern, "/") {
		// No slash: match against filename only
		return matchSimple(pattern, path.Base(filePath))
	}

	// Has slash: match against full path
	return matchSimple(pattern, filePath)
}

// matchSimple wraps path.Match, returning false on error.
func matchSimple(pattern, name string) bool {
	matched, _ := path.Match(pattern, name)
	return matched
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

	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, nil
	}
	return ParseGitAttributes(string(content)), nil
}
