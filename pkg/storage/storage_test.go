package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewStorageDefaults(t *testing.T) {
	s := NewStorage()

	if s.RootDir() != "./data" {
		t.Errorf("RootDir() = %q, want %q", s.RootDir(), "./data")
	}
	if s.RepositoriesDir() != filepath.Join("./data", "repositories") {
		t.Errorf("RepositoriesDir() = %q, want %q", s.RepositoriesDir(), filepath.Join("./data", "repositories"))
	}
	if s.ContentStore() == nil {
		t.Error("ContentStore() should not be nil")
	}
	if s.LocksStore() == nil {
		t.Error("LocksStore() should not be nil")
	}
	if s.S3Store() != nil {
		t.Error("S3Store() should be nil by default")
	}
}

func TestNewStorageWithRootDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	s := NewStorage(WithRootDir(tmpDir))

	if s.RootDir() != tmpDir {
		t.Errorf("RootDir() = %q, want %q", s.RootDir(), tmpDir)
	}
	if s.RepositoriesDir() != filepath.Join(tmpDir, "repositories") {
		t.Errorf("RepositoriesDir() = %q, want %q", s.RepositoriesDir(), filepath.Join(tmpDir, "repositories"))
	}
}

func TestNewStorageMultipleOptions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	s := NewStorage(WithRootDir(tmpDir))

	if s.RootDir() != tmpDir {
		t.Errorf("RootDir() = %q, want %q", s.RootDir(), tmpDir)
	}
	if s.ContentStore() == nil {
		t.Error("ContentStore() should not be nil")
	}
	if s.LocksStore() == nil {
		t.Error("LocksStore() should not be nil")
	}
}
