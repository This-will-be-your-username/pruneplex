package web

import (
	"path/filepath"
	"testing"
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
