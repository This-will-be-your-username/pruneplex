package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pruneplex/internal/sonarr"
)

func TestSonarrSettingsAreSeparateAndRemovalDeletesFiles(t *testing.T) {
	deleteFiles := ""
	sonarrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "sonarr-key" {
			t.Fatalf("Sonarr API key was not sent in the header")
		}
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v3/series/9" {
			t.Fatalf("unexpected Sonarr request %s %s", r.Method, r.URL.Path)
		}
		deleteFiles = r.URL.Query().Get("deleteFiles")
	}))
	defer sonarrServer.Close()

	dir := t.TempDir()
	handler := newHandler(newRadarrSettingsStoreAt(filepath.Join(dir, "radarr.json")))
	do := func(method, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	save := do(http.MethodPut, "/api/settings/sonarr", `{"fourK":{"url":"`+sonarrServer.URL+`","apiKey":"sonarr-key"},"hd":{"url":"","apiKey":""}}`)
	if save.Code != http.StatusOK {
		t.Fatalf("saving Sonarr settings: %d %s", save.Code, save.Body.String())
	}
	if got := do(http.MethodGet, "/api/settings/sonarr", ""); strings.Contains(got.Body.String(), "sonarr-key") {
		t.Fatal("Sonarr API key leaked from the settings endpoint")
	}
	if got := do(http.MethodGet, "/api/settings/radarr", ""); strings.Contains(got.Body.String(), sonarrServer.URL) {
		t.Fatal("Sonarr settings must not appear in Radarr settings")
	}

	remove := do(http.MethodPost, "/api/sonarr/series/remove", `{"quality":"4k","seriesId":9}`)
	if remove.Code != http.StatusOK || deleteFiles != "true" {
		t.Fatalf("remove: status %d, deleteFiles %q, body %s", remove.Code, deleteFiles, remove.Body.String())
	}
}

func TestSonarrMatchesAreKeptSeparatelyFromRadarr(t *testing.T) {
	dir := t.TempDir()
	radarrStore := newRadarrSettingsStoreAt(filepath.Join(dir, "radarr.json"))
	sonarrStore := newSonarrSettingsStore(radarrStore)

	if err := radarrStore.matches().set("4k", plexMatchKey("Show", 2020), 1); err != nil {
		t.Fatal(err)
	}
	if err := sonarrStore.matches().set("4k", plexMatchKey("Show", 2020), 2); err != nil {
		t.Fatal(err)
	}
	if id, _ := radarrStore.matches().get("4k", plexMatchKey("Show", 2020)); id != 1 {
		t.Fatalf("Radarr match = %d, want 1", id)
	}
	if id, _ := sonarrStore.matches().get("4k", plexMatchKey("Show", 2020)); id != 2 {
		t.Fatalf("Sonarr match = %d, want 2", id)
	}
	for _, name := range []string{"radarr-matches.json", "sonarr-matches.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}
}

func TestSonarrIdentityKeyPrefersTVDB(t *testing.T) {
	a := sonarr.Series{Title: "Show", Year: 2010, TVDBID: 5, TMDBID: 1}
	b := sonarr.Series{Title: "Show (2010)", Year: 2010, TVDBID: 5, TMDBID: 2}
	if sonarrIdentityKey(a) != sonarrIdentityKey(b) {
		t.Fatal("same TVDB ID should identify the same series across instances")
	}
	if sonarrIdentityKey(sonarr.Series{Title: "Show", Year: 2010}) == sonarrIdentityKey(sonarr.Series{Title: "Show", Year: 2011}) {
		t.Fatal("title-only keys must include the year")
	}
}

func TestClearMatchEndpointsRemoveManualMatches(t *testing.T) {
	key := plexMatchKey("Some Title", 2020)
	for _, test := range []struct{ endpoint, file string }{
		{"/api/radarr/match/clear", "radarr-matches.json"},
		{"/api/sonarr/match/clear", "sonarr-matches.json"},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, test.file)
		if err := newRadarrMatchStore(path).set("hd", key, 5); err != nil {
			t.Fatal(err)
		}
		// Build the handler after seeding so it loads the saved match from disk.
		handler := newHandler(newRadarrSettingsStoreAt(filepath.Join(dir, "radarr.json")))

		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, test.endpoint, strings.NewReader(`{"quality":"hd","plexTitle":"Some Title","plexYear":2020}`)))
		if response.Code != http.StatusOK {
			t.Fatalf("%s: status %d %s", test.endpoint, response.Code, response.Body.String())
		}
		if _, ok := newRadarrMatchStore(path).get("hd", key); ok {
			t.Fatalf("%s did not remove the match from %s", test.endpoint, test.file)
		}

		bad := httptest.NewRecorder()
		handler.ServeHTTP(bad, httptest.NewRequest(http.MethodPost, test.endpoint, strings.NewReader(`{"quality":"nope","plexTitle":"x"}`)))
		if bad.Code != http.StatusBadRequest {
			t.Fatalf("%s: invalid quality should be rejected, got %d", test.endpoint, bad.Code)
		}
	}
}

func TestExcludeOnDeleteSettingIsSavedAndAppliedToRemoval(t *testing.T) {
	var radarrQuery, sonarrQuery string
	radarrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { radarrQuery = r.URL.RawQuery }))
	defer radarrServer.Close()
	sonarrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sonarrQuery = r.URL.RawQuery }))
	defer sonarrServer.Close()

	handler := newHandler(newRadarrSettingsStoreAt(filepath.Join(t.TempDir(), "radarr.json")))
	do := func(method, path, body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
		return response
	}
	save := func(path, url string, exclude bool) {
		body := `{"fourK":{"url":"` + url + `","apiKey":"k","excludeOnDelete":` + map[bool]string{true: "true", false: "false"}[exclude] + `},"hd":{"url":"","apiKey":""}}`
		if response := do(http.MethodPut, path, body); response.Code != http.StatusOK {
			t.Fatalf("save %s: %d %s", path, response.Code, response.Body.String())
		}
	}

	save("/api/settings/radarr", radarrServer.URL, true)
	save("/api/settings/sonarr", sonarrServer.URL, false)
	for _, path := range []string{"/api/settings/radarr", "/api/settings/sonarr"} {
		want := path == "/api/settings/radarr"
		if got := do(http.MethodGet, path, "").Body.String(); strings.Contains(got, `"excludeOnDelete":true`) != want {
			t.Fatalf("%s should report excludeOnDelete=%v, got %s", path, want, got)
		}
	}

	do(http.MethodPost, "/api/radarr/movie/remove", `{"quality":"4k","movieId":1}`)
	do(http.MethodPost, "/api/sonarr/series/remove", `{"quality":"4k","seriesId":1}`)
	if !strings.Contains(radarrQuery, "addImportExclusion=true") {
		t.Fatalf("Radarr removal should add the exclusion, query %q", radarrQuery)
	}
	if strings.Contains(sonarrQuery, "addImportListExclusion") {
		t.Fatalf("Sonarr removal should not add the exclusion when unticked, query %q", sonarrQuery)
	}
}
