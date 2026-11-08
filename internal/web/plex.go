package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"space-analyzer/internal/plex"
	"space-analyzer/internal/radarr"
)

const plexSessionCookie = "space_analyzer_session"

type plexConnector struct {
	mu       sync.Mutex
	client   *plex.Client
	clientID string
	sessions map[string]*plexSession
	radarr   *radarrSettingsStore
}

type plexSession struct {
	pinID        int64
	pinCode      string
	pinCreatedAt time.Time
	accountToken string
	servers      []plex.Resource
	selected     *connectedServer
}

type connectedServer struct {
	id      string
	name    string
	baseURL string
	token   string
}

type serverOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func newPlexConnector(settings *radarrSettingsStore) *plexConnector {
	clientID := randomID()
	return &plexConnector{
		client:   plex.NewClient(&http.Client{Timeout: 30 * time.Second}, clientID),
		clientID: clientID,
		sessions: make(map[string]*plexSession),
		radarr:   settings,
	}
}

func (c *plexConnector) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/plex/status", c.status)
	mux.HandleFunc("POST /api/plex/auth/start", c.startAuth)
	mux.HandleFunc("GET /api/plex/auth/status", c.authStatus)
	mux.HandleFunc("POST /api/plex/server", c.selectServer)
	mux.HandleFunc("POST /api/plex/disconnect", c.disconnect)
	mux.HandleFunc("POST /api/plex/analyze", c.analyze)
	mux.HandleFunc("GET /api/plex/thumb", c.thumb)
}

func (c *plexConnector) status(w http.ResponseWriter, r *http.Request) {
	session, _ := c.getSession(w, r, false)
	if session == nil {
		writeJSON(w, http.StatusOK, map[string]any{"connected": false})
		return
	}

	c.mu.Lock()
	selected := session.selected
	accountToken := session.accountToken
	servers := append([]plex.Resource(nil), session.servers...)
	c.mu.Unlock()

	response := map[string]any{"connected": selected != nil}
	if selected != nil {
		response["serverName"] = selected.name
	} else if accountToken != "" {
		response["servers"] = serverOptions(servers)
	}
	writeJSON(w, http.StatusOK, response)
}

func (c *plexConnector) startAuth(w http.ResponseWriter, r *http.Request) {
	session, _ := c.getSession(w, r, true)
	pin, err := c.client.CreatePIN(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "Could not start Plex authentication")
		return
	}

	c.mu.Lock()
	session.pinID = pin.ID
	session.pinCode = pin.Code
	session.pinCreatedAt = time.Now()
	session.accountToken = ""
	session.servers = nil
	session.selected = nil
	c.mu.Unlock()

	authURL := "https://app.plex.tv/auth#?clientID=" + c.clientID + "&code=" + pin.Code + "&context%5Bdevice%5D%5Bproduct%5D=PrunePlex"
	writeJSON(w, http.StatusOK, map[string]string{"authURL": authURL})
}

func (c *plexConnector) authStatus(w http.ResponseWriter, r *http.Request) {
	session, _ := c.getSession(w, r, false)
	if session == nil {
		writeJSON(w, http.StatusOK, map[string]string{"state": "disconnected"})
		return
	}

	c.mu.Lock()
	pinID, pinCode, pinCreatedAt, accountToken := session.pinID, session.pinCode, session.pinCreatedAt, session.accountToken
	c.mu.Unlock()

	if accountToken == "" && pinID != 0 {
		if time.Since(pinCreatedAt) > 10*time.Minute {
			c.mu.Lock()
			session.pinID = 0
			session.pinCode = ""
			c.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]string{"state": "expired"})
			return
		}
		pinStatus, err := c.client.PollPIN(r.Context(), pinID, pinCode)
		if err != nil {
			writeAPIError(w, http.StatusBadGateway, "Could not check Plex authentication")
			return
		}
		if pinStatus.AuthToken != "" {
			c.mu.Lock()
			session.accountToken = pinStatus.AuthToken
			session.pinID = 0
			session.pinCode = ""
			accountToken = session.accountToken
			c.mu.Unlock()
		}
	}

	if accountToken == "" {
		writeJSON(w, http.StatusOK, map[string]string{"state": "pending"})
		return
	}

	c.mu.Lock()
	serversLoaded := session.servers != nil
	c.mu.Unlock()
	if !serversLoaded {
		servers, err := c.client.Resources(r.Context(), accountToken)
		if err != nil {
			writeAPIError(w, http.StatusBadGateway, "Signed in to Plex, but could not list servers")
			return
		}
		c.mu.Lock()
		session.servers = servers
		c.mu.Unlock()
	}

	c.mu.Lock()
	options := serverOptions(append([]plex.Resource(nil), session.servers...))
	connected := session.selected != nil
	c.mu.Unlock()
	if connected {
		writeJSON(w, http.StatusOK, map[string]any{"state": "connected"})
	} else if len(options) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"state": "no-servers"})
	} else {
		writeJSON(w, http.StatusOK, map[string]any{"state": "select-server", "servers": options})
	}
}

func (c *plexConnector) selectServer(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(w, r, &input); err != nil || input.ID == "" {
		writeAPIError(w, http.StatusBadRequest, "Choose a Plex server")
		return
	}
	session, _ := c.getSession(w, r, false)
	if session == nil {
		writeAPIError(w, http.StatusUnauthorized, "Authenticate with Plex first")
		return
	}

	c.mu.Lock()
	var resource *plex.Resource
	for index := range session.servers {
		if session.servers[index].ClientID == input.ID {
			copy := session.servers[index]
			resource = &copy
			break
		}
	}
	c.mu.Unlock()
	if resource == nil {
		writeAPIError(w, http.StatusBadRequest, "That Plex server is not available")
		return
	}

	connections := append([]plex.Connection(nil), resource.Connections...)
	sort.SliceStable(connections, func(i, j int) bool {
		if connections[i].Relay != connections[j].Relay {
			return !connections[i].Relay
		}
		if connections[i].Local != connections[j].Local {
			return connections[i].Local
		}
		return false
	})
	for _, connection := range connections {
		if err := c.client.VerifyServer(r.Context(), connection.URI, resource.AccessToken, resource.ClientID); err != nil {
			continue
		}
		c.mu.Lock()
		session.selected = &connectedServer{
			id:      resource.ClientID,
			name:    resource.Name,
			baseURL: connection.URI,
			token:   resource.AccessToken,
		}
		c.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"connected": true, "serverName": resource.Name})
		return
	}
	writeAPIError(w, http.StatusBadGateway, "Could not reach that Plex server")
}

func (c *plexConnector) disconnect(w http.ResponseWriter, r *http.Request) {
	if sessionID := cookieValue(r); sessionID != "" {
		c.mu.Lock()
		delete(c.sessions, sessionID)
		c.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     plexSessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"connected": false})
}

func (c *plexConnector) analyze(w http.ResponseWriter, r *http.Request) {
	session, _ := c.getSession(w, r, false)
	if session == nil {
		writeAPIError(w, http.StatusUnauthorized, "Authenticate with Plex and select a server first")
		return
	}
	c.mu.Lock()
	var server *connectedServer
	if session.selected != nil {
		copy := *session.selected
		server = &copy
	}
	c.mu.Unlock()
	if server == nil {
		writeAPIError(w, http.StatusUnauthorized, "Authenticate with Plex and select a server first")
		return
	}

	result, err := c.client.AnalyzeMovies(r.Context(), server.baseURL, server.token)
	if err != nil {
		log.Printf("Plex movie analysis failed: %v", err)
		writeAPIError(w, http.StatusBadGateway, "Plex movie analysis failed")
		return
	}
	settings, err := c.radarr.snapshot()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Could not read Radarr settings")
		return
	}
	fourKMovies, err := listRadarrMovies(r.Context(), settings.FourK)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "Could not read the 4K Radarr library")
		return
	}
	hdMovies, err := listRadarrMovies(r.Context(), settings.HD)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "Could not read the HD Radarr library")
		return
	}
	response := analysisResponse{Scanned: result.Scanned, Matches: make([]analyzedMovie, 0, len(result.Matches))}
	for _, movie := range result.Matches {
		match := analyzedMovie{
			Movie:              movie,
			Radarr4KConfigured: settings.FourK.APIKey != "",
			RadarrHDConfigured: settings.HD.APIKey != "",
		}
		identity := radarr.Identity{Title: movie.Title, Year: movie.Year, TMDBID: movie.TMDBID, IMDBID: movie.IMDBID}
		if radarrMovie, ok := radarr.FindMovie(identity, fourKMovies); ok {
			match.Radarr4K = &radarrMovieMatch{ID: radarrMovie.ID, Title: radarrMovie.Title, Year: radarrMovie.Year, SizeBytes: radarrMovie.SizeOnDisk}
		}
		if radarrMovie, ok := radarr.FindMovie(identity, hdMovies); ok {
			match.RadarrHD = &radarrMovieMatch{ID: radarrMovie.ID, Title: radarrMovie.Title, Year: radarrMovie.Year, SizeBytes: radarrMovie.SizeOnDisk}
		}
		response.Matches = append(response.Matches, match)
	}
	response.Matches = HighlightTop4KVideos(response.Matches)
	writeJSON(w, http.StatusOK, response)
}

func (c *plexConnector) thumb(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = r.URL.Query().Get("thumb")
	}
	if path == "" || !strings.HasPrefix(path, "/") || strings.Contains(path, "..") || strings.Contains(path, "://") {
		http.Error(w, "invalid thumb path", http.StatusBadRequest)
		return
	}

	session, _ := c.getSession(w, r, false)
	if session == nil {
		writeAPIError(w, http.StatusUnauthorized, "Authenticate with Plex and select a server first")
		return
	}

	c.mu.Lock()
	var server *connectedServer
	if session.selected != nil {
		copy := *session.selected
		server = &copy
	}
	c.mu.Unlock()
	if server == nil {
		writeAPIError(w, http.StatusUnauthorized, "Authenticate with Plex and select a server first")
		return
	}

	resp, err := c.client.FetchMedia(r.Context(), server.baseURL, server.token, path)
	if err != nil {
		log.Printf("Plex thumb fetch failed: %v", err)
		http.Error(w, "could not fetch cover art", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		http.Error(w, "cover art not found", resp.StatusCode)
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=604800")
	if length := resp.Header.Get("Content-Length"); length != "" {
		w.Header().Set("Content-Length", length)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, resp.Body)
}

type analysisResponse struct {
	Scanned int             `json:"scanned"`
	Matches []analyzedMovie `json:"matches"`
}

type analyzedMovie struct {
	plex.Movie
	Radarr4KConfigured bool              `json:"radarr4kConfigured"`
	RadarrHDConfigured bool              `json:"radarrHdConfigured"`
	Radarr4K           *radarrMovieMatch `json:"radarr4k,omitempty"`
	RadarrHD           *radarrMovieMatch `json:"radarrHd,omitempty"`
	IsTop20Percent4K   bool              `json:"isTop20Percent4k,omitempty"`
}

// HighlightTop4KVideos examines the 4K file sizes for all movies and marks the top 20%
// largest with IsTop20Percent4K = true.
func HighlightTop4KVideos(matches []analyzedMovie) []analyzedMovie {
	if len(matches) == 0 {
		return matches
	}
	type itemSize struct {
		index int
		size  int64
	}
	var valid []itemSize
	for i, m := range matches {
		s := movie4KSize(m)
		if s > 0 {
			valid = append(valid, itemSize{index: i, size: s})
		}
	}
	if len(valid) == 0 {
		return matches
	}
	sort.Slice(valid, func(i, j int) bool {
		return valid[i].size > valid[j].size
	})
	topCount := int(math.Ceil(float64(len(valid)) * 0.20))
	if topCount < 1 {
		topCount = 1
	}
	cutoff := valid[topCount-1].size
	for _, v := range valid {
		if v.size >= cutoff && v.size > 0 {
			matches[v.index].IsTop20Percent4K = true
		}
	}
	return matches
}

func movie4KSize(m analyzedMovie) int64 {
	if m.Radarr4K != nil && m.Radarr4K.SizeBytes > 0 {
		return m.Radarr4K.SizeBytes
	}
	if m.FourKSizeBytes > 0 {
		return m.FourKSizeBytes
	}
	return 0
}

type radarrMovieMatch struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	Year      int    `json:"year"`
	SizeBytes int64  `json:"sizeBytes"`
}

func listRadarrMovies(ctx context.Context, instance radarr.Instance) ([]radarr.Movie, error) {
	if instance.APIKey == "" {
		return nil, nil
	}
	return radarr.NewClient(&http.Client{Timeout: 30 * time.Second}, instance).Movies(ctx)
}

func (c *plexConnector) getSession(w http.ResponseWriter, r *http.Request, create bool) (*plexSession, string) {
	if id := cookieValue(r); id != "" {
		c.mu.Lock()
		session := c.sessions[id]
		c.mu.Unlock()
		if session != nil {
			return session, id
		}
	}
	if !create {
		return nil, ""
	}
	id := randomID()
	session := &plexSession{}
	c.mu.Lock()
	c.sessions[id] = session
	c.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     plexSessionCookie,
		Value:    id,
		Path:     "/",
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return session, id
}

func cookieValue(r *http.Request) string {
	cookie, err := r.Cookie(plexSessionCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func serverOptions(resources []plex.Resource) []serverOption {
	options := make([]serverOption, 0, len(resources))
	for _, resource := range resources {
		options = append(options, serverOption{ID: resource.ClientID, Name: resource.Name})
	}
	return options
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request must contain one JSON value")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func randomID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value[:])
}
