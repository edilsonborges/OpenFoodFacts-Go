package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/edilsonborges/openfoodfacts-go/internal/config"
	"github.com/edilsonborges/openfoodfacts-go/internal/database"
	"github.com/edilsonborges/openfoodfacts-go/internal/handler"
	"github.com/edilsonborges/openfoodfacts-go/internal/imagecache"
	"github.com/edilsonborges/openfoodfacts-go/internal/middleware"
	"github.com/edilsonborges/openfoodfacts-go/internal/scheduler"
)

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

	// Database
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

	// Image cache
	imgCache := imagecache.New(cfg)

	// Handlers
	productHandler := handler.NewProductHandler(db)
	searchHandler := handler.NewSearchHandler(db)
	imageHandler := handler.NewImageHandler(imgCache)
	statsHandler := handler.NewStatsHandler(db, imgCache)

	// Router (Go 1.22+ enhanced routing)
	mux := http.NewServeMux()

	// Stats — no auth (health check)
	mux.HandleFunc("GET /api/v1/stats", statsHandler.Stats)

	// Protected routes
	mux.HandleFunc("GET /api/v1/product/{barcode}", middleware.APIKey(cfg.APIKey, productHandler.GetByBarcode))
	mux.HandleFunc("GET /api/v1/search", middleware.APIKey(cfg.APIKey, searchHandler.Search))
	mux.HandleFunc("GET /api/v1/image/{barcode}/{imgtype}/{resolution}", middleware.APIKey(cfg.APIKey, imageHandler.GetImage))
	mux.HandleFunc("POST /api/v1/dataset/refresh", middleware.APIKey(cfg.APIKey, func(w http.ResponseWriter, r *http.Request) {
		go func() {
			if err := scheduler.RefreshDataset(cfg, db); err != nil {
				slog.Error("dataset refresh failed", "error", err)
			}
		}()
		handler.JSON(w, http.StatusAccepted, map[string]string{"message": "Dataset refresh started in background"})
	}))

	// Middleware stack
	var h http.Handler = mux
	h = middleware.Logger(h)
	h = middleware.CORS(h)

	// Server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      h,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second, // longer for image proxy
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
