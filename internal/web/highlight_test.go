package web

import (
	"testing"

	"pruneplex/internal/plex"
)

func TestHighlightTop4KVideos(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		res := HighlightTop4KVideos(nil)
		if len(res) != 0 {
			t.Fatalf("expected empty result, got %v", res)
		}
	})

	t.Run("no 4K sizes available", func(t *testing.T) {
		movies := []analyzedMovie{
			{Movie: plex.Movie{Title: "A"}},
			{Movie: plex.Movie{Title: "B"}},
		}
		res := HighlightTop4KVideos(movies)
		for _, m := range res {
			if m.IsTop20Percent4K {
				t.Fatalf("expected no movie to be highlighted, got %+v", m)
			}
		}
	})

	t.Run("top 20 percent of 5 movies is 1 movie", func(t *testing.T) {
		movies := []analyzedMovie{
			{Movie: plex.Movie{Title: "10GB"}, Radarr4K: &radarrMovieMatch{SizeBytes: 10}},
			{Movie: plex.Movie{Title: "20GB"}, Radarr4K: &radarrMovieMatch{SizeBytes: 20}},
			{Movie: plex.Movie{Title: "30GB"}, Radarr4K: &radarrMovieMatch{SizeBytes: 30}},
			{Movie: plex.Movie{Title: "40GB"}, Radarr4K: &radarrMovieMatch{SizeBytes: 40}},
			{Movie: plex.Movie{Title: "50GB"}, Radarr4K: &radarrMovieMatch{SizeBytes: 50}},
		}
		res := HighlightTop4KVideos(movies)
		for _, m := range res {
			if m.Title == "50GB" && !m.IsTop20Percent4K {
				t.Errorf("expected 50GB to be highlighted")
			}
			if m.Title != "50GB" && m.IsTop20Percent4K {
				t.Errorf("expected %s not to be highlighted", m.Title)
			}
		}
	})

	t.Run("top 20 percent of 10 movies is 2 movies", func(t *testing.T) {
		movies := make([]analyzedMovie, 10)
		for i := 0; i < 10; i++ {
			movies[i] = analyzedMovie{
				Movie:    plex.Movie{Title: string(rune('A' + i))},
				Radarr4K: &radarrMovieMatch{SizeBytes: int64((i + 1) * 10)},
			}
		}
		res := HighlightTop4KVideos(movies)
		var highlighted []string
		for _, m := range res {
			if m.IsTop20Percent4K {
				highlighted = append(highlighted, m.Title)
			}
		}
		if len(highlighted) != 2 {
			t.Fatalf("expected 2 highlighted movies, got %d: %v", len(highlighted), highlighted)
		}
		if highlighted[0] != "I" || highlighted[1] != "J" {
			t.Fatalf("expected I and J to be highlighted, got %v", highlighted)
		}
	})

	t.Run("ties at top boundary are both highlighted", func(t *testing.T) {
		movies := []analyzedMovie{
			{Movie: plex.Movie{Title: "A"}, Radarr4K: &radarrMovieMatch{SizeBytes: 100}},
			{Movie: plex.Movie{Title: "B"}, Radarr4K: &radarrMovieMatch{SizeBytes: 100}},
			{Movie: plex.Movie{Title: "C"}, Radarr4K: &radarrMovieMatch{SizeBytes: 50}},
			{Movie: plex.Movie{Title: "D"}, Radarr4K: &radarrMovieMatch{SizeBytes: 30}},
			{Movie: plex.Movie{Title: "E"}, Radarr4K: &radarrMovieMatch{SizeBytes: 10}},
		}
		res := HighlightTop4KVideos(movies)
		if !res[0].IsTop20Percent4K || !res[1].IsTop20Percent4K {
			t.Fatalf("expected both tied top movies to be highlighted")
		}
		if res[2].IsTop20Percent4K || res[3].IsTop20Percent4K || res[4].IsTop20Percent4K {
			t.Fatalf("expected other movies not to be highlighted")
		}
	})

	t.Run("supports Plex FourKSizeBytes fallback", func(t *testing.T) {
		movies := []analyzedMovie{
			{Movie: plex.Movie{Title: "A", FourKSizeBytes: 50}},
			{Movie: plex.Movie{Title: "B", FourKSizeBytes: 200}},
			{Movie: plex.Movie{Title: "C", FourKSizeBytes: 10}},
		}
		res := HighlightTop4KVideos(movies)
		if !res[1].IsTop20Percent4K {
			t.Fatalf("expected B (200) to be highlighted")
		}
		if res[0].IsTop20Percent4K || res[2].IsTop20Percent4K {
			t.Fatalf("unexpected highlight on A or C")
		}
	})
}
