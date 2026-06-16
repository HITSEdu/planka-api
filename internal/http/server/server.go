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
	scheduleHandler := handlers.NewSchedules(db)
	eventHandler := handlers.NewEvents(db)

	mux.HandleFunc("GET /healthz", handlers.Health)
	mux.HandleFunc("GET /swagger", handlers.SwaggerUI)
	mux.HandleFunc("GET /swagger/", handlers.SwaggerUI)
	mux.HandleFunc("GET /openapi.json", handlers.OpenAPI)
	mux.HandleFunc("POST /auth/register", authHandler.Register)
	mux.HandleFunc("POST /auth/login", authHandler.Login)
	mux.HandleFunc("POST /auth/refresh", authHandler.Refresh)
	mux.HandleFunc("POST /auth/logout", authHandler.Logout)
	mux.HandleFunc("GET /auth/me", authHandler.Me)
	mux.HandleFunc("POST /api/Auth/register", authHandler.APIRegister)
	mux.HandleFunc("POST /api/Auth/login", authHandler.APILogin)
	mux.HandleFunc("POST /api/Auth/refresh", authHandler.APIRefresh)
	mux.HandleFunc("POST /api/Auth/logout", authHandler.APILogout)
	mux.HandleFunc("POST /api/Auth/revoke_all", authHandler.APIRevokeAll)
	mux.HandleFunc("GET /api/Profile", authHandler.APIProfile)
	mux.HandleFunc("POST /oauth/token", authHandler.Token)
	mux.HandleFunc("POST /oauth/revoke", authHandler.Revoke)
	mux.HandleFunc("GET /schedules", scheduleHandler.List)
	mux.HandleFunc("POST /schedules", scheduleHandler.Create)
	mux.HandleFunc("GET /schedules/{id}", scheduleHandler.Get)
	mux.HandleFunc("PATCH /schedules/{id}", scheduleHandler.Update)
	mux.HandleFunc("DELETE /schedules/{id}", scheduleHandler.Delete)
	mux.HandleFunc("GET /events", eventHandler.List)
	mux.HandleFunc("POST /events", eventHandler.Create)
	mux.HandleFunc("GET /events/{id}", eventHandler.Get)
	mux.HandleFunc("PATCH /events/{id}", eventHandler.Update)
	mux.HandleFunc("DELETE /events/{id}", eventHandler.Delete)

	return &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           loggingMiddleware(corsMiddleware(cfg.CORSOrigins, mux)),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       cfg.HTTPReadTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
	}
}
