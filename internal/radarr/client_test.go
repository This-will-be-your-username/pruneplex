package radarr

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestMoviesAndRemoveMovieUseAPIKeyAndKeepFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "secret-key" {
			t.Fatalf("missing Radarr API key header")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie":
			fmt.Fprint(w, `[{"id":7,"title":"Example","year":2024,"tmdbId":42,"imdbId":"tt123"}]`)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v3/movie/7":
			if r.URL.Query().Get("deleteFiles") != "true" {
				t.Fatalf("expected movie files to be removed")
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client(), Instance{URL: server.URL, APIKey: "secret-key"})
	movies, err := client.Movies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(movies) != 1 || movies[0].TMDBID != 42 || movies[0].IMDBID != "tt123" {
		t.Fatalf("unexpected movies: %+v", movies)
	}
	if err := client.RemoveMovie(context.Background(), movies[0].ID); err != nil {
		t.Fatal(err)
	}
}

func TestFindMovieUsesExternalIDsThenExactTitleAndYear(t *testing.T) {
	movies := []Movie{
		{ID: 1, Title: "A Different Title", Year: 2020, TMDBID: 42, IMDBID: "tt123"},
		{ID: 2, Title: "Example", Year: 2024},
	}
	if movie, ok := FindMovie(Identity{TMDBID: 42, Title: "Example", Year: 2024}, movies); !ok || movie.ID != 1 {
		t.Fatalf("expected TMDB match, got %+v, %v", movie, ok)
	}
	if movie, ok := FindMovie(Identity{Title: " example ", Year: 2024}, movies); !ok || movie.ID != 2 {
		t.Fatalf("expected exact title/year match, got %+v, %v", movie, ok)
	}
	if _, ok := FindMovie(Identity{Title: "Example", Year: 2025}, movies); ok {
		t.Fatal("unexpected title/year match")
	}
}

func TestValidateInstance(t *testing.T) {
	for _, instance := range []Instance{
		{URL: "file:///etc/passwd", APIKey: "key"},
		{URL: "https://user:pass@example.com", APIKey: "key"},
		{URL: "http://radarr.example", APIKey: ""},
	} {
		if err := ValidateInstance(instance); err == nil {
			t.Errorf("ValidateInstance(%+v) unexpectedly succeeded", instance)
		}
	}
	if err := ValidateInstance(Instance{}); err != nil {
		t.Fatalf("empty instance should be allowed: %v", err)
	}
}

func TestChooseRootFolder(t *testing.T) {
	folders := []RootFolder{
		{Path: "/data/movies", FreeSpace: 100},
		{Path: "/data/movies-4k/", FreeSpace: 200},
		{Path: `D:\Media`, FreeSpace: 300},
	}
	usage := map[string]int64{
		NormalizePath("/data/movies"):     10,
		NormalizePath("/data/movies-4k"):  50,
		NormalizePath(`D:\Media`):         5,
	}
	if got, ok := ChooseRootFolder(folders, "", usage); !ok || got.FreeSpace != 200 {
		t.Fatalf("automatic should pick the folder holding the most media, got %+v", got)
	}
	if got, _ := ChooseRootFolder(folders, "/DATA/movies", usage); got.FreeSpace != 100 {
		t.Fatalf("configured folder should win (case-insensitively), got %+v", got)
	}
	if got, _ := ChooseRootFolder(folders, `d:\media\`, usage); got.FreeSpace != 300 {
		t.Fatalf("trailing separators must not matter, got %+v", got)
	}
	if got, _ := ChooseRootFolder(folders, "/gone", usage); got.FreeSpace != 200 {
		t.Fatalf("a configured folder that no longer exists falls back to automatic, got %+v", got)
	}
	if _, ok := ChooseRootFolder(nil, "", usage); ok {
		t.Fatal("no root folders should report not-found")
	}
}

func TestRemoveMovieAddsImportExclusionOnlyWhenConfigured(t *testing.T) {
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
	}))
	defer server.Close()

	for _, exclude := range []bool{false, true} {
		client := NewClient(server.Client(), Instance{URL: server.URL, APIKey: "k", ExcludeOnDelete: exclude})
		if err := client.RemoveMovie(context.Background(), 3); err != nil {
			t.Fatal(err)
		}
		values, _ := url.ParseQuery(query)
		if values.Get("deleteFiles") != "true" {
			t.Fatalf("exclude=%v: files must always be deleted, query %q", exclude, query)
		}
		if got := values.Get("addImportExclusion") == "true"; got != exclude {
			t.Fatalf("exclude=%v: addImportExclusion present = %v (query %q)", exclude, got, query)
		}
	}
}
