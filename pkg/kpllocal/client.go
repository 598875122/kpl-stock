package kpllocal

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"stock-kpl/internal/cache"
	"stock-kpl/internal/kplclient"
	"stock-kpl/internal/tools"
)

const (
	DefaultBaseURL   = "http://124.222.49.67:3000"
	DefaultCachePath = "/root/kpl-stock/data/kpl-cache.sqlite"
	DefaultWorkers   = 8
)

type Config struct {
	BaseURL   string
	APIKey    string
	Timeout   time.Duration
	CachePath string
	CacheTTL  time.Duration
}

type Client struct {
	registry *tools.Registry
	kpl      *kplclient.Client
	store    *cache.Store
	cacheMu  sync.Mutex
}

type Result struct {
	Tool       string          `json:"tool"`
	Arguments  map[string]any  `json:"arguments,omitempty"`
	Path       string          `json:"path"`
	Cached     bool            `json:"cached"`
	StatusCode int             `json:"statusCode"`
	Data       json.RawMessage `json:"data"`
	Error      string          `json:"error,omitempty"`
}

func LoadConfigFromEnv() (Config, error) {
	loadEnvFiles("/root/kpl-stock/.env", "/root/.hermes/.env")
	cfg := Config{
		BaseURL:   env("KPL_BASE_URL", DefaultBaseURL),
		APIKey:    os.Getenv("KPL_API_KEY"),
		Timeout:   10 * time.Second,
		CachePath: env("KPL_CACHE_PATH", DefaultCachePath),
		CacheTTL:  30 * 24 * time.Hour,
	}
	if cfg.APIKey == "" {
		return Config{}, errors.New("KPL_API_KEY is required")
	}
	if raw := os.Getenv("KPL_TIMEOUT_SECONDS"); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds <= 0 {
			return Config{}, fmt.Errorf("KPL_TIMEOUT_SECONDS must be a positive integer")
		}
		cfg.Timeout = time.Duration(seconds) * time.Second
	}
	if raw := os.Getenv("KPL_CACHE_TTL_HOURS"); raw != "" {
		hours, err := strconv.Atoi(raw)
		if err != nil || hours <= 0 {
			return Config{}, fmt.Errorf("KPL_CACHE_TTL_HOURS must be a positive integer")
		}
		cfg.CacheTTL = time.Duration(hours) * time.Hour
	}
	return cfg, nil
}

func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.CachePath == "" {
		cfg.CachePath = DefaultCachePath
	}
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 30 * 24 * time.Hour
	}

	kpl, err := kplclient.New(kplclient.Config{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Timeout: cfg.Timeout})
	if err != nil {
		return nil, err
	}
	store, err := cache.Open(cfg.CachePath, cfg.CacheTTL)
	if err != nil {
		return nil, err
	}
	return &Client{registry: tools.DefaultRegistry(), kpl: kpl, store: store}, nil
}

func (c *Client) Close() error {
	if c.store == nil {
		return nil
	}
	return c.store.Close()
}

func (c *Client) Tools() []tools.Tool {
	return c.registry.List()
}

func (c *Client) Call(ctx context.Context, toolName string, args map[string]any) (Result, error) {
	tool, ok := c.registry.Get(toolName)
	if !ok {
		return Result{}, fmt.Errorf("unknown tool %q", toolName)
	}
	prepared, err := tool.Prepare(args)
	if err != nil {
		return Result{}, err
	}
	result := Result{Tool: toolName, Arguments: cloneArgs(args), Path: prepared.Path}
	keyArgs := cloneArgs(args)
	keyArgs["__path"] = prepared.Path
	key := cache.Key(prepared.Method, prepared.Path, keyArgs)

	if tool.Cacheable && cache.ShouldCache(args, time.Now()) {
		if entry, hit, err := c.store.Get(ctx, key); err != nil {
			return Result{}, err
		} else if hit {
			result.Cached = true
			result.StatusCode = http.StatusOK
			result.Data = entry.Body
			return result, nil
		}
	}

	resp, err := c.kpl.Do(ctx, kplclient.Request{Method: prepared.Method, Path: prepared.Path, Query: prepared.Query})
	if err != nil {
		return Result{}, err
	}
	result.StatusCode = resp.StatusCode
	result.Data = resp.Body

	if tool.Cacheable && cache.ShouldCache(args, time.Now()) {
		c.cacheMu.Lock()
		err = c.store.Put(ctx, key, resp.Body, map[string][]string(resp.Header))
		c.cacheMu.Unlock()
		if err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

func (c *Client) CallPath(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	resp, err := c.kpl.Do(ctx, kplclient.Request{Method: http.MethodGet, Path: path, Query: query})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (c *Client) CallMany(ctx context.Context, toolName string, codes []string, args map[string]any, workers int) []Result {
	if workers <= 0 {
		workers = DefaultWorkers
	}
	jobs := make(chan string)
	results := make(chan Result, len(codes))
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for code := range jobs {
				callArgs := cloneArgs(args)
				callArgs["code"] = code
				res, err := c.Call(ctx, toolName, callArgs)
				if err != nil {
					res = Result{Tool: toolName, Arguments: callArgs, Error: err.Error()}
				}
				results <- res
			}
		}()
	}

	for _, code := range codes {
		jobs <- code
	}
	close(jobs)
	wg.Wait()
	close(results)

	out := make([]Result, 0, len(codes))
	for res := range results {
		out = append(out, res)
	}
	return out
}

func cloneArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for key, value := range args {
		out[key] = value
	}
	return out
}

func env(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func loadEnvFiles(paths ...string) {
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
				continue
			}
			key, value, _ := strings.Cut(line, "=")
			key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
			if key == "" || os.Getenv(key) != "" {
				continue
			}
			os.Setenv(key, strings.Trim(strings.TrimSpace(value), "'\""))
		}
		file.Close()
	}
}
