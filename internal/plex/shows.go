package plex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Show is a TV series with the resolutions found across all of its episodes.
type Show struct {
	Title   string        `json:"title"`
	Year    int           `json:"year,omitempty"`
	Thumb   string        `json:"thumb,omitempty"`
	Library string        `json:"library,omitempty"`
	Ratings []MovieRating `json:"ratings,omitempty"`
	// Resolutions lists each distinct resolution, 4K first. ResolutionEpisodes
	// counts the episodes that have a version at that resolution.
	Resolutions        []string       `json:"resolutions"`
	ResolutionEpisodes map[string]int `json:"resolutionEpisodes,omitempty"`
	Episodes           int            `json:"episodes"`
	TVDBID             int            `json:"tvdbId,omitempty"`
	TMDBID             int            `json:"tmdbId,omitempty"`
	IMDBID             string         `json:"imdbId,omitempty"`
	FourKSizeBytes     int64          `json:"fourKSizeBytes,omitempty"`
}

type ShowAnalysis struct {
	Scanned int    `json:"scanned"`
	Matches []Show `json:"matches"`
	Only4K  []Show `json:"only4k"`
	OnlyHD  []Show `json:"onlyHd"`
	Other   []Show `json:"other"`
}

// libraryEntry decodes either a show (type=2) or an episode (type=4) listing item.
type libraryEntry struct {
	libraryMovie
	RatingKey            flexString `json:"ratingKey"`
	GrandparentRatingKey flexString `json:"grandparentRatingKey"`
}

type flexString string

func (s *flexString) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		*s = ""
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*s = flexString(value)
		return nil
	}
	*s = flexString(data)
	return nil
}

type showAggregate struct {
	show     Show
	counts   map[string]int
	unknown  bool
	sizeFour int64
}

// AnalyzeShows scans one TV library, or all of them when libraryKey is empty,
// and classifies each show by the resolutions across all its episodes.
func (c *Client) AnalyzeShows(ctx context.Context, baseURL, token, libraryKey string) (ShowAnalysis, error) {
	libraries, err := c.ShowLibraries(ctx, baseURL, token)
	if err != nil {
		return ShowAnalysis{}, err
	}
	if libraryKey != "" {
		var selected []Library
		for _, library := range libraries {
			if library.Key == libraryKey {
				selected = append(selected, library)
			}
		}
		if len(selected) == 0 {
			return ShowAnalysis{}, ErrUnknownLibrary
		}
		libraries = selected
	}

	var result ShowAnalysis
	for _, section := range libraries {
		sectionID := url.PathEscape(section.Key)
		aggregates := map[string]*showAggregate{}
		var order []string

		// Pass 1: the shows themselves (title, year, ratings, IDs).
		err := c.eachEntry(ctx, baseURL, token, sectionID, "2", func(entry libraryEntry) {
			key := string(entry.RatingKey)
			if key == "" || aggregates[key] != nil {
				return
			}
			tvdbID, tmdbID, imdbID := parseAllIDs(entry.Guids)
			aggregates[key] = &showAggregate{
				counts: map[string]int{},
				show: Show{
					Title:   entry.Title,
					Year:    entry.Year,
					Thumb:   entry.Thumb,
					Library: section.Title,
					Ratings: consolidateRatings(entry.libraryMovie),
					TVDBID:  tvdbID,
					TMDBID:  tmdbID,
					IMDBID:  imdbID,
				},
			}
			order = append(order, key)
		})
		if err != nil {
			return ShowAnalysis{}, fmt.Errorf("read TV library %q: %w", section.Title, err)
		}

		// Pass 2: every episode, folded into its show.
		err = c.eachEntry(ctx, baseURL, token, sectionID, "4", func(entry libraryEntry) {
			aggregate := aggregates[string(entry.GrandparentRatingKey)]
			if aggregate == nil {
				return
			}
			aggregate.show.Episodes++
			seen := map[string]bool{}
			for _, media := range entry.Media {
				resolution := strings.TrimSpace(media.VideoResolution)
				if resolution == "" {
					aggregate.unknown = true
					continue
				}
				if is4K(resolution) {
					resolution = "4k"
				}
				resolution = strings.ToLower(resolution)
				if !seen[resolution] {
					seen[resolution] = true
					aggregate.counts[resolution]++
				}
			}
			aggregate.sizeFour += extract4KSize(entry.Media)
		})
		if err != nil {
			return ShowAnalysis{}, fmt.Errorf("read TV episodes in %q: %w", section.Title, err)
		}

		result.Scanned += len(order)
		for _, key := range order {
			aggregate := aggregates[key]
			show := aggregate.show
			var media []mediaItem
			for resolution := range aggregate.counts {
				media = append(media, mediaItem{VideoResolution: resolution})
			}
			show.Resolutions = uniqueResolutions(media)
			show.ResolutionEpisodes = aggregate.counts
			show.FourKSizeBytes = aggregate.sizeFour
			switch {
			case hasMixed4K(show.Resolutions):
				result.Matches = append(result.Matches, show)
			case aggregate.unknown, distinctResolutions(show.Resolutions) != 1:
				result.Other = append(result.Other, show)
			case is4K(show.Resolutions[0]):
				result.Only4K = append(result.Only4K, show)
			default:
				result.OnlyHD = append(result.OnlyHD, show)
			}
		}
	}
	for _, list := range [][]Show{result.Matches, result.Only4K, result.OnlyHD, result.Other} {
		sortShows(list)
	}
	return result, nil
}

func sortShows(shows []Show) {
	sort.Slice(shows, func(i, j int) bool {
		if shows[i].Title == shows[j].Title {
			return shows[i].Year < shows[j].Year
		}
		return strings.ToLower(shows[i].Title) < strings.ToLower(shows[j].Title)
	})
}

// eachEntry pages through a library section's items of one Plex type
// ("2" shows, "4" episodes) and calls fn for each. Plex sometimes answers a
// large page of episodes with a 500, so a failing page is retried with smaller
// and smaller pages; a single item that still fails is logged and skipped
// rather than aborting the whole scan.
func (c *Client) eachEntry(ctx context.Context, baseURL, token, sectionID, plexType string, fn func(libraryEntry)) error {
	sizes := []int{500}
	if plexType != "2" {
		sizes = []int{100, 20, 1}
	}
	start, level, skipped := 0, 0, 0
	for {
		entries, total, err := c.entryPage(ctx, baseURL, token, sectionID, plexType, start, sizes[level])
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			if level < len(sizes)-1 {
				level++
				continue
			}
			if sizes[level] == 1 && plexType != "2" {
				skipped++
				if skipped > 10 {
					return err // Plex is failing consistently, not on one bad item
				}
				log.Printf("Plex: skipping unreadable item %d in section %s: %v", start, sectionID, err)
				start++
				level = 0
				if total > 0 && start >= total {
					return nil
				}
				continue
			}
			return err
		}
		level, skipped = 0, 0
		for _, entry := range entries {
			fn(entry)
		}
		if len(entries) == 0 || (total > 0 && start+len(entries) >= total) {
			return nil
		}
		start += len(entries)
	}
}

func (c *Client) entryPage(ctx context.Context, baseURL, token, sectionID, plexType string, start, size int) ([]libraryEntry, int, error) {
	// Only shows need GUIDs; episodes carry heavy media details.
	query := "?type=" + plexType
	if plexType == "2" {
		query += "&includeGuids=1"
	}
	endpoint, err := serverEndpoint(baseURL, "/library/sections/"+sectionID+"/all"+query)
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
	request.Header.Set("X-Plex-Container-Size", strconv.Itoa(size))
	var response struct {
		MediaContainer struct {
			TotalSize int            `json:"totalSize"`
			Metadata  []libraryEntry `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if err := c.doJSON(request, &response); err != nil {
		return nil, 0, err
	}
	return response.MediaContainer.Metadata, response.MediaContainer.TotalSize, nil
}
