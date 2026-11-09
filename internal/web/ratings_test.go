package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pruneplex/internal/ratings"
)

func TestRatingsSettingsNeverExposeKeysAndLookupFillsInBackground(t *testing.T) {
	omdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != "omdb-secret" {
			t.Errorf("OMDb key not sent: %q", r.URL.RawQuery)
		}
		w.Write([]byte(`{"Response":"True","imdbRating":"7.5","Metascore":"61","Ratings":[{"Source":"Rotten Tomatoes","Value":"80%"}]}`))
	}))
	defer omdb.Close()
	trakt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"rating":7.9,"ids":{"slug":"some-movie-2020"}}`))
	}))
	defer trakt.Close()

	radarrStore := newRadarrSettingsStoreAt(filepath.Join(t.TempDir(), "radarr.json"))
	store := newRatingsSettingsStore(radarrStore)
	service := ratings.NewService("", store.get)
	service.SetEndpoints(omdb.URL, trakt.URL, 0)
	mux := http.NewServeMux()
	newRatingsHandlers(store, service).register(mux)

	do := func(method, path, body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
		return response
	}

	// Nothing configured: lookups report that and do no work.
	if got := do(http.MethodPost, "/api/ratings/lookup", `{"items":[{"key":"tt1","imdbId":"tt1"}]}`).Body.String(); !strings.Contains(got, `"configured":false`) {
		t.Fatalf("unconfigured lookup = %s", got)
	}

	if save := do(http.MethodPut, "/api/settings/ratings", `{"omdbKey":"omdb-secret","traktClientId":"trakt-id"}`); save.Code != http.StatusOK {
		t.Fatalf("save: %d %s", save.Code, save.Body.String())
	}
	settings := do(http.MethodGet, "/api/settings/ratings", "").Body.String()
	if strings.Contains(settings, "secret") || strings.Contains(settings, "trakt-id") || !strings.Contains(settings, `"omdb":true`) || !strings.Contains(settings, `"trakt":true`) {
		t.Fatalf("settings must report only whether keys exist: %s", settings)
	}
	// A blank field keeps the saved value.
	do(http.MethodPut, "/api/settings/ratings", `{"omdbKey":"","traktClientId":""}`)
	if got := store.get(); got.OMDbKey != "omdb-secret" || got.TraktClientID != "trakt-id" {
		t.Fatalf("blank fields must keep saved keys, got %+v", got)
	}

	var lookup struct {
		Configured bool                      `json:"configured"`
		Ratings    map[string]ratings.Result `json:"ratings"`
		Errors     map[string]string         `json:"errors"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		response := do(http.MethodPost, "/api/ratings/lookup", `{"items":[{"key":"tt1","imdbId":"tt1"}]}`)
		if err := json.Unmarshal(response.Body.Bytes(), &lookup); err != nil {
			t.Fatal(err)
		}
		if lookup.Ratings["tt1"].Complete {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("lookup never completed: %s", response.Body.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	sources := map[string]bool{}
	for _, rating := range lookup.Ratings["tt1"].Ratings {
		sources[rating.Source] = true
	}
	for _, want := range []string{"imdb", "rotten_tomatoes", "metacritic", "trakt"} {
		if !sources[want] {
			t.Fatalf("missing %s in %+v", want, lookup.Ratings["tt1"].Ratings)
		}
	}

	// Clearing a provider removes its key.
	if cleared := do(http.MethodDelete, "/api/settings/ratings/omdb", ""); cleared.Code != http.StatusOK || store.get().OMDbKey != "" {
		t.Fatalf("clear OMDb: %d, settings %+v", cleared.Code, store.get())
	}
	if bad := do(http.MethodDelete, "/api/settings/ratings/nope", ""); bad.Code != http.StatusBadRequest {
		t.Fatalf("unknown provider should be rejected, got %d", bad.Code)
	}
}

func TestLargeRatingsLookupBodiesAreAccepted(t *testing.T) {
	// A local stand-in, so the test never reaches the real OMDb.
	omdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Response":"False","Error":"Movie not found!"}`))
	}))
	defer omdb.Close()

	store := newRatingsSettingsStore(nil)
	_ = store.save(ratings.Settings{OMDbKey: "k"})
	service := ratings.NewService("", store.get)
	service.SetEndpoints(omdb.URL, omdb.URL, 0)
	mux := http.NewServeMux()
	newRatingsHandlers(store, service).register(mux)

	// ~1,500 movies is far beyond the 4 KB default body limit.
	var items []string
	for i := 0; i < 1500; i++ {
		items = append(items, `{"key":"k`+strings.Repeat("x", 20)+`","imdbId":"tt0000000"}`)
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/ratings/lookup", strings.NewReader(`{"items":[`+strings.Join(items, ",")+`]}`)))
	if response.Code != http.StatusOK {
		t.Fatalf("large lookup: %d %s", response.Code, response.Body.String())
	}
}
