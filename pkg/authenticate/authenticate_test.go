package authenticate

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/context"
)

func TestAuthenticate(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := context.Get(r, "USER")
		if user == nil {
			t.Fatal("expected USER to be set in context")
		}
		if user != "admin" {
			t.Fatalf("expected USER to be 'admin', got %q", user)
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := Authenticate("admin", "secret", inner)

	tests := []struct {
		name       string
		user       string
		pass       string
		setAuth    bool
		wantStatus int
	}{
		{
			name:       "no credentials",
			setAuth:    false,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong username",
			user:       "wrong",
			pass:       "secret",
			setAuth:    true,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong password",
			user:       "admin",
			pass:       "wrong",
			setAuth:    true,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "correct credentials",
			user:       "admin",
			pass:       "secret",
			setAuth:    true,
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.setAuth {
				req.SetBasicAuth(tt.user, tt.pass)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusUnauthorized {
				if w.Header().Get("WWW-Authenticate") != `Basic realm="hfd"` {
					t.Error("expected WWW-Authenticate header to be set")
				}
			}
		})
	}
}
