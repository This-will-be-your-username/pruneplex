package web

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pruneplex/internal/lidarr"
	"pruneplex/internal/radarr"
)

// lidarrSettingsStore keeps the single Lidarr connection in lidarr.json, beside
// radarr.json.
type lidarrSettingsStore struct {
	mu    sync.RWMutex
	path  string // empty means in-memory only
	value lidarr.Instance
}

func newLidarrSettingsStore(radarrStore *radarrSettingsStore) *lidarrSettingsStore {
	store := &lidarrSettingsStore{}
	if radarrStore == nil || radarrStore.path == "" {
		return store
	}
	store.path = filepath.Join(filepath.Dir(radarrStore.path), "lidarr.json")
	if data, err := os.ReadFile(store.path); err == nil {
		_ = json.Unmarshal(data, &store.value)
	}
	return store
}

func (s *lidarrSettingsStore) get() lidarr.Instance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value
}

func (s *lidarrSettingsStore) save(value lidarr.Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path != "" {
		if err := writeFileAtomic(s.path, value); err != nil {
			return err
		}
	}
	s.value = value
	return nil
}

type lidarrHandlers struct {
	store  *lidarrSettingsStore
	client *http.Client
}

func newLidarrHandlers(store *lidarrSettingsStore) *lidarrHandlers {
	return &lidarrHandlers{store: store, client: &http.Client{Timeout: 60 * time.Second}}
}

func (h *lidarrHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/lidarr", h.getSettings)
	mux.HandleFunc("PUT /api/settings/lidarr", h.saveSettings)
	mux.HandleFunc("POST /api/settings/lidarr/test", h.testConnection)
	mux.HandleFunc("DELETE /api/settings/lidarr", h.clearSettings)
	mux.HandleFunc("GET /api/lidarr/library", h.library)
	mux.HandleFunc("POST /api/lidarr/album/remove", h.removeAlbum)
	mux.HandleFunc("POST /api/lidarr/artist/remove", h.removeArtist)
	mux.HandleFunc("GET /api/settings/lidarr/rootfolders", h.rootFolders)
	mux.HandleFunc("GET /api/lidarr/cover", h.cover)
}

func (h *lidarrHandlers) getSettings(w http.ResponseWriter, _ *http.Request) {
	instance := h.store.get()
	writeJSON(w, http.StatusOK, map[string]any{"url": instance.URL, "configured": instance.APIKey != "", "rootFolder": instance.RootFolder})
}

func (h *lidarrHandlers) saveSettings(w http.ResponseWriter, r *http.Request) {
	var input lidarr.Instance
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid Lidarr settings")
		return
	}
	merged := mergeInstance(input, h.store.get())
	if err := radarr.ValidateInstance(merged); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.save(merged); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not save Lidarr settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

func (h *lidarrHandlers) testConnection(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Instance lidarr.Instance `json:"instance"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid Lidarr connection settings")
		return
	}
	instance := mergeInstance(input.Instance, h.store.get())
	if err := radarr.ValidateInstance(instance); err != nil || instance.APIKey == "" {
		writeAPIError(w, http.StatusBadRequest, "Enter a Lidarr URL and API key")
		return
	}
	if err := lidarr.NewClient(h.client, instance).Test(r.Context()); err != nil {
		writeAPIError(w, http.StatusBadGateway, "Could not connect to that Lidarr instance")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"connected": true})
}

func (h *lidarrHandlers) clearSettings(w http.ResponseWriter, _ *http.Request) {
	if err := h.store.save(lidarr.Instance{}); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not save Lidarr settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// configured returns the saved instance, or writes an error and returns false.
func (h *lidarrHandlers) configured(w http.ResponseWriter) (lidarr.Instance, bool) {
	instance := h.store.get()
	if instance.APIKey == "" {
		writeAPIError(w, http.StatusBadRequest, "Connect Lidarr on the Settings page first")
		return instance, false
	}
	return instance, true
}

// --- Library ---

type musicAlbum struct {
	ID         int    `json:"id"`
	Title      string `json:"title"`
	Type       string `json:"type,omitempty"`
	Year       int    `json:"year,omitempty"`
	Monitored  bool   `json:"monitored"`
	Cover      string `json:"cover,omitempty"`
	TrackFiles int    `json:"trackFiles"`
	Tracks     int    `json:"tracks"`
	SizeBytes  int64  `json:"sizeBytes"`
}

type musicArtist struct {
	ID         int          `json:"id"`
	Name       string       `json:"name"`
	Status     string       `json:"status,omitempty"`
	Monitored  bool         `json:"monitored"`
	Cover      string       `json:"cover,omitempty"`
	TrackFiles int          `json:"trackFiles"`
	SizeBytes  int64        `json:"sizeBytes"`
	Albums     []musicAlbum `json:"albums"`
}

// musicStorage is the free space of the root folder holding the music.
type musicStorage struct {
	FreeBytes int64  `json:"freeBytes"`
	Folder    string `json:"folder"`
}

type musicLibrary struct {
	Artists   []musicArtist `json:"artists"`
	UsedBytes int64         `json:"usedBytes"`
	Storage   *musicStorage `json:"storage,omitempty"`
}

func (h *lidarrHandlers) library(w http.ResponseWriter, r *http.Request) {
	instance, ok := h.configured(w)
	if !ok {
		return
	}
	client := lidarr.NewClient(h.client, instance)
	artists, err := client.Artists(r.Context())
	if err != nil {
		log.Printf("Lidarr artist listing failed: %v", err)
		writeAPIError(w, http.StatusBadGateway, "Could not read the Lidarr artists")
		return
	}
	albums, err := client.Albums(r.Context())
	if err != nil {
		log.Printf("Lidarr album listing failed: %v", err)
		writeAPIError(w, http.StatusBadGateway, "Could not read the Lidarr albums")
		return
	}
	library := buildMusicLibrary(artists, albums)
	// Free space comes from the root folder holding the music (best effort: the
	// list is still useful if Lidarr cannot report it).
	if folders, err := client.RootFolders(r.Context()); err == nil {
		usage := newMediaUsage()
		for _, artist := range artists {
			usage.add(artist.RootFolderPath, artist.Statistics.SizeOnDisk)
		}
		if figures, ok := usage.figures(folders, instance.RootFolder); ok {
			library.Storage = &musicStorage{FreeBytes: figures.free, Folder: figures.folder}
		}
	}
	writeJSON(w, http.StatusOK, library)
}

// buildMusicLibrary groups albums under their artists, sorted for display:
// artists by sort name, albums newest first.
func buildMusicLibrary(artists []lidarr.Artist, albums []lidarr.Album) musicLibrary {
	byArtist := map[int][]musicAlbum{}
	for _, album := range albums {
		byArtist[album.ArtistID] = append(byArtist[album.ArtistID], musicAlbum{
			ID:         album.ID,
			Title:      album.Title,
			Type:       album.AlbumType,
			Year:       releaseYear(album.ReleaseDate),
			Monitored:  album.Monitored,
			Cover:      lidarr.CoverURL(album.Images, "cover"),
			TrackFiles: album.Statistics.TrackFileCount,
			Tracks:     album.Statistics.TrackCount,
			SizeBytes:  album.Statistics.SizeOnDisk,
		})
	}
	type entry struct {
		artist  musicArtist
		sortKey string
	}
	entries := make([]entry, 0, len(artists))
	var used int64
	for _, artist := range artists {
		list := byArtist[artist.ID]
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Year != list[j].Year {
				return list[i].Year > list[j].Year
			}
			return strings.ToLower(list[i].Title) < strings.ToLower(list[j].Title)
		})
		used += artist.Statistics.SizeOnDisk
		sortKey := artist.SortName
		if sortKey == "" {
			sortKey = artist.Name
		}
		entries = append(entries, entry{
			sortKey: strings.ToLower(sortKey),
			artist: musicArtist{
				ID:         artist.ID,
				Name:       artist.Name,
				Status:     artist.Status,
				Monitored:  artist.Monitored,
				Cover:      lidarr.CoverURL(artist.Images, "poster", "cover"),
				TrackFiles: artist.Statistics.TrackFileCount,
				SizeBytes:  artist.Statistics.SizeOnDisk,
				Albums:     list,
			},
		})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].sortKey < entries[j].sortKey })
	library := musicLibrary{Artists: make([]musicArtist, 0, len(entries)), UsedBytes: used}
	for _, e := range entries {
		library.Artists = append(library.Artists, e.artist)
	}
	return library
}

func releaseYear(date string) int {
	if len(date) < 4 {
		return 0
	}
	year, err := strconv.Atoi(date[:4])
	if err != nil || year < 1000 {
		return 0
	}
	return year
}

// --- Pruning an album ---

// removeAlbum prunes one album: it stops monitoring it (so Lidarr will not
// search for it again) and deletes its track files from disk. The album itself
// stays in Lidarr, never deleted.
func (h *lidarrHandlers) removeAlbum(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AlbumID int `json:"albumId"`
	}
	if err := decodeJSON(w, r, &input); err != nil || input.AlbumID <= 0 {
		writeAPIError(w, http.StatusBadRequest, "Invalid album")
		return
	}
	instance, ok := h.configured(w)
	if !ok {
		return
	}
	client := lidarr.NewClient(h.client, instance)

	files, err := client.TrackFiles(r.Context(), input.AlbumID)
	if err != nil {
		log.Printf("Lidarr track file listing failed: %v", err)
		writeAPIError(w, http.StatusBadGateway, "Could not read the album's tracks")
		return
	}
	// Unfollow first so nothing re-grabs the album once its files are gone.
	if err := client.SetAlbumMonitored(r.Context(), input.AlbumID, false); err != nil {
		log.Printf("Lidarr unmonitor failed: %v", err)
		writeAPIError(w, http.StatusBadGateway, "Could not stop monitoring the album; nothing was deleted")
		return
	}
	ids := make([]int, 0, len(files))
	var freed int64
	for _, file := range files {
		ids = append(ids, file.ID)
		freed += file.Size
	}
	if err := client.DeleteTrackFiles(r.Context(), ids); err != nil {
		log.Printf("Lidarr track file deletion failed: %v", err)
		writeAPIError(w, http.StatusBadGateway, "The album is no longer monitored, but its tracks could not be deleted")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removedFiles": len(ids), "freedBytes": freed, "monitored": false})
}

// --- Cover art ---

// cover proxies artwork from Lidarr, which needs the API key the browser lacks.
func (h *lidarrHandlers) cover(w http.ResponseWriter, r *http.Request) {
	instance := h.store.get()
	path := r.URL.Query().Get("path")
	if instance.APIKey == "" || path == "" {
		http.NotFound(w, r)
		return
	}
	response, err := lidarr.NewClient(h.client, instance).Cover(r.Context(), path)
	if err != nil {
		http.Error(w, "invalid cover", http.StatusBadRequest)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		http.NotFound(w, r)
		return
	}
	contentType := response.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = io.Copy(w, io.LimitReader(response.Body, 8<<20))
}


// removeArtist removes an artist from Lidarr entirely, with all its albums, and
// deletes its files from disk.
func (h *lidarrHandlers) removeArtist(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ArtistID int `json:"artistId"`
	}
	if err := decodeJSON(w, r, &input); err != nil || input.ArtistID <= 0 {
		writeAPIError(w, http.StatusBadRequest, "Invalid artist")
		return
	}
	instance, ok := h.configured(w)
	if !ok {
		return
	}
	if err := lidarr.NewClient(h.client, instance).RemoveArtist(r.Context(), input.ArtistID); err != nil {
		log.Printf("Lidarr artist removal failed: %v", err)
		writeAPIError(w, http.StatusBadGateway, "Could not remove the artist from Lidarr")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}

// rootFolders lists Lidarr's root folders for the Settings page's folder picker.
func (h *lidarrHandlers) rootFolders(w http.ResponseWriter, r *http.Request) {
	instance, ok := h.configured(w)
	if !ok {
		return
	}
	folders, err := lidarr.NewClient(h.client, instance).RootFolders(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "Could not read the root folders")
		return
	}
	options := make([]rootFolderOption, 0, len(folders))
	for _, folder := range folders {
		options = append(options, rootFolderOption{Path: folder.Path, FreeSpace: folder.FreeSpace})
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": options})
}
