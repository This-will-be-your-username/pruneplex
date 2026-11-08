package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// radarrMatchStore remembers manual Plex-to-Radarr pairings so a movie that
// automatic ID matching misses only has to be matched once. Matches are kept per
// Radarr instance ("4k" / "hd"), keyed by the Plex movie's title and year.
type radarrMatchStore struct {
	mu    sync.Mutex
	path  string // empty means in-memory only
	value map[string]map[string]int
}

func newRadarrMatchStore(path string) *radarrMatchStore {
	store := &radarrMatchStore{path: path, value: map[string]map[string]int{}}
	if path == "" {
		return store
	}
	if data, err := os.ReadFile(path); err == nil {
		var loaded map[string]map[string]int
		if json.Unmarshal(data, &loaded) == nil && loaded != nil {
			store.value = loaded
		}
	}
	return store
}

func plexMatchKey(title string, year int) string {
	return fmt.Sprintf("%s|%d", strings.Join(strings.Fields(strings.ToLower(title)), " "), year)
}

func (s *radarrMatchStore) get(quality, key string) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.value[quality][key]
	return id, ok
}

func (s *radarrMatchStore) set(quality, key string, radarrID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := map[string]map[string]int{}
	for q, m := range s.value {
		next[q] = map[string]int{}
		for k, v := range m {
			next[q][k] = v
		}
	}
	if next[quality] == nil {
		next[quality] = map[string]int{}
	}
	next[quality][key] = radarrID
	if s.path != "" {
		if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
			return err
		}
		if err := writeFileAtomic(s.path, next); err != nil {
			return err
		}
	}
	s.value = next
	return nil
}

// matches returns the shared match store, stored beside the Radarr settings.
func (s *radarrSettingsStore) matches() *radarrMatchStore {
	if s == nil {
		return newRadarrMatchStore("")
	}
	s.matchOnce.Do(func() {
		path := ""
		if s.path != "" {
			name := strings.TrimSuffix(filepath.Base(s.path), ".json") + "-matches.json" // radarr.json -> radarr-matches.json
			path = filepath.Join(filepath.Dir(s.path), name)
		}
		s.matchStore = newRadarrMatchStore(path)
	})
	return s.matchStore
}

// remove forgets a manual match; the movie or show then falls back to automatic
// matching. Removing a match that doesn't exist is not an error.
func (s *radarrMatchStore) remove(quality, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.value[quality][key]; !ok {
		return nil
	}
	next := map[string]map[string]int{}
	for q, m := range s.value {
		next[q] = map[string]int{}
		for k, v := range m {
			if q == quality && k == key {
				continue
			}
			next[q][k] = v
		}
	}
	if s.path != "" {
		if err := writeFileAtomic(s.path, next); err != nil {
			return err
		}
	}
	s.value = next
	return nil
}

// clearMatchRequest identifies the manual match to forget.
type clearMatchRequest struct {
	Quality   string `json:"quality"`
	PlexTitle string `json:"plexTitle"`
	PlexYear  int    `json:"plexYear"`
}

// clearManualMatch implements POST .../match/clear for both Radarr and Sonarr.
func clearManualMatch(store *radarrSettingsStore, w http.ResponseWriter, r *http.Request) {
	var input clearMatchRequest
	if err := decodeJSON(w, r, &input); err != nil || strings.TrimSpace(input.PlexTitle) == "" || (input.Quality != "4k" && input.Quality != "hd") {
		writeAPIError(w, http.StatusBadRequest, "Invalid match")
		return
	}
	if err := store.matches().remove(input.Quality, plexMatchKey(input.PlexTitle, input.PlexYear)); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not clear the match")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"cleared": true})
}
