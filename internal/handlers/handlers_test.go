package handlers

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResponseLogger(t *testing.T) {
	w := httptest.NewRecorder()
	logger := &responseLogger{w: w, status: http.StatusOK}

	// Write data
	n, err := logger.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != 5 {
		t.Errorf("Write() = %d, want 5", n)
	}
	if logger.Size() != 5 {
		t.Errorf("Size() = %d, want 5", logger.Size())
	}
	if logger.Status() != http.StatusOK {
		t.Errorf("Status() = %d, want %d", logger.Status(), http.StatusOK)
	}

	// Write more data
	n, err = logger.Write([]byte(" world"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if logger.Size() != 11 {
		t.Errorf("Size() = %d, want 11", logger.Size())
	}

	// Write header
	logger.WriteHeader(http.StatusNotFound)
	if logger.Status() != http.StatusNotFound {
		t.Errorf("Status() = %d, want %d", logger.Status(), http.StatusNotFound)
	}
}

func TestLoggingHandler(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	var buf bytes.Buffer
	handler := LoggingHandler(&buf, inner)

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	logLine := buf.String()
	if logLine == "" {
		t.Error("expected log output, got empty")
	}
	if !strings.Contains(logLine, "GET") {
		t.Errorf("log line should contain method, got %q", logLine)
	}
	if !strings.Contains(logLine, "/test") {
		t.Errorf("log line should contain path, got %q", logLine)
	}
	if !strings.Contains(logLine, "200") {
		t.Errorf("log line should contain status code 200, got %q", logLine)
	}
}

func TestCombinedLoggingHandler(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	var buf bytes.Buffer
	handler := CombinedLoggingHandler(&buf, inner)

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("User-Agent", "TestAgent/1.0")
	req.Header.Set("Referer", "http://example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	logLine := buf.String()
	if !strings.Contains(logLine, "TestAgent/1.0") {
		t.Errorf("combined log should contain user agent, got %q", logLine)
	}
	if !strings.Contains(logLine, "http://example.com") {
		t.Errorf("combined log should contain referer, got %q", logLine)
	}
}

func TestCustomLoggingHandler(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("created"))
	})

	var captured LogFormatterParams
	handler := CustomLoggingHandler(&bytes.Buffer{}, inner, func(writer io.Writer, params LogFormatterParams) {
		captured = params
	})

	req := httptest.NewRequest("POST", "/create", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if captured.StatusCode != http.StatusCreated {
		t.Errorf("StatusCode = %d, want %d", captured.StatusCode, http.StatusCreated)
	}
	if captured.Size != 7 {
		t.Errorf("Size = %d, want 7", captured.Size)
	}
}

func TestAppendQuoted(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "simple string", input: "hello", want: "hello"},
		{name: "with quotes", input: `"hello"`, want: `\"hello\"`},
		{name: "with backslash", input: `a\b`, want: `a\\b`},
		{name: "with newline", input: "a\nb", want: `a\nb`},
		{name: "with tab", input: "a\tb", want: `a\tb`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendQuoted(nil, tt.input)
			if string(got) != tt.want {
				t.Errorf("appendQuoted(%q) = %q, want %q", tt.input, string(got), tt.want)
			}
		})
	}
}

func TestBuildCommonLogLine(t *testing.T) {
	req := httptest.NewRequest("GET", "/path", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	ts := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	logLine := string(buildCommonLogLine(req, *req.URL, ts, 200, 42))

	if !strings.Contains(logLine, "192.168.1.1") {
		t.Errorf("log should contain IP, got %q", logLine)
	}
	if !strings.Contains(logLine, "GET") {
		t.Errorf("log should contain method, got %q", logLine)
	}
	if !strings.Contains(logLine, "/path") {
		t.Errorf("log should contain path, got %q", logLine)
	}
	if !strings.Contains(logLine, "200") {
		t.Errorf("log should contain status, got %q", logLine)
	}
	if !strings.Contains(logLine, "42") {
		t.Errorf("log should contain size, got %q", logLine)
	}
}
