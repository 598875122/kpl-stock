package cache

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestShouldCacheHistoricalDateOnly(t *testing.T) {
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))

	if !ShouldCache(map[string]any{"date": "20260430"}, now) {
		t.Fatal("historical date should cache")
	}
	if ShouldCache(map[string]any{"date": "20260605"}, now) {
		t.Fatal("current date should not cache")
	}
	if ShouldCache(map[string]any{}, now) {
		t.Fatal("missing date should not cache")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	store, err := OpenForTest(db, time.Hour, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	key := Key("GET", "/api/sentiment", map[string]any{"date": "20260430"})
	body := json.RawMessage(`{"ok":true}`)
	headers := map[string][]string{"X-RateLimit-Limit": {"2000"}}
	if err := store.Put(t.Context(), key, body, headers); err != nil {
		t.Fatal(err)
	}

	entry, hit, err := store.Get(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	if !hit {
		t.Fatal("expected cache hit")
	}
	if string(entry.Body) != string(body) {
		t.Fatalf("body = %s", entry.Body)
	}
}
