package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestStorageReportsFreeSpaceOfTheMediaRootFolder(t *testing.T) {
	arr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/movie":
			w.Write([]byte(`[
				{"id":1,"title":"A","sizeOnDisk":1000,"rootFolderPath":"/data/movies"},
				{"id":2,"title":"B","sizeOnDisk":2500,"rootFolderPath":"/data/movies/"},
				{"id":3,"title":"C","sizeOnDisk":10,"rootFolderPath":"/data/other"}]`))
		case "/api/v3/rootfolder":
			w.Write([]byte(`[{"path":"/data/other","freeSpace":111},{"path":"/data/movies","freeSpace":400}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer arr.Close()

	handler := newHandler(newRadarrSettingsStoreAt(filepath.Join(t.TempDir(), "radarr.json")))
	do := func(method, path, body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
		return response
	}
	storage := func() instanceStorage {
		response := do(http.MethodGet, "/api/radarr/storage", "")
		if response.Code != http.StatusOK {
			t.Fatalf("storage: %d %s", response.Code, response.Body.String())
		}
		var got map[string]instanceStorage
		if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got["hd"].Configured {
			t.Fatalf("unconfigured HD instance should not be reported: %+v", got["hd"])
		}
		return got["fourK"]
	}

	if save := do(http.MethodPut, "/api/settings/radarr", `{"fourK":{"url":"`+arr.URL+`","apiKey":"k"},"hd":{"url":"","apiKey":""}}`); save.Code != http.StatusOK {
		t.Fatalf("save: %d %s", save.Code, save.Body.String())
	}
	// Automatic: the root folder that holds the most media (/data/movies), not a sum of every disk.
	if got := storage(); !got.Configured || got.UsedBytes != 3510 || got.FreeBytes != 400 || got.Folder != "/data/movies" || got.Error != "" {
		t.Fatalf("automatic storage = %+v", got)
	}

	// The Settings page lists the root folders to choose from.
	folders := do(http.MethodGet, "/api/settings/radarr/rootfolders?quality=4k", "")
	if folders.Code != http.StatusOK || !strings.Contains(folders.Body.String(), "/data/other") {
		t.Fatalf("root folders: %d %s", folders.Code, folders.Body.String())
	}

	// An explicit choice wins and is saved with the instance.
	if save := do(http.MethodPut, "/api/settings/radarr", `{"fourK":{"url":"","apiKey":"","rootFolder":"/data/other"},"hd":{"url":"","apiKey":""}}`); save.Code != http.StatusOK {
		t.Fatalf("save folder: %d %s", save.Code, save.Body.String())
	}
	if got := storage(); got.FreeBytes != 111 || got.Folder != "/data/other" {
		t.Fatalf("configured storage = %+v", got)
	}
	if settings := do(http.MethodGet, "/api/settings/radarr", ""); !strings.Contains(settings.Body.String(), `"rootFolder":"/data/other"`) {
		t.Fatalf("settings should return the chosen folder: %s", settings.Body.String())
	}
}
