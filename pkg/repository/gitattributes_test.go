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
	// *.bin and *.safetensors should match as LFS
	if !attrs.IsLFS("test.bin") {
		t.Error("Expected *.bin to be LFS")
	}
	if !attrs.IsLFS("test.safetensors") {
		t.Error("Expected *.safetensors to be LFS")
	}
	// *.txt has -filter so should NOT be LFS
	if attrs.IsLFS("test.txt") {
		t.Error("Expected *.txt to not be LFS")
	}
}

func TestParseGitAttributesEmpty(t *testing.T) {
	attrs := ParseGitAttributes("")
	if attrs == nil {
		t.Fatal("Expected non-nil GitAttributes")
	}
	if attrs.IsLFS("anything") {
		t.Error("Expected empty GitAttributes to return false")
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
