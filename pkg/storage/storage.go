package storage

import (
	"path/filepath"

	"github.com/wzshiming/hfd/pkg/lfs"
)

type Storage struct {
	rootDir         string
	repositoriesDir string

	lfsStore   lfs.Store
	locksStore *lfs.LockDB
}

type Option func(*Storage)

func WithRootDir(rootDir string) Option {
	return func(h *Storage) {
		h.rootDir = rootDir
	}
}

// WithLFSStore configures the LFS storage backend.
func WithLFSStore(store lfs.Store) Option {
	return func(h *Storage) {
		h.lfsStore = store
	}
}

// NewStorage creates a new Storage with the given options.
func NewStorage(opts ...Option) *Storage {
	h := &Storage{
		rootDir: "./data",
	}

	for _, opt := range opts {
		opt(h)
	}

	h.locksStore = lfs.NewLock()
	if h.lfsStore == nil {
		h.lfsStore = lfs.NewContent(filepath.Join(h.rootDir, "lfs"))
	}

	h.repositoriesDir = filepath.Join(h.rootDir, "repositories")

	return h
}

func (s *Storage) RootDir() string {
	return s.rootDir
}

func (s *Storage) RepositoriesDir() string {
	return s.repositoriesDir
}

func (s *Storage) LFSStore() lfs.Store {
	return s.lfsStore
}

func (s *Storage) LocksStore() *lfs.LockDB {
	return s.locksStore
}
