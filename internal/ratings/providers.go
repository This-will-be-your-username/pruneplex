package ratings

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// lookupOMDb asks OMDb for the movie's IMDb, Rotten Tomatoes and Metacritic
// scores. OMDb is queried by IMDb ID only.
func (s *Service) lookupOMDb(ctx context.Context, key string, ref Ref) ([]Rating, error) {
	imdb := strings.TrimSpace(ref.IMDBID)
	if imdb == "" {
		return nil, ErrNotFound
	}
	endpoint := strings.TrimRight(s.omdbBase, "/") + "/?i=" + url.QueryEscape(imdb) + "&apikey=" + url.QueryEscape(key)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	response, err := s.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("OMDb request: %w", err)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return nil, ErrAuth
	case http.StatusTooManyRequests:
		return nil, ErrQuota
	}
	var body struct {
		Response   string `json:"Response"`
		Error      string `json:"Error"`
		ImdbRating string `json:"imdbRating"`
		Metascore  string `json:"Metascore"`
		Ratings    []struct {
			Source string `json:"Source"`
			Value  string `json:"Value"`
		} `json:"Ratings"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode OMDb response: %w", err)
	}
	if strings.EqualFold(body.Response, "False") {
		message := strings.ToLower(body.Error)
		switch {
		case strings.Contains(message, "limit"):
			return nil, ErrQuota
		case strings.Contains(message, "api key"):
			return nil, ErrAuth
		case strings.Contains(message, "not found"), strings.Contains(message, "incorrect"):
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("OMDb: %s", body.Error)
	}

	var out []Rating
	if value, ok := parseNumber(body.ImdbRating); ok {
		out = append(out, Rating{Source: "imdb", Type: "audience", Value: value})
	}
	for _, rating := range body.Ratings {
		if strings.EqualFold(rating.Source, "Rotten Tomatoes") {
			if value, ok := parseNumber(strings.TrimSuffix(rating.Value, "%")); ok {
				out = append(out, Rating{Source: "rotten_tomatoes", Type: "critic", Value: value / 10})
			}
		}
	}
	if value, ok := parseNumber(body.Metascore); ok {
		out = append(out, Rating{Source: "metacritic", Type: "critic", Value: value / 10})
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

// parseNumber reads "8.4" or "93"; OMDb writes "N/A" for a missing score.
func parseNumber(text string) (float64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}

// lookupTrakt asks Trakt for the community rating, by IMDb ID or, failing that,
// by TMDB ID.
func (s *Service) lookupTrakt(ctx context.Context, clientID string, ref Ref) ([]Rating, error) {
	base := strings.TrimRight(s.traktBase, "/")
	var endpoint string
	byTMDB := false
	switch {
	case strings.TrimSpace(ref.IMDBID) != "":
		endpoint = base + "/movies/" + url.PathEscape(strings.TrimSpace(ref.IMDBID)) + "?extended=full"
	case ref.TMDBID > 0:
		endpoint = base + "/search/tmdb/" + strconv.Itoa(ref.TMDBID) + "?type=movie&extended=full"
		byTMDB = true
	default:
		return nil, ErrNotFound
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("trakt-api-version", "2")
	request.Header.Set("trakt-api-key", clientID)
	request.Header.Set("Content-Type", "application/json")
	response, err := s.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Trakt request: %w", err)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusNotFound:
		return nil, ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrAuth
	case http.StatusTooManyRequests:
		return nil, ErrQuota
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Trakt returned HTTP %d", response.StatusCode)
	}

	type movie struct {
		Rating float64 `json:"rating"`
		IDs    struct {
			Slug string `json:"slug"`
		} `json:"ids"`
	}
	var found movie
	if byTMDB {
		var results []struct {
			Movie movie `json:"movie"`
		}
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&results); err != nil {
			return nil, fmt.Errorf("decode Trakt response: %w", err)
		}
		if len(results) == 0 {
			return nil, ErrNotFound
		}
		found = results[0].Movie
	} else if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&found); err != nil {
		return nil, fmt.Errorf("decode Trakt response: %w", err)
	}
	if found.Rating <= 0 {
		return nil, ErrNotFound // known to Trakt but not rated yet
	}
	rating := Rating{Source: "trakt", Type: "audience", Value: found.Rating}
	if found.IDs.Slug != "" {
		rating.URL = "https://trakt.tv/movies/" + found.IDs.Slug
	}
	return []Rating{rating}, nil
}
