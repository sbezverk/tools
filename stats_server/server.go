package stats_server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type errorResponse struct {
	Error string `json:"error"`
}

type Server struct {
	addr             string
	server           *http.Server
	mu               sync.RWMutex
	stats            map[string]StatsProvider
	processStartedAt time.Time
}

func New(addr string, processStartedAt time.Time) (*Server, error) {
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	if processStartedAt.IsZero() {
		processStartedAt = time.Now()
	}
	processStartedAt = processStartedAt.UTC()

	s := &Server{
		addr:             addr,
		stats:            make(map[string]StatsProvider),
		processStartedAt: processStartedAt,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.healthz)
	mux.HandleFunc("/v1/stats/", s.handleStats)
	mux.HandleFunc("/v1/stats", s.handleStats)

	s.server = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return s, nil
}

func (s *Server) RegisterStatsProvider(name string, provider StatsProvider) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return fmt.Errorf("stats provider name cannot be empty")
	}
	if name == "all" {
		return fmt.Errorf("stats provider name %q is reserved", name)
	}
	if provider == nil {
		return fmt.Errorf("stats provider %q cannot be nil", name)
	}
	if _, exists := s.stats[name]; exists {
		return fmt.Errorf("stats provider with name %q already exists", name)
	}
	s.stats[name] = provider
	return nil
}

func (s *Server) ListenAndServe() error {
	if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) Addr() string {
	return s.addr
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type StatsEnvelope struct {
	ProcessStartedAt time.Time       `json:"process_started_at"`
	ProcessUptimeSec int64           `json:"process_uptime_sec"`
	StatsSnapshotAt  time.Time       `json:"stats_snapshot_at"`
	Stats            json.RawMessage `json:"stats"`
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	if r.URL.Path == "/v1/stats" || r.URL.Path == "/v1/stats/" {
		writeJSON(w, http.StatusOK, map[string]any{
			"available_stats": s.providerNames(),
		})
		return
	}

	statsName, ok := strings.CutPrefix(r.URL.Path, "/v1/stats/")
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("endpoint not found"))
		return
	}
	statsName = strings.TrimSpace(strings.ToLower(statsName))
	if statsName == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"available_stats": s.providerNames(),
		})
		return
	}
	envelope := s.newStatsEnvelope()

	if statsName == "all" {
		allStats := make(map[string]json.RawMessage)
		for name, provider := range s.statsProviders() {
			stats, err := getStatsJSON(name, provider)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			allStats[name] = stats
		}
		b, err := json.Marshal(allStats)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("failed to marshal all stats: %w", err))
			return
		}
		envelope.Stats = b
		writeJSON(w, http.StatusOK, envelope)
		return
	}

	provider, ok := s.statsProvider(statsName)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("stats provider %q not found", statsName))
		return
	}
	stats, err := getStatsJSON(statsName, provider)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	envelope.Stats = stats
	writeJSON(w, http.StatusOK, envelope)
}

func (s *Server) providerNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	providers := make([]string, 0, len(s.stats)+1)
	providers = append(providers, "all")
	for name := range s.stats {
		providers = append(providers, name)
	}
	sort.Strings(providers)
	return providers
}

func (s *Server) statsProvider(name string) (StatsProvider, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	provider, ok := s.stats[name]
	return provider, ok
}

func (s *Server) statsProviders() map[string]StatsProvider {
	s.mu.RLock()
	defer s.mu.RUnlock()

	providers := make(map[string]StatsProvider, len(s.stats))
	for name, provider := range s.stats {
		providers[name] = provider
	}
	return providers
}

func (s *Server) newStatsEnvelope() StatsEnvelope {
	now := time.Now().UTC()
	uptime := int64(now.Sub(s.processStartedAt).Seconds())
	if uptime < 0 {
		uptime = 0
	}
	return StatsEnvelope{
		ProcessStartedAt: s.processStartedAt,
		ProcessUptimeSec: uptime,
		StatsSnapshotAt:  now,
	}
}

func getStatsJSON(name string, provider StatsProvider) (json.RawMessage, error) {
	b, err := provider.GetStatsJson()
	if err != nil {
		return nil, fmt.Errorf("failed to get stats from provider %q: %w", name, err)
	}
	if b != nil && !json.Valid(b) {
		return nil, fmt.Errorf("stats provider %q returned invalid JSON", name)
	}
	return json.RawMessage(b), nil
}

func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", http.MethodGet)
	writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, errorResponse{Error: err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
