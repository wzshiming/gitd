package repository

import (
	"testing"
)

func TestParseGitAttributes(t *testing.T) {
	content := `# This is a comment
*.bin filter=lfs diff=lfs merge=lfs -text
*.safetensors filter=lfs diff=lfs merge=lfs -text
*.txt -filter
`
	attrs := ParseGitAttributes(content)
	if attrs == nil {
		t.Fatal("Expected non-nil GitAttributes")
	}
	if len(attrs.patterns) != 3 {
		t.Fatalf("Expected 3 patterns (2 LFS + 1 unset), got %d", len(attrs.patterns))
	}
	if !attrs.patterns[0].isLFS || !attrs.patterns[1].isLFS {
		t.Error("Expected first two patterns to be LFS")
	}
	if attrs.patterns[2].isLFS {
		t.Error("Expected third pattern to not be LFS")
	}
}

func TestParseGitAttributesEmpty(t *testing.T) {
	attrs := ParseGitAttributes("")
	if attrs == nil {
		t.Fatal("Expected non-nil GitAttributes")
	}
	if len(attrs.patterns) != 0 {
		t.Fatalf("Expected 0 patterns, got %d", len(attrs.patterns))
	}
}

func TestGitAttributesIsLFS(t *testing.T) {
	content := `*.bin filter=lfs diff=lfs merge=lfs -text
*.safetensors filter=lfs diff=lfs merge=lfs -text
*.gguf filter=lfs diff=lfs merge=lfs -text
`
	attrs := ParseGitAttributes(content)

	tests := []struct {
		path     string
		expected bool
	}{
		{"model.bin", true},
		{"weights.safetensors", true},
		{"model.gguf", true},
		{"README.md", false},
		{"config.json", false},
		{"subdir/model.bin", true},
		{"deep/nested/weights.safetensors", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := attrs.IsLFS(tt.path)
			if got != tt.expected {
				t.Errorf("IsLFS(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestGitAttributesIsLFSNegate(t *testing.T) {
	content := `*.bin filter=lfs diff=lfs merge=lfs -text
small.bin -filter -diff -merge text
`
	attrs := ParseGitAttributes(content)

	tests := []struct {
		path     string
		expected bool
	}{
		{"model.bin", true},
		{"small.bin", false},
		{"other.bin", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := attrs.IsLFS(tt.path)
			if got != tt.expected {
				t.Errorf("IsLFS(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestGitAttributesIsLFSWithDirPattern(t *testing.T) {
	content := `models/*.bin filter=lfs diff=lfs merge=lfs -text
**/data/*.csv filter=lfs diff=lfs merge=lfs -text
`
	attrs := ParseGitAttributes(content)

	tests := []struct {
		path     string
		expected bool
	}{
		{"models/model.bin", true},
		{"model.bin", false},
		{"other/model.bin", false},
		{"data/train.csv", true},
		{"deep/data/train.csv", true},
		{"train.csv", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := attrs.IsLFS(tt.path)
			if got != tt.expected {
				t.Errorf("IsLFS(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestGitAttributesNil(t *testing.T) {
	var attrs *GitAttributes
	if attrs.IsLFS("model.bin") {
		t.Error("Expected nil GitAttributes to return false")
	}
}

func TestMatchGitPattern(t *testing.T) {
	tests := []struct {
		pattern  string
		path     string
		expected bool
	}{
		// Simple extension patterns
		{"*.bin", "model.bin", true},
		{"*.bin", "README.md", false},
		{"*.bin", "dir/model.bin", true},
		// Directory patterns
		{"models/*.bin", "models/model.bin", true},
		{"models/*.bin", "model.bin", false},
		{"models/*.bin", "other/model.bin", false},
		// Double-star patterns
		{"**/*.bin", "model.bin", true},
		{"**/*.bin", "dir/model.bin", true},
		{"**/*.bin", "a/b/c/model.bin", true},
		// Exact filename patterns
		{"model.bin", "model.bin", true},
		{"model.bin", "dir/model.bin", true},
		{"model.bin", "other.bin", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.path, func(t *testing.T) {
			got := matchGitPattern(tt.pattern, tt.path)
			if got != tt.expected {
				t.Errorf("matchGitPattern(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.expected)
			}
		})
	}
}
