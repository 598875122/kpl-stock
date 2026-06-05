package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultBaseURL   = "http://124.222.49.67:3000"
	defaultListen    = "127.0.0.1:8787"
	defaultCachePath = "./data/kpl-cache.sqlite"
)

type Config struct {
	KPLBaseURL string
	KPLAPIKey  string
	KPLTimeout time.Duration
	ListenAddr string
	CachePath  string
}

func Load() (Config, error) {
	cfg := Config{
		KPLBaseURL: getEnv("KPL_BASE_URL", defaultBaseURL),
		KPLAPIKey:  os.Getenv("KPL_API_KEY"),
		KPLTimeout: 10 * time.Second,
		ListenAddr: getEnv("KPL_LISTEN_ADDR", defaultListen),
		CachePath:  getEnv("KPL_CACHE_PATH", defaultCachePath),
	}

	if cfg.KPLAPIKey == "" {
		return Config{}, errors.New("KPL_API_KEY is required")
	}

	if raw := os.Getenv("KPL_TIMEOUT_SECONDS"); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds <= 0 {
			return Config{}, fmt.Errorf("KPL_TIMEOUT_SECONDS must be a positive integer")
		}
		cfg.KPLTimeout = time.Duration(seconds) * time.Second
	}

	return cfg, nil
}

func getEnv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
