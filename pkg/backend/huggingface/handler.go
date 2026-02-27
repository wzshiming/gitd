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

// repoStorageName returns the storage name for a repository based on the request.
// For datasets and spaces, the repo type from the URL is prepended as a storage directory prefix.
func repoStorageName(r *http.Request) string {
	vars := mux.Vars(r)
	repo := vars["repo"]
	switch vars["repoType"] {
	case "datasets", "spaces":
		return vars["repoType"] + "/" + repo
	default:
		return repo
	}
}

// registryHuggingFace registers the HuggingFace-compatible API endpoints.
// These endpoints allow using huggingface-cli and huggingface_hub library
// with HF_ENDPOINT pointing to this server.
func (h *Handler) registryHuggingFace(r *mux.Router) {
	// Repository management endpoints - used by huggingface_hub for repo CRUD
	r.HandleFunc("/api/repos/create", h.handleHFCreateRepo).Methods(http.MethodPost)
	r.HandleFunc("/api/repos/delete", h.handleHFDeleteRepo).Methods(http.MethodDelete)
	r.HandleFunc("/api/repos/move", h.handleHFMoveRepo).Methods(http.MethodPost)

	// YAML validation endpoint - used by huggingface_hub to validate README YAML front matter
	r.HandleFunc("/api/validate-yaml", h.handleHFValidateYAML).Methods(http.MethodPost)

	// Repository settings, branch, tag, and refs endpoints
	// These must be registered before the generic model info catch-all route.
	r.HandleFunc("/api/{repoType:models|datasets|spaces}/{repo:.+}/settings", h.handleHFRepoSettings).Methods(http.MethodPut)
	r.HandleFunc("/api/{repoType:models|datasets|spaces}/{repo:.+}/branch/{branch:.*}", h.handleHFCreateBranch).Methods(http.MethodPost)
	r.HandleFunc("/api/{repoType:models|datasets|spaces}/{repo:.+}/branch/{branch:.*}", h.handleHFDeleteBranch).Methods(http.MethodDelete)
	r.HandleFunc("/api/{repoType:models|datasets|spaces}/{repo:.+}/tag/{tag:.*}", h.handleHFCreateTag).Methods(http.MethodPost)
	r.HandleFunc("/api/{repoType:models|datasets|spaces}/{repo:.+}/tag/{tag:.*}", h.handleHFDeleteTag).Methods(http.MethodDelete)
	r.HandleFunc("/api/{repoType:models|datasets|spaces}/{repo:.+}/refs", h.handleHFListRefs).Methods(http.MethodGet)

	// API endpoints for all repo types (models, datasets, spaces)
	r.HandleFunc("/api/{repoType:models|datasets|spaces}/{repo:.+}/preupload/{revision:.*}", h.handleHFPreupload).Methods(http.MethodPost)
	r.HandleFunc("/api/{repoType:models|datasets|spaces}/{repo:.+}/commit/{revision:.*}", h.handleHFCommit).Methods(http.MethodPost)
	r.HandleFunc("/api/{repoType:models|datasets|spaces}/{repo:.+}/revision/{revision:.*}", h.handleHFModelInfoRevision).Methods(http.MethodGet)
	r.HandleFunc("/api/{repoType:models|datasets|spaces}/{repo:.+}/tree/{refpath:.*}", h.handleHFTree).Methods(http.MethodGet)
	r.HandleFunc("/api/{repoType:models|datasets|spaces}/{repo:.+}", h.handleHFModelInfo).Methods(http.MethodGet)

	// File download endpoints - datasets and spaces use a type prefix, models use the root
	r.HandleFunc("/{repoType:datasets|spaces}/{repo:.+}/resolve/{refpath:.*}", h.handleHFResolve).Methods(http.MethodGet, http.MethodHead)
	r.HandleFunc("/{repo:.+}/resolve/{refpath:.*}", h.handleHFResolve).Methods(http.MethodGet, http.MethodHead)
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
