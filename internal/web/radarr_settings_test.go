package web

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"pruneplex/internal/radarr"
)

func TestRadarrSettingsStorePersistsPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings", "radarr.json")
	store := newRadarrSettingsStoreAt(path)
	want := radarr.Settings{
		FourK: radarr.Instance{URL: "http://radarr-4k:7878", APIKey: "four-k-secret"},
		HD:    radarr.Instance{URL: "http://radarr-hd:7878", APIKey: "hd-secret"},
	}
	if err := store.save(want); err != nil {
		t.Fatal(err)
	}

	loaded, err := newRadarrSettingsStoreAt(path).snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if loaded != want {
		t.Fatalf("got settings %+v, want %+v", loaded, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows does not implement Unix permission bits.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("settings file permissions are %o, want 600", info.Mode().Perm())
	}
}

func TestMergeSettingsKeepsSavedAPIKeysWhenFieldsAreBlank(t *testing.T) {
	current := radarr.Settings{FourK: radarr.Instance{URL: "http://radarr:7878", APIKey: "saved"}}
	merged, err := mergeSettings(radarr.Settings{FourK: radarr.Instance{URL: "http://radarr:7878"}}, current)
	if err != nil {
		t.Fatal(err)
	}
	if merged.FourK.APIKey != "saved" {
		t.Fatalf("saved API key was not retained: %+v", merged.FourK)
	}
}
