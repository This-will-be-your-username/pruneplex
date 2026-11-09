// Package lidarr is a minimal Lidarr v1 API client: list artists and albums,
// and prune an album by deleting its track files and unmonitoring it.
package lidarr

import (
	"bytes"
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

// Instance is a Lidarr URL and API key (the same shape as a Radarr instance).
type Instance = radarr.Instance

type Statistics struct {
	AlbumCount     int   `json:"albumCount"`
	TrackFileCount int   `json:"trackFileCount"`
	TrackCount     int   `json:"trackCount"`
	TotalTrackCount int  `json:"totalTrackCount"`
	SizeOnDisk     int64 `json:"sizeOnDisk"`
}

type Image struct {
	CoverType string `json:"coverType"`
	URL       string `json:"url"`
}

type Artist struct {
	ID         int        `json:"id"`
	Name       string     `json:"artistName"`
	SortName   string     `json:"sortName"`
	Status     string     `json:"status"` // "continuing" or "ended"
	RootFolderPath string `json:"rootFolderPath"`
	Monitored  bool       `json:"monitored"`
	Images     []Image    `json:"images"`
	Statistics Statistics `json:"statistics"`
}

type Album struct {
	ID          int        `json:"id"`
	ArtistID    int        `json:"artistId"`
	Title       string     `json:"title"`
	AlbumType   string     `json:"albumType"`
	ReleaseDate string     `json:"releaseDate"`
	Monitored   bool       `json:"monitored"`
	Images      []Image    `json:"images"`
	Statistics  Statistics `json:"statistics"`
}

type TrackFile struct {
	ID      int    `json:"id"`
	AlbumID int    `json:"albumId"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
}

// CoverURL returns the local cover path (under /MediaCover/) of the first image
// of one of the given types, or "".
func CoverURL(images []Image, types ...string) string {
	for _, want := range types {
		for _, image := range images {
			if image.CoverType == want && strings.HasPrefix(image.URL, "/MediaCover/") {
				return image.URL
			}
		}
	}
	return ""
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
	request, err := c.newRequest(ctx, http.MethodGet, "/system/status", nil)
	if err != nil {
		return err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("connect to Lidarr: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Lidarr returned HTTP %d", response.StatusCode)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

func (c *Client) Artists(ctx context.Context) ([]Artist, error) {
	var artists []Artist
	if err := c.getJSON(ctx, "/artist", &artists); err != nil {
		return nil, err
	}
	return artists, nil
}

// Albums lists every album across all artists.
func (c *Client) Albums(ctx context.Context) ([]Album, error) {
	var albums []Album
	if err := c.getJSON(ctx, "/album", &albums); err != nil {
		return nil, err
	}
	return albums, nil
}

// TrackFiles lists the files on disk for one album.
func (c *Client) TrackFiles(ctx context.Context, albumID int) ([]TrackFile, error) {
	var files []TrackFile
	if err := c.getJSON(ctx, "/trackfile?albumId="+fmt.Sprint(albumID), &files); err != nil {
		return nil, err
	}
	return files, nil
}

// SetAlbumMonitored follows or unfollows an album. An unmonitored album stays
// in Lidarr but is no longer searched for or upgraded.
func (c *Client) SetAlbumMonitored(ctx context.Context, albumID int, monitored bool) error {
	body := map[string]any{"albumIds": []int{albumID}, "monitored": monitored}
	return c.send(ctx, http.MethodPut, "/album/monitor", body)
}

// DeleteTrackFiles deletes the given track files from Lidarr and from disk.
func (c *Client) DeleteTrackFiles(ctx context.Context, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	return c.send(ctx, http.MethodDelete, "/trackfile/bulk", map[string]any{"trackFileIds": ids})
}

// Cover fetches an image served under /MediaCover/, which needs the API key.
// The caller closes the response body.
func (c *Client) Cover(ctx context.Context, path string) (*http.Response, error) {
	if !strings.HasPrefix(path, "/MediaCover/") || strings.Contains(path, "..") || strings.Contains(path, "://") {
		return nil, fmt.Errorf("invalid cover path")
	}
	base, err := c.baseURL()
	if err != nil {
		return nil, err
	}
	relative, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("invalid cover path")
	}
	base.Path = strings.TrimRight(base.Path, "/") + relative.Path
	base.RawQuery = relative.RawQuery
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("X-Api-Key", c.instance.APIKey)
	return c.httpClient.Do(request)
}

func (c *Client) baseURL() (*url.URL, error) {
	base, err := url.Parse(strings.TrimRight(c.instance.URL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil {
		return nil, fmt.Errorf("invalid Lidarr URL")
	}
	return base, nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	base, err := c.baseURL()
	if err != nil {
		return nil, err
	}
	relative, err := url.Parse(path)
	if err != nil {
		return nil, err
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/api/v1" + relative.Path
	base.RawQuery = relative.RawQuery
	request, err := http.NewRequestWithContext(ctx, method, base.String(), body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Api-Key", c.instance.APIKey)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return request, nil
}

func (c *Client) getJSON(ctx context.Context, path string, destination any) error {
	request, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request Lidarr: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Lidarr returned HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 128<<20)).Decode(destination); err != nil {
		return fmt.Errorf("decode Lidarr response: %w", err)
	}
	return nil
}

func (c *Client) send(ctx context.Context, method, path string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := c.newRequest(ctx, method, path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request Lidarr: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Lidarr returned HTTP %d", response.StatusCode)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

// RemoveArtist deletes an artist, with all its albums, from Lidarr and deletes
// the artist's files from disk. It does not add an import-list exclusion.
func (c *Client) RemoveArtist(ctx context.Context, artistID int) error {
	if artistID <= 0 {
		return fmt.Errorf("invalid Lidarr artist ID")
	}
	request, err := c.newRequest(ctx, http.MethodDelete, "/artist/"+strconv.Itoa(artistID)+"?deleteFiles=true&addImportListExclusion=false", nil)
	if err != nil {
		return err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("remove artist from Lidarr: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Lidarr returned HTTP %d", response.StatusCode)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

// RootFolder is a media root folder with its disk's free space.
type RootFolder = radarr.RootFolder

// RootFolders lists the root folders Lidarr stores music in.
func (c *Client) RootFolders(ctx context.Context) ([]RootFolder, error) {
	var folders []RootFolder
	if err := c.getJSON(ctx, "/rootfolder", &folders); err != nil {
		return nil, err
	}
	return folders, nil
}
