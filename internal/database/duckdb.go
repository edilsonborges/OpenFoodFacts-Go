package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/marcboeker/go-duckdb"

	"github.com/edilsonborges/openfoodfacts-go/internal/config"
)

type DB struct {
	conn      *sql.DB
	cfg       *config.Config
	mu        sync.RWMutex
	startTime time.Time

	TotalProducts int64
	DatasetDate   string
	DuckDBVersion string
}

type Product struct {
	Code            string          `json:"code"`
	ProductName     string          `json:"product_name"`
	Brands          string          `json:"brands"`
	Quantity        string          `json:"quantity"`
	NutriscoreGrade string          `json:"nutriscore_grade,omitempty"`
	NovaGroup       *int            `json:"nova_group,omitempty"`
	Categories      []string        `json:"categories,omitempty"`
	Countries       []string        `json:"countries,omitempty"`
	Allergens       []string        `json:"allergens,omitempty"`
	Labels          []string        `json:"labels,omitempty"`
	Nutriments      json.RawMessage `json:"nutriments,omitempty"`
	ImageURL        string          `json:"image_url"`
	ImageThumbURL   string          `json:"image_thumb_url"`
	Source          string          `json:"source"`
	LastModified    string          `json:"last_modified,omitempty"`
}

type ProductSummary struct {
	Code          string `json:"code"`
	ProductName   string `json:"product_name"`
	Brands        string `json:"brands"`
	ImageThumbURL string `json:"image_thumb_url"`
}

func New(cfg *config.Config) (*DB, error) {
	conn, err := sql.Open("duckdb", cfg.DuckDBPath)
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}
	conn.SetMaxOpenConns(1)

	return &DB{
		conn:      conn,
		cfg:       cfg,
		startTime: time.Now(),
	}, nil
}

func (db *DB) Initialize() error {
	slog.Info("initializing DuckDB", "path", db.cfg.DuckDBPath)

	db.exec(fmt.Sprintf("SET memory_limit = '%s'", db.cfg.DuckDBMemoryLimit))
	db.exec(fmt.Sprintf("SET threads = %s", db.cfg.DuckDBThreads))

	row := db.conn.QueryRow("SELECT version()")
	row.Scan(&db.DuckDBVersion)
	slog.Info("DuckDB version", "version", db.DuckDBVersion)

	var tableCount int
	db.conn.QueryRow("SELECT count(*) FROM information_schema.tables WHERE table_name = 'products'").Scan(&tableCount)

	if tableCount == 0 {
		// Check if parquet file exists before attempting import
		if _, statErr := os.Stat(db.cfg.ParquetPath); os.IsNotExist(statErr) {
			slog.Warn("parquet file not found, creating empty products table", "path", db.cfg.ParquetPath)
			db.exec(`CREATE TABLE products (
				code VARCHAR, product_name VARCHAR[], brands VARCHAR,
				categories_tags VARCHAR[], countries_tags VARCHAR[],
				nutriscore_grade VARCHAR, nova_group INTEGER,
				quantity VARCHAR, serving_size VARCHAR,
				allergens_tags VARCHAR[], labels_tags VARCHAR[],
				stores_tags VARCHAR, nutriments JSON,
				images VARCHAR[], last_modified_t BIGINT
			)`)
			db.exec("CREATE INDEX idx_code ON products(code)")
		} else {
			slog.Info("importing Parquet dataset (this may take a few minutes)...", "path", db.cfg.ParquetPath)
			if err := db.importParquet(db.cfg.ParquetPath); err != nil {
				return fmt.Errorf("import parquet: %w", err)
			}
		}
	} else {
		slog.Info("products table already exists, skipping import")
	}

	db.refreshStats()
	slog.Info("database ready", "total_products", db.TotalProducts, "dataset_date", db.DatasetDate)
	return nil
}

func (db *DB) importParquet(path string) error {
	query := fmt.Sprintf(`
		CREATE TABLE products AS
		SELECT
			CAST(code AS VARCHAR) AS code,
			product_name,
			brands,
			categories_tags,
			countries_tags,
			nutriscore_grade,
			nova_group,
			quantity,
			serving_size,
			allergens_tags,
			labels_tags,
			stores_tags,
			nutriments,
			images,
			last_modified_t
		FROM read_parquet('%s')
		WHERE code IS NOT NULL
		  AND code != ''
		  AND length(code) >= 4
	`, path)

	if _, err := db.conn.Exec(query); err != nil {
		return err
	}

	if _, err := db.conn.Exec("CREATE UNIQUE INDEX idx_code ON products(code)"); err != nil {
		slog.Warn("unique index failed (duplicates?), creating non-unique", "error", err)
		db.exec("CREATE INDEX idx_code ON products(code)")
	}

	return nil
}

func (db *DB) refreshStats() {
	db.conn.QueryRow("SELECT count(*) FROM products").Scan(&db.TotalProducts)
	db.conn.QueryRow("SELECT COALESCE(strftime(max(to_timestamp(last_modified_t)), '%Y-%m-%d'), 'unknown') FROM products").Scan(&db.DatasetDate)
}

// GetByBarcode looks up a product by its barcode.
// IMPORTANT: We CAST all list/array columns to VARCHAR in SQL so Go can scan them as strings.
// Then we parse the string representation in Go.
func (db *DB) GetByBarcode(barcode string) (*Product, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	normalized := strings.TrimLeft(barcode, "0")

	row := db.conn.QueryRow(`
		SELECT
			code,
			COALESCE(product_name[1].text, ''),
			COALESCE(brands, ''),
			COALESCE(quantity, ''),
			nutriscore_grade,
			nova_group,
			CAST(categories_tags AS VARCHAR),
			CAST(countries_tags AS VARCHAR),
			CAST(allergens_tags AS VARCHAR),
			CAST(labels_tags AS VARCHAR),
			CAST(nutriments AS VARCHAR),
			last_modified_t
		FROM products
		WHERE code = ? OR code = ?
		LIMIT 1
	`, barcode, normalized)

	var (
		code, name, brands, qty string
		nutriscore              sql.NullString
		novaGroup               sql.NullInt64
		categoriesRaw           sql.NullString
		countriesRaw            sql.NullString
		allergensRaw            sql.NullString
		labelsRaw               sql.NullString
		nutrimentsRaw           sql.NullString
		lastModifiedT           sql.NullInt64
	)

	err := row.Scan(
		&code, &name, &brands, &qty,
		&nutriscore, &novaGroup,
		&categoriesRaw, &countriesRaw, &allergensRaw, &labelsRaw,
		&nutrimentsRaw, &lastModifiedT,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan product: %w", err)
	}

	p := &Product{
		Code:          code,
		ProductName:   name,
		Brands:        brands,
		Quantity:      qty,
		Categories:    parseTags(categoriesRaw),
		Countries:     parseTags(countriesRaw),
		Allergens:     parseTags(allergensRaw),
		Labels:        parseTags(labelsRaw),
		ImageURL:      fmt.Sprintf("/api/v1/image/%s/front/400", code),
		ImageThumbURL: fmt.Sprintf("/api/v1/image/%s/front/200", code),
		Source:        "openfoodfacts",
	}

	if nutriscore.Valid {
		p.NutriscoreGrade = nutriscore.String
	}
	if novaGroup.Valid {
		v := int(novaGroup.Int64)
		p.NovaGroup = &v
	}
	if nutrimentsRaw.Valid && nutrimentsRaw.String != "" {
		// Validate that the string is valid JSON before assigning
		if json.Valid([]byte(nutrimentsRaw.String)) {
			p.Nutriments = json.RawMessage(nutrimentsRaw.String)
		}
	}
	if lastModifiedT.Valid {
		t := time.Unix(lastModifiedT.Int64, 0).UTC()
		p.LastModified = t.Format(time.RFC3339)
	}

	return p, nil
}

// Search finds products by name or brand.
// Supports optional country filter: ?country=en:brazil (default: no filter)
func (db *DB) Search(query string, limit int, country string) ([]ProductSummary, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	if limit < 1 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	pattern := "%" + query + "%"

	var rows *sql.Rows
	var err error

	if country != "" {
		rows, err = db.conn.Query(`
			SELECT code, COALESCE(product_name[1].text, ''), COALESCE(brands, '')
			FROM products
			WHERE (product_name[1].text ILIKE ? OR brands ILIKE ?)
			  AND list_contains(countries_tags, ?)
			LIMIT ?
		`, pattern, pattern, country, limit)
	} else {
		rows, err = db.conn.Query(`
			SELECT code, COALESCE(product_name[1].text, ''), COALESCE(brands, '')
			FROM products
			WHERE product_name[1].text ILIKE ? OR brands ILIKE ?
			LIMIT ?
		`, pattern, pattern, limit)
	}

	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	var results []ProductSummary
	for rows.Next() {
		var s ProductSummary
		if err := rows.Scan(&s.Code, &s.ProductName, &s.Brands); err != nil {
			continue
		}
		s.ImageThumbURL = fmt.Sprintf("/api/v1/image/%s/front/100", s.Code)
		results = append(results, s)
	}
	if results == nil {
		results = []ProductSummary{}
	}
	return results, nil
}

func (db *DB) RefreshFromParquet(path string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	slog.Info("rebuilding products table from new parquet...")

	query := fmt.Sprintf(`
		CREATE OR REPLACE TABLE products_new AS
		SELECT
			CAST(code AS VARCHAR) AS code,
			product_name, brands, categories_tags, countries_tags,
			nutriscore_grade, nova_group, quantity, serving_size,
			allergens_tags, labels_tags, stores_tags,
			nutriments, images, last_modified_t
		FROM read_parquet('%s')
		WHERE code IS NOT NULL AND code != '' AND length(code) >= 4
	`, path)

	if _, err := db.conn.Exec(query); err != nil {
		return fmt.Errorf("create products_new: %w", err)
	}

	db.exec("DROP TABLE IF EXISTS products_old")
	db.exec("ALTER TABLE products RENAME TO products_old")
	db.exec("ALTER TABLE products_new RENAME TO products")
	db.exec("DROP TABLE IF EXISTS products_old")

	if _, err := db.conn.Exec("CREATE UNIQUE INDEX idx_code ON products(code)"); err != nil {
		db.exec("CREATE INDEX idx_code ON products(code)")
	}

	db.refreshStats()
	slog.Info("dataset refresh complete", "total_products", db.TotalProducts, "date", db.DatasetDate)
	return nil
}

func (db *DB) StartTime() time.Time { return db.startTime }
func (db *DB) Close() error         { return db.conn.Close() }

func (db *DB) exec(sql string) {
	if _, err := db.conn.Exec(sql); err != nil {
		slog.Warn("exec failed", "sql", sql, "error", err)
	}
}

// parseTags parses DuckDB's string representation of a list.
// DuckDB CAST(list AS VARCHAR) produces: [item1, item2, item3]
func parseTags(raw sql.NullString) []string {
	if !raw.Valid || raw.String == "" || raw.String == "[]" {
		return nil
	}
	s := raw.String

	// Try JSON array first
	var tags []string
	if err := json.Unmarshal([]byte(s), &tags); err == nil {
		return tags
	}

	// DuckDB list format: [val1, val2, ...]
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return nil
	}

	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, "'\"")
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
