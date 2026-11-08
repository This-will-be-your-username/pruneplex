package web

import (
	"path/filepath"
	"testing"
)

func TestPlexAuthStorePersistsAcrossLaunches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plex.json")

	first := newPlexAuthStore(path)
	if err := first.update(func(a *plexAuth) {
		a.ClientID = "client-1"
		a.AccountToken = "token-1"
		a.ServerID = "server-1"
	}); err != nil {
		t.Fatal(err)
	}

	got := newPlexAuthStore(path).get()
	if got.ClientID != "client-1" || got.AccountToken != "token-1" || got.ServerID != "server-1" {
		t.Fatalf("reloaded auth = %+v", got)
	}

	connector := newPlexConnector(&radarrSettingsStore{path: filepath.Join(filepath.Dir(path), "radarr.json")})
	if connector.clientID != "client-1" {
		t.Fatalf("client ID not reused across launches: %q", connector.clientID)
	}

	connector.persistAuth(func(a *plexAuth) { a.AccountToken, a.ServerID = "", "" })
	if got := newPlexAuthStore(path).get(); got.AccountToken != "" || got.ClientID != "client-1" {
		t.Fatalf("after disconnect = %+v", got)
	}
}
