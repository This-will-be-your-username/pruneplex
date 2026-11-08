package web

import (
	"context"
	"net/http"
	"sync"

	"pruneplex/internal/radarr"
	"pruneplex/internal/sonarr"
)

// instanceStorage summarizes one Radarr or Sonarr instance for the page header:
// how much its media uses, and the free space of the folder that media lives in.
type instanceStorage struct {
	Configured bool   `json:"configured"`
	UsedBytes  int64  `json:"usedBytes"`
	FreeBytes  int64  `json:"freeBytes"`
	Folder     string `json:"folder,omitempty"` // the root folder FreeBytes was read from
	Error      string `json:"error,omitempty"`
}

// storageFigures is what an instance-specific fetch returns.
type storageFigures struct {
	used   int64
	free   int64
	folder string
}

// mediaUsage totals bytes on disk, overall and per root folder.
type mediaUsage struct {
	total    int64
	byFolder map[string]int64
}

func newMediaUsage() *mediaUsage { return &mediaUsage{byFolder: map[string]int64{}} }

func (u *mediaUsage) add(rootFolderPath string, size int64) {
	u.total += size
	u.byFolder[radarr.NormalizePath(rootFolderPath)] += size
}

// figures combines usage with the root folder the free space should come from.
func (u *mediaUsage) figures(folders []radarr.RootFolder, configured string) (storageFigures, bool) {
	folder, ok := radarr.ChooseRootFolder(folders, configured, u.byFolder)
	if !ok {
		return storageFigures{}, false
	}
	return storageFigures{used: u.total, free: folder.FreeSpace, folder: folder.Path}, true
}

// storageFor gathers storage for the 4K and HD instances concurrently, since
// each needs a full library listing.
func storageFor(ctx context.Context, settings radarr.Settings, fetch func(context.Context, radarr.Instance) (storageFigures, error)) map[string]instanceStorage {
	instances := map[string]radarr.Instance{"fourK": settings.FourK, "hd": settings.HD}
	out := map[string]instanceStorage{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for name, instance := range instances {
		if instance.APIKey == "" {
			out[name] = instanceStorage{}
			continue
		}
		wg.Add(1)
		go func(name string, instance radarr.Instance) {
			defer wg.Done()
			result := instanceStorage{Configured: true}
			figures, err := fetch(ctx, instance)
			if err != nil {
				result.Error = "unavailable"
			} else {
				result.UsedBytes, result.FreeBytes, result.Folder = figures.used, figures.free, figures.folder
			}
			mu.Lock()
			out[name] = result
			mu.Unlock()
		}(name, instance)
	}
	wg.Wait()
	return out
}

var errNoRootFolders = &storageError{"no root folders reported"}

type storageError struct{ message string }

func (e *storageError) Error() string { return e.message }

func (h *radarrHandlers) storage(w http.ResponseWriter, r *http.Request) {
	settings, err := h.store.snapshot()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not read Radarr settings")
		return
	}
	writeJSON(w, http.StatusOK, storageFor(r.Context(), settings, func(ctx context.Context, instance radarr.Instance) (storageFigures, error) {
		client := radarr.NewClient(h.client, instance)
		movies, err := client.Movies(ctx)
		if err != nil {
			return storageFigures{}, err
		}
		folders, err := client.RootFolders(ctx)
		if err != nil {
			return storageFigures{}, err
		}
		usage := newMediaUsage()
		for _, movie := range movies {
			usage.add(movie.RootFolderPath, movie.SizeOnDisk)
		}
		figures, ok := usage.figures(folders, instance.RootFolder)
		if !ok {
			return storageFigures{}, errNoRootFolders
		}
		return figures, nil
	}))
}

func (h *sonarrHandlers) storage(w http.ResponseWriter, r *http.Request) {
	settings, err := h.store.snapshot()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not read Sonarr settings")
		return
	}
	writeJSON(w, http.StatusOK, storageFor(r.Context(), settings, func(ctx context.Context, instance radarr.Instance) (storageFigures, error) {
		client := sonarr.NewClient(h.client, instance)
		series, err := client.Series(ctx)
		if err != nil {
			return storageFigures{}, err
		}
		folders, err := client.RootFolders(ctx)
		if err != nil {
			return storageFigures{}, err
		}
		usage := newMediaUsage()
		for _, s := range series {
			usage.add(s.RootFolderPath, s.SizeOnDisk())
		}
		figures, ok := usage.figures(folders, instance.RootFolder)
		if !ok {
			return storageFigures{}, errNoRootFolders
		}
		return figures, nil
	}))
}

// --- Root folders, for the Settings page's folder picker ---

type rootFolderOption struct {
	Path      string `json:"path"`
	FreeSpace int64  `json:"freeSpace"`
}

// rootFolders lists the saved instance's root folders so the user can pick the
// one whose free space is reported.
func rootFolders(store *radarrSettingsStore, w http.ResponseWriter, r *http.Request, list func(context.Context, radarr.Instance) ([]radarr.RootFolder, error)) {
	settings, err := store.snapshot()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not read settings")
		return
	}
	instance, err := configuredInstance(settings, r.URL.Query().Get("quality"))
	if err != nil || instance.APIKey == "" {
		writeAPIError(w, http.StatusBadRequest, "That instance is not configured")
		return
	}
	folders, err := list(r.Context(), instance)
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

func (h *radarrHandlers) rootFolders(w http.ResponseWriter, r *http.Request) {
	rootFolders(h.store, w, r, func(ctx context.Context, instance radarr.Instance) ([]radarr.RootFolder, error) {
		return radarr.NewClient(h.client, instance).RootFolders(ctx)
	})
}

func (h *sonarrHandlers) rootFolders(w http.ResponseWriter, r *http.Request) {
	rootFolders(h.store, w, r, func(ctx context.Context, instance radarr.Instance) ([]radarr.RootFolder, error) {
		return sonarr.NewClient(h.client, instance).RootFolders(ctx)
	})
}
