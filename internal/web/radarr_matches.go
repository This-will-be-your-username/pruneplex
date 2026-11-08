package web

import (
	"encoding/json"
	"fmt"
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
			path = filepath.Join(filepath.Dir(s.path), "radarr-matches.json")
		}
		s.matchStore = newRadarrMatchStore(path)
	})
	return s.matchStore
}
