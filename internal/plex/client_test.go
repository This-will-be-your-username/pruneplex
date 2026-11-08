package plex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreatePIN(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/pins" || r.URL.Query().Get("strong") != "true" {
			t.Fatalf("unexpected PIN request: %s %s", r.Method, r.URL.String())
		}
		if r.Header.Get("X-Plex-Client-Identifier") != "test-client" {
			t.Fatalf("missing Plex client identifier")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":42,"code":"pin-code"}`)
	}))
	defer server.Close()

	client := NewClient(server.Client(), "test-client")
	client.pinsEndpoint = server.URL + "/api/v2/pins"
	pin, err := client.CreatePIN(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pin.ID != 42 || pin.Code != "pin-code" {
		t.Fatalf("got PIN %+v", pin)
	}
}

func TestGuidListAcceptsArrayAndSingleObject(t *testing.T) {
	tests := []struct {
		input  string
		tmdbID int
		imdbID string
		length int
	}{
		{input: `[{"id":"tmdb://105"},{"id":"imdb://tt0088763"}]`, tmdbID: 105, imdbID: "tt0088763", length: 2},
		{input: `["tmdb://105","imdb://tt0088763"]`, tmdbID: 105, imdbID: "tt0088763", length: 2},
		{input: `["com.plexapp.agents.themoviedb://105?lang=en","com.plexapp.agents.imdb://tt0088763"]`, tmdbID: 105, imdbID: "tt0088763", length: 2},
		{input: `{"id":"tmdb://105"}`, tmdbID: 105, length: 1},
		{input: `"tmdb://105"`, tmdbID: 105, length: 1},
		{input: `null`, length: 0},
	}
	for _, test := range tests {
		var guids guidList
		if err := json.Unmarshal([]byte(test.input), &guids); err != nil {
			t.Fatalf("could not decode %s: %v", test.input, err)
		}
		tmdbID, imdbID := parseExternalIDs(guids)
		if len(guids) != test.length || tmdbID != test.tmdbID || imdbID != test.imdbID {
			t.Fatalf("decoded %s to %+v, IDs %d/%s", test.input, guids, tmdbID, imdbID)
		}
	}
}

func TestAnalyzeMoviesFindsMixed4KVersions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "server-token" {
			t.Fatalf("missing server token")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Films","type":"movie"},{"key":"2","title":"Shows","type":"show"}]}}`)
		case "/library/sections/1/all":
			if r.URL.Query().Get("type") != "1" || r.Header.Get("X-Plex-Container-Start") != "0" || r.Header.Get("X-Plex-Container-Size") != "500" {
				t.Fatalf("missing movie query or paging headers")
			}
			fmt.Fprint(w, `{"MediaContainer":{"totalSize":5,"Metadata":[{"title":"Both Versions","year":2020,"thumb":"/library/metadata/101/thumb/123","Rating":[{"image":"rottentomatoes://image.rating.ripe","value":9.4,"type":"critic"},{"image":"themoviedb://image.rating","value":8.1,"type":"audience"},{"image":"imdb://image.rating","value":8.4,"type":"audience"}],"Guid":[{"id":"tmdb://105"},{"id":"imdb://tt0088763"}],"Media":[{"videoResolution":"4k","Part":[{"size":50000000000}]},{"videoResolution":"1080","Part":[{"size":10000000000}]}]},{"title":"Only 4K","Media":[{"videoResolution":"4k"}]},{"title":"Only HD","Media":[{"videoResolution":"1080"}]},{"title":"Two HD","Media":[{"videoResolution":"1080"},{"videoResolution":"720"}]},{"title":"2160 and HD","thumb":"/library/metadata/102/thumb/456","Media":[{"videoResolution":"2160p","size":30000000000},{"videoResolution":"720"}]}]}}`)
		case "/library/metadata/101/thumb/123":
			w.Header().Set("Content-Type", "image/jpeg")
			fmt.Fprint(w, "fake-image-bytes")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client(), "test-client")
	result, err := client.AnalyzeMovies(context.Background(), server.URL, "server-token", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned != 5 || len(result.Matches) != 2 || len(result.Only4K) != 1 || len(result.OnlyHD) != 1 || len(result.Other) != 1 || result.Other[0].Title != "Two HD" || result.Only4K[0].Title != "Only 4K" || result.OnlyHD[0].Title != "Only HD" {
		t.Fatalf("got analysis %+v", result)
	}
	if result.Matches[0].Title != "2160 and HD" || result.Matches[1].Title != "Both Versions" {
		t.Fatalf("unexpected matches: %+v", result.Matches)
	}
	if result.Matches[0].FourKSizeBytes != 30000000000 {
		t.Fatalf("expected 4K size 30000000000 for 2160 and HD, got %d", result.Matches[0].FourKSizeBytes)
	}
	if result.Matches[1].FourKSizeBytes != 50000000000 {
		t.Fatalf("expected 4K size 50000000000 for Both Versions, got %d", result.Matches[1].FourKSizeBytes)
	}
	if result.Matches[1].TMDBID != 105 || result.Matches[1].IMDBID != "tt0088763" {
		t.Fatalf("external IDs were not preserved: %+v", result.Matches[1])
	}
	if result.Matches[0].Thumb != "/library/metadata/102/thumb/456" {
		t.Fatalf("expected thumb /library/metadata/102/thumb/456, got %q", result.Matches[0].Thumb)
	}
	if result.Matches[1].Thumb != "/library/metadata/101/thumb/123" {
		t.Fatalf("expected thumb /library/metadata/101/thumb/123, got %q", result.Matches[1].Thumb)
	}
	if len(result.Matches[1].Ratings) != 3 {
		t.Fatalf("expected 3 ratings for Both Versions, got %d: %+v", len(result.Matches[1].Ratings), result.Matches[1].Ratings)
	}
	if result.Matches[1].Ratings[0].Source != "rotten_tomatoes" || result.Matches[1].Ratings[0].Value != 9.4 {
		t.Fatalf("unexpected RT rating: %+v", result.Matches[1].Ratings[0])
	}

	mediaResp, err := client.FetchMedia(context.Background(), server.URL, "server-token", result.Matches[1].Thumb)
	if err != nil {
		t.Fatalf("FetchMedia failed: %v", err)
	}
	defer mediaResp.Body.Close()
	if mediaResp.StatusCode != http.StatusOK {
		t.Fatalf("FetchMedia got status %d", mediaResp.StatusCode)
	}
}

func TestServerEndpointRejectsInvalidURLs(t *testing.T) {
	for _, input := range []string{"file:///etc", "https://user:pass@example.com", "not a URL"} {
		if _, err := serverEndpoint(input, "/identity"); err == nil {
			t.Errorf("serverEndpoint(%q) unexpectedly succeeded", input)
		}
	}
}

func TestMovieLibrariesAndLibraryFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"HD","type":"movie"},{"key":"2","title":"4K","type":"movie"},{"key":"3","title":"Shows","type":"show"}]}}`)
		case "/library/sections/2/all":
			fmt.Fprint(w, `{"MediaContainer":{"totalSize":1,"Metadata":[{"title":"UHD","Media":[{"videoResolution":"4k"}]}]}}`)
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client(), "test-client")
	libraries, err := client.MovieLibraries(context.Background(), server.URL, "t")
	if err != nil || len(libraries) != 2 || libraries[1].Title != "4K" {
		t.Fatalf("libraries = %+v, err = %v", libraries, err)
	}
	result, err := client.AnalyzeMovies(context.Background(), server.URL, "t", "2")
	if err != nil || result.Scanned != 1 || len(result.Only4K) != 1 {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if _, err := client.AnalyzeMovies(context.Background(), server.URL, "t", "9"); !errors.Is(err, ErrUnknownLibrary) {
		t.Fatalf("expected ErrUnknownLibrary, got %v", err)
	}
}
