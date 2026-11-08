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

	"pruneplex/internal/plex"
	"pruneplex/internal/radarr"
)

const plexSessionCookie = "pruneplex_session"

type plexConnector struct {
	mu       sync.Mutex
	client   *plex.Client
	clientID string
	sessions map[string]*plexSession
	radarr   *radarrSettingsStore
	sonarr   *radarrSettingsStore
	auth     *plexAuthStore
}

type plexSession struct {
	pinID        int64
	pinCode      string
	pinCreatedAt time.Time
	accountToken string
	servers      []plex.Resource
	selected     *connectedServer

	// Latest Plex scans, kept so pages can show them again after navigation.
	movieScan *plex.Analysis
	showScan  *plex.ShowAnalysis
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
	auth := newPlexAuthStore(plexAuthPath(settings))
	clientID := auth.get().ClientID
	if clientID == "" {
		clientID = randomID()
		if err := auth.update(func(a *plexAuth) { a.ClientID = clientID }); err != nil {
			log.Printf("Could not persist Plex client ID: %v", err)
		}
	}
	return &plexConnector{
		client:   plex.NewClient(&http.Client{Timeout: 30 * time.Second}, clientID),
		clientID: clientID,
		sessions: make(map[string]*plexSession),
		radarr:   settings,
		auth:     auth,
	}
}

func (c *plexConnector) persistAuth(fn func(*plexAuth)) {
	if err := c.auth.update(fn); err != nil {
		log.Printf("Could not persist Plex auth: %v", err)
	}
}

// restoreServer reconnects to the previously selected server using the
// persisted account token. If Plex rejects the token, the saved auth is
// discarded so the user is prompted to sign in again.
func (c *plexConnector) restoreServer(ctx context.Context, session *plexSession) {
	saved := c.auth.get()
	c.mu.Lock()
	token, needServers := session.accountToken, session.servers == nil
	c.mu.Unlock()
	if token == "" {
		return
	}
	if needServers {
		servers, err := c.client.Resources(ctx, token)
		if err != nil {
			if strings.Contains(err.Error(), "(401") {
				c.persistAuth(func(a *plexAuth) { a.AccountToken, a.ServerID = "", "" })
				c.mu.Lock()
				session.accountToken, session.servers = "", nil
				c.mu.Unlock()
			}
			return
		}
		c.mu.Lock()
		session.servers = servers
		c.mu.Unlock()
	}
	if saved.ServerID == "" {
		return
	}
	c.mu.Lock()
	var resource *plex.Resource
	for index := range session.servers {
		if session.servers[index].ClientID == saved.ServerID {
			copy := session.servers[index]
			resource = &copy
			break
		}
	}
	c.mu.Unlock()
	if resource == nil {
		return
	}
	if server := c.connectResource(ctx, resource); server != nil {
		c.mu.Lock()
		session.selected = server
		c.mu.Unlock()
	}
}

// connectResource tries each of the resource's connections (direct before
// relay, local first) and returns the first one that verifies.
func (c *plexConnector) connectResource(ctx context.Context, resource *plex.Resource) *connectedServer {
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
		if err := c.client.VerifyServer(ctx, connection.URI, resource.AccessToken, resource.ClientID); err != nil {
			continue
		}
		return &connectedServer{
			id:      resource.ClientID,
			name:    resource.Name,
			baseURL: connection.URI,
			token:   resource.AccessToken,
		}
	}
	return nil
}

func (c *plexConnector) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/plex/status", c.status)
	mux.HandleFunc("POST /api/plex/auth/start", c.startAuth)
	mux.HandleFunc("GET /api/plex/auth/status", c.authStatus)
	mux.HandleFunc("POST /api/plex/server", c.selectServer)
	mux.HandleFunc("POST /api/plex/disconnect", c.disconnect)
	mux.HandleFunc("GET /api/plex/libraries", c.libraries)
	mux.HandleFunc("POST /api/plex/analyze", c.analyze)
	mux.HandleFunc("POST /api/plex/analyze-tv", c.analyzeTV)
	mux.HandleFunc("GET /api/plex/analysis", c.lastMovieAnalysis)
	mux.HandleFunc("GET /api/plex/analysis-tv", c.lastShowAnalysis)
	mux.HandleFunc("GET /api/plex/thumb", c.thumb)
}

func (c *plexConnector) status(w http.ResponseWriter, r *http.Request) {
	session, _ := c.getSession(w, r, false)
	if session == nil {
		writeJSON(w, http.StatusOK, map[string]any{"connected": false})
		return
	}

	c.mu.Lock()
	needRestore := session.selected == nil && session.accountToken != "" && session.pinID == 0
	c.mu.Unlock()
	if needRestore {
		c.restoreServer(r.Context(), session)
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
			c.persistAuth(func(a *plexAuth) { a.AccountToken, a.ServerID = pinStatus.AuthToken, "" })
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

	server := c.connectResource(r.Context(), resource)
	if server == nil {
		writeAPIError(w, http.StatusBadGateway, "Could not reach that Plex server")
		return
	}
	c.mu.Lock()
	session.selected = server
	c.mu.Unlock()
	c.persistAuth(func(a *plexAuth) { a.ServerID = resource.ClientID })
	writeJSON(w, http.StatusOK, map[string]any{"connected": true, "serverName": resource.Name})
}

func (c *plexConnector) disconnect(w http.ResponseWriter, r *http.Request) {
	c.persistAuth(func(a *plexAuth) { a.AccountToken, a.ServerID = "", "" })
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

func (c *plexConnector) connectedServerFor(w http.ResponseWriter, r *http.Request) *connectedServer {
	session, _ := c.getSession(w, r, false)
	if session != nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		if session.selected != nil {
			copy := *session.selected
			return &copy
		}
	}
	writeAPIError(w, http.StatusUnauthorized, "Authenticate with Plex and select a server first")
	return nil
}

func (c *plexConnector) libraries(w http.ResponseWriter, r *http.Request) {
	server := c.connectedServerFor(w, r)
	if server == nil {
		return
	}
	sectionType := "movie"
	if r.URL.Query().Get("type") == "show" {
		sectionType = "show"
	}
	libraries, err := c.client.Libraries(r.Context(), server.baseURL, server.token, sectionType)
	if err != nil {
		log.Printf("Plex library listing failed: %v", err)
		writeAPIError(w, http.StatusBadGateway, "Could not list Plex libraries")
		return
	}
	if libraries == nil {
		libraries = []plex.Library{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"libraries": libraries})
}

func (c *plexConnector) analyze(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Library string `json:"library"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &input); err != nil {
			writeAPIError(w, http.StatusBadRequest, "Invalid analysis request")
			return
		}
	}
	server := c.connectedServerFor(w, r)
	if server == nil {
		return
	}

	result, err := c.client.AnalyzeMovies(r.Context(), server.baseURL, server.token, input.Library)
	if errors.Is(err, plex.ErrUnknownLibrary) {
		writeAPIError(w, http.StatusBadRequest, "That Plex library is not available")
		return
	}
	if err != nil {
		log.Printf("Plex movie analysis failed: %v", err)
		writeAPIError(w, http.StatusBadGateway, "Plex movie analysis failed")
		return
	}
	c.rememberScan(w, r, func(session *plexSession) { session.movieScan = &result })
	response, failure := c.movieResponse(r.Context(), result)
	if failure != nil {
		writeAPIError(w, failure.status, failure.message)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// apiFailure is an error to report to the client.
type apiFailure struct {
	status  int
	message string
}

// rememberScan stores the latest Plex scan on the session so the page can show
// it again after the user navigates elsewhere and comes back.
func (c *plexConnector) rememberScan(w http.ResponseWriter, r *http.Request, set func(*plexSession)) {
	session, _ := c.getSession(w, r, false)
	if session == nil {
		return
	}
	c.mu.Lock()
	set(session)
	c.mu.Unlock()
}

// lastMovieAnalysis returns the remembered movie scan, matched afresh against
// Radarr so deletions and manual matches made since are reflected.
func (c *plexConnector) lastMovieAnalysis(w http.ResponseWriter, r *http.Request) {
	session, _ := c.getSession(w, r, false)
	var scan *plex.Analysis
	if session != nil {
		c.mu.Lock()
		scan = session.movieScan
		c.mu.Unlock()
	}
	if scan == nil {
		writeJSON(w, http.StatusOK, map[string]any{"analysis": nil})
		return
	}
	response, failure := c.movieResponse(r.Context(), *scan)
	if failure != nil {
		writeAPIError(w, failure.status, failure.message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"analysis": response})
}

// movieResponse matches a Plex movie scan against the configured Radarr instances.
func (c *plexConnector) movieResponse(ctx context.Context, result plex.Analysis) (analysisResponse, *apiFailure) {
	settings, err := c.radarr.snapshot()
	if err != nil {
		return analysisResponse{}, &apiFailure{http.StatusInternalServerError, "Could not read Radarr settings"}
	}
	fourKMovies, err := listRadarrMovies(ctx, settings.FourK)
	if err != nil {
		return analysisResponse{}, &apiFailure{http.StatusBadGateway, "Could not read the 4K Radarr library"}
	}
	hdMovies, err := listRadarrMovies(ctx, settings.HD)
	if err != nil {
		return analysisResponse{}, &apiFailure{http.StatusBadGateway, "Could not read the HD Radarr library"}
	}
	manual := c.radarr.matches()
	// findMatch prefers a pairing the user chose by hand, then automatic ID/title matching.
	findMatch := func(quality string, movie plex.Movie, library []radarr.Movie) *radarrMovieMatch {
		var found radarr.Movie
		ok := false
		if id, saved := manual.get(quality, plexMatchKey(movie.Title, movie.Year)); saved {
			for _, candidate := range library {
				if candidate.ID == id {
					found, ok = candidate, true
					break
				}
			}
		}
		manualMatch := ok
		if !ok {
			identity := radarr.Identity{Title: movie.Title, Year: movie.Year, TMDBID: movie.TMDBID, IMDBID: movie.IMDBID}
			found, ok = radarr.FindMovie(identity, library)
		}
		if !ok {
			return nil
		}
		return &radarrMovieMatch{ID: found.ID, Title: found.Title, Year: found.Year, SizeBytes: found.SizeOnDisk, Manual: manualMatch}
	}
	enrich := func(movies []plex.Movie) []analyzedMovie {
		out := make([]analyzedMovie, 0, len(movies))
		for _, movie := range movies {
			match := analyzedMovie{
				Movie:              movie,
				Radarr4KConfigured: settings.FourK.APIKey != "",
				RadarrHDConfigured: settings.HD.APIKey != "",
			}
			match.Radarr4K = findMatch("4k", movie, fourKMovies)
			match.RadarrHD = findMatch("hd", movie, hdMovies)
			out = append(out, match)
		}
		return out
	}
	response := analysisResponse{
		Scanned: result.Scanned,
		Matches: enrich(result.Matches),
		Only4K:  enrich(result.Only4K),
		OnlyHD:  enrich(result.OnlyHD),
		Other:   enrich(result.Other),
	}
	response.Matches = HighlightTop4KVideos(response.Matches)
	return response, nil
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
	Only4K  []analyzedMovie `json:"only4k"`
	OnlyHD  []analyzedMovie `json:"onlyHd"`
	Other   []analyzedMovie `json:"other"`
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
	Status    string `json:"status,omitempty"` // Sonarr series status: continuing, ended, ...
	Manual    bool   `json:"manual,omitempty"` // chosen by hand rather than matched automatically
	// Sonarr only: episodes on disk out of the monitored, aired episodes.
	EpisodeFiles int `json:"episodeFiles,omitempty"`
	EpisodeCount int `json:"episodeCount,omitempty"`
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
	saved := c.auth.get()
	if !create && saved.AccountToken == "" {
		return nil, ""
	}
	id := randomID()
	// A fresh session (new browser or app restart) inherits the persisted login.
	session := &plexSession{accountToken: saved.AccountToken}
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
