package kplclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestClientInjectsAPIKeyAndBuildsRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q, want test-key", got)
		}
		if got := r.URL.Path; got != "/api/sentiment" {
			t.Fatalf("path = %q, want /api/sentiment", got)
		}
		if got := r.URL.Query().Get("date"); got != "20260430" {
			t.Fatalf("date = %q, want 20260430", got)
		}
		w.Header().Set("X-RateLimit-Limit", "2000")
		_ = json.NewEncoder(w).Encode(map[string]any{"requestedDate": "20260430"})
	}))
	defer upstream.Close()

	client, err := New(Config{BaseURL: upstream.URL, APIKey: "test-key", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Do(context.Background(), Request{
		Method: http.MethodGet,
		Path:   "/api/sentiment",
		Query:  url.Values{"date": []string{"20260430"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Header.Get("X-RateLimit-Limit") != "2000" {
		t.Fatalf("rate limit header not preserved")
	}
	if !json.Valid(resp.Body) {
		t.Fatalf("response body is not valid JSON")
	}
}

func TestClientConvertsUpstreamHTTPError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer upstream.Close()

	client, err := New(Config{BaseURL: upstream.URL, APIKey: "test-key", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Do(context.Background(), Request{Path: "/api/sentiment"})
	if err == nil {
		t.Fatal("expected error")
	}
	upstreamErr, ok := err.(*UpstreamError)
	if !ok {
		t.Fatalf("error type = %T, want *UpstreamError", err)
	}
	if upstreamErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", upstreamErr.StatusCode)
	}
}
