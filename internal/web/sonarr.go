package web

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"pruneplex/internal/plex"
	"pruneplex/internal/radarr"
	"pruneplex/internal/sonarr"
)

type sonarrHandlers struct {
	store  *radarrSettingsStore // same persistence and shape as the Radarr settings
	client *http.Client
}

func newSonarrHandlers(store *radarrSettingsStore) *sonarrHandlers {
	return &sonarrHandlers{store: store, client: &http.Client{Timeout: 30 * time.Second}}
}

func (h *sonarrHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/sonarr", h.getSettings)
	mux.HandleFunc("PUT /api/settings/sonarr", h.saveSettings)
	mux.HandleFunc("POST /api/settings/sonarr/test", h.testConnection)
	mux.HandleFunc("GET /api/settings/sonarr/rootfolders", h.rootFolders)
	mux.HandleFunc("DELETE /api/settings/sonarr/{quality}", h.clearInstance)
	mux.HandleFunc("POST /api/sonarr/series/remove", h.removeSeries)
	mux.HandleFunc("GET /api/sonarr/storage", h.storage)
	mux.HandleFunc("POST /api/sonarr/candidates", h.candidates)
	mux.HandleFunc("POST /api/sonarr/match", h.saveMatch)
}

func (h *sonarrHandlers) getSettings(w http.ResponseWriter, _ *http.Request) {
	settings, err := h.store.snapshot()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not read Sonarr settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]radarrInstanceStatus{
		"fourK": {URL: settings.FourK.URL, Configured: settings.FourK.APIKey != "", RootFolder: settings.FourK.RootFolder},
		"hd":    {URL: settings.HD.URL, Configured: settings.HD.APIKey != "", RootFolder: settings.HD.RootFolder},
	})
}

func (h *sonarrHandlers) saveSettings(w http.ResponseWriter, r *http.Request) {
	var input sonarr.Settings
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid Sonarr settings")
		return
	}
	current, err := h.store.snapshot()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not read Sonarr settings")
		return
	}
	settings, err := mergeSettings(input, current)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.save(settings); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not save Sonarr settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

func (h *sonarrHandlers) testConnection(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Quality  string          `json:"quality"`
		Instance sonarr.Instance `json:"instance"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid Sonarr connection settings")
		return
	}
	current, err := h.store.snapshot()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not read Sonarr settings")
		return
	}
	previous, err := configuredInstance(current, input.Quality)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	instance := mergeInstance(input.Instance, previous)
	if err := radarr.ValidateInstance(instance); err != nil || instance.APIKey == "" {
		writeAPIError(w, http.StatusBadRequest, "Enter a Sonarr URL and API key")
		return
	}
	if err := sonarr.NewClient(h.client, instance).Test(r.Context()); err != nil {
		writeAPIError(w, http.StatusBadGateway, "Could not connect to that Sonarr instance")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"connected": true})
}

func (h *sonarrHandlers) clearInstance(w http.ResponseWriter, r *http.Request) {
	settings, err := h.store.snapshot()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not read Sonarr settings")
		return
	}
	switch r.PathValue("quality") {
	case "4k":
		settings.FourK = sonarr.Instance{}
	case "hd":
		settings.HD = sonarr.Instance{}
	default:
		writeAPIError(w, http.StatusBadRequest, "Unknown Sonarr instance")
		return
	}
	if err := h.store.save(settings); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not save Sonarr settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// removeSeries deletes a series from Sonarr together with its files on disk.
func (h *sonarrHandlers) removeSeries(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Quality  string `json:"quality"`
		SeriesID int    `json:"seriesId"`
	}
	if err := decodeJSON(w, r, &input); err != nil || input.SeriesID <= 0 {
		writeAPIError(w, http.StatusBadRequest, "Invalid Sonarr series")
		return
	}
	settings, err := h.store.snapshot()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not read Sonarr settings")
		return
	}
	instance, err := configuredInstance(settings, input.Quality)
	if err != nil || instance.APIKey == "" {
		writeAPIError(w, http.StatusBadRequest, "That Sonarr instance is not configured")
		return
	}
	if err := sonarr.NewClient(h.client, instance).RemoveSeries(r.Context(), input.SeriesID); err != nil {
		log.Printf("Sonarr series removal failed: %v", err)
		writeAPIError(w, http.StatusBadGateway, "Could not remove the series from Sonarr")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}

// --- TV analysis ---

type analyzedShow struct {
	plex.Show
	Sonarr4KConfigured bool              `json:"sonarr4kConfigured"`
	SonarrHDConfigured bool              `json:"sonarrHdConfigured"`
	Sonarr4K           *radarrMovieMatch `json:"sonarr4k,omitempty"`
	SonarrHD           *radarrMovieMatch `json:"sonarrHd,omitempty"`
}

type showAnalysisResponse struct {
	Scanned int            `json:"scanned"`
	Matches []analyzedShow `json:"matches"`
	Only4K  []analyzedShow `json:"only4k"`
	OnlyHD  []analyzedShow `json:"onlyHd"`
	Other   []analyzedShow `json:"other"`
}

func listSonarrSeries(ctx context.Context, instance sonarr.Instance) ([]sonarr.Series, error) {
	if instance.APIKey == "" {
		return nil, nil
	}
	return sonarr.NewClient(&http.Client{Timeout: 60 * time.Second}, instance).Series(ctx)
}

func (c *plexConnector) analyzeTV(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Library string `json:"library"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &input); err != nil {
			writeAPIError(w, http.StatusBadRequest, "Invalid analysis request")
			return
		}
	}
	server := c.connectedServerFor(w, r)
	if server == nil {
		return
	}
	if c.sonarr == nil {
		writeAPIError(w, http.StatusInternalServerError, "Sonarr settings are unavailable")
		return
	}

	result, err := c.client.AnalyzeShows(r.Context(), server.baseURL, server.token, input.Library)
	if err != nil {
		if errors.Is(err, plex.ErrUnknownLibrary) {
			writeAPIError(w, http.StatusBadRequest, "That Plex library is not available")
			return
		}
		log.Printf("Plex TV analysis failed: %v", err)
		writeAPIError(w, http.StatusBadGateway, "Plex TV analysis failed: "+err.Error())
		return
	}
	c.rememberScan(w, r, func(session *plexSession) { session.showScan = &result })
	response, failure := c.showResponse(r.Context(), result)
	if failure != nil {
		writeAPIError(w, failure.status, failure.message)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// lastShowAnalysis returns the remembered TV scan, matched afresh against
// Sonarr so deletions and manual matches made since are reflected.
func (c *plexConnector) lastShowAnalysis(w http.ResponseWriter, r *http.Request) {
	session, _ := c.getSession(w, r, false)
	var scan *plex.ShowAnalysis
	if session != nil {
		c.mu.Lock()
		scan = session.showScan
		c.mu.Unlock()
	}
	if scan == nil || c.sonarr == nil {
		writeJSON(w, http.StatusOK, map[string]any{"analysis": nil})
		return
	}
	response, failure := c.showResponse(r.Context(), *scan)
	if failure != nil {
		writeAPIError(w, failure.status, failure.message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"analysis": response})
}

// showResponse matches a Plex TV scan against the configured Sonarr instances.
func (c *plexConnector) showResponse(ctx context.Context, result plex.ShowAnalysis) (showAnalysisResponse, *apiFailure) {
	settings, err := c.sonarr.snapshot()
	if err != nil {
		return showAnalysisResponse{}, &apiFailure{http.StatusInternalServerError, "Could not read Sonarr settings"}
	}
	fourK, err := listSonarrSeries(ctx, settings.FourK)
	if err != nil {
		return showAnalysisResponse{}, &apiFailure{http.StatusBadGateway, "Could not read the 4K Sonarr library"}
	}
	hd, err := listSonarrSeries(ctx, settings.HD)
	if err != nil {
		return showAnalysisResponse{}, &apiFailure{http.StatusBadGateway, "Could not read the HD Sonarr library"}
	}

	manual := c.sonarr.matches()
	// find prefers a pairing the user chose by hand, then automatic ID/title matching.
	find := func(quality string, show plex.Show, library []sonarr.Series) *radarrMovieMatch {
		var series sonarr.Series
		ok := false
		if id, saved := manual.get(quality, plexMatchKey(show.Title, show.Year)); saved {
			for _, candidate := range library {
				if candidate.ID == id {
					series, ok = candidate, true
					break
				}
			}
		}
		if !ok {
			series, ok = sonarr.FindSeries(sonarr.Identity{
				Title: show.Title, Year: show.Year, TVDBID: show.TVDBID, TMDBID: show.TMDBID, IMDBID: show.IMDBID,
			}, library)
		}
		if !ok {
			return nil
		}
		return &radarrMovieMatch{ID: series.ID, Title: series.Title, Year: series.Year, SizeBytes: series.SizeOnDisk(), Status: series.Status, EpisodeFiles: series.Statistics.EpisodeFileCount, EpisodeCount: series.Statistics.EpisodeCount}
	}
	enrich := func(shows []plex.Show) []analyzedShow {
		out := make([]analyzedShow, 0, len(shows))
		for _, show := range shows {
			out = append(out, analyzedShow{
				Show:               show,
				Sonarr4KConfigured: settings.FourK.APIKey != "",
				SonarrHDConfigured: settings.HD.APIKey != "",
				Sonarr4K:           find("4k", show, fourK),
				SonarrHD:           find("hd", show, hd),
			})
		}
		return out
	}
	return showAnalysisResponse{
		Scanned: result.Scanned,
		Matches: enrich(result.Matches),
		Only4K:  enrich(result.Only4K),
		OnlyHD:  enrich(result.OnlyHD),
		Other:   enrich(result.Other),
	}, nil
}
