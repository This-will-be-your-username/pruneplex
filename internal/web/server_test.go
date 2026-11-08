package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"pruneplex/internal/radarr"
)

func TestHandlerRoutes(t *testing.T) {
	handler := NewHandler()
	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string
	}{
		{name: "home page", path: "/", wantStatus: http.StatusOK, wantBody: "PrunePlex"},
		{name: "Radarr settings page", path: "/settings", wantStatus: http.StatusOK, wantBody: "API key"},
		{name: "health check", path: "/healthz", wantStatus: http.StatusOK, wantBody: `"status":"ok"`},
		{name: "Plex browser script", path: "/assets/app.js", wantStatus: http.StatusOK, wantBody: "plex-connect"},
		{name: "unknown route", path: "/missing", wantStatus: http.StatusNotFound},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("got status %d, want %d", response.Code, test.wantStatus)
			}
			if !strings.Contains(response.Body.String(), test.wantBody) {
				t.Fatalf("response body does not contain %q", test.wantBody)
			}
		})
	}
}

func TestRadarrSettingsDoNotExposeAPIKeysAndRemovalKeepsFiles(t *testing.T) {
	deleteFiles := ""
	radarrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "private-key" {
			t.Fatalf("Radarr API key was not sent in the header")
		}
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v3/movie/17" {
			t.Fatalf("unexpected Radarr request: %s %s", r.Method, r.URL.String())
		}
		deleteFiles = r.URL.Query().Get("deleteFiles")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer radarrServer.Close()

	store := newRadarrSettingsStoreAt(filepath.Join(t.TempDir(), "radarr.json"))
	if err := store.save(radarr.Settings{FourK: radarr.Instance{URL: radarrServer.URL, APIKey: "private-key"}}); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(store)

	settingsResponse := httptest.NewRecorder()
	handler.ServeHTTP(settingsResponse, httptest.NewRequest(http.MethodGet, "/api/settings/radarr", nil))
	if strings.Contains(settingsResponse.Body.String(), "private-key") {
		t.Fatal("settings response exposed a Radarr API key")
	}
	if !strings.Contains(settingsResponse.Body.String(), radarrServer.URL) {
		t.Fatalf("settings response did not include the configured URL: %s", settingsResponse.Body.String())
	}

	removeResponse := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/radarr/movie/remove", strings.NewReader(`{"quality":"4k","movieId":17}`))
	handler.ServeHTTP(removeResponse, request)
	if removeResponse.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d: %s", removeResponse.Code, http.StatusOK, removeResponse.Body.String())
	}
	if deleteFiles != "true" {
		t.Fatalf("Radarr deleteFiles was %q, want true", deleteFiles)
	}
}

func TestPlexAPIRequiresAuthenticationForAnalysis(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/plex/analyze", nil)
	response := httptest.NewRecorder()
	NewHandler().ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(response.Body.String(), "Authenticate with Plex") {
		t.Fatalf("unexpected response body: %s", response.Body.String())
	}
}

func TestPlexThumbRequiresAuthentication(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/plex/thumb?path=/library/metadata/101/thumb/123", nil)
	response := httptest.NewRecorder()
	NewHandler().ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want %d", response.Code, http.StatusUnauthorized)
	}
}
