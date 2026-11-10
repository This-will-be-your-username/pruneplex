package web

import (
	"fmt"
	"net/http"
	"strings"

	"pruneplex/internal/radarr"
	"pruneplex/internal/sonarr"
)

// sonarrCandidate is one series that might be the Plex show being matched. It
// carries the copy found in each configured Sonarr instance, so a single pick
// can fix both the 4K and HD columns.
type sonarrCandidate struct {
	Title  string           `json:"title"`
	Year   int              `json:"year"`
	TVDBID int              `json:"tvdbId,omitempty"`
	TMDBID int              `json:"tmdbId,omitempty"`
	IMDBID string           `json:"imdbId,omitempty"`
	FourK  *radarrMovieCopy `json:"fourK,omitempty"`
	HD     *radarrMovieCopy `json:"hd,omitempty"`
}

func newSonarrCopy(s sonarr.Series) *radarrMovieCopy {
	return &radarrMovieCopy{ID: s.ID, Title: s.Title, Year: s.Year, SizeBytes: s.SizeOnDisk(), Status: s.Status, EpisodeFiles: s.Statistics.EpisodeFileCount, EpisodeCount: s.Statistics.EpisodeCount, AddedAt: s.AddedAt()}
}

// sonarrIdentityKey identifies a series across Sonarr instances: TVDB ID, else
// TMDB ID, else IMDb ID, else title and year.
func sonarrIdentityKey(s sonarr.Series) string {
	switch {
	case s.TVDBID > 0:
		return fmt.Sprintf("tvdb:%d", s.TVDBID)
	case s.TMDBID > 0:
		return fmt.Sprintf("tmdb:%d", s.TMDBID)
	case s.IMDBID != "":
		return "imdb:" + strings.ToLower(s.IMDBID)
	default:
		return "title:" + plexMatchKey(s.Title, s.Year)
	}
}

// libraries loads the series list of every configured Sonarr instance, keyed by
// quality. It writes the error response itself and returns nil on failure.
func (h *sonarrHandlers) libraries(w http.ResponseWriter, r *http.Request) map[string][]sonarr.Series {
	settings, err := h.store.snapshot()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not read Sonarr settings")
		return nil
	}
	libraries := map[string][]sonarr.Series{}
	for _, quality := range radarrQualities {
		instance, _ := configuredInstance(settings, quality)
		if instance.APIKey == "" {
			continue
		}
		series, err := listSonarrSeries(r.Context(), instance)
		if err != nil {
			writeAPIError(w, http.StatusBadGateway, "Could not read the "+strings.ToUpper(quality)+" Sonarr library")
			return nil
		}
		libraries[quality] = series
	}
	if len(libraries) == 0 {
		writeAPIError(w, http.StatusBadRequest, "Configure a Sonarr instance first")
		return nil
	}
	return libraries
}

// candidates returns one shortlist of series, merged across all configured
// Sonarr instances, that might be the given Plex show. If query is set it
// replaces the title so users can refine the search.
func (h *sonarrHandlers) candidates(w http.ResponseWriter, r *http.Request) {
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

	// Union of series across instances (first instance's title/year wins), plus
	// a per-instance lookup to attach each copy afterwards.
	byInstance := map[string]map[string]sonarr.Series{}
	var union []sonarr.Series
	seen := map[string]bool{}
	for _, quality := range radarrQualities {
		byInstance[quality] = map[string]sonarr.Series{}
		for _, s := range libraries[quality] {
			key := sonarrIdentityKey(s)
			byInstance[quality][key] = s
			if !seen[key] {
				seen[key] = true
				union = append(union, s)
			}
		}
	}

	// Rank with the shared fuzzy matcher: it works on titles and years, so feed
	// it lightweight movies whose ID is the series' index in the union.
	rankable := make([]radarr.Movie, len(union))
	for i, s := range union {
		rankable[i] = radarr.Movie{ID: i, Title: s.Title, Year: s.Year}
	}
	title, year := input.Title, input.Year
	if strings.TrimSpace(input.Query) != "" {
		title, year = input.Query, 0
	}
	out := []sonarrCandidate{}
	for _, ranked := range radarr.Suggest(radarr.Identity{Title: title, Year: year}, rankable, 8) {
		s := union[ranked.ID]
		key := sonarrIdentityKey(s)
		candidate := sonarrCandidate{Title: s.Title, Year: s.Year, TVDBID: s.TVDBID, TMDBID: s.TMDBID, IMDBID: s.IMDBID}
		if c, ok := byInstance["4k"][key]; ok {
			candidate.FourK = newSonarrCopy(c)
		}
		if c, ok := byInstance["hd"][key]; ok {
			candidate.HD = newSonarrCopy(c)
		}
		out = append(out, candidate)
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": out})
}

// saveMatch records that a Plex show is the chosen series, in every requested
// Sonarr instance that has a copy of it.
func (h *sonarrHandlers) saveMatch(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PlexTitle string   `json:"plexTitle"`
		PlexYear  int      `json:"plexYear"`
		Title     string   `json:"title"`
		Year      int      `json:"year"`
		TVDBID    int      `json:"tvdbId"`
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
	wantKey := sonarrIdentityKey(sonarr.Series{Title: input.Title, Year: input.Year, TVDBID: input.TVDBID, TMDBID: input.TMDBID, IMDBID: input.IMDBID})

	matches := map[string]*radarrMovieCopy{}
	for _, quality := range input.Instances {
		library, ok := libraries[quality]
		if !ok {
			continue
		}
		for _, s := range library {
			if sonarrIdentityKey(s) != wantKey {
				continue
			}
			if err := h.store.matches().set(quality, plexMatchKey(input.PlexTitle, input.PlexYear), s.ID); err != nil {
				writeAPIError(w, http.StatusInternalServerError, "Could not save the match")
				return
			}
			copy := newSonarrCopy(s)
			copy.Manual = true
			matches[quality] = copy
			break
		}
	}
	if len(matches) == 0 {
		writeAPIError(w, http.StatusBadRequest, "That show is not in the Sonarr library")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": matches})
}

func (h *sonarrHandlers) clearMatch(w http.ResponseWriter, r *http.Request) {
	clearManualMatch(h.store, w, r)
}
