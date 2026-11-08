package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"pruneplex/internal/plex"
)

func TestLastScanIsReturnedForTheSessionAndEmptyOtherwise(t *testing.T) {
	dir := t.TempDir()
	store := newRadarrSettingsStoreAt(filepath.Join(dir, "radarr.json"))
	connector := newPlexConnector(store)
	connector.sonarr = newSonarrSettingsStore(store)
	connector.sessions["s1"] = &plexSession{
		movieScan: &plex.Analysis{Scanned: 7, Only4K: []plex.Movie{{Title: "UHD", Resolutions: []string{"4k"}}}},
		showScan:  &plex.ShowAnalysis{Scanned: 3, OnlyHD: []plex.Show{{Title: "HD Show", Resolutions: []string{"1080"}}}},
	}

	get := func(handler http.HandlerFunc, cookie string) map[string]json.RawMessage {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		if cookie != "" {
			request.AddCookie(&http.Cookie{Name: plexSessionCookie, Value: cookie})
		}
		response := httptest.NewRecorder()
		handler(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status %d: %s", response.Code, response.Body.String())
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}

	var movies struct{ Scanned int }
	if err := json.Unmarshal(get(connector.lastMovieAnalysis, "s1")["analysis"], &movies); err != nil || movies.Scanned != 7 {
		t.Fatalf("movie analysis = %+v, err = %v", movies, err)
	}
	var shows struct{ Scanned int }
	if err := json.Unmarshal(get(connector.lastShowAnalysis, "s1")["analysis"], &shows); err != nil || shows.Scanned != 3 {
		t.Fatalf("show analysis = %+v, err = %v", shows, err)
	}
	if got := string(get(connector.lastMovieAnalysis, "")["analysis"]); got != "null" {
		t.Fatalf("without a session the analysis should be null, got %s", got)
	}
}
