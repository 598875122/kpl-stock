package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"stock-kpl/internal/cache"
	"stock-kpl/internal/kplclient"
	"stock-kpl/internal/tools"
)

type Config struct {
	Version string
	BaseURL string
}

type Server struct {
	cfg      Config
	registry *tools.Registry
	kpl      kplDoer
	cache    cacheStore
	now      func() time.Time
}

type kplDoer interface {
	Do(ctx context.Context, req kplclient.Request) (kplclient.Response, error)
}

type cacheStore interface {
	Get(ctx context.Context, key string) (cache.Entry, bool, error)
	Put(ctx context.Context, key string, body json.RawMessage, headers map[string][]string) error
}

type toolRequest struct {
	Arguments map[string]any `json:"arguments"`
	Cache     string         `json:"cache"`
}

type envelope struct {
	OK    bool            `json:"ok"`
	Tool  string          `json:"tool,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *apiError       `json:"error,omitempty"`
	Meta  meta            `json:"meta"`
}

type apiError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Status  int            `json:"status"`
	Details map[string]any `json:"details"`
}

type meta struct {
	UpstreamPath  string         `json:"upstreamPath,omitempty"`
	RequestedDate any            `json:"requestedDate,omitempty"`
	DataDate      any            `json:"dataDate,omitempty"`
	IsFallback    any            `json:"isFallback,omitempty"`
	Source        any            `json:"source,omitempty"`
	Cached        bool           `json:"cached"`
	RateLimit     map[string]any `json:"rateLimit,omitempty"`
}

func New(cfg Config, registry *tools.Registry, kpl kplDoer, store cacheStore) *Server {
	return &Server{
		cfg:      cfg,
		registry: registry,
		kpl:      kpl,
		cache:    store,
		now:      time.Now,
	}
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", s.handleHealth)
	r.Get("/v1/tools", s.handleTools)
	r.Post("/v1/tools/{toolName}", s.handleToolCall)
	r.Get("/api/*", s.handleLegacyAPIPassthrough)
	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                 true,
		"version":            s.cfg.Version,
		"upstreamBaseURL":    s.cfg.BaseURL,
		"upstreamConfigured": s.cfg.BaseURL != "",
	})
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"tools": s.registry.List(),
	})
}

func (s *Server) handleToolCall(w http.ResponseWriter, r *http.Request) {
	toolName := chi.URLParam(r, "toolName")
	tool, ok := s.registry.Get(toolName)
	if !ok {
		s.writeError(w, toolName, "", false, http.StatusNotFound, "tool_not_found", "tool not found", nil)
		return
	}

	var req toolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, toolName, tool.Path, false, http.StatusBadRequest, "bad_request", "invalid JSON request body", nil)
		return
	}
	if req.Arguments == nil {
		req.Arguments = map[string]any{}
	}
	if req.Cache == "" {
		req.Cache = "auto"
	}
	if req.Cache != "auto" && req.Cache != "bypass" {
		s.writeError(w, toolName, tool.Path, false, http.StatusBadRequest, "bad_request", "cache must be auto or bypass", nil)
		return
	}

	prepared, err := tool.Prepare(req.Arguments)
	if err != nil {
		s.writeError(w, toolName, tool.Path, false, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}

	cacheable := req.Cache != "bypass" && tool.Cacheable && cache.ShouldCache(req.Arguments, s.now())
	cacheKey := cache.Key(prepared.Method, prepared.Path, req.Arguments)
	if cacheable {
		entry, hit, err := s.cache.Get(r.Context(), cacheKey)
		if err != nil {
			s.writeError(w, toolName, prepared.Path, false, http.StatusInternalServerError, "cache_error", err.Error(), nil)
			return
		}
		if hit {
			s.writeSuccess(w, toolName, prepared.Path, entry.Body, http.Header(entry.Header), true)
			return
		}
	}

	resp, err := s.kpl.Do(r.Context(), kplclient.Request{
		Method: prepared.Method,
		Path:   prepared.Path,
		Query:  prepared.Query,
	})
	if err != nil {
		var upstream *kplclient.UpstreamError
		if errors.As(err, &upstream) {
			code := upstreamCode(upstream.StatusCode)
			s.writeError(w, toolName, prepared.Path, false, upstream.StatusCode, code, upstreamMessage(upstream.StatusCode), map[string]any{
				"body": string(upstream.Body),
			})
			return
		}
		s.writeError(w, toolName, prepared.Path, false, http.StatusBadGateway, "upstream_error", err.Error(), nil)
		return
	}

	if cacheable && len(resp.Body) > 0 && string(resp.Body) != "null" {
		if err := s.cache.Put(r.Context(), cacheKey, resp.Body, map[string][]string(resp.Header)); err != nil {
			s.writeError(w, toolName, prepared.Path, false, http.StatusInternalServerError, "cache_error", err.Error(), nil)
			return
		}
	}

	s.writeSuccess(w, toolName, prepared.Path, resp.Body, resp.Header, false)
}

func (s *Server) handleLegacyAPIPassthrough(w http.ResponseWriter, r *http.Request) {
	resp, err := s.kpl.Do(r.Context(), kplclient.Request{
		Method: http.MethodGet,
		Path:   r.URL.Path,
		Query:  r.URL.Query(),
	})
	if err != nil {
		var upstream *kplclient.UpstreamError
		if errors.As(err, &upstream) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(upstream.StatusCode)
			_, _ = w.Write(upstream.Body)
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok":      false,
			"error":   "upstream_error",
			"message": err.Error(),
		})
		return
	}
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(resp.Body)
}

func (s *Server) writeSuccess(w http.ResponseWriter, toolName string, path string, data json.RawMessage, header http.Header, cached bool) {
	m := extractMeta(data, header)
	m.UpstreamPath = path
	m.Cached = cached
	writeJSON(w, http.StatusOK, envelope{
		OK:   true,
		Tool: toolName,
		Data: data,
		Meta: m,
	})
}

func (s *Server) writeError(w http.ResponseWriter, toolName string, path string, cached bool, status int, code string, message string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	writeJSON(w, status, envelope{
		OK:   false,
		Tool: toolName,
		Error: &apiError{
			Code:    code,
			Message: message,
			Status:  status,
			Details: details,
		},
		Meta: meta{
			UpstreamPath: path,
			Cached:       cached,
		},
	})
}

func extractMeta(data json.RawMessage, header http.Header) meta {
	m := meta{Cached: false}
	if limit := header.Get("X-RateLimit-Limit"); limit != "" {
		m.ensureRateLimit()["limit"] = limit
	}
	if remaining := header.Get("X-RateLimit-Remaining"); remaining != "" {
		m.ensureRateLimit()["remaining"] = remaining
	}
	if tier := header.Get("X-Client-Tier"); tier != "" {
		m.ensureRateLimit()["clientTier"] = tier
	}

	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err == nil {
		m.RequestedDate = obj["requestedDate"]
		m.DataDate = obj["dataDate"]
		m.IsFallback = obj["isFallback"]
		m.Source = obj["_source"]
		if m.DataDate == nil {
			m.DataDate = obj["date"]
		}
	}
	return m
}

func (m *meta) ensureRateLimit() map[string]any {
	if m.RateLimit == nil {
		m.RateLimit = map[string]any{}
	}
	return m.RateLimit
}

func upstreamCode(status int) string {
	return fmt.Sprintf("upstream_%d", statusCodeOrFallback(status))
}

func upstreamMessage(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "upstream unauthorized"
	case http.StatusForbidden:
		return "upstream forbidden"
	case http.StatusTooManyRequests:
		return "upstream rate limit exceeded"
	default:
		return "upstream returned an error"
	}
}

func statusCodeOrFallback(status int) int {
	if status == 0 {
		return http.StatusBadGateway
	}
	return status
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

var _ cacheStore = (*cache.Store)(nil)
