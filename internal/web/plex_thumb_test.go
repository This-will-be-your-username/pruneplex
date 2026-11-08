package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlexThumbValidationAndProxy(t *testing.T) {
	t.Run("rejects invalid path", func(t *testing.T) {
		for _, badPath := range []string{"", "relative/path", "/../secret", "http://evil.com/img"} {
			req := httptest.NewRequest(http.MethodGet, "/api/plex/thumb?path="+badPath, nil)
			rec := httptest.NewRecorder()
			NewHandler().ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("path %q: got status %d, want %d", badPath, rec.Code, http.StatusBadRequest)
			}
		}
	})

	t.Run("proxies authenticated cover art from Plex", func(t *testing.T) {
		plexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Plex-Token") != "test-token" {
				t.Fatalf("missing Plex token header")
			}
			if r.URL.Path == "/library/metadata/42/thumb/12345" {
				w.Header().Set("Content-Type", "image/jpeg")
				w.Header().Set("Content-Length", "4")
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, "JPEG")
				return
			}
			http.NotFound(w, r)
		}))
		defer plexServer.Close()

		connector := newPlexConnector(nil)
		connector.sessions["test-session"] = &plexSession{
			selected: &connectedServer{
				id:      "srv-1",
				name:    "Home Server",
				baseURL: plexServer.URL,
				token:   "test-token",
			},
		}

		mux := http.NewServeMux()
		connector.register(mux)

		req := httptest.NewRequest(http.MethodGet, "/api/plex/thumb?path=/library/metadata/42/thumb/12345", nil)
		req.AddCookie(&http.Cookie{Name: plexSessionCookie, Value: "test-session"})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("got status %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if rec.Header().Get("Content-Type") != "image/jpeg" {
			t.Errorf("got Content-Type %q, want image/jpeg", rec.Header().Get("Content-Type"))
		}
		if rec.Header().Get("Cache-Control") == "" {
			t.Error("missing Cache-Control header")
		}
		if rec.Body.String() != "JPEG" {
			t.Errorf("got body %q, want JPEG", rec.Body.String())
		}
	})
}
