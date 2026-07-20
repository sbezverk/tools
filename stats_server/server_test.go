package stats_server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type staticProvider []byte

func (p staticProvider) GetStatsJson() ([]byte, error) {
	return []byte(p), nil
}

type failingProvider struct {
	err error
}

func (p failingProvider) GetStatsJson() ([]byte, error) {
	return nil, p.err
}

func TestRegisterStatsProviderValidation(t *testing.T) {
	s := newTestServer(t, time.Now())

	if err := s.RegisterStatsProvider(" ", staticProvider(`{}`)); err == nil {
		t.Fatal("expected empty provider name to fail")
	}
	if err := s.RegisterStatsProvider("all", staticProvider(`{}`)); err == nil {
		t.Fatal("expected reserved provider name to fail")
	}
	if err := s.RegisterStatsProvider("nil", nil); err == nil {
		t.Fatal("expected nil provider to fail")
	}
	if err := s.RegisterStatsProvider(" Queue ", staticProvider(`{"depth":3}`)); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	if err := s.RegisterStatsProvider("queue", staticProvider(`{"depth":4}`)); err == nil {
		t.Fatal("expected duplicate normalized provider name to fail")
	}
}

func TestStatsEndpoints(t *testing.T) {
	started := time.Now().Add(-2 * time.Second)
	s := newTestServer(t, started)
	mustRegister(t, s, "queue", staticProvider(`{"depth":3}`))
	mustRegister(t, s, "worker", staticProvider(`{"running":true}`))

	rec := request(s, http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want %d", rec.Code, http.StatusOK)
	}

	rec = request(s, http.MethodGet, "/v1/stats")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats list status = %d, want %d", rec.Code, http.StatusOK)
	}
	var list struct {
		AvailableStats []string `json:"available_stats"`
	}
	decodeResponse(t, rec, &list)
	wantProviders := []string{"all", "queue", "worker"}
	if !reflect.DeepEqual(list.AvailableStats, wantProviders) {
		t.Fatalf("available stats = %#v, want %#v", list.AvailableStats, wantProviders)
	}

	rec = request(s, http.MethodGet, "/v1/stats/queue")
	if rec.Code != http.StatusOK {
		t.Fatalf("queue stats status = %d, want %d", rec.Code, http.StatusOK)
	}
	var envelope StatsEnvelope
	decodeResponse(t, rec, &envelope)
	if !envelope.ProcessStartedAt.Equal(started.UTC()) {
		t.Fatalf("process started at = %s, want %s", envelope.ProcessStartedAt, started.UTC())
	}
	if envelope.ProcessUptimeSec < 0 || envelope.ProcessUptimeSec > 30 {
		t.Fatalf("process uptime = %d, want a small non-negative value", envelope.ProcessUptimeSec)
	}
	if envelope.StatsSnapshotAt.IsZero() {
		t.Fatal("stats snapshot time was not set")
	}
	requireJSONEqual(t, envelope.Stats, `{"depth":3}`)

	rec = request(s, http.MethodGet, "/v1/stats/all")
	if rec.Code != http.StatusOK {
		t.Fatalf("all stats status = %d, want %d", rec.Code, http.StatusOK)
	}
	decodeResponse(t, rec, &envelope)
	var allStats map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Stats, &allStats); err != nil {
		t.Fatalf("decode all stats: %v", err)
	}
	requireJSONEqual(t, allStats["queue"], `{"depth":3}`)
	requireJSONEqual(t, allStats["worker"], `{"running":true}`)

	rec = request(s, http.MethodGet, "/v1/stats/missing")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing provider status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	rec = request(s, http.MethodPost, "/healthz")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post healthz status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if allow := rec.Header().Get("Allow"); allow != http.MethodGet {
		t.Fatalf("Allow header = %q, want %q", allow, http.MethodGet)
	}
}

func TestProviderFailuresReturnErrors(t *testing.T) {
	s := newTestServer(t, time.Now())
	mustRegister(t, s, "bad-json", staticProvider(`not-json`))
	mustRegister(t, s, "failing", failingProvider{err: errors.New("boom")})

	rec := request(s, http.MethodGet, "/v1/stats/bad-json")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("bad json status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	var body errorResponse
	decodeResponse(t, rec, &body)
	if !strings.Contains(body.Error, "invalid JSON") {
		t.Fatalf("bad json error = %q, want invalid JSON", body.Error)
	}

	rec = request(s, http.MethodGet, "/v1/stats/failing")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("failing provider status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	decodeResponse(t, rec, &body)
	if !strings.Contains(body.Error, "boom") {
		t.Fatalf("failing provider error = %q, want boom", body.Error)
	}

	rec = request(s, http.MethodGet, "/v1/stats/all")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("all stats with bad provider status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestNewDefaultsZeroProcessStartTime(t *testing.T) {
	before := time.Now().UTC()
	s := newTestServer(t, time.Time{})
	after := time.Now().UTC()

	if s.processStartedAt.IsZero() {
		t.Fatal("process start time is zero")
	}
	if s.processStartedAt.Before(before) || s.processStartedAt.After(after) {
		t.Fatalf("process start time = %s, want between %s and %s", s.processStartedAt, before, after)
	}
}

func TestAtomicTimeGuard(t *testing.T) {
	var value atomic.Value
	if got := AtomicTimeGuard(&value); !got.IsZero() {
		t.Fatalf("empty atomic time = %s, want zero", got)
	}
	if got := AtomicTimeGuard(nil); !got.IsZero() {
		t.Fatalf("nil atomic time = %s, want zero", got)
	}

	want := time.Now().UTC().Truncate(time.Second)
	value.Store(want)
	if got := AtomicTimeGuard(&value); !got.Equal(want) {
		t.Fatalf("atomic time = %s, want %s", got, want)
	}
}

func newTestServer(t *testing.T, started time.Time) *Server {
	t.Helper()

	s, err := New("127.0.0.1:0", started)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return s
}

func mustRegister(t *testing.T, s *Server, name string, provider StatsProvider) {
	t.Helper()

	if err := s.RegisterStatsProvider(name, provider); err != nil {
		t.Fatalf("register %q provider: %v", name, err)
	}
}

func request(s *Server, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)
	return rec
}

func decodeResponse(t *testing.T, rec *httptest.ResponseRecorder, target any) {
	t.Helper()

	if err := json.NewDecoder(rec.Body).Decode(target); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}

func requireJSONEqual(t *testing.T, got json.RawMessage, want string) {
	t.Helper()

	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode got JSON %q: %v", got, err)
	}
	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("decode want JSON %q: %v", want, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSON = %#v, want %#v", gotValue, wantValue)
	}
}
