package lidarr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoveArtistDeletesFilesAndNeverExcludesTheArtist(t *testing.T) {
	var method, path, deleteFiles, exclusion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		deleteFiles = r.URL.Query().Get("deleteFiles")
		exclusion = r.URL.Query().Get("addImportListExclusion")
	}))
	defer server.Close()

	client := NewClient(server.Client(), Instance{URL: server.URL, APIKey: "key"})
	if err := client.RemoveArtist(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodDelete || path != "/api/v1/artist/7" || deleteFiles != "true" || exclusion != "false" {
		t.Fatalf("got %s %s deleteFiles=%q addImportListExclusion=%q", method, path, deleteFiles, exclusion)
	}
	if err := client.RemoveArtist(context.Background(), 0); err == nil {
		t.Fatal("an invalid artist ID must be rejected before any request")
	}
}

func TestRootFolders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/rootfolder" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`[{"path":"/music","freeSpace":123}]`))
	}))
	defer server.Close()

	folders, err := NewClient(server.Client(), Instance{URL: server.URL, APIKey: "key"}).RootFolders(context.Background())
	if err != nil || len(folders) != 1 || folders[0].Path != "/music" || folders[0].FreeSpace != 123 {
		t.Fatalf("folders = %+v, err = %v", folders, err)
	}
}
