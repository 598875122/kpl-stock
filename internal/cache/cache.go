package cache

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db  *sql.DB
	ttl time.Duration
	now func() time.Time
}

type Entry struct {
	Body      json.RawMessage
	Header    map[string][]string
	CreatedAt time.Time
}

func Open(path string, ttl time.Duration) (*Store, error) {
	if path == "" {
		return nil, errors.New("cache path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db, ttl: ttl, now: time.Now}
	if err := store.init(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func OpenForTest(db *sql.DB, ttl time.Duration, now func() time.Time) (*Store, error) {
	store := &Store{db: db, ttl: ttl, now: now}
	if store.now == nil {
		store.now = time.Now
	}
	if err := store.init(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Get(ctx context.Context, key string) (Entry, bool, error) {
	row := s.db.QueryRowContext(ctx, `select body, headers, created_at from responses where key = ?`, key)
	var body []byte
	var headers []byte
	var createdAt string
	if err := row.Scan(&body, &headers, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Entry{}, false, nil
		}
		return Entry{}, false, err
	}
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Entry{}, false, err
	}
	if s.now().Sub(created) > s.ttl {
		return Entry{}, false, nil
	}

	var headerMap map[string][]string
	if err := json.Unmarshal(headers, &headerMap); err != nil {
		return Entry{}, false, err
	}

	return Entry{Body: body, Header: headerMap, CreatedAt: created}, true, nil
}

func (s *Store) Put(ctx context.Context, key string, body json.RawMessage, headers map[string][]string) error {
	headerJSON, err := json.Marshal(headers)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
insert into responses(key, body, headers, created_at)
values(?, ?, ?, ?)
on conflict(key) do update set body=excluded.body, headers=excluded.headers, created_at=excluded.created_at
`, key, []byte(body), headerJSON, s.now().Format(time.RFC3339Nano))
	return err
}

func ShouldCache(args map[string]any, now time.Time) bool {
	if raw, ok := args["date"]; ok && raw != nil {
		return isBeforeShanghaiDate(valueString(raw), now)
	}
	if raw, ok := args["endDate"]; ok && raw != nil {
		return isBeforeShanghaiDate(valueString(raw), now)
	}
	return false
}

func Key(method string, path string, args map[string]any) string {
	normalized := normalizeArgs(args)
	sum := sha256.Sum256([]byte(method + "\n" + path + "\n" + normalized))
	return hex.EncodeToString(sum[:])
}

func (s *Store) init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
create table if not exists responses (
	key text primary key,
	body blob not null,
	headers text not null,
	created_at text not null
);
`)
	return err
}

func normalizeArgs(args map[string]any) string {
	values := url.Values{}
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		values.Set(key, valueString(args[key]))
	}
	return values.Encode()
}

func valueString(value any) string {
	switch v := value.(type) {
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	default:
		return fmt.Sprint(value)
	}
}

func isBeforeShanghaiDate(raw string, now time.Time) bool {
	parsed, ok := parseDate(raw)
	if !ok {
		return false
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*60*60)
	}
	today := now.In(loc)
	todayDate := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)
	return parsed.Before(todayDate)
}

func parseDate(raw string) (time.Time, bool) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*60*60)
	}
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{"20060102", "2006-01-02"} {
		parsed, err := time.ParseInLocation(layout, raw, loc)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
