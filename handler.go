package gitd

import (
	"net/http"
	"path/filepath"
	"sync"

	"github.com/gorilla/context"
	"github.com/gorilla/mux"
)

type Authenticator interface {
	Authenticate(user, password string) (string, bool)
}

// LazyMirrorSourceFunc is a function that maps a repository name to an upstream URL.
// If the function returns an empty string, the repository will not be lazily mirrored.
type LazyMirrorSourceFunc func(repoName string) string

// Handler
type Handler struct {
	rootDir string

	authenticate Authenticator

	// lazyMirrorSource is used to determine the upstream URL for lazy mirroring.
	// When a repository is requested but doesn't exist locally, and this function
	// returns a non-empty URL, the repository will be created as a mirror of that URL.
	lazyMirrorSource LazyMirrorSourceFunc

	// importStatus tracks the status of ongoing import operations
	importStatus   map[string]*ImportStatus
	importStatusMu sync.RWMutex

	locksStore   *lfsLockDB
	contentStore *lfsContent
	root         *mux.Router
}

// ImportStatus represents the status of an import operation
type ImportStatus struct {
	Status string `json:"status"` // "pending", "in_progress", "completed", "failed"
	Step   string `json:"step"`   // Current step description
	Error  string `json:"error"`  // Error message if failed
}

type Option func(*Handler)

func WithAuthenticate(auth Authenticator) Option {
	return func(h *Handler) {
		h.authenticate = auth
	}
}

func WithRootDir(rootDir string) Option {
	return func(h *Handler) {
		h.rootDir = rootDir
	}
}

// WithLazyMirrorSource sets a function that determines the upstream URL for lazy mirroring.
// When a git operation is requested for a repository that doesn't exist locally,
// this function is called with the repository name. If it returns a non-empty URL,
// the repository is automatically created as a mirror of that upstream URL.
// This enables "pull-through" caching behavior similar to Google's Goblet.
func WithLazyMirrorSource(fn LazyMirrorSourceFunc) Option {
	return func(h *Handler) {
		h.lazyMirrorSource = fn
	}
}

// NewHandler creates a new Handler with the given repository directory.
func NewHandler(opts ...Option) *Handler {
	h := &Handler{
		rootDir:      "./data",
		importStatus: make(map[string]*ImportStatus),
	}

	for _, opt := range opts {
		opt(h)
	}

	h.locksStore = newLFSLock(filepath.Join(h.rootDir, "lfs", "locks.db"))
	h.contentStore = &lfsContent{basePath: filepath.Join(h.rootDir, "lfs")}
	h.root = h.router()
	return h
}

// ServeHTTP implements the http.Handler interface.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.root.ServeHTTP(w, r)
}

func (h *Handler) router() *mux.Router {
	r := mux.NewRouter()

	// Git protocol endpoints
	h.registryGit(r)

	// Git LFS protocol endpoints
	h.registryLFS(r)
	h.registryLFSLock(r)

	// Repository management endpoints
	h.registryRepositoriesImport(r)
	h.registryRepositoriesInfo(r)
	h.registryRepositories(r)
	return r
}

func (h *Handler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.authenticate == nil {
			context.Set(r, "USER", "anonymous")
			next(w, r)
			return
		}

		user, password, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="gitd"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		name, ret := h.authenticate.Authenticate(user, password)
		if !ret {
			w.Header().Set("WWW-Authenticate", `Basic realm="gitd"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		context.Set(r, "USER", name)
		next(w, r)
	}
}
