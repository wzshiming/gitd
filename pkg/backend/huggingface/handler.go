package huggingface

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/wzshiming/gitd/pkg/storage"
)

// Handler
type Handler struct {
	storage *storage.Storage

	root *mux.Router

	next http.Handler
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
	// HuggingFace-compatible API endpoints
	h.registryHuggingFace(h.root)

	h.root.NotFoundHandler = h.next
}

// withRepoPrefix returns a handler wrapper that prepends a storage prefix to the repo name in route vars.
// This allows datasets and spaces to be stored in separate directories from models.
func withRepoPrefix(prefix string, handler http.HandlerFunc) http.HandlerFunc {
	if prefix == "" {
		return handler
	}
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		vars["repo"] = prefix + "/" + vars["repo"]
		handler(w, r)
	}
}

// registerRepoTypeRoutes registers all repo-type-specific routes.
// apiPrefix is the API path prefix (e.g., "/api/models", "/api/datasets", "/api/spaces").
// resolvePrefix is the resolve path prefix (e.g., "" for models, "/datasets", "/spaces").
// storagePrefix is the directory prefix for storing repos (e.g., "" for models, "datasets", "spaces").
func (h *Handler) registerRepoTypeRoutes(r *mux.Router, apiPrefix string, resolvePrefix string, storagePrefix string) {
	wrap := func(handler http.HandlerFunc) http.HandlerFunc {
		return withRepoPrefix(storagePrefix, handler)
	}

	r.HandleFunc(apiPrefix+"/{repo:.+}/preupload/{revision:.*}", wrap(h.handleHFPreupload)).Methods(http.MethodPost)
	r.HandleFunc(apiPrefix+"/{repo:.+}/commit/{revision:.*}", wrap(h.handleHFCommit)).Methods(http.MethodPost)
	r.HandleFunc(apiPrefix+"/{repo:.+}/revision/{revision:.*}", wrap(h.handleHFModelInfoRevision)).Methods(http.MethodGet)
	r.HandleFunc(apiPrefix+"/{repo:.+}/tree/{refpath:.*}", wrap(h.handleHFTree)).Methods(http.MethodGet)
	r.HandleFunc(apiPrefix+"/{repo:.+}", wrap(h.handleHFModelInfo)).Methods(http.MethodGet)
	r.HandleFunc(resolvePrefix+"/{repo:.+}/resolve/{refpath:.*}", wrap(h.handleHFResolve)).Methods(http.MethodGet, http.MethodHead)
}

// registryHuggingFace registers the HuggingFace-compatible API endpoints.
// These endpoints allow using huggingface-cli and huggingface_hub library
// with HF_ENDPOINT pointing to this server.
func (h *Handler) registryHuggingFace(r *mux.Router) {
	// Repository creation endpoint - used by huggingface_hub to create repos
	r.HandleFunc("/api/repos/create", h.handleHFCreateRepo).Methods(http.MethodPost)

	// YAML validation endpoint - used by huggingface_hub to validate README YAML front matter
	r.HandleFunc("/api/validate-yaml", h.handleHFValidateYAML).Methods(http.MethodPost)

	// Register routes for datasets
	h.registerRepoTypeRoutes(r, "/api/datasets", "/datasets", "datasets")

	// Register routes for spaces
	h.registerRepoTypeRoutes(r, "/api/spaces", "/spaces", "spaces")

	// Register routes for models (no prefix for backward compatibility)
	h.registerRepoTypeRoutes(r, "/api/models", "", "")
}

func responseJSON(w http.ResponseWriter, data any, sc int) {
	header := w.Header()
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", "application/json; charset=utf-8")
	}

	if sc >= http.StatusBadRequest {
		header.Del("Content-Length")
		header.Set("X-Content-Type-Options", "nosniff")
	}

	if sc != 0 {
		w.WriteHeader(sc)
	}

	if data == nil {
		_, _ = w.Write([]byte("{}"))
		return
	}

	switch t := data.(type) {
	case error:
		var dataErr struct {
			Error string `json:"error"`
		}
		dataErr.Error = t.Error()
		data = dataErr
	case string:
		var dataErr struct {
			Error string `json:"error"`
		}
		dataErr.Error = t
		data = dataErr
	}

	enc := json.NewEncoder(w)
	_ = enc.Encode(data)
}
