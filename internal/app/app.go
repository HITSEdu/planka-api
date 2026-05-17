package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"planka-api/internal/config"
	httpserver "planka-api/internal/http/server"
	"planka-api/internal/postgres"
)

type App struct {
	config config.Config
	db     *sql.DB
	server *http.Server
}

func New() (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	db, err := postgres.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	srv := httpserver.New(cfg, db)

	return &App{
		config: cfg,
		db:     db,
		server: srv,
	}, nil
}

func (a *App) Run() (err error) {
	defer func() {
		if closeErr := a.db.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close postgres: %w", closeErr)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)

	go func() {
		log.Printf("starting HTTP server on %s (%s)", a.server.Addr, a.config.AppEnv)

		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		log.Println("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := a.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown server: %w", err)
	}

	return nil
}
