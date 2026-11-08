package web

import (
	"path/filepath"
	"testing"

	"pruneplex/internal/radarr"
)

func TestRadarrMatchStorePersistsManualMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "radarr-matches.json")
	store := newRadarrMatchStore(path)
	key := plexMatchKey("  The  Movie ", 2020)
	if key != plexMatchKey("the movie", 2020) {
		t.Fatalf("key should ignore case and spacing, got %q", key)
	}
	if err := store.set("4k", key, 42); err != nil {
		t.Fatal(err)
	}

	reloaded := newRadarrMatchStore(path)
	if id, ok := reloaded.get("4k", key); !ok || id != 42 {
		t.Fatalf("reloaded match = %d, %v", id, ok)
	}
	if _, ok := reloaded.get("hd", key); ok {
		t.Fatal("matches must be kept per Radarr instance")
	}
}

func TestRadarrIdentityKeyMatchesSameFilmAcrossInstances(t *testing.T) {
	fourK := radarr.Movie{ID: 1, Title: "Dune", Year: 2021, TMDBID: 438631}
	hd := radarr.Movie{ID: 77, Title: "Dune (2021)", Year: 2021, TMDBID: 438631}
	if radarrIdentityKey(fourK) != radarrIdentityKey(hd) {
		t.Fatal("same TMDB ID should identify the same film in both instances")
	}
	if radarrIdentityKey(radarr.Movie{IMDBID: "TT123"}) != radarrIdentityKey(radarr.Movie{IMDBID: "tt123"}) {
		t.Fatal("IMDb IDs should compare case-insensitively")
	}
	if radarrIdentityKey(radarr.Movie{Title: "Dune", Year: 1984}) == radarrIdentityKey(radarr.Movie{Title: "Dune", Year: 2021}) {
		t.Fatal("title-only keys must include the year")
	}
}

func TestRadarrMatchStoreRemoveForgetsOnlyThatMatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "radarr-matches.json")
	store := newRadarrMatchStore(path)
	a, b := plexMatchKey("A", 2000), plexMatchKey("B", 2001)
	for _, key := range []string{a, b} {
		if err := store.set("4k", key, 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.remove("4k", a); err != nil {
		t.Fatal(err)
	}
	if err := store.remove("4k", plexMatchKey("never saved", 1999)); err != nil {
		t.Fatalf("removing an unknown match must not fail: %v", err)
	}
	reloaded := newRadarrMatchStore(path)
	if _, ok := reloaded.get("4k", a); ok {
		t.Fatal("removed match came back after reload")
	}
	if _, ok := reloaded.get("4k", b); !ok {
		t.Fatal("unrelated match was removed")
	}
}
