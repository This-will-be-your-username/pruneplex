package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestMusicLibraryReportsFreeSpaceAndArtistsCanBeRemoved(t *testing.T) {
	var removed, removedFiles, removedExclusion string
	lidarrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/artist":
			w.Write([]byte(`[
				{"id":1,"artistName":"Band","sortName":"band","rootFolderPath":"/music","statistics":{"sizeOnDisk":900}},
				{"id":2,"artistName":"Other","sortName":"other","rootFolderPath":"/archive","statistics":{"sizeOnDisk":5}}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/album":
			w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/rootfolder":
			w.Write([]byte(`[{"path":"/archive","freeSpace":111},{"path":"/music/","freeSpace":4000}]`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/artist/"):
			removed = r.URL.Path
			removedFiles = r.URL.Query().Get("deleteFiles")
			removedExclusion = r.URL.Query().Get("addImportListExclusion")
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
	if save := do(http.MethodPut, "/api/settings/lidarr", `{"url":"`+lidarrServer.URL+`","apiKey":"k"}`); save.Code != http.StatusOK {
		t.Fatalf("save: %d %s", save.Code, save.Body.String())
	}

	// Automatic: the root folder holding the most music, not a sum of every disk.
	var library musicLibrary
	if err := json.Unmarshal(do(http.MethodGet, "/api/lidarr/library", "").Body.Bytes(), &library); err != nil {
		t.Fatal(err)
	}
	if library.Storage == nil || library.Storage.FreeBytes != 4000 || library.Storage.Folder != "/music/" {
		t.Fatalf("storage = %+v", library.Storage)
	}

	// An explicit folder choice wins and is saved with the connection.
	if save := do(http.MethodPut, "/api/settings/lidarr", `{"url":"","apiKey":"","rootFolder":"/archive"}`); save.Code != http.StatusOK {
		t.Fatalf("save folder: %d %s", save.Code, save.Body.String())
	}
	if err := json.Unmarshal(do(http.MethodGet, "/api/lidarr/library", "").Body.Bytes(), &library); err != nil {
		t.Fatal(err)
	}
	if library.Storage == nil || library.Storage.FreeBytes != 111 {
		t.Fatalf("configured storage = %+v", library.Storage)
	}
	if got := do(http.MethodGet, "/api/settings/lidarr/rootfolders", "").Body.String(); !strings.Contains(got, "/archive") {
		t.Fatalf("root folders = %s", got)
	}

	// Removing an artist deletes it and its files in Lidarr.
	if response := do(http.MethodPost, "/api/lidarr/artist/remove", `{"artistId":2}`); response.Code != http.StatusOK {
		t.Fatalf("remove artist: %d %s", response.Code, response.Body.String())
	}
	if removed != "/api/v1/artist/2" || removedFiles != "true" || removedExclusion != "false" {
		t.Fatalf("Lidarr was asked %q deleteFiles=%q addImportListExclusion=%q", removed, removedFiles, removedExclusion)
	}
	if response := do(http.MethodPost, "/api/lidarr/artist/remove", `{"artistId":0}`); response.Code != http.StatusBadRequest {
		t.Fatalf("an invalid artist should be rejected, got %d", response.Code)
	}
}
