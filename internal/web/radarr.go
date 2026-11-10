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

	matchOnce  sync.Once
	matchStore *radarrMatchStore
}

type radarrHandlers struct {
	store  *radarrSettingsStore
	client *http.Client
}

type radarrInstanceStatus struct {
	URL        string `json:"url"`
	Configured bool   `json:"configured"`
	RootFolder string `json:"rootFolder"`
	ExcludeOnDelete bool `json:"excludeOnDelete"`
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
	mux.HandleFunc("GET /api/settings/radarr/rootfolders", h.rootFolders)
	mux.HandleFunc("DELETE /api/settings/radarr/{quality}", h.clearInstance)
	mux.HandleFunc("POST /api/radarr/movie/remove", h.removeMovie)
	mux.HandleFunc("GET /api/radarr/storage", h.storage)
	mux.HandleFunc("POST /api/radarr/candidates", h.candidates)
	mux.HandleFunc("POST /api/radarr/match", h.saveMatch)
	mux.HandleFunc("POST /api/radarr/match/clear", h.clearMatch)
}

type settingsSection struct {
	Prefix, Name, Eyebrow, Port, Items string
}

var settingsSections = []settingsSection{
	{Prefix: "radarr", Name: "Radarr", Eyebrow: "MOVIE MATCHING", Port: "7878", Items: "movies"},
	{Prefix: "sonarr", Name: "Sonarr", Eyebrow: "TV MATCHING", Port: "8989", Items: "shows"},
}

func settingsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := page.ExecuteTemplate(w, "settings.html", map[string]any{"Sections": settingsSections}); err != nil {
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
		"fourK": {URL: settings.FourK.URL, Configured: settings.FourK.APIKey != "", RootFolder: settings.FourK.RootFolder, ExcludeOnDelete: settings.FourK.ExcludeOnDelete},
		"hd":    {URL: settings.HD.URL, Configured: settings.HD.APIKey != "", RootFolder: settings.HD.RootFolder, ExcludeOnDelete: settings.HD.ExcludeOnDelete},
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
	input.RootFolder = strings.TrimSpace(input.RootFolder)
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

// radarrCandidate is one movie that might be the Plex movie being matched. It
// carries the copy found in each configured Radarr instance, so a single pick
// can fix both the 4K and HD columns.
type radarrCandidate struct {
	Title  string           `json:"title"`
	Year   int              `json:"year"`
	TMDBID int              `json:"tmdbId,omitempty"`
	IMDBID string           `json:"imdbId,omitempty"`
	FourK  *radarrMovieCopy `json:"fourK,omitempty"`
	HD     *radarrMovieCopy `json:"hd,omitempty"`
}

type radarrMovieCopy struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	Year      int    `json:"year"`
	SizeBytes int64  `json:"sizeBytes"`
	Status    string `json:"status,omitempty"` // Sonarr series status
	Manual    bool   `json:"manual,omitempty"`
	EpisodeFiles int `json:"episodeFiles,omitempty"`
	EpisodeCount int `json:"episodeCount,omitempty"`
	AddedAt      int64 `json:"addedAt,omitempty"`
}

func newRadarrMovieCopy(m radarr.Movie) *radarrMovieCopy {
	return &radarrMovieCopy{ID: m.ID, Title: m.Title, Year: m.Year, SizeBytes: m.SizeOnDisk, AddedAt: m.ImportedAt()}
}

var radarrQualities = []string{"4k", "hd"}

// radarrIdentityKey identifies a film across Radarr instances: TMDB ID, else
// IMDb ID, else title and year.
func radarrIdentityKey(m radarr.Movie) string {
	switch {
	case m.TMDBID > 0:
		return fmt.Sprintf("tmdb:%d", m.TMDBID)
	case m.IMDBID != "":
		return "imdb:" + strings.ToLower(m.IMDBID)
	default:
		return "title:" + plexMatchKey(m.Title, m.Year)
	}
}

// libraries loads the movie list of every configured Radarr instance, keyed by
// quality. Unconfigured instances are omitted. It writes the error response
// itself and returns nil on failure.
func (h *radarrHandlers) libraries(w http.ResponseWriter, r *http.Request) map[string][]radarr.Movie {
	settings, err := h.store.snapshot()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not read Radarr settings")
		return nil
	}
	libraries := map[string][]radarr.Movie{}
	for _, quality := range radarrQualities {
		instance, _ := configuredInstance(settings, quality)
		if instance.APIKey == "" {
			continue
		}
		movies, err := h.listMovies(r.Context(), instance)
		if err != nil {
			writeAPIError(w, http.StatusBadGateway, "Could not read the "+strings.ToUpper(quality)+" Radarr library")
			return nil
		}
		libraries[quality] = movies
	}
	if len(libraries) == 0 {
		writeAPIError(w, http.StatusBadRequest, "Configure a Radarr instance first")
		return nil
	}
	return libraries
}

// candidates returns one shortlist of films, merged across all configured
// Radarr instances, that might be the given Plex movie. If query is set it
// replaces the title so users can refine the search.
func (h *radarrHandlers) candidates(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Title string `json:"title"`
		Year  int    `json:"year"`
		Query string `json:"query"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid candidate request")
		return
	}
	libraries := h.libraries(w, r)
	if libraries == nil {
		return
	}

	// Union of films across instances (first instance's title/year wins), plus
	// a per-instance lookup to attach each copy afterwards.
	byInstance := map[string]map[string]radarr.Movie{}
	var union []radarr.Movie
	seen := map[string]bool{}
	for _, quality := range radarrQualities {
		byInstance[quality] = map[string]radarr.Movie{}
		for _, m := range libraries[quality] {
			key := radarrIdentityKey(m)
			byInstance[quality][key] = m
			if !seen[key] {
				seen[key] = true
				union = append(union, m)
			}
		}
	}

	title, year := input.Title, input.Year
	if strings.TrimSpace(input.Query) != "" {
		title, year = input.Query, 0
	}
	out := []radarrCandidate{}
	for _, m := range radarr.Suggest(radarr.Identity{Title: title, Year: year}, union, 8) {
		key := radarrIdentityKey(m)
		candidate := radarrCandidate{Title: m.Title, Year: m.Year, TMDBID: m.TMDBID, IMDBID: m.IMDBID}
		if c, ok := byInstance["4k"][key]; ok {
			candidate.FourK = newRadarrMovieCopy(c)
		}
		if c, ok := byInstance["hd"][key]; ok {
			candidate.HD = newRadarrMovieCopy(c)
		}
		out = append(out, candidate)
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": out})
}

// saveMatch records that a Plex movie is the chosen film, in every requested
// Radarr instance that has a copy of it.
func (h *radarrHandlers) saveMatch(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PlexTitle string   `json:"plexTitle"`
		PlexYear  int      `json:"plexYear"`
		Title     string   `json:"title"`
		Year      int      `json:"year"`
		TMDBID    int      `json:"tmdbId"`
		IMDBID    string   `json:"imdbId"`
		Instances []string `json:"instances"`
	}
	if err := decodeJSON(w, r, &input); err != nil || strings.TrimSpace(input.PlexTitle) == "" || len(input.Instances) == 0 {
		writeAPIError(w, http.StatusBadRequest, "Invalid match")
		return
	}
	libraries := h.libraries(w, r)
	if libraries == nil {
		return
	}
	wantKey := radarrIdentityKey(radarr.Movie{Title: input.Title, Year: input.Year, TMDBID: input.TMDBID, IMDBID: input.IMDBID})

	matches := map[string]*radarrMovieCopy{}
	for _, quality := range input.Instances {
		library, ok := libraries[quality]
		if !ok {
			continue
		}
		for _, m := range library {
			if radarrIdentityKey(m) != wantKey {
				continue
			}
			if err := h.store.matches().set(quality, plexMatchKey(input.PlexTitle, input.PlexYear), m.ID); err != nil {
				writeAPIError(w, http.StatusInternalServerError, "Could not save the match")
				return
			}
			copy := newRadarrMovieCopy(m)
			copy.Manual = true
			matches[quality] = copy
			break
		}
	}
	if len(matches) == 0 {
		writeAPIError(w, http.StatusBadRequest, "That movie is not in the Radarr library")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": matches})
}

func (h *radarrHandlers) clearMatch(w http.ResponseWriter, r *http.Request) {
	clearManualMatch(h.store, w, r)
}
