package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePath(t *testing.T) {
	tests := []struct {
		name    string
		repoDir string
		urlPath string
		want    string
	}{
		{
			name:    "empty path",
			repoDir: "/repos",
			urlPath: "",
			want:    "",
		},
		{
			name:    "simple repo path",
			repoDir: "/repos",
			urlPath: "user/repo",
			want:    "/repos/user/repo.git",
		},
		{
			name:    "path with .git suffix",
			repoDir: "/repos",
			urlPath: "user/repo.git",
			want:    "/repos/user/repo.git",
		},
		{
			name:    "path with leading slash",
			repoDir: "/repos",
			urlPath: "/user/repo",
			want:    "/repos/user/repo.git",
		},
		{
			name:    "path traversal attempt",
			repoDir: "/repos",
			urlPath: "../etc/passwd",
			want:    "",
		},
		{
			name:    "path traversal with dotdot in middle",
			repoDir: "/repos",
			urlPath: "user/../../../etc/passwd",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolvePath(tt.repoDir, tt.urlPath)
			if got != tt.want {
				t.Errorf("ResolvePath(%q, %q) = %q, want %q", tt.repoDir, tt.urlPath, got, tt.want)
			}
		})
	}
}

func TestValidateRefName(t *testing.T) {
	tests := []struct {
		name    string
		refName string
		wantErr bool
	}{
		{name: "valid simple name", refName: "main", wantErr: false},
		{name: "valid with slash", refName: "feature/branch", wantErr: false},
		{name: "valid with hyphens", refName: "fix-123", wantErr: false},
		{name: "empty name", refName: "", wantErr: true},
		{name: "starts with slash", refName: "/branch", wantErr: true},
		{name: "ends with slash", refName: "branch/", wantErr: true},
		{name: "starts with dot", refName: ".hidden", wantErr: true},
		{name: "contains double dot", refName: "a..b", wantErr: true},
		{name: "ends with .lock", refName: "branch.lock", wantErr: true},
		{name: "contains space", refName: "my branch", wantErr: true},
		{name: "contains tilde", refName: "branch~1", wantErr: true},
		{name: "contains caret", refName: "branch^2", wantErr: true},
		{name: "contains colon", refName: "a:b", wantErr: true},
		{name: "contains question", refName: "a?b", wantErr: true},
		{name: "contains asterisk", refName: "a*b", wantErr: true},
		{name: "contains bracket", refName: "a[b", wantErr: true},
		{name: "contains backslash", refName: "a\\b", wantErr: true},
		{name: "contains @{", refName: "a@{b", wantErr: true},
		{name: "contains double slash", refName: "a//b", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRefName(tt.refName)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRefName(%q) error = %v, wantErr %v", tt.refName, err, tt.wantErr)
			}
		})
	}
}

func TestIsRepository(t *testing.T) {
	t.Run("not a repository", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "repo-test-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		if IsRepository(tmpDir) {
			t.Error("expected false for empty directory")
		}
	})

	t.Run("is a repository", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "repo-test-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		_, err = Init(tmpDir, "main")
		if err != nil {
			t.Fatal(err)
		}

		if !IsRepository(tmpDir) {
			t.Error("expected true for initialized repository")
		}
	})
}

func assertDefaultBranch(t *testing.T, repo *Repository, want string) {
	t.Helper()
	if branch := repo.DefaultBranch(); branch != want {
		t.Errorf("DefaultBranch() = %q, want %q", branch, want)
	}
}

func TestInitAndOpen(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	repoPath := filepath.Join(tmpDir, "test.git")

	// Init
	repo, err := Init(repoPath, "main")
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if repo == nil {
		t.Fatal("Init() returned nil repository")
	}

	assertDefaultBranch(t, repo, "main")

	// Open
	repo2, err := Open(repoPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if repo2 == nil {
		t.Fatal("Open() returned nil repository")
	}
	assertDefaultBranch(t, repo2, "main")
}

func TestOpenNonexistent(t *testing.T) {
	_, err := Open("/nonexistent/path")
	if err == nil {
		t.Error("Open() expected error for nonexistent path")
	}
}

func TestInitWithDifferentBranch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	repoPath := filepath.Join(tmpDir, "test.git")

	repo, err := Init(repoPath, "develop")
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	assertDefaultBranch(t, repo, "develop")
}

func TestBranchesAndTags(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	repoPath := filepath.Join(tmpDir, "test.git")
	repo, err := Init(repoPath, "main")
	if err != nil {
		t.Fatal(err)
	}

	// Create a commit so we can create branches/tags
	_, err = repo.CreateCommit(context.Background(), "main", "initial commit", "Test", "test@test.com", []CommitOperation{
		{Type: CommitOperationAdd, Path: "README.md", Content: []byte("# Test")},
	}, "")
	if err != nil {
		t.Fatalf("CreateCommit() error = %v", err)
	}

	// Test Branches
	branches, err := repo.Branches()
	if err != nil {
		t.Fatalf("Branches() error = %v", err)
	}
	if len(branches) != 1 || branches[0] != "main" {
		t.Errorf("Branches() = %v, want [main]", branches)
	}

	// Create branch
	err = repo.CreateBranch("feature", "main")
	if err != nil {
		t.Fatalf("CreateBranch() error = %v", err)
	}

	exists, err := repo.BranchExists("feature")
	if err != nil {
		t.Fatalf("BranchExists() error = %v", err)
	}
	if !exists {
		t.Error("BranchExists('feature') = false, want true")
	}

	exists, err = repo.BranchExists("nonexistent")
	if err != nil {
		t.Fatalf("BranchExists() error = %v", err)
	}
	if exists {
		t.Error("BranchExists('nonexistent') = true, want false")
	}

	// Delete branch
	err = repo.DeleteBranch("feature")
	if err != nil {
		t.Fatalf("DeleteBranch() error = %v", err)
	}

	exists, err = repo.BranchExists("feature")
	if err != nil {
		t.Fatalf("BranchExists() error = %v", err)
	}
	if exists {
		t.Error("BranchExists('feature') = true after delete")
	}

	// Create tag
	err = repo.CreateTag("v1.0", "main")
	if err != nil {
		t.Fatalf("CreateTag() error = %v", err)
	}

	exists, err = repo.TagExists("v1.0")
	if err != nil {
		t.Fatalf("TagExists() error = %v", err)
	}
	if !exists {
		t.Error("TagExists('v1.0') = false, want true")
	}

	// Tags list
	tags, err := repo.Tags()
	if err != nil {
		t.Fatalf("Tags() error = %v", err)
	}
	if len(tags) != 1 || tags[0] != "v1.0" {
		t.Errorf("Tags() = %v, want [v1.0]", tags)
	}

	// Delete tag
	err = repo.DeleteTag("v1.0")
	if err != nil {
		t.Fatalf("DeleteTag() error = %v", err)
	}

	exists, err = repo.TagExists("v1.0")
	if err != nil {
		t.Fatalf("TagExists() error = %v", err)
	}
	if exists {
		t.Error("TagExists('v1.0') = true after delete")
	}
}

func TestCreateBranchInvalidName(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	repoPath := filepath.Join(tmpDir, "test.git")
	repo, err := Init(repoPath, "main")
	if err != nil {
		t.Fatal(err)
	}

	// Create a commit first
	_, err = repo.CreateCommit(context.Background(), "main", "init", "Test", "test@test.com", []CommitOperation{
		{Type: CommitOperationAdd, Path: "README.md", Content: []byte("# Test")},
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	err = repo.CreateBranch("invalid..name", "main")
	if err == nil {
		t.Error("CreateBranch() expected error for invalid name")
	}
}

func TestCreateTagInvalidName(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	repoPath := filepath.Join(tmpDir, "test.git")
	repo, err := Init(repoPath, "main")
	if err != nil {
		t.Fatal(err)
	}

	// Create a commit first
	_, err = repo.CreateCommit(context.Background(), "main", "init", "Test", "test@test.com", []CommitOperation{
		{Type: CommitOperationAdd, Path: "README.md", Content: []byte("# Test")},
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	err = repo.CreateTag("invalid..name", "main")
	if err == nil {
		t.Error("CreateTag() expected error for invalid name")
	}
}

func TestMove(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	repoPath := filepath.Join(tmpDir, "test.git")
	repo, err := Init(repoPath, "main")
	if err != nil {
		t.Fatal(err)
	}

	newPath := filepath.Join(tmpDir, "moved.git")
	err = repo.Move(newPath)
	if err != nil {
		t.Fatalf("Move() error = %v", err)
	}

	if IsRepository(repoPath) {
		t.Error("old path should no longer be a repository")
	}
	if !IsRepository(newPath) {
		t.Error("new path should be a repository")
	}
}

func TestResolveRevision(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	repoPath := filepath.Join(tmpDir, "test.git")
	repo, err := Init(repoPath, "main")
	if err != nil {
		t.Fatal(err)
	}

	commitHash, err := repo.CreateCommit(context.Background(), "main", "initial", "Test", "test@test.com", []CommitOperation{
		{Type: CommitOperationAdd, Path: "README.md", Content: []byte("# Test")},
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	// Resolve by branch name
	resolved, err := repo.ResolveRevision("main")
	if err != nil {
		t.Fatalf("ResolveRevision() error = %v", err)
	}
	if resolved != commitHash {
		t.Errorf("ResolveRevision('main') = %q, want %q", resolved, commitHash)
	}

	// Resolve empty (should use default branch)
	resolved, err = repo.ResolveRevision("")
	if err != nil {
		t.Fatalf("ResolveRevision('') error = %v", err)
	}
	if resolved != commitHash {
		t.Errorf("ResolveRevision('') = %q, want %q", resolved, commitHash)
	}
}

func TestCreateCommitAndBlob(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	repoPath := filepath.Join(tmpDir, "test.git")
	repo, err := Init(repoPath, "main")
	if err != nil {
		t.Fatal(err)
	}

	content := []byte("Hello, World!")
	commitHash, err := repo.CreateCommit(context.Background(), "main", "add readme", "Test", "test@test.com", []CommitOperation{
		{Type: CommitOperationAdd, Path: "README.md", Content: content},
	}, "")
	if err != nil {
		t.Fatalf("CreateCommit() error = %v", err)
	}
	if commitHash == "" {
		t.Fatal("CreateCommit() returned empty hash")
	}

	// Read it back via Blob
	blob, err := repo.Blob("main", "README.md")
	if err != nil {
		t.Fatalf("Blob() error = %v", err)
	}
	if blob.Name() != "README.md" {
		t.Errorf("Blob.Name() = %q, want %q", blob.Name(), "README.md")
	}
	if blob.Size() != int64(len(content)) {
		t.Errorf("Blob.Size() = %d, want %d", blob.Size(), len(content))
	}

	reader, err := blob.NewReader()
	if err != nil {
		t.Fatalf("Blob.NewReader() error = %v", err)
	}
	defer reader.Close()

	data := make([]byte, blob.Size())
	_, err = reader.Read(data)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("Blob content = %q, want %q", string(data), string(content))
	}
}

func TestCreateCommitDelete(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	repoPath := filepath.Join(tmpDir, "test.git")
	repo, err := Init(repoPath, "main")
	if err != nil {
		t.Fatal(err)
	}

	// Create initial commit with a file
	hash1, err := repo.CreateCommit(context.Background(), "main", "add file", "Test", "test@test.com", []CommitOperation{
		{Type: CommitOperationAdd, Path: "file.txt", Content: []byte("content")},
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	// Delete the file
	_, err = repo.CreateCommit(context.Background(), "main", "delete file", "Test", "test@test.com", []CommitOperation{
		{Type: CommitOperationDelete, Path: "file.txt"},
	}, hash1)
	if err != nil {
		t.Fatalf("CreateCommit(delete) error = %v", err)
	}

	// Verify file is gone
	_, err = repo.Blob("main", "file.txt")
	if err == nil {
		t.Error("Blob() expected error for deleted file")
	}
}

func TestIsMirror(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	repoPath := filepath.Join(tmpDir, "test.git")
	repo, err := Init(repoPath, "main")
	if err != nil {
		t.Fatal(err)
	}

	isMirror, sourceURL, err := repo.IsMirror()
	if err != nil {
		t.Fatalf("IsMirror() error = %v", err)
	}
	if isMirror {
		t.Error("IsMirror() = true for normal repo, want false")
	}
	if sourceURL != "" {
		t.Errorf("IsMirror() sourceURL = %q, want empty", sourceURL)
	}
}

func TestSplitRevisionAndPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	repoPath := filepath.Join(tmpDir, "test.git")
	repo, err := Init(repoPath, "main")
	if err != nil {
		t.Fatal(err)
	}

	// Create a commit so branches exist
	_, err = repo.CreateCommit(context.Background(), "main", "init", "Test", "test@test.com", []CommitOperation{
		{Type: CommitOperationAdd, Path: "README.md", Content: []byte("# Test")},
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		refpath  string
		wantRef  string
		wantPath string
	}{
		{name: "empty", refpath: "", wantRef: "main", wantPath: ""},
		{name: "branch only", refpath: "main", wantRef: "main", wantPath: ""},
		{name: "branch and path", refpath: "main/file.txt", wantRef: "main", wantPath: "file.txt"},
		{name: "unknown branch", refpath: "unknown/file.txt", wantRef: "unknown", wantPath: "file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, path, err := repo.SplitRevisionAndPath(tt.refpath)
			if err != nil {
				t.Fatalf("SplitRevisionAndPath() error = %v", err)
			}
			if ref != tt.wantRef {
				t.Errorf("ref = %q, want %q", ref, tt.wantRef)
			}
			if path != tt.wantPath {
				t.Errorf("path = %q, want %q", path, tt.wantPath)
			}
		})
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"0", 0},
		{"1024", 1024},
		{"999999", 999999},
		{"123abc", 123},
		{"", 0},
		{"abc", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseSize(tt.input)
			if got != tt.want {
				t.Errorf("parseSize(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
