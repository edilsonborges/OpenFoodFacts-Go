package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/edilsonborges/openfoodfacts-go/internal/auth"
)

// JWTAuth validates a JWT Bearer token or falls back to X-API-Key.
func JWTAuth(jwtSvc *auth.JWTService, apiKey string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// Try Bearer token first
			if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
				tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
				claims, err := jwtSvc.ValidateToken(tokenStr)
				if err != nil {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					w.Write([]byte(`{"error":"invalid or expired token"}`))
					return
				}
				ctx := auth.ContextWithClaims(r.Context(), claims)
				next(w, r.WithContext(ctx))
				return
			}

			// Fallback to API key for programmatic access
			if key := r.Header.Get("X-API-Key"); key != "" && key == apiKey {
				next(w, r)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"missing or invalid authentication"}`))
		}
	}
}

// RequireRole checks that the authenticated user has one of the allowed roles.
func RequireRole(roles ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.ClaimsFromContext(r.Context())
			if !ok {
				// API key access — allow through (no claims in context)
				next(w, r)
				return
			}
			for _, role := range roles {
				if claims.Role == role {
					next(w, r)
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"insufficient permissions"}`))
		}
	}
}

// Logger logs each request with method, path, status, and duration.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusWriter{ResponseWriter: w, status: 200}

		next.ServeHTTP(wrapped, r)

		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", r.RemoteAddr,
		)
	})
}

// CORS adds permissive CORS headers.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
