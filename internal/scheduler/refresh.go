package scheduler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/edilsonborges/openfoodfacts-go/internal/config"
	"github.com/edilsonborges/openfoodfacts-go/internal/database"
	"github.com/edilsonborges/openfoodfacts-go/internal/imagecache"
)

// Run starts the background scheduler for dataset refresh and cache cleanup.
func Run(ctx context.Context, cfg *config.Config, db *database.DB, cache *imagecache.Cache) {
	slog.Info("scheduler started", "refresh_hour_utc", cfg.RefreshHourUTC)

	for {
		// Calculate time until next refresh
		now := time.Now().UTC()
		next := time.Date(now.Year(), now.Month(), now.Day(), cfg.RefreshHourUTC, 0, 0, 0, time.UTC)
		if now.After(next) {
			next = next.Add(24 * time.Hour)
		}
		delay := next.Sub(now)

		slog.Info("next scheduled refresh", "at", next.Format(time.RFC3339), "in_hours", int(delay.Hours()))

		select {
		case <-ctx.Done():
			slog.Info("scheduler stopped")
			return
		case <-time.After(delay):
		}

		// Run refresh
		slog.Info("starting scheduled dataset refresh...")
		if err := RefreshDataset(cfg, db); err != nil {
			slog.Error("scheduled refresh failed", "error", err)
		}

		// Cleanup cache
		cache.Cleanup()
	}
}

// RefreshDataset downloads a new Parquet file and swaps the DuckDB table.
func RefreshDataset(cfg *config.Config, db *database.DB) error {
	tempPath := "/data/food_new.parquet"
	finalPath := cfg.ParquetPath

	slog.Info("downloading new dataset from Hugging Face...")

	// Download
	client := &http.Client{Timeout: 1 * time.Hour}
	resp, err := client.Get(cfg.ParquetURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	written, err := io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("write: %w", err)
	}

	slog.Info("download complete", "size_mb", written/(1024*1024))

	// Rebuild DuckDB table
	if err := db.RefreshFromParquet(tempPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("refresh table: %w", err)
	}

	// Move file
	os.Rename(tempPath, finalPath)

	slog.Info("dataset refresh complete")
	return nil
}
