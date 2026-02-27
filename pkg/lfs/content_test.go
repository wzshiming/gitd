package lfs

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContentPutAndGet(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lfs-content-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewContent(tmpDir)

	// Create test data
	data := []byte("Hello, LFS Content Store!")
	hash := sha256.Sum256(data)
	oid := hex.EncodeToString(hash[:])
	size := int64(len(data))

	// Put
	err = store.Put(oid, strings.NewReader(string(data)), size)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	// Exists
	if !store.Exists(oid) {
		t.Error("Exists() = false after Put")
	}

	// Get
	reader, info, err := store.Get(oid)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer reader.Close()

	if info.Size() != size {
		t.Errorf("Get() size = %d, want %d", info.Size(), size)
	}

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("Get() content = %q, want %q", string(got), string(data))
	}

	// Info
	fi, err := store.Info(oid)
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	if fi.Size() != size {
		t.Errorf("Info() size = %d, want %d", fi.Size(), size)
	}
}

func TestContentExistsNotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lfs-content-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewContent(tmpDir)
	if store.Exists("nonexistent") {
		t.Error("Exists() = true for nonexistent oid")
	}
}

func TestContentGetNotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lfs-content-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewContent(tmpDir)
	_, _, err = store.Get("nonexistent")
	if err == nil {
		t.Error("Get() expected error for nonexistent oid")
	}
}

func TestContentPutHashMismatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lfs-content-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewContent(tmpDir)
	data := []byte("some data")
	wrongOid := "0000000000000000000000000000000000000000000000000000000000000000"

	err = store.Put(wrongOid, strings.NewReader(string(data)), int64(len(data)))
	if err == nil {
		t.Error("Put() expected error for hash mismatch")
	}
}

func TestContentPutSizeMismatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "lfs-content-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewContent(tmpDir)
	data := []byte("some data")
	hash := sha256.Sum256(data)
	oid := hex.EncodeToString(hash[:])

	err = store.Put(oid, strings.NewReader(string(data)), int64(len(data)+100))
	if err == nil {
		t.Error("Put() expected error for size mismatch")
	}
}

func TestTransformKey(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"abcde", filepath.Join("ab", "cd", "e")},
		{"ab", "ab"},
		{"abcd", "abcd"},
		{"abcdefgh", filepath.Join("ab", "cd", "efgh")},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := transformKey(tt.key)
			// Normalize path separators for cross-platform compatibility
			if got != tt.want {
				t.Errorf("transformKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}
