package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"pruneplex/internal/ratings"
)

// ratingsSettingsStore keeps the OMDb and Trakt credentials in ratings.json,
// beside radarr.json.
type ratingsSettingsStore struct {
	mu    sync.RWMutex
	path  string // empty means in-memory only
	value ratings.Settings
}

func newRatingsSettingsStore(radarrStore *radarrSettingsStore) *ratingsSettingsStore {
	store := &ratingsSettingsStore{}
	if radarrStore == nil || radarrStore.path == "" {
		return store
	}
	store.path = filepath.Join(filepath.Dir(radarrStore.path), "ratings.json")
	if data, err := os.ReadFile(store.path); err == nil {
		_ = json.Unmarshal(data, &store.value)
	}
	return store
}

func (s *ratingsSettingsStore) get() ratings.Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value
}

func (s *ratingsSettingsStore) save(value ratings.Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path != "" {
		if err := writeFileAtomic(s.path, value); err != nil {
			return err
		}
	}
	s.value = value
	return nil
}

// ratingsCachePath is where looked-up ratings are cached, beside radarr.json.
func ratingsCachePath(radarrStore *radarrSettingsStore) string {
	if radarrStore == nil || radarrStore.path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(radarrStore.path), "ratings-cache.json")
}

type ratingsHandlers struct {
	store   *ratingsSettingsStore
	service *ratings.Service
}

func newRatingsHandlers(store *ratingsSettingsStore, service *ratings.Service) *ratingsHandlers {
	return &ratingsHandlers{store: store, service: service}
}

func (h *ratingsHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/ratings", h.getSettings)
	mux.HandleFunc("PUT /api/settings/ratings", h.saveSettings)
	mux.HandleFunc("DELETE /api/settings/ratings/{provider}", h.clearProvider)
	mux.HandleFunc("POST /api/ratings/lookup", h.lookup)
}

// getSettings reports which providers are configured, never the credentials.
func (h *ratingsHandlers) getSettings(w http.ResponseWriter, _ *http.Request) {
	settings := h.store.get()
	writeJSON(w, http.StatusOK, map[string]bool{
		"omdb":  settings.OMDbKey != "",
		"trakt": settings.TraktClientID != "",
	})
}

// saveSettings updates the credentials; a blank field keeps the saved value.
func (h *ratingsHandlers) saveSettings(w http.ResponseWriter, r *http.Request) {
	var input ratings.Settings
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid ratings settings")
		return
	}
	merged := h.store.get()
	if key := strings.TrimSpace(input.OMDbKey); key != "" {
		merged.OMDbKey = key
	}
	if id := strings.TrimSpace(input.TraktClientID); id != "" {
		merged.TraktClientID = id
	}
	if err := h.store.save(merged); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not save ratings settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

func (h *ratingsHandlers) clearProvider(w http.ResponseWriter, r *http.Request) {
	settings := h.store.get()
	switch r.PathValue("provider") {
	case "omdb":
		settings.OMDbKey = ""
	case "trakt":
		settings.TraktClientID = ""
	default:
		writeAPIError(w, http.StatusBadRequest, "Unknown ratings provider")
		return
	}
	if err := h.store.save(settings); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not save ratings settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

type ratingsLookupItem struct {
	Key    string `json:"key"`
	IMDBID string `json:"imdbId"`
	TMDBID int    `json:"tmdbId"`
}

// lookup returns the ratings known for the given movies and queues the rest.
// The page polls it until every movie is complete.
func (h *ratingsHandlers) lookup(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Items []ratingsLookupItem `json:"items"`
	}
	if err := decodeJSONLimit(w, r, &input, 4<<20); err != nil || len(input.Items) > 50000 {
		writeAPIError(w, http.StatusBadRequest, "Invalid ratings lookup")
		return
	}
	if !h.service.Configured() {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false})
		return
	}
	refs := make([]ratings.Ref, 0, len(input.Items))
	for _, item := range input.Items {
		if item.Key != "" {
			refs = append(refs, ratings.Ref{Key: item.Key, IMDBID: item.IMDBID, TMDBID: item.TMDBID})
		}
	}
	results, errs := h.service.Lookup(refs)
	writeJSON(w, http.StatusOK, map[string]any{"configured": true, "ratings": results, "errors": errs})
}
