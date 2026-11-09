package web

import (
	"embed"
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
)

//go:embed templates/*.html assets/*.css assets/*.js assets/*.svg
var files embed.FS

var page = template.Must(template.ParseFS(files, "templates/*.html"))

func NewHandler() http.Handler {
	return newHandler(newRadarrSettingsStore())
}

func newHandler(settings *radarrSettingsStore) http.Handler {
	mux := http.NewServeMux()
	assets, err := fs.Sub(files, "assets")
	if err != nil {
		panic(err)
	}
	sonarrSettings := newSonarrSettingsStore(settings)

	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
	mux.HandleFunc("GET /healthz", health)
	newRadarrHandlers(settings).register(mux)
	newSonarrHandlers(sonarrSettings).register(mux)
	newLidarrHandlers(newLidarrSettingsStore(settings)).register(mux)
	connector := newPlexConnector(settings)
	connector.sonarr = sonarrSettings
	connector.register(mux)
	mux.HandleFunc("GET /tv", tvIndex)
	mux.HandleFunc("GET /music", musicIndex)
	mux.HandleFunc("GET /", index)
	return mux
}

// newSonarrSettingsStore keeps Sonarr settings in sonarr.json beside radarr.json.
func newSonarrSettingsStore(radarrStore *radarrSettingsStore) *radarrSettingsStore {
	if radarrStore == nil || radarrStore.path == "" {
		return &radarrSettingsStore{}
	}
	return newRadarrSettingsStoreAt(filepath.Join(filepath.Dir(radarrStore.path), "sonarr.json"))
}

// overviewPage holds the wording that differs between the Movies and TV Shows pages.
type overviewPage struct {
	TV       bool
	Media    string // value of <body data-media>
	Noun     string // plural, lower case
	NounCap  string
	Singular string // capitalised
	Arr      string
	Headline string
	Intro    string
	Analyze  string
}

var (
	moviesPage = overviewPage{
		Media: "movies", Noun: "movies", NounCap: "Movies", Singular: "Movie", Arr: "Radarr",
		Headline: "Select a movie library",
		Intro:    "Connect your Plex account to choose a server and scan its movie libraries.",
		Analyze:  "Analyze movies",
	}
	tvPage = overviewPage{
		TV: true, Media: "tv", Noun: "shows", NounCap: "Shows", Singular: "Show", Arr: "Sonarr",
		Headline: "Select a TV library",
		Intro:    "Connect your Plex account to choose a server and scan its TV libraries.",
		Analyze:  "Analyze shows",
	}
)

func index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	renderOverview(w, moviesPage)
}

func tvIndex(w http.ResponseWriter, _ *http.Request) {
	renderOverview(w, tvPage)
}

func renderOverview(w http.ResponseWriter, data overviewPage) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := page.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, "could not render page", http.StatusInternalServerError)
	}
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func musicIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := page.ExecuteTemplate(w, "music.html", nil); err != nil {
		http.Error(w, "could not render page", http.StatusInternalServerError)
	}
}
