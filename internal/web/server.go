package web

import (
	"embed"
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed templates/*.html assets/*.css assets/*.js
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

	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
	mux.HandleFunc("GET /healthz", health)
	newRadarrHandlers(settings).register(mux)
	newPlexConnector(settings).register(mux)
	mux.HandleFunc("GET /", index)
	return mux
}

func index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := page.ExecuteTemplate(w, "index.html", nil); err != nil {
		http.Error(w, "could not render page", http.StatusInternalServerError)
	}
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
