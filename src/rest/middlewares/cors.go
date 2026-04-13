package middlewares

import (
	"net/http"
	"strings"

	"github.com/gin-contrib/cors"
)

func CORS(allowedOrigins []string) cors.Config {
	origins := normalizeOrigins(allowedOrigins)
	return cors.Config{
		AllowOrigins:     origins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		AllowCredentials: false,
		MaxAge:           12 * 60 * 60,
	}
}

func WSOriginChecker(allowedOrigins []string, allowEmptyOrigin bool) func(r *http.Request) bool {
	allowedSet := make(map[string]struct{})
	for _, origin := range normalizeOrigins(allowedOrigins) {
		allowedSet[origin] = struct{}{}
	}

	return func(r *http.Request) bool {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			return allowEmptyOrigin
		}
		_, ok := allowedSet[origin]
		return ok
	}
}

func normalizeOrigins(items []string) []string {
	if len(items) == 0 {
		return []string{"http://localhost:5173", "http://127.0.0.1:5173"}
	}

	out := make([]string, 0, len(items))
	seen := make(map[string]struct{})
	for _, item := range items {
		trimmed := strings.TrimRight(strings.TrimSpace(item), "/")
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}

	if len(out) == 0 {
		return []string{"http://localhost:5173", "http://127.0.0.1:5173"}
	}

	return out
}
