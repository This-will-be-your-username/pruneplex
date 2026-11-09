package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestMusicPageAndLibraryWithoutAConnection(t *testing.T) {
	handler := newHandler(newRadarrSettingsStoreAt(filepath.Join(t.TempDir(), "radarr.json")))
	get := func(path string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		return response
	}
	if page := get("/music"); page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Music") {
		t.Fatalf("music page: %d", page.Code)
	}
	if script := get("/assets/music.js"); script.Code != http.StatusOK {
		t.Fatalf("music script: %d", script.Code)
	}
	// Without a saved connection the library asks the user to connect Lidarr.
	if library := get("/api/lidarr/library"); library.Code != http.StatusBadRequest || !strings.Contains(library.Body.String(), "Settings page") {
		t.Fatalf("library without Lidarr: %d %s", library.Code, library.Body.String())
	}
}

func TestLidarrAlbumPruneUnmonitorsThenDeletesTracksButKeepsTheAlbum(t *testing.T) {
	var calls []string
	lidarrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "lidarr-key" {
			t.Fatal("API key header missing")
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/artist":
			w.Write([]byte(`[{"id":1,"artistName":"Band","sortName":"band","status":"ended","monitored":true,"statistics":{"trackFileCount":3,"sizeOnDisk":300}}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/album":
			w.Write([]byte(`[
				{"id":10,"artistId":1,"title":"Old","releaseDate":"1999-01-01T00:00:00Z","monitored":true,"statistics":{"trackFileCount":2,"trackCount":2,"sizeOnDisk":200}},
				{"id":11,"artistId":1,"title":"New","releaseDate":"2010-01-01T00:00:00Z","monitored":true,"statistics":{"trackFileCount":1,"trackCount":9,"sizeOnDisk":100}}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/trackfile":
			w.Write([]byte(`[{"id":100,"albumId":10,"size":120},{"id":101,"albumId":10,"size":80}]`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/album/monitor":
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/trackfile/bulk":
		default:
			t.Fatalf("unexpected Lidarr request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer lidarrServer.Close()

	handler := newHandler(newRadarrSettingsStoreAt(filepath.Join(t.TempDir(), "radarr.json")))
	do := func(method, path, body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
		return response
	}
	if save := do(http.MethodPut, "/api/settings/lidarr", `{"url":"`+lidarrServer.URL+`","apiKey":"lidarr-key"}`); save.Code != http.StatusOK {
		t.Fatalf("save: %d %s", save.Code, save.Body.String())
	}
	if got := do(http.MethodGet, "/api/settings/lidarr", "").Body.String(); strings.Contains(got, "lidarr-key") || !strings.Contains(got, `"configured":true`) {
		t.Fatalf("settings must not expose the API key: %s", got)
	}

	var library musicLibrary
	response := do(http.MethodGet, "/api/lidarr/library", "")
	if err := json.Unmarshal(response.Body.Bytes(), &library); err != nil || len(library.Artists) != 1 {
		t.Fatalf("library: %v %s", err, response.Body.String())
	}
	albums := library.Artists[0].Albums
	if len(albums) != 2 || albums[0].Title != "New" || albums[1].Title != "Old" || albums[0].Year != 2010 || library.UsedBytes != 300 {
		t.Fatalf("albums should be newest first with years: %+v (used %d)", albums, library.UsedBytes)
	}

	calls = nil
	prune := do(http.MethodPost, "/api/lidarr/album/remove", `{"albumId":10}`)
	if prune.Code != http.StatusOK {
		t.Fatalf("prune: %d %s", prune.Code, prune.Body.String())
	}
	var result struct {
		RemovedFiles int   `json:"removedFiles"`
		FreedBytes   int64 `json:"freedBytes"`
	}
	if err := json.Unmarshal(prune.Body.Bytes(), &result); err != nil || result.RemovedFiles != 2 || result.FreedBytes != 200 {
		t.Fatalf("prune result = %+v, err = %v", result, err)
	}
	want := []string{"GET /api/v1/trackfile", "PUT /api/v1/album/monitor", "DELETE /api/v1/trackfile/bulk"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v (unmonitor before deleting files, and never delete the album)", calls, want)
	}
}

func TestReleaseYear(t *testing.T) {
	for date, want := range map[string]int{"2001-05-01T00:00:00Z": 2001, "": 0, "abc": 0, "0001-01-01T00:00:00Z": 0} {
		if got := releaseYear(date); got != want {
			t.Fatalf("releaseYear(%q) = %d, want %d", date, got, want)
		}
	}
}
