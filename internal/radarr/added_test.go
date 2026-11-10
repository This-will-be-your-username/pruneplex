package radarr

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseTimestampTreatsPlaceholdersAsUnknown(t *testing.T) {
	want := time.Date(2024, 5, 1, 12, 30, 0, 0, time.UTC).Unix()
	for text, expected := range map[string]int64{
		"2024-05-01T12:30:00Z":        want,
		"2024-05-01T12:30:00.123456Z": want, // fractional seconds
		"2024-05-01T14:30:00+02:00":   want, // offsets
		"0001-01-01T00:00:00Z":        0,    // Radarr's "never"
		"":                            0,
		"not a date":                  0,
	} {
		if got := ParseTimestamp(text); got != expected {
			t.Errorf("ParseTimestamp(%q) = %d, want %d", text, got, expected)
		}
	}
}

func TestMovieImportDateComesFromItsFileNotItsFileTimestamp(t *testing.T) {
	var movies []Movie
	err := json.Unmarshal([]byte(`[
		{"id":1,"title":"With file","added":"2020-01-01T00:00:00Z","movieFile":{"dateAdded":"2024-05-01T12:30:00Z"}},
		{"id":2,"title":"No file","added":"2020-01-01T00:00:00Z","movieFile":null},
		{"id":3,"title":"Never imported","movieFile":{"dateAdded":"0001-01-01T00:00:00Z"}}]`), &movies)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2024, 5, 1, 12, 30, 0, 0, time.UTC).Unix()
	if got := movies[0].ImportedAt(); got != want {
		t.Fatalf("ImportedAt = %d, want %d", got, want)
	}
	if movies[1].ImportedAt() != 0 || movies[2].ImportedAt() != 0 {
		t.Fatal("a movie with no file, or a placeholder date, has no import date")
	}
}
