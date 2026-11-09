// Package ratings looks up extra movie ratings (OMDb and Trakt) in the
// background and caches them on disk, so a library of a thousand movies fills in
// gradually without hammering the providers or slowing the page down.
package ratings

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// Rating is one score. Value is always on a 0-10 scale, whatever the provider
// natively uses, to match how the page renders Plex's own ratings.
type Rating struct {
	Source string  `json:"source"` // "imdb", "rotten_tomatoes", "metacritic", "trakt"
	Type   string  `json:"type"`   // "critic" or "audience"
	Value  float64 `json:"value"`
	URL    string  `json:"url,omitempty"` // where the provider shows this movie, when known
}

// Settings are the credentials for the providers. A provider with no credential
// is simply not queried.
type Settings struct {
	OMDbKey       string `json:"omdbKey,omitempty"`
	TraktClientID string `json:"traktClientId,omitempty"`
}

// Ref identifies a movie to look up. Key is whatever the caller wants echoed
// back; IMDBID and TMDBID are what the providers are queried by.
type Ref struct {
	Key    string
	IMDBID string
	TMDBID int
}

// Result is what is known so far about one movie. Complete is false while some
// configured provider has not answered yet.
type Result struct {
	Ratings  []Rating `json:"ratings"`
	Complete bool     `json:"complete"`
}

var (
	// ErrNotFound means the provider does not know the movie (or has no score).
	ErrNotFound = errors.New("not found")
	// ErrQuota means the provider's rate or daily limit was hit.
	ErrQuota = errors.New("quota exceeded")
	// ErrAuth means the provider rejected the credential.
	ErrAuth = errors.New("invalid credential")
)

const (
	providerOMDb  = "omdb"
	providerTrakt = "trakt"

	freshFor    = 30 * 24 * time.Hour // how long a found rating is trusted
	notFoundFor = 7 * 24 * time.Hour  // how long "no such movie" is trusted
)

// cacheKey is what a movie is cached under: its IMDb ID, else its TMDB ID.
func cacheKey(ref Ref) string {
	if id := strings.ToLower(strings.TrimSpace(ref.IMDBID)); id != "" {
		return id
	}
	if ref.TMDBID > 0 {
		return "tmdb:" + strconv.Itoa(ref.TMDBID)
	}
	return ""
}
