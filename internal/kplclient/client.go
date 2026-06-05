package kplclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
}

type Client struct {
	baseURL    *url.URL
	apiKey     string
	httpClient *http.Client
}

type Request struct {
	Method string
	Path   string
	Query  url.Values
}

type Response struct {
	StatusCode int
	Header     http.Header
	Body       json.RawMessage
}

type UpstreamError struct {
	StatusCode int
	Body       []byte
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream returned HTTP %d", e.StatusCode)
}

func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("base URL is required")
	}
	if cfg.APIKey == "" {
		return nil, errors.New("API key is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}

	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}

	return &Client{
		baseURL: parsed,
		apiKey:  cfg.APIKey,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}, nil
}

func (c *Client) Do(ctx context.Context, req Request) (Response, error) {
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := c.doOnce(ctx, method, req.Path, req.Query)
		if err == nil {
			return resp, nil
		}

		lastErr = err
		if !shouldRetry(method, err) || attempt == 2 {
			break
		}

		timer := time.NewTimer(time.Duration(attempt+1) * 150 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Response{}, ctx.Err()
		case <-timer.C:
		}
	}

	return Response{}, lastErr
}

func (c *Client) doOnce(ctx context.Context, method string, path string, query url.Values) (Response, error) {
	u := *c.baseURL
	u.Path = strings.TrimRight(c.baseURL.Path, "/") + ensureLeadingSlash(path)
	u.RawQuery = query.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("Accept", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return Response{}, err
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return Response{}, err
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return Response{}, &UpstreamError{StatusCode: httpResp.StatusCode, Body: body}
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		trimmed = []byte("null")
	}
	if !json.Valid(trimmed) {
		trimmed, err = json.Marshal(string(body))
		if err != nil {
			return Response{}, err
		}
	}

	return Response{
		StatusCode: httpResp.StatusCode,
		Header:     httpResp.Header.Clone(),
		Body:       json.RawMessage(trimmed),
	}, nil
}

func shouldRetry(method string, err error) bool {
	if method != http.MethodGet {
		return false
	}

	var upstream *UpstreamError
	if errors.As(err, &upstream) {
		return upstream.StatusCode >= 500
	}

	return true
}

func ensureLeadingSlash(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}
