package config

import "os"

// Config holds all server configuration loaded from environment variables.
type Config struct {
	// Server
	Port   string
	APIKey string

	// JWT
	JWTSecret  string
	SQLitePath string

	// DuckDB
	DuckDBPath        string
	DuckDBMemoryLimit string
	DuckDBThreads     string
	ParquetPath       string
	ParquetURL        string

	// Image cache
	ImageCachePath  string
	MaxImageCacheGB int
	ImageCacheTTL   int // days
	S3BaseURL       string

	// Pre-cache
	PrecacheBrazilian bool
	PrecacheRateMS    int

	// Scheduler
	RefreshHourUTC int
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	return &Config{
		Port:              envOrDefault("PORT", "8080"),
		APIKey:            envOrDefault("API_KEY", "changeme"),
		JWTSecret:         envOrDefault("JWT_SECRET", "change-this-secret-in-production"),
		SQLitePath:        envOrDefault("SQLITE_PATH", "/data/app.db"),
		DuckDBPath:        envOrDefault("DUCKDB_PATH", "/data/off.duckdb"),
		DuckDBMemoryLimit: envOrDefault("DUCKDB_MEMORY_LIMIT", "2GB"),
		DuckDBThreads:     envOrDefault("DUCKDB_THREADS", "4"),
		ParquetPath:       "/data/food.parquet",
		ParquetURL:        envOrDefault("PARQUET_URL", "https://huggingface.co/datasets/openfoodfacts/product-database/resolve/main/food.parquet?download=true"),
		ImageCachePath:    envOrDefault("IMAGE_CACHE_PATH", "/data/images"),
		MaxImageCacheGB:   envOrDefaultInt("MAX_IMAGE_CACHE_GB", 20),
		ImageCacheTTL:     envOrDefaultInt("IMAGE_CACHE_TTL_DAYS", 90),
		S3BaseURL:         envOrDefault("S3_BASE_URL", "https://openfoodfacts-images.s3.eu-west-3.amazonaws.com/data"),
		PrecacheBrazilian: envOrDefault("PRECACHE_BRAZILIAN", "false") == "true",
		PrecacheRateMS:    envOrDefaultInt("PRECACHE_RATE_MS", 50),
		RefreshHourUTC:    envOrDefaultInt("REFRESH_HOUR_UTC", 4),
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrDefaultInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	var n int
	for _, c := range v {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	if n == 0 {
		return def
	}
	return n
}
