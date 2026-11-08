// Package sonarr is a minimal Sonarr v3 API client for matching and removing series.
package sonarr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"pruneplex/internal/radarr"
)

// Instance and Settings have the same shape as Radarr's: a URL and API key for
// each of a 4K and an HD instance.
type (
	Instance = radarr.Instance
	Settings = radarr.Settings
)

type Series struct {
	ID         int    `json:"id"`
	Title      string `json:"title"`
	Year       int    `json:"year"`
	TVDBID     int    `json:"tvdbId"`
	TMDBID     int    `json:"tmdbId"`
	IMDBID     string `json:"imdbId"`
	// Status is "continuing", "ended", "upcoming" or "deleted".
	Status     string `json:"status"`
	RootFolderPath string `json:"rootFolderPath"`
	Statistics struct {
		SizeOnDisk int64 `json:"sizeOnDisk"`
		// EpisodeFileCount of EpisodeCount (monitored, aired) episodes are on disk.
		EpisodeFileCount int `json:"episodeFileCount"`
		EpisodeCount     int `json:"episodeCount"`
	} `json:"statistics"`
}

// SizeOnDisk is the total size of the series' episode files.
func (s Series) SizeOnDisk() int64 { return s.Statistics.SizeOnDisk }

type Identity struct {
	Title  string
	Year   int
	TVDBID int
	TMDBID int
	IMDBID string
}

type Client struct {
	httpClient *http.Client
	instance   Instance
}

func NewClient(httpClient *http.Client, instance Instance) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{httpClient: httpClient, instance: instance}
}

func (c *Client) Test(ctx context.Context) error {
	request, err := c.newRequest(ctx, http.MethodGet, "/system/status")
	if err != nil {
		return err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("connect to Sonarr: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Sonarr returned HTTP %d", response.StatusCode)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

func (c *Client) Series(ctx context.Context) ([]Series, error) {
	request, err := c.newRequest(ctx, http.MethodGet, "/series")
	if err != nil {
		return nil, err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request Sonarr series: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Sonarr returned HTTP %d", response.StatusCode)
	}
	var series []Series
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<20)).Decode(&series); err != nil {
		return nil, fmt.Errorf("decode Sonarr response: %w", err)
	}
	return series, nil
}

// RemoveSeries deletes the series from Sonarr together with all its files on disk.
func (c *Client) RemoveSeries(ctx context.Context, seriesID int) error {
	if seriesID <= 0 {
		return fmt.Errorf("invalid Sonarr series ID")
	}
	request, err := c.newRequest(ctx, http.MethodDelete, "/series/"+strconv.Itoa(seriesID))
	if err != nil {
		return err
	}
	query := request.URL.Query()
	query.Set("deleteFiles", "true")
	request.URL.RawQuery = query.Encode()
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("remove series from Sonarr: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Sonarr returned HTTP %d", response.StatusCode)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

// FindSeries matches by TVDB, TMDB or IMDb ID, then, if Plex supplied no IDs,
// by exact title and year.
func FindSeries(identity Identity, series []Series) (Series, bool) {
	for _, s := range series {
		if identity.TVDBID > 0 && s.TVDBID == identity.TVDBID {
			return s, true
		}
		if identity.TMDBID > 0 && s.TMDBID == identity.TMDBID {
			return s, true
		}
		if identity.IMDBID != "" && strings.EqualFold(s.IMDBID, identity.IMDBID) {
			return s, true
		}
	}
	if identity.TVDBID > 0 || identity.TMDBID > 0 || identity.IMDBID != "" || identity.Year <= 0 {
		return Series{}, false
	}
	title := normalizeTitle(identity.Title)
	for _, s := range series {
		if s.Year == identity.Year && normalizeTitle(s.Title) == title {
			return s, true
		}
	}
	return Series{}, false
}

func normalizeTitle(title string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(title))), " ")
}

func (c *Client) newRequest(ctx context.Context, method, path string) (*http.Request, error) {
	base, err := url.Parse(strings.TrimRight(c.instance.URL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil {
		return nil, fmt.Errorf("invalid Sonarr URL")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/api/v3" + path
	request, err := http.NewRequestWithContext(ctx, method, base.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Api-Key", c.instance.APIKey)
	return request, nil
}

// RootFolder is a media root folder with its disk's free space.
type RootFolder = radarr.RootFolder

// RootFolders lists the root folders Sonarr stores series in.
func (c *Client) RootFolders(ctx context.Context) ([]RootFolder, error) {
	request, err := c.newRequest(ctx, http.MethodGet, "/rootfolder")
	if err != nil {
		return nil, err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request Sonarr root folders: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Sonarr returned HTTP %d", response.StatusCode)
	}
	var folders []RootFolder
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&folders); err != nil {
		return nil, fmt.Errorf("decode Sonarr response: %w", err)
	}
	return folders, nil
}
