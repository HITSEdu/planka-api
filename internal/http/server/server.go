package server

import (
	"net/http"
	"time"

	"planka-api/internal/config"
	"planka-api/internal/http/handlers"
)

func New(cfg config.Config) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handlers.Health)

	return &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           loggingMiddleware(mux),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       cfg.HTTPReadTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
	}
}
