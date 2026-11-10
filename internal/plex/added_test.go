package plex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDateAddedIsCarriedThroughForMoviesAndShows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Films","type":"movie"},{"key":"2","title":"TV","type":"show"}]}}`)
		case r.URL.Path == "/library/sections/1/all":
			fmt.Fprint(w, `{"MediaContainer":{"totalSize":2,"Metadata":[
				{"title":"Added","addedAt":1700000000,"Media":[{"videoResolution":"1080"}]},
				{"title":"No date","Media":[{"videoResolution":"1080"}]}]}}`)
		case r.URL.Path == "/library/sections/2/all" && r.URL.Query().Get("type") == "2":
			fmt.Fprint(w, `{"MediaContainer":{"totalSize":1,"Metadata":[{"ratingKey":"9","title":"Show","addedAt":1650000000}]}}`)
		case r.URL.Path == "/library/sections/2/all" && r.URL.Query().Get("type") == "4":
			fmt.Fprint(w, `{"MediaContainer":{"totalSize":1,"Metadata":[{"grandparentRatingKey":"9","Media":[{"videoResolution":"1080"}]}]}}`)
		default:
			t.Fatalf("unexpected request %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := NewClient(server.Client(), "test-client")
	movies, err := client.AnalyzeMovies(context.Background(), server.URL, "t", "1")
	if err != nil {
		t.Fatal(err)
	}
	added := map[string]int64{}
	for _, movie := range movies.OnlyHD {
		added[movie.Title] = movie.AddedAt
	}
	if added["Added"] != 1700000000 || added["No date"] != 0 {
		t.Fatalf("movie dates = %+v", added)
	}

	shows, err := client.AnalyzeShows(context.Background(), server.URL, "t", "2")
	if err != nil {
		t.Fatal(err)
	}
	if len(shows.OnlyHD) != 1 || shows.OnlyHD[0].AddedAt != 1650000000 {
		t.Fatalf("show = %+v", shows.OnlyHD)
	}
}
