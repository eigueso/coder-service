package api

import (
	"net/http"
	"strings"

	"github.com/malmonte/go-backend/internal/config"
)

// SecurityHeaders sets the same response headers as the FastAPI middleware.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// CORS implements the same policy as the FastAPI CORSMiddleware configuration:
// the configured origins with credentials, any method, any header.
func CORS(settings *config.Settings, next http.Handler) http.Handler {
	allowed := make(map[string]bool)
	for _, origin := range settings.AllowedCORSOrigins() {
		allowed[origin] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || !allowed[strings.TrimRight(origin, "/")] {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Set("Access-Control-Allow-Credentials", "true")
		h.Add("Vary", "Origin")
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH, HEAD")
			requestedHeaders := r.Header.Get("Access-Control-Request-Headers")
			if requestedHeaders == "" {
				requestedHeaders = "*"
			}
			h.Set("Access-Control-Allow-Headers", requestedHeaders)
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
