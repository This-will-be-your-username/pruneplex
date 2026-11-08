package plex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const productName = "PrunePlex"

type Client struct {
	httpClient   *http.Client
	clientID     string
	pinsEndpoint string
	resourcesURL string
}

type PIN struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
}

type PINStatus struct {
	AuthToken string `json:"authToken"`
}

type Resource struct {
	Name        string       `json:"name"`
	ClientID    string       `json:"clientIdentifier"`
	Provides    string       `json:"provides"`
	AccessToken string       `json:"accessToken"`
	Connections []Connection `json:"connections"`
}

type Connection struct {
	URI   string `json:"uri"`
	Local bool   `json:"local"`
	Relay bool   `json:"relay"`
}

type MovieRating struct {
	Source string  `json:"source"`
	Type   string  `json:"type"`
	Value  float64 `json:"value"`
	Image  string  `json:"image,omitempty"`
}

type Movie struct {
	Title          string        `json:"title"`
	Year           int           `json:"year,omitempty"`
	Thumb          string        `json:"thumb,omitempty"`
	Library        string        `json:"library,omitempty"`
	Ratings        []MovieRating `json:"ratings,omitempty"`
	Resolutions    []string      `json:"resolutions"`
	TMDBID         int           `json:"tmdbId,omitempty"`
	IMDBID         string        `json:"imdbId,omitempty"`
	FourKSizeBytes int64         `json:"fourKSizeBytes,omitempty"`
}

type Analysis struct {
	Scanned int     `json:"scanned"`
	Matches []Movie `json:"matches"`
	Only4K  []Movie `json:"only4k"`
	OnlyHD  []Movie `json:"onlyHd"`
	Other   []Movie `json:"other"`
}

func NewClient(httpClient *http.Client, clientID string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{
		httpClient:   httpClient,
		clientID:     clientID,
		pinsEndpoint: "https://plex.tv/api/v2/pins",
		resourcesURL: "https://clients.plex.tv/api/v2/resources?includeHttps=1&includeRelay=1&includeIPv6=1",
	}
}

func (c *Client) CreatePIN(ctx context.Context) (PIN, error) {
	endpoint, err := url.Parse(c.pinsEndpoint)
	if err != nil {
		return PIN{}, err
	}
	query := endpoint.Query()
	query.Set("strong", "true")
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), nil)
	if err != nil {
		return PIN{}, err
	}
	c.setPlexHeaders(request)
	var pin PIN
	if err := c.doJSON(request, &pin); err != nil {
		return PIN{}, err
	}
	if pin.ID == 0 || pin.Code == "" {
		return PIN{}, fmt.Errorf("Plex returned an incomplete PIN")
	}
	return pin, nil
}

func (c *Client) PollPIN(ctx context.Context, pinID int64, code string) (PINStatus, error) {
	endpoint, err := url.Parse(strings.TrimRight(c.pinsEndpoint, "/") + "/" + strconv.FormatInt(pinID, 10))
	if err != nil {
		return PINStatus{}, err
	}
	query := endpoint.Query()
	query.Set("code", code)
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return PINStatus{}, err
	}
	c.setPlexHeaders(request)
	var status PINStatus
	if err := c.doJSON(request, &status); err != nil {
		return PINStatus{}, err
	}
	return status, nil
}

func (c *Client) Resources(ctx context.Context, accountToken string) ([]Resource, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.resourcesURL, nil)
	if err != nil {
		return nil, err
	}
	c.setPlexHeaders(request)
	request.Header.Set("X-Plex-Token", accountToken)
	var resources []Resource
	if err := c.doJSON(request, &resources); err != nil {
		return nil, err
	}
	servers := make([]Resource, 0, len(resources))
	for _, resource := range resources {
		if strings.Contains(resource.Provides, "server") && resource.AccessToken != "" {
			servers = append(servers, resource)
		}
	}
	return servers, nil
}

func (c *Client) VerifyServer(ctx context.Context, baseURL, token, expectedID string) error {
	endpoint, err := serverEndpoint(baseURL, "/identity")
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	c.setPlexHeaders(request)
	request.Header.Set("X-Plex-Token", token)
	var response struct {
		MediaContainer struct {
			MachineIdentifier string `json:"machineIdentifier"`
		} `json:"MediaContainer"`
	}
	if err := c.doJSON(request, &response); err != nil {
		return err
	}
	if response.MediaContainer.MachineIdentifier != expectedID {
		return fmt.Errorf("Plex server identity did not match")
	}
	return nil
}

// Library is a Plex movie library (section).
type Library struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// MovieLibraries lists the server's movie libraries.
func (c *Client) MovieLibraries(ctx context.Context, baseURL, token string) ([]Library, error) {
	endpoint, err := serverEndpoint(baseURL, "/library/sections")
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	c.setPlexHeaders(request)
	request.Header.Set("X-Plex-Token", token)

	var sections struct {
		MediaContainer struct {
			Directory []struct {
				Key   string `json:"key"`
				Title string `json:"title"`
				Type  string `json:"type"`
			} `json:"Directory"`
		} `json:"MediaContainer"`
	}
	if err := c.doJSON(request, &sections); err != nil {
		return nil, err
	}
	var libraries []Library
	for _, section := range sections.MediaContainer.Directory {
		if section.Type == "movie" {
			libraries = append(libraries, Library{Key: section.Key, Title: section.Title})
		}
	}
	return libraries, nil
}

// ErrUnknownLibrary is returned when the requested library key does not exist.
var ErrUnknownLibrary = errors.New("unknown Plex library")

// AnalyzeMovies scans one movie library, or all of them when libraryKey is empty.
func (c *Client) AnalyzeMovies(ctx context.Context, baseURL, token, libraryKey string) (Analysis, error) {
	libraries, err := c.MovieLibraries(ctx, baseURL, token)
	if err != nil {
		return Analysis{}, err
	}
	if libraryKey != "" {
		var selected []Library
		for _, library := range libraries {
			if library.Key == libraryKey {
				selected = append(selected, library)
			}
		}
		if len(selected) == 0 {
			return Analysis{}, ErrUnknownLibrary
		}
		libraries = selected
	}

	var result Analysis
	for _, section := range libraries {
		sectionID := url.PathEscape(section.Key)
		start := 0
		for {
			movies, totalSize, err := c.moviePage(ctx, baseURL, token, sectionID, start)
			if err != nil {
				return Analysis{}, fmt.Errorf("read movie library %q: %w", section.Title, err)
			}
			result.Scanned += len(movies)
			for _, movie := range movies {
				resolutions := uniqueResolutions(movie.Media)
				tmdbID, imdbID := parseExternalIDs(movie.Guids)
				item := Movie{
					Title:          movie.Title,
					Year:           movie.Year,
					Thumb:          movie.Thumb,
					Library:        section.Title,
					Ratings:        consolidateRatings(movie),
					Resolutions:    resolutions,
					TMDBID:         tmdbID,
					IMDBID:         imdbID,
					FourKSizeBytes: extract4KSize(movie.Media),
				}
				switch {
				case hasMixed4K(resolutions):
					result.Matches = append(result.Matches, item)
				case hasUnknownResolution(movie.Media), distinctResolutions(resolutions) != 1:
					result.Other = append(result.Other, item)
				case is4K(resolutions[0]):
					result.Only4K = append(result.Only4K, item)
				default:
					result.OnlyHD = append(result.OnlyHD, item)
				}
			}
			if len(movies) == 0 || (totalSize > 0 && start+len(movies) >= totalSize) {
				break
			}
			start += len(movies)
		}
	}
	for _, list := range [][]Movie{result.Matches, result.Only4K, result.OnlyHD, result.Other} {
		sortMovies(list)
	}
	return result, nil
}

func sortMovies(movies []Movie) {
	sort.Slice(movies, func(i, j int) bool {
		if movies[i].Title == movies[j].Title {
			return movies[i].Year < movies[j].Year
		}
		return strings.ToLower(movies[i].Title) < strings.ToLower(movies[j].Title)
	})
}

type mediaItem struct {
	VideoResolution string   `json:"videoResolution"`
	Size            int64    `json:"size"`
	Part            partList `json:"Part"`
}

type flexFloat float64

func (f *flexFloat) UnmarshalJSON(data []byte) error {
	data = bytes.Trim(bytes.TrimSpace(data), `"`)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*f = 0
		return nil
	}
	val, err := strconv.ParseFloat(string(data), 64)
	if err != nil {
		*f = 0
		return nil
	}
	*f = flexFloat(val)
	return nil
}

type ratingItem struct {
	Image string  `json:"image"`
	Value float64 `json:"value"`
	Type  string  `json:"type"`
}

func (r *ratingItem) UnmarshalJSON(data []byte) error {
	type rawItem struct {
		Image any `json:"image"`
		Value any `json:"value"`
		Type  any `json:"type"`
	}
	var raw rawItem
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Image != nil {
		r.Image = fmt.Sprint(raw.Image)
	}
	if raw.Type != nil {
		r.Type = fmt.Sprint(raw.Type)
	}
	if raw.Value != nil {
		switch v := raw.Value.(type) {
		case float64:
			r.Value = v
		case string:
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				r.Value = parsed
			}
		}
	}
	return nil
}

type ratingList []ratingItem

func (ratings *ratingList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*ratings = nil
		return nil
	}
	if data[0] == '{' {
		var single ratingItem
		if err := json.Unmarshal(data, &single); err != nil {
			return err
		}
		*ratings = []ratingItem{single}
		return nil
	}
	var multiple []ratingItem
	if err := json.Unmarshal(data, &multiple); err != nil {
		return err
	}
	*ratings = multiple
	return nil
}

type libraryMovie struct {
	Title               string      `json:"title"`
	Year                int         `json:"year"`
	Thumb               string      `json:"thumb"`
	Rating              flexFloat   `json:"rating"`
	RatingImage         string      `json:"ratingImage"`
	AudienceRating      flexFloat   `json:"audienceRating"`
	AudienceRatingImage string      `json:"audienceRatingImage"`
	UserRating          flexFloat   `json:"userRating"`
	Ratings             ratingList  `json:"Rating"`
	Guids               guidList    `json:"Guid"`
	Media               []mediaItem `json:"Media"`
}

func consolidateRatings(m libraryMovie) []MovieRating {
	seen := make(map[string]bool)
	var list []MovieRating

	add := func(source, ratingType string, val float64, img string) {
		if val <= 0 {
			return
		}
		key := source + ":" + ratingType
		if seen[key] {
			return
		}
		seen[key] = true
		list = append(list, MovieRating{
			Source: source,
			Type:   ratingType,
			Value:  val,
			Image:  img,
		})
	}

	for _, r := range m.Ratings {
		source, rType := identifyRatingSource(r.Image, r.Type)
		add(source, rType, r.Value, r.Image)
	}

	if m.Rating > 0 {
		source, rType := identifyRatingSource(m.RatingImage, "critic")
		add(source, rType, float64(m.Rating), m.RatingImage)
	}
	if m.AudienceRating > 0 {
		source, rType := identifyRatingSource(m.AudienceRatingImage, "audience")
		add(source, rType, float64(m.AudienceRating), m.AudienceRatingImage)
	}
	if m.UserRating > 0 {
		add("user", "user", float64(m.UserRating), "")
	}

	return list
}

func identifyRatingSource(image, defaultType string) (string, string) {
	imgLower := strings.ToLower(image)
	rType := defaultType
	if rType == "" {
		rType = "critic"
	}

	switch {
	case strings.Contains(imgLower, "rottentomatoes") || strings.Contains(imgLower, "rotten_tomatoes"):
		if strings.Contains(imgLower, "upright") || strings.Contains(imgLower, "spilled") || strings.Contains(imgLower, "popcorn") || defaultType == "audience" {
			return "rotten_tomatoes_audience", "audience"
		}
		return "rotten_tomatoes", "critic"
	case strings.Contains(imgLower, "imdb"):
		return "imdb", "audience"
	case strings.Contains(imgLower, "themoviedb") || strings.Contains(imgLower, "tmdb"):
		return "tmdb", "audience"
	case strings.Contains(imgLower, "metacritic"):
		return "metacritic", "critic"
	case strings.Contains(imgLower, "user") || strings.Contains(imgLower, "star"):
		return "user", "user"
	default:
		if defaultType != "" {
			return defaultType, defaultType
		}
		return "critic", "critic"
	}
}

func (c *Client) FetchMedia(ctx context.Context, baseURL, token, path string) (*http.Response, error) {
	endpoint, err := serverEndpoint(baseURL, path)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	c.setPlexHeaders(request)
	request.Header.Set("X-Plex-Token", token)
	return c.httpClient.Do(request)
}

type partItem struct {
	Size int64 `json:"size"`
}

type partList []partItem

func (parts *partList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*parts = nil
		return nil
	}
	if data[0] == '{' {
		var single partItem
		if err := json.Unmarshal(data, &single); err != nil {
			return err
		}
		*parts = []partItem{single}
		return nil
	}
	var multiple []partItem
	if err := json.Unmarshal(data, &multiple); err != nil {
		return err
	}
	*parts = multiple
	return nil
}

func extract4KSize(media []mediaItem) int64 {
	var total int64
	for _, version := range media {
		if is4K(version.VideoResolution) {
			var partSum int64
			for _, part := range version.Part {
				partSum += part.Size
			}
			if partSum > 0 {
				total += partSum
			} else if version.Size > 0 {
				total += version.Size
			}
		}
	}
	return total
}

type guidItem struct {
	ID string `json:"id"`
}

func (guid *guidItem) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &guid.ID)
	}
	type plain guidItem
	return json.Unmarshal(data, (*plain)(guid))
}

type guidList []guidItem

func (guids *guidList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*guids = nil
		return nil
	}
	if data[0] == '{' {
		var single guidItem
		if err := json.Unmarshal(data, &single); err != nil {
			return err
		}
		*guids = []guidItem{single}
		return nil
	}
	if data[0] == '"' {
		var single guidItem
		if err := json.Unmarshal(data, &single); err != nil {
			return err
		}
		*guids = []guidItem{single}
		return nil
	}
	var multiple []guidItem
	if err := json.Unmarshal(data, &multiple); err != nil {
		return err
	}
	*guids = multiple
	return nil
}

func parseExternalIDs(guids guidList) (int, string) {
	var tmdbID int
	var imdbID string
	for _, guid := range guids {
		provider, value, found := strings.Cut(guid.ID, "://")
		if !found {
			continue
		}
		provider = strings.ToLower(provider)
		value = strings.TrimSpace(strings.SplitN(value, "?", 2)[0])
		value = strings.TrimSpace(strings.SplitN(value, "#", 2)[0])
		switch {
		case provider == "tmdb" || provider == "themoviedb" || strings.HasSuffix(provider, ".tmdb") || strings.HasSuffix(provider, ".themoviedb"):
			if parsed, err := strconv.Atoi(value); err == nil {
				tmdbID = parsed
			}
		case provider == "imdb" || strings.HasSuffix(provider, ".imdb"):
			imdbID = value
		}
	}
	return tmdbID, imdbID
}

func (c *Client) moviePage(ctx context.Context, baseURL, token, sectionID string, start int) ([]libraryMovie, int, error) {
	endpoint, err := serverEndpoint(baseURL, "/library/sections/"+sectionID+"/all?type=1")
	if err != nil {
		return nil, 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	c.setPlexHeaders(request)
	request.Header.Set("X-Plex-Token", token)
	request.Header.Set("X-Plex-Container-Start", strconv.Itoa(start))
	request.Header.Set("X-Plex-Container-Size", "500")
	var response struct {
		MediaContainer struct {
			TotalSize int            `json:"totalSize"`
			Metadata  []libraryMovie `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if err := c.doJSON(request, &response); err != nil {
		return nil, 0, err
	}
	return response.MediaContainer.Metadata, response.MediaContainer.TotalSize, nil
}

func uniqueResolutions(media []mediaItem) []string {
	seen := make(map[string]bool)
	var resolutions []string
	for _, version := range media {
		resolution := strings.TrimSpace(version.VideoResolution)
		if resolution != "" && !seen[strings.ToLower(resolution)] {
			seen[strings.ToLower(resolution)] = true
			resolutions = append(resolutions, resolution)
		}
	}
	sort.Slice(resolutions, func(i, j int) bool {
		if is4K(resolutions[i]) != is4K(resolutions[j]) {
			return is4K(resolutions[i])
		}
		return resolutions[i] > resolutions[j]
	})
	return resolutions
}

func hasMixed4K(resolutions []string) bool {
	has4K, hasOther := false, false
	for _, resolution := range resolutions {
		if is4K(resolution) {
			has4K = true
		} else {
			hasOther = true
		}
	}
	return has4K && hasOther
}

func is4K(resolution string) bool {
	resolution = strings.ToLower(strings.TrimSpace(resolution))
	return resolution == "4k" || strings.HasPrefix(resolution, "2160")
}

func serverEndpoint(baseURL, path string) (string, error) {
	base, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil {
		return "", fmt.Errorf("invalid Plex server URL")
	}
	relative, err := url.Parse(path)
	if err != nil || !strings.HasPrefix(relative.Path, "/") {
		return "", fmt.Errorf("invalid Plex endpoint path")
	}
	base.Path = strings.TrimRight(base.Path, "/") + relative.Path
	base.RawPath = ""
	base.RawQuery = relative.RawQuery
	base.Fragment = ""
	return base.String(), nil
}

func (c *Client) setPlexHeaders(request *http.Request) {
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Plex-Product", productName)
	request.Header.Set("X-Plex-Client-Identifier", c.clientID)
}

func (c *Client) doJSON(request *http.Request, destination any) error {
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("Plex request failed (%d): %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(destination); err != nil {
		return fmt.Errorf("decode Plex response: %w", err)
	}
	return nil
}

// distinctResolutions counts resolutions, treating every 4K spelling
// ("4k", "2160", "2160p") as the same resolution.
func distinctResolutions(resolutions []string) int {
	seen := make(map[string]bool)
	for _, resolution := range resolutions {
		if is4K(resolution) {
			seen["4k"] = true
		} else {
			seen[strings.ToLower(strings.TrimSpace(resolution))] = true
		}
	}
	return len(seen)
}

// hasUnknownResolution reports whether any version lacks a resolution. Such a
// movie can't be called single-resolution because the unknown version may differ.
func hasUnknownResolution(media []mediaItem) bool {
	for _, version := range media {
		if strings.TrimSpace(version.VideoResolution) == "" {
			return true
		}
	}
	return false
}
