package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"stock-kpl/internal/cache"
	"stock-kpl/internal/config"
	"stock-kpl/internal/kplclient"
	"stock-kpl/internal/server"
	"stock-kpl/internal/tools"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	kpl, err := kplclient.New(kplclient.Config{
		BaseURL: cfg.KPLBaseURL,
		APIKey:  cfg.KPLAPIKey,
		Timeout: cfg.KPLTimeout,
	})
	if err != nil {
		log.Fatalf("kpl client error: %v", err)
	}

	store, err := cache.Open(cfg.CachePath, 30*24*time.Hour)
	if err != nil {
		log.Fatalf("cache error: %v", err)
	}
	defer store.Close()

	app := server.New(server.Config{
		Version: "0.1.0",
		BaseURL: cfg.KPLBaseURL,
	}, tools.DefaultRegistry(), kpl, store)

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		log.Printf("stock-kpl adapter listening on http://%s", cfg.ListenAddr)
		errs <- httpServer.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-signals:
		log.Printf("received %s, shutting down", sig)
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}
}
