package radarr

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

type Instance struct {
	URL    string `json:"url"`
	APIKey string `json:"apiKey"`
}

type Settings struct {
	FourK Instance `json:"fourK"`
	HD    Instance `json:"hd"`
}

type Movie struct {
	ID         int    `json:"id"`
	Title      string `json:"title"`
	Year       int    `json:"year"`
	TMDBID     int    `json:"tmdbId"`
	IMDBID     string `json:"imdbId"`
	SizeOnDisk int64  `json:"sizeOnDisk"`
}

type Identity struct {
	Title  string
	Year   int
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

func ValidateInstance(instance Instance) error {
	instance.URL = strings.TrimSpace(instance.URL)
	instance.APIKey = strings.TrimSpace(instance.APIKey)
	if instance.URL == "" && instance.APIKey == "" {
		return nil
	}
	if instance.URL == "" || instance.APIKey == "" {
		return fmt.Errorf("both the Radarr URL and API key are required")
	}
	parsed, err := url.Parse(instance.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("enter a valid Radarr URL using HTTP or HTTPS")
	}
	return nil
}

func (c *Client) Test(ctx context.Context) error {
	request, err := c.newRequest(ctx, http.MethodGet, "/system/status")
	if err != nil {
		return err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("connect to Radarr: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Radarr returned HTTP %d", response.StatusCode)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

func (c *Client) Movies(ctx context.Context) ([]Movie, error) {
	request, err := c.newRequest(ctx, http.MethodGet, "/movie")
	if err != nil {
		return nil, err
	}
	var movies []Movie
	if err := c.doJSON(request, &movies); err != nil {
		return nil, err
	}
	return movies, nil
}

func (c *Client) RemoveMovie(ctx context.Context, movieID int) error {
	if movieID <= 0 {
		return fmt.Errorf("invalid Radarr movie ID")
	}
	request, err := c.newRequest(ctx, http.MethodDelete, "/movie/"+strconv.Itoa(movieID))
	if err != nil {
		return err
	}
	query := request.URL.Query()
	query.Set("deleteFiles", "true")
	request.URL.RawQuery = query.Encode()
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("remove movie from Radarr: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Radarr returned HTTP %d", response.StatusCode)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

func FindMovie(identity Identity, movies []Movie) (Movie, bool) {
	for _, movie := range movies {
		if identity.TMDBID > 0 && movie.TMDBID == identity.TMDBID {
			return movie, true
		}
		if identity.IMDBID != "" && strings.EqualFold(movie.IMDBID, identity.IMDBID) {
			return movie, true
		}
	}
	if identity.TMDBID > 0 || identity.IMDBID != "" || identity.Year <= 0 {
		return Movie{}, false
	}
	title := normalizeTitle(identity.Title)
	for _, movie := range movies {
		if movie.Year == identity.Year && normalizeTitle(movie.Title) == title {
			return movie, true
		}
	}
	return Movie{}, false
}

func normalizeTitle(title string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(title))), " ")
}

func (c *Client) newRequest(ctx context.Context, method, path string) (*http.Request, error) {
	base, err := url.Parse(strings.TrimRight(c.instance.URL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil {
		return nil, fmt.Errorf("invalid Radarr URL")
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

func (c *Client) doJSON(request *http.Request, destination any) error {
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request Radarr movies: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Radarr returned HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(destination); err != nil {
		return fmt.Errorf("decode Radarr response: %w", err)
	}
	return nil
}
