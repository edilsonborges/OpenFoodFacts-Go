package imagecache

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/edilsonborges/openfoodfacts-go/internal/config"
)

// Cache manages on-disk caching of product images from Open Food Facts S3.
type Cache struct {
	cfg    *config.Config
	client *http.Client
}

// New creates a new image cache.
func New(cfg *config.Config) *Cache {
	os.MkdirAll(cfg.ImageCachePath, 0755)
	return &Cache{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetImage returns image bytes, fetching from S3 on cache miss.
func (c *Cache) GetImage(barcode, imgType string, resolution int) ([]byte, error) {
	// Validate resolution
	switch resolution {
	case 100, 200, 400:
	default:
		resolution = 400
	}

	// Check local cache
	localPath := c.localPath(barcode, imgType, resolution)
	if data, err := os.ReadFile(localPath); err == nil {
		// Update access time for LRU
		now := time.Now()
		os.Chtimes(localPath, now, now)
		return data, nil
	}

	// Try S3 with multiple URL strategies
	urls := c.buildCandidateURLs(barcode, imgType, resolution)

	for _, u := range urls {
		data, err := c.download(u)
		if err == nil && len(data) > 0 {
			// Save to cache
			dir := filepath.Dir(localPath)
			os.MkdirAll(dir, 0755)
			os.WriteFile(localPath, data, 0644)
			slog.Debug("cached image", "barcode", barcode, "type", imgType, "size_kb", len(data)/1024)
			return data, nil
		}
	}

	// Last resort: try the image_front_url directly from the dataset
	if imgType == "front" {
		return c.tryDatasetURL(barcode, resolution, localPath)
	}

	return nil, fmt.Errorf("image not found for %s/%s/%d", barcode, imgType, resolution)
}

// buildCandidateURLs generates multiple possible S3 URLs to try.
func (c *Cache) buildCandidateURLs(barcode, imgType string, resolution int) []string {
	folder := barcodeFolderPath(barcode)
	base := c.cfg.S3BaseURL

	var urls []string

	switch strings.ToLower(imgType) {
	case "front":
		// Try selected images with common language variants and revisions
		for _, lang := range []string{"pt", "en", "fr", "es", ""} {
			var prefix string
			if lang == "" {
				prefix = "front"
			} else {
				prefix = "front_" + lang
			}
			// Try revisions 1-5
			for rev := 1; rev <= 5; rev++ {
				urls = append(urls, fmt.Sprintf("%s/%s/%s.%d.%d.jpg", base, folder, prefix, rev, resolution))
			}
		}
		// Fallback: raw image "1"
		urls = append(urls, fmt.Sprintf("%s/%s/1.%d.jpg", base, folder, resolution))

	case "ingredients":
		for _, lang := range []string{"pt", "en", "fr"} {
			for rev := 1; rev <= 3; rev++ {
				urls = append(urls, fmt.Sprintf("%s/%s/ingredients_%s.%d.%d.jpg", base, folder, lang, rev, resolution))
			}
		}

	case "nutrition":
		for _, lang := range []string{"pt", "en", "fr"} {
			for rev := 1; rev <= 3; rev++ {
				urls = append(urls, fmt.Sprintf("%s/%s/nutrition_%s.%d.%d.jpg", base, folder, lang, rev, resolution))
			}
		}

	default:
		// Raw image by ID
		urls = append(urls, fmt.Sprintf("%s/%s/%s.%d.jpg", base, folder, imgType, resolution))
		urls = append(urls, fmt.Sprintf("%s/%s/%s.jpg", base, folder, imgType))
	}

	return urls
}

// tryDatasetURL tries the OFF images server directly as a fallback.
func (c *Cache) tryDatasetURL(barcode string, resolution int, localPath string) ([]byte, error) {
	// Build URL from images.openfoodfacts.org directly
	folder := barcodeFolderPath(barcode)
	baseURL := "https://images.openfoodfacts.org/images/products"

	// Try raw image "1" at requested resolution
	urls := []string{
		fmt.Sprintf("%s/%s/1.%d.jpg", baseURL, folder, resolution),
		fmt.Sprintf("%s/%s/1.jpg", baseURL, folder),
	}

	for _, url := range urls {
		data, err := c.download(url)
		if err == nil && len(data) > 0 {
			dir := filepath.Dir(localPath)
			os.MkdirAll(dir, 0755)
			os.WriteFile(localPath, data, 0644)
			return data, nil
		}
	}

	return nil, fmt.Errorf("fallback download failed")
}

func (c *Cache) download(url string) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "OpenFoodFacts-Go/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func (c *Cache) localPath(barcode, imgType string, resolution int) string {
	folder := barcodeFolderPath(barcode)
	return filepath.Join(c.cfg.ImageCachePath, folder, fmt.Sprintf("%s_%d.jpg", imgType, resolution))
}

// CachedImageCount returns number of cached images on disk.
func (c *Cache) CachedImageCount() int {
	count := 0
	filepath.Walk(c.cfg.ImageCachePath, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".jpg") {
			count++
		}
		return nil
	})
	return count
}

// CachedImageSizeMB returns total cache size in MB.
func (c *Cache) CachedImageSizeMB() int64 {
	var total int64
	filepath.Walk(c.cfg.ImageCachePath, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total / (1024 * 1024)
}

// Cleanup removes images not accessed within the TTL.
func (c *Cache) Cleanup() {
	cutoff := time.Now().Add(-time.Duration(c.cfg.ImageCacheTTL) * 24 * time.Hour)
	deleted := 0

	filepath.Walk(c.cfg.ImageCachePath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(path)
			deleted++
		}
		return nil
	})

	if deleted > 0 {
		slog.Info("cache cleanup", "deleted", deleted, "ttl_days", c.cfg.ImageCacheTTL)
	}
}

// barcodeFolderPath converts barcode to OFF folder structure.
// Barcodes > 8 chars: 3435660768163 → 343/566/076/8163
// Barcodes ≤ 8 chars: used as-is
func barcodeFolderPath(barcode string) string {
	if len(barcode) > 8 {
		return fmt.Sprintf("%s/%s/%s/%s",
			barcode[:3], barcode[3:6], barcode[6:9], barcode[9:])
	}
	return barcode
}
