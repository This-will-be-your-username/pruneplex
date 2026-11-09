package sonarr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestFindSeries(t *testing.T) {
	series := []Series{
		{ID: 1, Title: "Show A", Year: 2010, TVDBID: 100},
		{ID: 2, Title: "Show B", Year: 2015, TMDBID: 200},
		{ID: 3, Title: "Plain Title", Year: 2000},
	}
	if s, ok := FindSeries(Identity{TVDBID: 100}, series); !ok || s.ID != 1 {
		t.Fatalf("TVDB match = %+v, %v", s, ok)
	}
	if s, ok := FindSeries(Identity{TMDBID: 200}, series); !ok || s.ID != 2 {
		t.Fatalf("TMDB match = %+v, %v", s, ok)
	}
	if s, ok := FindSeries(Identity{Title: " plain  TITLE", Year: 2000}, series); !ok || s.ID != 3 {
		t.Fatalf("title match = %+v, %v", s, ok)
	}
	if _, ok := FindSeries(Identity{Title: "Plain Title", Year: 2000, TVDBID: 999}, series); ok {
		t.Fatal("a non-matching ID must not fall back to title matching")
	}
}

func TestClientListsAndRemovesSeriesWithFiles(t *testing.T) {
	var deleted string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "key" {
			t.Fatal("API key header missing")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/series":
			w.Write([]byte(`[{"id":7,"title":"Show","year":2020,"tvdbId":55,"status":"continuing","statistics":{"sizeOnDisk":1234,"episodeFileCount":8,"episodeCount":10}}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v3/series/7":
			deleted = r.URL.Query().Get("deleteFiles")
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client(), Instance{URL: server.URL, APIKey: "key"})
	series, err := client.Series(context.Background())
	if err != nil || len(series) != 1 || series[0].SizeOnDisk() != 1234 || series[0].TVDBID != 55 || series[0].Status != "continuing" || series[0].Statistics.EpisodeFileCount != 8 || series[0].Statistics.EpisodeCount != 10 {
		t.Fatalf("series = %+v, err = %v", series, err)
	}
	if err := client.RemoveSeries(context.Background(), 7); err != nil || deleted != "true" {
		t.Fatalf("remove err = %v, deleteFiles = %q", err, deleted)
	}
}

func TestRemoveSeriesAddsImportListExclusionOnlyWhenConfigured(t *testing.T) {
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
	}))
	defer server.Close()

	for _, exclude := range []bool{false, true} {
		client := NewClient(server.Client(), Instance{URL: server.URL, APIKey: "k", ExcludeOnDelete: exclude})
		if err := client.RemoveSeries(context.Background(), 3); err != nil {
			t.Fatal(err)
		}
		values, _ := url.ParseQuery(query)
		if values.Get("deleteFiles") != "true" {
			t.Fatalf("exclude=%v: files must always be deleted, query %q", exclude, query)
		}
		if got := values.Get("addImportListExclusion") == "true"; got != exclude {
			t.Fatalf("exclude=%v: addImportListExclusion present = %v (query %q)", exclude, got, query)
		}
	}
}
