package backend

import (
	"io"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/wzshiming/gitd/pkg/repository"
	"github.com/wzshiming/gitd/pkg/storage"
)

// Handler
type Handler struct {
	storage *storage.Storage

	root *mux.Router

	next http.Handler

	proxyManager *repository.ProxyManager
}

type Option func(*Handler)

func WithStorage(storage *storage.Storage) Option {
	return func(h *Handler) {
		h.storage = storage
	}
}

// WithNext sets the next http.Handler to call if the request is not handled by this handler.
func WithNext(next http.Handler) Option {
	return func(h *Handler) {
		h.next = next
	}
}

// WithProxyManager sets the repository proxy manager for transparent upstream repository fetching.
func WithProxyManager(pm *repository.ProxyManager) Option {
	return func(h *Handler) {
		h.proxyManager = pm
	}
}

// NewHandler creates a new Handler with the given repository directory.
func NewHandler(opts ...Option) *Handler {
	h := &Handler{
		root: mux.NewRouter(),
	}

	for _, opt := range opts {
		opt(h)
	}

	h.register()
	return h
}

// ServeHTTP implements the http.Handler interface.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.root.ServeHTTP(w, r)
}

func (h *Handler) register() {
	// Git protocol endpoints
	h.registryGit(h.root)

	h.root.NotFoundHandler = h.next
}

func responseText(w http.ResponseWriter, text string, sc int) {
	header := w.Header()
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", "text/plain; charset=utf-8")
	}

	if sc >= http.StatusBadRequest {
		header.Del("Content-Length")
		header.Set("X-Content-Type-Options", "nosniff")
	}

	if sc != 0 {
		w.WriteHeader(sc)
	}

	if text == "" {
		return
	}

	_, _ = io.WriteString(w, text)
}
