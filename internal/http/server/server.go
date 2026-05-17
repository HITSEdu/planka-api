package server

import (
	"database/sql"
	"net/http"
	"time"

	"planka-api/internal/config"
	"planka-api/internal/http/handlers"
)

func New(cfg config.Config, db *sql.DB) *http.Server {
	mux := http.NewServeMux()
	authHandler := handlers.NewAuth(db, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)

	mux.HandleFunc("GET /healthz", handlers.Health)
	mux.HandleFunc("POST /auth/register", authHandler.Register)
	mux.HandleFunc("POST /auth/login", authHandler.Login)
	mux.HandleFunc("POST /auth/refresh", authHandler.Refresh)
	mux.HandleFunc("POST /auth/logout", authHandler.Logout)
	mux.HandleFunc("GET /auth/me", authHandler.Me)
	mux.HandleFunc("POST /oauth/token", authHandler.Token)
	mux.HandleFunc("POST /oauth/revoke", authHandler.Revoke)

	return &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           loggingMiddleware(corsMiddleware(cfg.CORSOrigins, mux)),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       cfg.HTTPReadTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
	}
}
