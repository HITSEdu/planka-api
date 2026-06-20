package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	AppEnv           string
	HTTPPort         string
	HTTPReadTimeout  time.Duration
	HTTPWriteTimeout time.Duration
	HTTPIdleTimeout  time.Duration
	DatabaseURL      string
	CORSOrigins      []string
	AccessTokenTTL   time.Duration
	RefreshTokenTTL  time.Duration
}

func Load() (Config, error) {
	readTimeout, err := durationFromEnv("HTTP_READ_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}

	writeTimeout, err := durationFromEnv("HTTP_WRITE_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}

	idleTimeout, err := durationFromEnv("HTTP_IDLE_TIMEOUT", 30*time.Second)
	if err != nil {
		return Config{}, err
	}

	accessTokenTTL, err := durationFromEnv("ACCESS_TOKEN_TTL", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}

	refreshTokenTTL, err := durationFromEnv("REFRESH_TOKEN_TTL", 30*24*time.Hour)
	if err != nil {
		return Config{}, err
	}

	return Config{
		AppEnv:           stringFromEnv("APP_ENV", "development"),
		HTTPPort:         stringFromEnv("HTTP_PORT", "8080"),
		HTTPReadTimeout:  readTimeout,
		HTTPWriteTimeout: writeTimeout,
		HTTPIdleTimeout:  idleTimeout,
		DatabaseURL:      stringFromEnv("DATABASE_URL", "postgres://plankauser:postgres@amvera-yungoleg-cnpg-planka-rw:5432/plankaapi?sslmode=disable"),
		CORSOrigins: csvFromEnv("CORS_ORIGINS", []string{
			"http://localhost:3000",
			"http://localhost:5173",
			"http://127.0.0.1:3000",
			"http://127.0.0.1:5173",
			"https://planka-web.vercel.app/",
		}),
		AccessTokenTTL:  accessTokenTTL,
		RefreshTokenTTL: refreshTokenTTL,
	}, nil
}

func stringFromEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	return value
}

func csvFromEnv(key string, fallback []string) []string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}

	if len(result) == 0 {
		return fallback
	}

	return result
}

func durationFromEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: parse duration: %w", key, err)
	}

	return duration, nil
}
