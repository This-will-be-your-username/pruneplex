package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// plexAuth is the Plex state persisted between launches. The account token is
// a credential, so the file is written with 0600 permissions.
type plexAuth struct {
	ClientID     string `json:"clientId"`
	AccountToken string `json:"accountToken,omitempty"`
	ServerID     string `json:"serverId,omitempty"`
}

type plexAuthStore struct {
	mu    sync.Mutex
	path  string // empty means in-memory only
	value plexAuth
}

// newPlexAuthStore loads persisted auth from path. A missing or unreadable
// file yields an empty state; the user simply signs in again.
func newPlexAuthStore(path string) *plexAuthStore {
	store := &plexAuthStore{path: path}
	if path == "" {
		return store
	}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &store.value)
	}
	return store
}

func (s *plexAuthStore) get() plexAuth {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value
}

// update applies fn to the stored value and persists the result.
func (s *plexAuthStore) update(fn func(*plexAuth)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.value
	fn(&next)
	if s.path != "" {
		if err := writeFileAtomic(s.path, next); err != nil {
			return err
		}
	}
	s.value = next
	return nil
}

func writeFileAtomic(path string, value any) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".plex-auth-*")
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	defer os.Remove(temporaryPath)
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return err
	}
	if err := json.NewEncoder(file).Encode(value); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func plexAuthPath(settings *radarrSettingsStore) string {
	if settings == nil || settings.path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(settings.path), "plex.json")
}

