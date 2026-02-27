package handlers

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompressHandlerGzip(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Hello, World!"))
	})

	handler := CompressHandler(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("Content-Encoding = %q, want %q", w.Header().Get("Content-Encoding"), "gzip")
	}

	reader, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	defer reader.Close()

	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(body) != "Hello, World!" {
		t.Errorf("body = %q, want %q", string(body), "Hello, World!")
	}
}

func TestCompressHandlerNoEncoding(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello, World!"))
	})

	handler := CompressHandler(inner)
	req := httptest.NewRequest("GET", "/", nil)
	// No Accept-Encoding header

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("Content-Encoding") != "" {
		t.Errorf("Content-Encoding = %q, want empty", w.Header().Get("Content-Encoding"))
	}
	if w.Body.String() != "Hello, World!" {
		t.Errorf("body = %q, want %q", w.Body.String(), "Hello, World!")
	}
}

func TestCompressHandlerDeflate(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello, World!"))
	})

	handler := CompressHandler(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "deflate")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("Content-Encoding") != "deflate" {
		t.Errorf("Content-Encoding = %q, want %q", w.Header().Get("Content-Encoding"), "deflate")
	}
}

func TestCompressHandlerVaryHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("test"))
	})

	handler := CompressHandler(inner)
	req := httptest.NewRequest("GET", "/", nil)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	vary := w.Header().Get("Vary")
	if !strings.Contains(vary, "Accept-Encoding") {
		t.Errorf("Vary header = %q, should contain Accept-Encoding", vary)
	}
}

func TestCompressHandlerGzipRequestBody(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		w.Write(body)
	})

	handler := CompressHandler(inner)

	// Create gzip-compressed request body
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write([]byte("compressed input"))
	gw.Close()

	req := httptest.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Encoding", "gzip")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "compressed input" {
		t.Errorf("body = %q, want %q", w.Body.String(), "compressed input")
	}
}

func TestCompressHandlerUnsupportedContentEncoding(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("should not reach here"))
	})

	handler := CompressHandler(inner)
	req := httptest.NewRequest("POST", "/", strings.NewReader("data"))
	req.Header.Set("Content-Encoding", "br")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCompressHandlerUpgradeBypass(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("upgrade response"))
	})

	handler := CompressHandler(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Upgrade", "websocket")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// When Upgrade is set, compression should be skipped
	if w.Header().Get("Content-Encoding") == "gzip" {
		t.Error("should not compress when Upgrade header is present")
	}
}

func TestCompressHandlerLevelInvalid(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("test"))
	})

	// Invalid level should default to DefaultCompression (no panic)
	handler := CompressHandlerLevel(inner, -3)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("Content-Encoding = %q, want %q", w.Header().Get("Content-Encoding"), "gzip")
	}
}
