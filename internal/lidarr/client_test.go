package lidarr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientListsAndPrunesAnAlbum(t *testing.T) {
	var calls []string
	var monitorBody, deleteBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "key" {
			t.Fatal("API key header missing")
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/artist":
			w.Write([]byte(`[{"id":1,"artistName":"Band","sortName":"band","status":"continuing","monitored":true,
				"images":[{"coverType":"poster","url":"/MediaCover/1/poster.jpg?lastWrite=1"}],
				"statistics":{"albumCount":2,"trackFileCount":9,"sizeOnDisk":900}}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/album":
			w.Write([]byte(`[{"id":10,"artistId":1,"title":"First","albumType":"Album","releaseDate":"2001-05-01T00:00:00Z","monitored":true,
				"statistics":{"trackFileCount":9,"trackCount":10,"sizeOnDisk":900}}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/trackfile":
			if r.URL.Query().Get("albumId") != "10" {
				t.Fatalf("trackfile query = %q", r.URL.RawQuery)
			}
			w.Write([]byte(`[{"id":100,"albumId":10,"size":50},{"id":101,"albumId":10,"size":60}]`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/album/monitor":
			json.Unmarshal(body, &monitorBody)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/trackfile/bulk":
			json.Unmarshal(body, &deleteBody)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client(), Instance{URL: server.URL, APIKey: "key"})
	ctx := context.Background()

	artists, err := client.Artists(ctx)
	if err != nil || len(artists) != 1 || artists[0].Name != "Band" || artists[0].Statistics.SizeOnDisk != 900 {
		t.Fatalf("artists = %+v, err = %v", artists, err)
	}
	if cover := CoverURL(artists[0].Images, "poster"); cover != "/MediaCover/1/poster.jpg?lastWrite=1" {
		t.Fatalf("cover = %q", cover)
	}
	albums, err := client.Albums(ctx)
	if err != nil || len(albums) != 1 || albums[0].Statistics.TrackFileCount != 9 {
		t.Fatalf("albums = %+v, err = %v", albums, err)
	}
	files, err := client.TrackFiles(ctx, 10)
	if err != nil || len(files) != 2 {
		t.Fatalf("files = %+v, err = %v", files, err)
	}
	if err := client.SetAlbumMonitored(ctx, 10, false); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteTrackFiles(ctx, []int{100, 101}); err != nil {
		t.Fatal(err)
	}

	if ids, _ := monitorBody["albumIds"].([]any); len(ids) != 1 || ids[0].(float64) != 10 || monitorBody["monitored"] != false {
		t.Fatalf("monitor body = %+v", monitorBody)
	}
	if ids, _ := deleteBody["trackFileIds"].([]any); len(ids) != 2 {
		t.Fatalf("delete body = %+v", deleteBody)
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "DELETE /api/v1/album") {
			t.Fatalf("the album itself must never be deleted from Lidarr: %s", call)
		}
	}
}

func TestDeleteTrackFilesWithNoIDsSendsNothing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no request expected")
	}))
	defer server.Close()
	client := NewClient(server.Client(), Instance{URL: server.URL, APIKey: "key"})
	if err := client.DeleteTrackFiles(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestCoverRejectsPathsOutsideMediaCover(t *testing.T) {
	client := NewClient(nil, Instance{URL: "http://lidarr:8686", APIKey: "key"})
	for _, path := range []string{"/api/v1/artist", "/MediaCover/../system", "/MediaCover/1/x.jpg?u=http://evil", "http://evil/MediaCover/1"} {
		if resp, err := client.Cover(context.Background(), path); err == nil {
			resp.Body.Close()
			t.Fatalf("Cover(%q) should be rejected", path)
		}
	}
}
