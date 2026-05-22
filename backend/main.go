package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"nullpointer/backend/internal/adapters"
	"nullpointer/backend/internal/handlers"
	"nullpointer/backend/internal/normalizer"
)

func main() {
	addr := getenv("ADDR", ":8080")
	mlURL := getenv("ML_URL", "http://localhost:8000")
	webDir := getenv("WEB_DIR", "web")

	ml := normalizer.NewClient(mlURL)
	ads := adapters.All()

	mux := http.NewServeMux()
	mux.Handle("GET /health", handlers.NewHealthHandler(ml))
	mux.Handle("GET /search", handlers.NewSearchHandler(ads, ml))
	mux.Handle("POST /search", handlers.NewSearchHandler(ads, ml))

	// Static frontend.
	fs := http.FileServer(http.Dir(webDir))
	mux.Handle("GET /", fs)

	srv := &http.Server{
		Addr:              addr,
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("backend listening on %s (ml=%s, web=%s)", addr, mlURL, webDir)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// withCORS enables permissive CORS so the static page can also be opened
// from disk during quick demos.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
