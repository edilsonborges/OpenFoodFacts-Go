package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/edilsonborges/openfoodfacts-go/internal/config"
	"github.com/edilsonborges/openfoodfacts-go/internal/database"
	"github.com/edilsonborges/openfoodfacts-go/internal/imagecache"
)

// JSON writes a JSON response.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("json marshal failed", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal serialization error"}`))
		return
	}
	w.WriteHeader(status)
	w.Write(data)
	w.Write([]byte("\n"))
}

// --- Product Handler ---

type ProductHandler struct {
	db *database.DB
}

func NewProductHandler(db *database.DB) *ProductHandler {
	return &ProductHandler{db: db}
}

func (h *ProductHandler) GetByBarcode(w http.ResponseWriter, r *http.Request) {
	barcode := r.PathValue("barcode")
	if barcode == "" {
		JSON(w, http.StatusBadRequest, map[string]string{"error": "barcode required"})
		return
	}

	product, err := h.db.GetByBarcode(barcode)
	if err != nil {
		JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if product == nil {
		JSON(w, http.StatusNotFound, map[string]any{
			"code":    barcode,
			"found":   false,
			"message": "Product not found in local database",
		})
		return
	}

	JSON(w, http.StatusOK, product)
}

// --- Search Handler ---

type SearchHandler struct {
	db *database.DB
}

func NewSearchHandler(db *database.DB) *SearchHandler {
	return &SearchHandler{db: db}
}

func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		JSON(w, http.StatusBadRequest, map[string]string{"error": "query parameter 'q' required"})
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
	}

	// Optional country filter, e.g. ?country=en:brazil
	country := r.URL.Query().Get("country")

	results, err := h.db.Search(q, limit, country)
	if err != nil {
		JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	JSON(w, http.StatusOK, map[string]any{
		"query":    q,
		"count":    len(results),
		"products": results,
	})
}

// --- Image Handler ---

type ImageHandler struct {
	cache *imagecache.Cache
}

func NewImageHandler(cache *imagecache.Cache) *ImageHandler {
	return &ImageHandler{cache: cache}
}

func (h *ImageHandler) GetImage(w http.ResponseWriter, r *http.Request) {
	barcode := r.PathValue("barcode")
	imgType := r.PathValue("imgtype")
	resStr := r.PathValue("resolution")

	resolution := 400
	if n, err := strconv.Atoi(resStr); err == nil {
		resolution = n
	}

	data, err := h.cache.GetImage(barcode, imgType, resolution)
	if err != nil || data == nil {
		http.Error(w, "Image not available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=2592000") // 30 days
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}

// --- Stats Handler ---

type StatsHandler struct {
	db    *database.DB
	cache *imagecache.Cache
	cfg   *config.Config
}

func NewStatsHandler(db *database.DB, cache *imagecache.Cache, cfg *config.Config) *StatsHandler {
	return &StatsHandler{db: db, cache: cache, cfg: cfg}
}

func (h *StatsHandler) Stats(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(h.db.StartTime()).Hours()

	resp := map[string]any{
		"status":                "ok",
		"dataset_date":          h.db.DatasetDate,
		"total_products":        h.db.TotalProducts,
		"cached_images":         h.cache.CachedImageCount(),
		"cached_images_size_mb": h.cache.CachedImageSizeMB(),
		"duckdb_version":        h.db.DuckDBVersion,
		"uptime_hours":          int(uptime),
		"parquet_url":           h.cfg.ParquetURL,
	}

	if info, err := os.Stat(h.cfg.ParquetPath); err == nil {
		resp["parquet_size_mb"] = int(info.Size() / (1024 * 1024))
	}

	JSON(w, http.StatusOK, resp)
}
