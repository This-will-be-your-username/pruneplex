package ratings

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

const omdbFound = `{"Response":"True","imdbRating":"8.4","Metascore":"80",
	"Ratings":[{"Source":"Internet Movie Database","Value":"8.4/10"},{"Source":"Rotten Tomatoes","Value":"93%"},{"Source":"Metacritic","Value":"80/100"}]}`

func newTestService(t *testing.T, omdb, trakt http.HandlerFunc, settings Settings) (*Service, *int32, *int32) {
	t.Helper()
	var omdbCalls, traktCalls int32
	omdbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&omdbCalls, 1)
		omdb(w, r)
	}))
	traktServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&traktCalls, 1)
		trakt(w, r)
	}))
	t.Cleanup(omdbServer.Close)
	t.Cleanup(traktServer.Close)
	service := NewService(filepath.Join(t.TempDir(), "ratings-cache.json"), func() Settings { return settings })
	service.SetEndpoints(omdbServer.URL, traktServer.URL, 0)
	return service, &omdbCalls, &traktCalls
}

// waitComplete polls Lookup until every result is complete.
func waitComplete(t *testing.T, service *Service, refs []Ref) (map[string]Result, map[string]string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		results, errs := service.Lookup(refs)
		complete := true
		for _, result := range results {
			complete = complete && result.Complete
		}
		if complete || len(errs) > 0 {
			return results, errs
		}
		if time.Now().After(deadline) {
			t.Fatalf("lookup did not complete: %+v", results)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func bySource(ratings []Rating) map[string]Rating {
	out := map[string]Rating{}
	for _, r := range ratings {
		out[r.Source] = r
	}
	return out
}

func TestLookupFetchesFromBothProvidersOnceAndCaches(t *testing.T) {
	service, omdbCalls, traktCalls := newTestService(t,
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("apikey") != "omdb-key" || r.URL.Query().Get("i") != "tt0111161" {
				t.Errorf("unexpected OMDb query %q", r.URL.RawQuery)
			}
			w.Write([]byte(omdbFound))
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("trakt-api-key") != "trakt-id" || r.Header.Get("trakt-api-version") != "2" {
				t.Errorf("missing Trakt headers: %v", r.Header)
			}
			if r.URL.Path != "/movies/tt0111161" {
				t.Errorf("unexpected Trakt path %q", r.URL.Path)
			}
			w.Write([]byte(`{"rating":9.2,"ids":{"slug":"the-shawshank-redemption-1994"}}`))
		},
		Settings{OMDbKey: "omdb-key", TraktClientID: "trakt-id"})

	refs := []Ref{{Key: "a", IMDBID: "tt0111161"}}
	results, errs := waitComplete(t, service, refs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	got := bySource(results["a"].Ratings)
	if got["imdb"].Value != 8.4 || got["rotten_tomatoes"].Value != 9.3 || got["metacritic"].Value != 8.0 {
		t.Fatalf("OMDb ratings = %+v", got)
	}
	if got["rotten_tomatoes"].Type != "critic" || got["imdb"].Type != "audience" {
		t.Fatalf("rating types = %+v", got)
	}
	if trakt := got["trakt"]; trakt.Value != 9.2 || trakt.URL != "https://trakt.tv/movies/the-shawshank-redemption-1994" {
		t.Fatalf("Trakt rating = %+v", trakt)
	}

	// A second lookup is served from the cache with no further requests.
	before := atomic.LoadInt32(omdbCalls) + atomic.LoadInt32(traktCalls)
	if again, _ := service.Lookup(refs); !again["a"].Complete || len(again["a"].Ratings) != 4 {
		t.Fatalf("cached lookup = %+v", again["a"])
	}
	if after := atomic.LoadInt32(omdbCalls) + atomic.LoadInt32(traktCalls); after != before {
		t.Fatalf("the cache should have answered: %d requests before, %d after", before, after)
	}

	// The cache survives a restart.
	reloaded := NewService(service.path, func() Settings { return Settings{OMDbKey: "omdb-key", TraktClientID: "trakt-id"} })
	if cached, _ := reloaded.Lookup(refs); !cached["a"].Complete || len(cached["a"].Ratings) != 4 {
		t.Fatalf("reloaded cache = %+v", cached["a"])
	}
}

func TestLookupUsesTraktTMDBSearchWhenThereIsNoIMDBID(t *testing.T) {
	service, omdbCalls, _ := newTestService(t,
		func(w http.ResponseWriter, r *http.Request) { t.Error("OMDb needs an IMDb ID and must not be called") },
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/search/tmdb/603" {
				t.Errorf("unexpected Trakt path %q", r.URL.Path)
			}
			w.Write([]byte(`[{"type":"movie","movie":{"rating":8.1,"ids":{"slug":"the-matrix-1999"}}}]`))
		},
		Settings{OMDbKey: "k", TraktClientID: "id"})

	results, _ := waitComplete(t, service, []Ref{{Key: "m", TMDBID: 603}})
	if trakt := bySource(results["m"].Ratings)["trakt"]; trakt.Value != 8.1 {
		t.Fatalf("Trakt rating = %+v", trakt)
	}
	if atomic.LoadInt32(omdbCalls) != 0 {
		t.Fatal("OMDb should not have been queried without an IMDb ID")
	}
}

func TestNotFoundIsCachedAndQuotaPausesAProvider(t *testing.T) {
	service, omdbCalls, _ := newTestService(t,
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("i") == "tt0000001" {
				w.Write([]byte(`{"Response":"False","Error":"Incorrect IMDb ID."}`))
				return
			}
			w.Write([]byte(`{"Response":"False","Error":"Request limit reached!"}`))
		},
		func(w http.ResponseWriter, r *http.Request) {},
		Settings{OMDbKey: "k"})

	// "not found" is a real answer: it completes and is remembered.
	results, errs := waitComplete(t, service, []Ref{{Key: "x", IMDBID: "tt0000001"}})
	if !results["x"].Complete || len(results["x"].Ratings) != 0 || len(errs) != 0 {
		t.Fatalf("not-found result = %+v, errs = %v", results["x"], errs)
	}

	// Hitting the daily limit surfaces a message and stops further requests.
	_, errs = waitComplete(t, service, []Ref{{Key: "y", IMDBID: "tt0000002"}})
	if errs["omdb"] == "" {
		t.Fatalf("expected a rate-limit message, got %v", errs)
	}
	calls := atomic.LoadInt32(omdbCalls)
	service.Lookup([]Ref{{Key: "z", IMDBID: "tt0000003"}})
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(omdbCalls) != calls {
		t.Fatal("a paused provider must not be queried again")
	}
}

func TestInvalidKeyIsReportedAndNoProvidersMeansNothingToDo(t *testing.T) {
	service, _, _ := newTestService(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"Response":"False","Error":"Invalid API key!"}`))
		},
		func(w http.ResponseWriter, r *http.Request) {},
		Settings{OMDbKey: "bad"})
	if _, errs := waitComplete(t, service, []Ref{{Key: "a", IMDBID: "tt1"}}); errs["omdb"] == "" {
		t.Fatalf("expected an invalid-key message, got %v", errs)
	}

	none := NewService("", func() Settings { return Settings{} })
	results, errs := none.Lookup([]Ref{{Key: "a", IMDBID: "tt1"}})
	if !results["a"].Complete || len(errs) != 0 || none.Configured() {
		t.Fatalf("with no providers: %+v %v configured=%v", results, errs, none.Configured())
	}
}
