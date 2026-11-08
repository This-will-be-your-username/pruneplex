package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"pruneplex/internal/radarr"
)

type radarrSettingsStore struct {
	mu      sync.RWMutex
	path    string
	value   radarr.Settings
	loadErr error
}

type radarrHandlers struct {
	store  *radarrSettingsStore
	client *http.Client
}

type radarrInstanceStatus struct {
	URL        string `json:"url"`
	Configured bool   `json:"configured"`
}

func newRadarrSettingsStore() *radarrSettingsStore {
	path := os.Getenv("PRUNEPLEX_CONFIG")
	if path == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return &radarrSettingsStore{loadErr: err}
		}
		path = filepath.Join(configDir, "pruneplex", "radarr.json")
	}
	return newRadarrSettingsStoreAt(path)
}

func newRadarrSettingsStoreAt(path string) *radarrSettingsStore {
	store := &radarrSettingsStore{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store
	}
	if err != nil {
		store.loadErr = err
		return store
	}
	if err := json.Unmarshal(data, &store.value); err != nil {
		store.loadErr = fmt.Errorf("read Radarr settings: %w", err)
	}
	return store
}

func (s *radarrSettingsStore) snapshot() (radarr.Settings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.loadErr != nil {
		return radarr.Settings{}, s.loadErr
	}
	return s.value, nil
}

func (s *radarrSettingsStore) save(value radarr.Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return s.loadErr
	}
	if s.path != "" {
		directory := filepath.Dir(s.path)
		if err := os.MkdirAll(directory, 0700); err != nil {
			return err
		}
		file, err := os.CreateTemp(directory, ".radarr-settings-*")
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
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		if err := os.Rename(temporaryPath, s.path); err != nil {
			return err
		}
	}
	s.value = value
	return nil
}

func newRadarrHandlers(store *radarrSettingsStore) *radarrHandlers {
	return &radarrHandlers{store: store, client: &http.Client{Timeout: 30 * time.Second}}
}

func (h *radarrHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /settings", settingsPage)
	mux.HandleFunc("GET /api/settings/radarr", h.getSettings)
	mux.HandleFunc("PUT /api/settings/radarr", h.saveSettings)
	mux.HandleFunc("POST /api/settings/radarr/test", h.testConnection)
	mux.HandleFunc("DELETE /api/settings/radarr/{quality}", h.clearInstance)
	mux.HandleFunc("POST /api/radarr/movie/remove", h.removeMovie)
}

func settingsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := page.ExecuteTemplate(w, "settings.html", nil); err != nil {
		http.Error(w, "could not render settings", http.StatusInternalServerError)
	}
}

func (h *radarrHandlers) getSettings(w http.ResponseWriter, _ *http.Request) {
	settings, err := h.store.snapshot()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not read Radarr settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]radarrInstanceStatus{
		"fourK": {URL: settings.FourK.URL, Configured: settings.FourK.APIKey != ""},
		"hd":    {URL: settings.HD.URL, Configured: settings.HD.APIKey != ""},
	})
}

func (h *radarrHandlers) saveSettings(w http.ResponseWriter, r *http.Request) {
	var input radarr.Settings
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid Radarr settings")
		return
	}
	current, err := h.store.snapshot()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not read Radarr settings")
		return
	}
	settings, err := mergeSettings(input, current)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.save(settings); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not save Radarr settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

func mergeSettings(input, current radarr.Settings) (radarr.Settings, error) {
	input.FourK = mergeInstance(input.FourK, current.FourK)
	input.HD = mergeInstance(input.HD, current.HD)
	for _, instance := range []radarr.Instance{input.FourK, input.HD} {
		if err := radarr.ValidateInstance(instance); err != nil {
			return radarr.Settings{}, err
		}
	}
	return input, nil
}

func mergeInstance(input, current radarr.Instance) radarr.Instance {
	input.URL = strings.TrimSpace(input.URL)
	input.APIKey = strings.TrimSpace(input.APIKey)
	if input.URL == "" {
		input.URL = current.URL
	}
	if input.APIKey == "" {
		input.APIKey = current.APIKey
	}
	return input
}

func (h *radarrHandlers) testConnection(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Quality  string          `json:"quality"`
		Instance radarr.Instance `json:"instance"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid Radarr connection settings")
		return
	}
	current, err := h.store.snapshot()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not read Radarr settings")
		return
	}
	previous, err := configuredInstance(current, input.Quality)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	instance := mergeInstance(input.Instance, previous)
	if err := radarr.ValidateInstance(instance); err != nil || instance.APIKey == "" {
		writeAPIError(w, http.StatusBadRequest, "Enter a Radarr URL and API key")
		return
	}
	if err := radarr.NewClient(h.client, instance).Test(r.Context()); err != nil {
		writeAPIError(w, http.StatusBadGateway, "Could not connect to that Radarr instance")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"connected": true})
}

func (h *radarrHandlers) clearInstance(w http.ResponseWriter, r *http.Request) {
	settings, err := h.store.snapshot()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not read Radarr settings")
		return
	}
	switch r.PathValue("quality") {
	case "4k":
		settings.FourK = radarr.Instance{}
	case "hd":
		settings.HD = radarr.Instance{}
	default:
		writeAPIError(w, http.StatusBadRequest, "Unknown Radarr instance")
		return
	}
	if err := h.store.save(settings); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not save Radarr settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

func configuredInstance(settings radarr.Settings, quality string) (radarr.Instance, error) {
	switch quality {
	case "4k":
		return settings.FourK, nil
	case "hd":
		return settings.HD, nil
	default:
		return radarr.Instance{}, fmt.Errorf("unknown Radarr instance")
	}
}

func (h *radarrHandlers) removeMovie(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Quality string `json:"quality"`
		MovieID int    `json:"movieId"`
	}
	if err := decodeJSON(w, r, &input); err != nil || input.MovieID <= 0 {
		writeAPIError(w, http.StatusBadRequest, "Invalid Radarr movie")
		return
	}
	settings, err := h.store.snapshot()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not read Radarr settings")
		return
	}
	instance, err := configuredInstance(settings, input.Quality)
	if err != nil || instance.APIKey == "" {
		writeAPIError(w, http.StatusBadRequest, "That Radarr instance is not configured")
		return
	}
	if err := radarr.NewClient(h.client, instance).RemoveMovie(r.Context(), input.MovieID); err != nil {
		writeAPIError(w, http.StatusBadGateway, "Could not remove the movie from Radarr")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}

func (h *radarrHandlers) listMovies(ctx context.Context, instance radarr.Instance) ([]radarr.Movie, error) {
	return radarr.NewClient(h.client, instance).Movies(ctx)
}
