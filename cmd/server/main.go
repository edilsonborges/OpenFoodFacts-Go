package main

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/edilsonborges/openfoodfacts-go/internal/auth"
	"github.com/edilsonborges/openfoodfacts-go/internal/config"
	"github.com/edilsonborges/openfoodfacts-go/internal/database"
	"github.com/edilsonborges/openfoodfacts-go/internal/handler"
	"github.com/edilsonborges/openfoodfacts-go/internal/imagecache"
	"github.com/edilsonborges/openfoodfacts-go/internal/middleware"
	"github.com/edilsonborges/openfoodfacts-go/internal/scheduler"
)

//go:embed web/*
var webFS embed.FS

func main() {
	// Logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// Config
	cfg := config.Load()
	slog.Info("OFF Barcode Lookup Server starting",
		"port", cfg.Port,
		"duckdb_path", cfg.DuckDBPath,
		"duckdb_memory", cfg.DuckDBMemoryLimit,
	)

	// DuckDB (product dataset)
	db, err := database.New(cfg)
	if err != nil {
		slog.Error("failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Initialize(); err != nil {
		slog.Error("failed to initialize products table", "error", err)
		os.Exit(1)
	}

	// SQLite (users + custom products)
	sqlite, err := database.NewSQLite(cfg.SQLitePath)
	if err != nil {
		slog.Error("failed to initialize SQLite", "error", err)
		os.Exit(1)
	}
	defer sqlite.Close()

	// Seed admin user
	if err := sqlite.SeedAdmin(uuid.New().String(), cfg.AdminEmail, cfg.AdminName, cfg.AdminPassword); err != nil {
		slog.Error("failed to seed admin user", "error", err)
		os.Exit(1)
	}

	// JWT service
	jwtSvc := auth.NewJWTService(cfg.JWTSecret)

	// Image cache
	imgCache := imagecache.New(cfg)

	// Handlers
	authHandler := auth.NewHandler(sqlite, jwtSvc)
	customProductHandler := handler.NewCustomProductHandler(sqlite, db)
	searchHandler := handler.NewSearchHandler(db)
	imageHandler := handler.NewImageHandler(imgCache)
	statsHandler := handler.NewStatsHandler(db, imgCache, cfg)

	// Auth middleware
	protect := middleware.JWTAuth(jwtSvc, cfg.APIKey)

	// Router (Go 1.22+ enhanced routing)
	mux := http.NewServeMux()

	// --- Auth routes (public) ---
	mux.HandleFunc("POST /api/v1/auth/login", authHandler.Login)
	mux.HandleFunc("POST /api/v1/auth/refresh", authHandler.Refresh)
	mux.HandleFunc("POST /api/v1/auth/register", protect(authHandler.Register))
	mux.HandleFunc("GET /api/v1/auth/me", protect(authHandler.Me))

	// --- Stats (public health check) ---
	mux.HandleFunc("GET /api/v1/stats", statsHandler.Stats)

	// --- Unified barcode lookup (custom + OFF) ---
	mux.HandleFunc("GET /api/v1/product/{barcode}", protect(customProductHandler.UnifiedLookup))

	// --- Search (OFF dataset) ---
	mux.HandleFunc("GET /api/v1/search", protect(searchHandler.Search))

	// --- Image proxy ---
	mux.HandleFunc("GET /api/v1/image/{barcode}/{imgtype}/{resolution}", protect(imageHandler.GetImage))

	// --- Custom products CRUD ---
	mux.HandleFunc("POST /api/v1/products", protect(customProductHandler.Create))
	mux.HandleFunc("GET /api/v1/products", protect(customProductHandler.List))
	mux.HandleFunc("GET /api/v1/products/{id}", protect(customProductHandler.Get))
	mux.HandleFunc("PUT /api/v1/products/{id}", protect(customProductHandler.Update))
	mux.HandleFunc("DELETE /api/v1/products/{id}", protect(customProductHandler.Delete))

	// --- Dataset refresh ---
	mux.HandleFunc("POST /api/v1/dataset/refresh", protect(func(w http.ResponseWriter, r *http.Request) {
		go func() {
			if err := scheduler.RefreshDataset(cfg, db); err != nil {
				slog.Error("dataset refresh failed", "error", err)
			}
		}()
		handler.JSON(w, http.StatusAccepted, map[string]string{"message": "Dataset refresh started in background"})
	}))

	// --- Web frontend (embedded) ---
	webContent, err := fs.Sub(webFS, "web")
	if err != nil {
		slog.Error("failed to load embedded web files", "error", err)
		os.Exit(1)
	}
	fileServer := http.FileServer(http.FS(webContent))
	mux.Handle("GET /", fileServer)

	// Middleware stack
	var h http.Handler = mux
	h = middleware.Logger(h)
	h = middleware.CORS(h)

	// Server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      h,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Background scheduler
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go scheduler.Run(ctx, cfg, db, imgCache)

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("server listening", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-done
	slog.Info("shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)

	slog.Info("server stopped")
}
