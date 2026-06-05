package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"stock-kpl/internal/cache"
	"stock-kpl/internal/kplclient"
	"stock-kpl/internal/tools"
)

type fakeKPL struct {
	count int
	resp  kplclient.Response
	err   error
	last  kplclient.Request
}

func (f *fakeKPL) Do(ctx context.Context, req kplclient.Request) (kplclient.Response, error) {
	f.count++
	f.last = req
	return f.resp, f.err
}

func newTestServer(t *testing.T, kpl *fakeKPL) *Server {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := cache.OpenForTest(db, 30*24*time.Hour, func() time.Time {
		return time.Date(2026, 6, 5, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Config{Version: "test", BaseURL: "http://upstream"}, tools.DefaultRegistry(), kpl, store)
	srv.now = func() time.Time {
		return time.Date(2026, 6, 5, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	}
	return srv
}

func TestToolsList(t *testing.T) {
	kpl := &fakeKPL{}
	srv := newTestServer(t, kpl)

	req := httptest.NewRequest(http.MethodGet, "/v1/tools", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		OK    bool         `json:"ok"`
		Tools []tools.Tool `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || len(body.Tools) < 85 {
		t.Fatalf("unexpected tools response: ok=%v count=%d", body.OK, len(body.Tools))
	}
}

func TestToolCallEnvelopeAndMeta(t *testing.T) {
	kpl := &fakeKPL{
		resp: kplclient.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"X-Ratelimit-Limit":     {"2000"},
				"X-Ratelimit-Remaining": {"1847"},
				"X-Client-Tier":         {"pro"},
			},
			Body: json.RawMessage(`{"requestedDate":"20260430","dataDate":"20260430","isFallback":false,"_source":"history","value":1}`),
		},
	}
	srv := newTestServer(t, kpl)

	req := httptest.NewRequest(http.MethodPost, "/v1/tools/market.sentiment", jsonBody(`{"arguments":{"date":"20260430"}}`))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || body.Meta.UpstreamPath != "/api/sentiment" || body.Meta.Cached {
		t.Fatalf("unexpected envelope: %+v", body)
	}
	if body.Meta.RateLimit["limit"] != "2000" {
		t.Fatalf("rate limit meta missing: %+v", body.Meta.RateLimit)
	}
}

func TestToolCallRejectsUnknownToolAndBadArguments(t *testing.T) {
	srv := newTestServer(t, &fakeKPL{})

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/tools/nope", jsonBody(`{"arguments":{}}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown tool status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/tools/stock.kline", jsonBody(`{"arguments":{"type":1}}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing param status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/tools/market.sentiment", jsonBody(`{"arguments":{"wat":1}}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown param status = %d", rec.Code)
	}
}

func TestHistoricalCacheHitAndBypass(t *testing.T) {
	kpl := &fakeKPL{
		resp: kplclient.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       json.RawMessage(`{"date":"20260430","value":1}`),
		},
	}
	srv := newTestServer(t, kpl)
	handler := srv.Routes()

	body := `{"arguments":{"date":"20260430"}}`
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/tools/market.sentiment", jsonBody(body)))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/tools/market.sentiment", jsonBody(body)))
	if kpl.count != 1 {
		t.Fatalf("kpl count = %d, want 1 due to cache hit", kpl.count)
	}

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/tools/market.sentiment", jsonBody(`{"cache":"bypass","arguments":{"date":"20260430"}}`)))
	if kpl.count != 2 {
		t.Fatalf("kpl count = %d, want 2 after bypass", kpl.count)
	}
}

func TestUpstreamErrorEnvelope(t *testing.T) {
	kpl := &fakeKPL{err: &kplclient.UpstreamError{StatusCode: http.StatusUnauthorized, Body: []byte("no")}}
	srv := newTestServer(t, kpl)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/tools/market.sentiment", jsonBody(`{"arguments":{}}`)))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
	var body envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.OK || body.Error.Code != "upstream_401" {
		t.Fatalf("unexpected error envelope: %+v", body.Error)
	}
}

func TestLegacyAPIPassthrough(t *testing.T) {
	kpl := &fakeKPL{
		resp: kplclient.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"X-Ratelimit-Remaining": {"9999"},
			},
			Body: json.RawMessage(`{"items":[{"code":"002384"}]}`),
		},
	}
	srv := newTestServer(t, kpl)

	req := httptest.NewRequest(http.MethodGet, "/api/stock/bigorder/002384?date=20260605", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"items":[{"code":"002384"}]}` {
		t.Fatalf("body = %s", got)
	}
	if kpl.last.Path != "/api/stock/bigorder/002384" || kpl.last.Query.Get("date") != "20260605" {
		t.Fatalf("unexpected upstream request: path=%s query=%s", kpl.last.Path, kpl.last.Query.Encode())
	}
	if rec.Header().Get("X-Ratelimit-Remaining") != "9999" {
		t.Fatalf("upstream header not forwarded: %v", rec.Header())
	}
}

func jsonBody(s string) *strings.Reader {
	return strings.NewReader(s)
}
