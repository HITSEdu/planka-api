package server

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		next.ServeHTTP(w, r)

		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(startedAt))
	})
}

func corsMiddleware(appEnv string, allowedOrigins []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With, Accept, Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Max-Age", "86400")
		w.Header().Set("Vary", "Origin")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func normalizeOrigin(origin string) string {
	return strings.TrimRight(strings.TrimSpace(origin), "/")
}

func allowedHeaders(r *http.Request) string {
	requested := strings.TrimSpace(r.Header.Get("Access-Control-Request-Headers"))
	if requested != "" {
		return requested
	}

	return "Authorization, Content-Type"
}

func isLocalDevelopmentOrigin(origin string) bool {
	parsedOrigin, err := url.Parse(origin)
	if err != nil {
		return false
	}

	if parsedOrigin.Scheme != "http" && parsedOrigin.Scheme != "https" {
		return false
	}

	switch parsedOrigin.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}
