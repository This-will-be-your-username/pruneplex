package ratings

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type providerEntry struct {
	FetchedAt time.Time `json:"fetchedAt"`
	Found     bool      `json:"found"`
	Ratings   []Rating  `json:"ratings,omitempty"`
}

type entry struct {
	OMDb  *providerEntry `json:"omdb,omitempty"`
	Trakt *providerEntry `json:"trakt,omitempty"`
}

func (e *entry) provider(name string) *providerEntry {
	if e == nil {
		return nil
	}
	if name == providerOMDb {
		return e.OMDb
	}
	return e.Trakt
}

type task struct {
	ref  Ref
	ckey string
}

// Service looks ratings up in the background. Lookup returns whatever is cached
// right away and queues the rest; callers poll Lookup until every result is
// Complete. Requests to each provider are made one at a time, slowly.
type Service struct {
	mu       sync.Mutex
	path     string // cache file; empty means memory only
	cache    map[string]*entry
	inflight map[string]bool
	paused   map[string]time.Time
	lastErr  map[string]string
	stores   int

	settings  func() Settings
	http      *http.Client
	omdbBase  string
	traktBase string
	pace      map[string]time.Duration
	queues    map[string]chan task
	startOnce sync.Once
	now       func() time.Time
}

// NewService creates a service that caches to cachePath (empty for memory only)
// and reads credentials from settings on every request.
func NewService(cachePath string, settings func() Settings) *Service {
	s := &Service{
		path:      cachePath,
		cache:     map[string]*entry{},
		inflight:  map[string]bool{},
		paused:    map[string]time.Time{},
		lastErr:   map[string]string{},
		settings:  settings,
		http:      &http.Client{Timeout: 20 * time.Second},
		omdbBase:  "https://www.omdbapi.com",
		traktBase: "https://api.trakt.tv",
		// OMDb's free tier is capped per day rather than per second; Trakt asks
		// clients to stay well under its rate limit.
		pace:   map[string]time.Duration{providerOMDb: 150 * time.Millisecond, providerTrakt: 300 * time.Millisecond},
		queues: map[string]chan task{providerOMDb: make(chan task, 8192), providerTrakt: make(chan task, 8192)},
		now:    time.Now,
	}
	if cachePath != "" {
		if data, err := os.ReadFile(cachePath); err == nil {
			_ = json.Unmarshal(data, &s.cache)
		}
		if s.cache == nil {
			s.cache = map[string]*entry{}
		}
	}
	return s
}

// SetEndpoints points the service at other servers and sets how long it waits
// between requests. It exists for tests and must be called before Lookup.
func (s *Service) SetEndpoints(omdbBase, traktBase string, pace time.Duration) {
	s.omdbBase, s.traktBase = omdbBase, traktBase
	s.pace = map[string]time.Duration{providerOMDb: pace, providerTrakt: pace}
}

// Configured reports whether any provider has a credential.
func (s *Service) Configured() bool {
	cfg := s.settings()
	return cfg.OMDbKey != "" || cfg.TraktClientID != ""
}

func (s *Service) providers() []string {
	cfg := s.settings()
	var out []string
	if cfg.OMDbKey != "" {
		out = append(out, providerOMDb)
	}
	if cfg.TraktClientID != "" {
		out = append(out, providerTrakt)
	}
	return out
}

func (s *Service) start() {
	s.startOnce.Do(func() {
		for name := range s.queues {
			go s.worker(name)
		}
	})
}

// Lookup returns the ratings known for each ref (keyed by Ref.Key) and queues
// lookups for what is missing or stale. The second result maps a provider to a
// message when it is currently refusing requests (bad key, quota).
func (s *Service) Lookup(refs []Ref) (map[string]Result, map[string]string) {
	providers := s.providers()
	out := make(map[string]Result, len(refs))
	if len(providers) == 0 {
		for _, ref := range refs {
			out[ref.Key] = Result{Complete: true}
		}
		return out, nil
	}
	s.start()

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	// A provider that was refusing requests gets another chance once its pause
	// is over, and its old complaint is dropped so the page keeps polling.
	for name, until := range s.paused {
		if !until.After(now) {
			delete(s.paused, name)
			s.lastErr[name] = ""
		}
	}
	for _, ref := range refs {
		ckey := cacheKey(ref)
		if ckey == "" {
			out[ref.Key] = Result{Complete: true}
			continue
		}
		result := Result{Complete: true}
		for _, name := range providers {
			pe := s.cache[ckey].provider(name)
			if pe != nil {
				result.Ratings = append(result.Ratings, pe.Ratings...)
				if isFresh(pe, now) {
					continue
				}
			}
			result.Complete = false
			s.enqueueLocked(name, ref, ckey, now)
		}
		out[ref.Key] = result
	}
	errs := map[string]string{}
	for name, message := range s.lastErr {
		if message != "" {
			errs[name] = message
		}
	}
	return out, errs
}

func isFresh(pe *providerEntry, now time.Time) bool {
	limit := freshFor
	if !pe.Found {
		limit = notFoundFor
	}
	return now.Sub(pe.FetchedAt) < limit
}

func (s *Service) enqueueLocked(name string, ref Ref, ckey string, now time.Time) {
	if s.paused[name].After(now) {
		return
	}
	id := name + "|" + ckey
	if s.inflight[id] {
		return
	}
	select {
	case s.queues[name] <- task{ref: ref, ckey: ckey}:
		s.inflight[id] = true
	default: // queue full: it will be picked up by a later poll
	}
}

func (s *Service) worker(name string) {
	for t := range s.queues[name] {
		s.process(name, t)
		time.Sleep(s.pace[name])
	}
}

func (s *Service) process(name string, t task) {
	id := name + "|" + t.ckey
	s.mu.Lock()
	if s.paused[name].After(s.now()) {
		delete(s.inflight, id) // refusing requests right now; a later poll retries
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := s.settings()
	var found []Rating
	var err error
	switch name {
	case providerOMDb:
		found, err = s.lookupOMDb(ctx, cfg.OMDbKey, t.ref)
	default:
		found, err = s.lookupTrakt(ctx, cfg.TraktClientID, t.ref)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inflight, id)
	now := s.now()
	switch {
	case err == nil:
		s.lastErr[name] = ""
		s.storeLocked(name, t.ckey, &providerEntry{FetchedAt: now, Found: true, Ratings: found})
	case errors.Is(err, ErrNotFound):
		s.lastErr[name] = ""
		s.storeLocked(name, t.ckey, &providerEntry{FetchedAt: now})
	case errors.Is(err, ErrQuota):
		s.paused[name] = now.Add(time.Hour)
		s.lastErr[name] = displayName(name) + " rate limit reached; remaining ratings will load later"
	case errors.Is(err, ErrAuth):
		s.paused[name] = now.Add(10 * time.Minute)
		s.lastErr[name] = displayName(name) + " rejected the saved key; check it in Settings"
	default:
		log.Printf("ratings: %s lookup failed: %v", name, err)
		s.lastErr[name] = displayName(name) + " lookups are failing"
	}
}

func displayName(name string) string {
	if name == providerOMDb {
		return "OMDb"
	}
	return "Trakt"
}

func (s *Service) storeLocked(name, ckey string, pe *providerEntry) {
	e := s.cache[ckey]
	if e == nil {
		e = &entry{}
		s.cache[ckey] = e
	}
	if name == providerOMDb {
		e.OMDb = pe
	} else {
		e.Trakt = pe
	}
	s.stores++
	if s.stores%25 == 0 || (len(s.queues[providerOMDb]) == 0 && len(s.queues[providerTrakt]) == 0) {
		s.saveLocked()
	}
}

func (s *Service) saveLocked() {
	if s.path == "" {
		return
	}
	data, err := json.Marshal(s.cache)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		log.Printf("ratings: could not create cache directory: %v", err)
		return
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".ratings-cache-*")
	if err != nil {
		log.Printf("ratings: could not write cache: %v", err)
		return
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return
	}
	if err := temporary.Close(); err != nil {
		return
	}
	if err := os.Rename(name, s.path); err != nil {
		log.Printf("ratings: could not save cache: %v", err)
	}
}
