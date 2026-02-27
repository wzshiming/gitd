package utils

import (
	"net/http"
	"testing"
)

func TestFixHFMirrorRoundTripper(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		wantHost string
	}{
		{
			name:     "rewrite hf-mirror host",
			host:     "cas-bridge.xethub.hf-mirror.org",
			wantHost: "cas-bridge.xethub.hf.co",
		},
		{
			name:     "pass through other hosts",
			host:     "example.com",
			wantHost: "example.com",
		},
		{
			name:     "pass through localhost",
			host:     "localhost",
			wantHost: "localhost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use a recording transport that captures the request after fixHFMirrorRoundTripper
			recorder := &recordingTransport{}
			rt := newFixHFMirrorRoundTripper(recorder)

			req, err := http.NewRequest("GET", "https://"+tt.host+"/test", nil)
			if err != nil {
				t.Fatal(err)
			}

			// RoundTrip will fail in recordingTransport, but we can check
			// the request state that was passed to the base transport
			rt.RoundTrip(req)

			if recorder.lastReq == nil {
				t.Fatal("expected base transport to receive a request")
			}
			if recorder.lastReq.URL.Host != tt.wantHost {
				t.Errorf("URL.Host = %q, want %q", recorder.lastReq.URL.Host, tt.wantHost)
			}
			if tt.host == "cas-bridge.xethub.hf-mirror.org" {
				if recorder.lastReq.Host != tt.wantHost {
					t.Errorf("Host = %q, want %q", recorder.lastReq.Host, tt.wantHost)
				}
			}
		})
	}
}

// recordingTransport captures the request passed to RoundTrip without making a real HTTP call.
type recordingTransport struct {
	lastReq *http.Request
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.lastReq = req
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       http.NoBody,
	}, nil
}

func TestNewFixHFMirrorRoundTripperNilBase(t *testing.T) {
	rt := newFixHFMirrorRoundTripper(nil)
	if rt.base == nil {
		t.Error("expected base to be set to DefaultTransport")
	}
}
