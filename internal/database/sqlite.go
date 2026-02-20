package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"golang.org/x/crypto/bcrypt"
)

// SQLiteDB handles users and custom products.
type SQLiteDB struct {
	conn *sql.DB
}

// User represents an application user.
type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"` // admin, editor, viewer
	CreatedAt string `json:"created_at"`
}

// CustomProduct represents a user-managed product.
type CustomProduct struct {
	ID              string          `json:"id"`
	Code            string          `json:"code"`
	ProductName     string          `json:"product_name"`
	Brands          string          `json:"brands"`
	Quantity        string          `json:"quantity,omitempty"`
	NutriscoreGrade string          `json:"nutriscore_grade,omitempty"`
	NovaGroup       *int            `json:"nova_group,omitempty"`
	Categories      []string        `json:"categories,omitempty"`
	Countries       []string        `json:"countries,omitempty"`
	Allergens       []string        `json:"allergens,omitempty"`
	Labels          []string        `json:"labels,omitempty"`
	Nutriments      json.RawMessage `json:"nutriments,omitempty"`
	ImageURL        string          `json:"image_url,omitempty"`
	Status          string          `json:"status"` // draft, published
	CreatedBy       string          `json:"created_by"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
}

// NewSQLite opens and initializes the SQLite database.
func NewSQLite(path string) (*SQLiteDB, error) {
	conn, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	conn.SetMaxOpenConns(1)

	db := &SQLiteDB{conn: conn}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("migrate sqlite: %w", err)
	}

	slog.Info("SQLite ready", "path", path)
	return db, nil
}

func (db *SQLiteDB) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'editor',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS custom_products (
			id TEXT PRIMARY KEY,
			code TEXT UNIQUE NOT NULL,
			product_name TEXT NOT NULL DEFAULT '',
			brands TEXT NOT NULL DEFAULT '',
			quantity TEXT NOT NULL DEFAULT '',
			nutriscore_grade TEXT,
			nova_group INTEGER,
			categories TEXT NOT NULL DEFAULT '[]',
			countries TEXT NOT NULL DEFAULT '[]',
			allergens TEXT NOT NULL DEFAULT '[]',
			labels TEXT NOT NULL DEFAULT '[]',
			nutriments TEXT,
			image_url TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'draft',
			created_by TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now')),
			FOREIGN KEY (created_by) REFERENCES users(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_custom_products_code ON custom_products(code)`,
	}
	for _, m := range migrations {
		if _, err := db.conn.Exec(m); err != nil {
			return fmt.Errorf("migration: %w", err)
		}
	}
	return nil
}

func (db *SQLiteDB) Close() error { return db.conn.Close() }

// SeedAdmin ensures the admin user exists. Creates it if missing.
func (db *SQLiteDB) SeedAdmin(id, email, name, password string) error {
	var exists int
	db.conn.QueryRow("SELECT count(*) FROM users WHERE email = ?", email).Scan(&exists)
	if exists > 0 {
		return nil
	}
	_, err := db.CreateUser(id, email, name, password, "admin")
	if err != nil {
		return fmt.Errorf("seed admin: %w", err)
	}
	slog.Info("admin user seeded", "email", email)
	return nil
}

// --- User operations ---

// CreateUser creates a new user. Returns the user or error.
func (db *SQLiteDB) CreateUser(id, email, name, password, role string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.conn.Exec(
		`INSERT INTO users (id, email, name, password_hash, role, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, email, name, string(hash), role, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}

	return &User{ID: id, Email: email, Name: name, Role: role, CreatedAt: now}, nil
}

// Authenticate checks email/password and returns the user if valid.
func (db *SQLiteDB) Authenticate(email, password string) (*User, error) {
	var u User
	var hash string
	err := db.conn.QueryRow(
		`SELECT id, email, name, role, created_at, password_hash FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.CreatedAt, &hash)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, nil // wrong password
	}
	return &u, nil
}

// GetUserByID fetches a user by ID.
func (db *SQLiteDB) GetUserByID(id string) (*User, error) {
	var u User
	err := db.conn.QueryRow(
		`SELECT id, email, name, role, created_at FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// UserCount returns the total number of users.
func (db *SQLiteDB) UserCount() int {
	var count int
	db.conn.QueryRow("SELECT count(*) FROM users").Scan(&count)
	return count
}

// --- Custom product operations ---

// CreateProduct inserts a new custom product.
func (db *SQLiteDB) CreateProduct(p *CustomProduct) error {
	cats, _ := json.Marshal(p.Categories)
	countries, _ := json.Marshal(p.Countries)
	allergens, _ := json.Marshal(p.Allergens)
	labels, _ := json.Marshal(p.Labels)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(`
		INSERT INTO custom_products (id, code, product_name, brands, quantity,
			nutriscore_grade, nova_group, categories, countries, allergens, labels,
			nutriments, image_url, status, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Code, p.ProductName, p.Brands, p.Quantity,
		p.NutriscoreGrade, p.NovaGroup,
		string(cats), string(countries), string(allergens), string(labels),
		string(p.Nutriments), p.ImageURL, p.Status, p.CreatedBy, now, now,
	)
	return err
}

// UpdateProduct updates an existing custom product.
func (db *SQLiteDB) UpdateProduct(p *CustomProduct) error {
	cats, _ := json.Marshal(p.Categories)
	countries, _ := json.Marshal(p.Countries)
	allergens, _ := json.Marshal(p.Allergens)
	labels, _ := json.Marshal(p.Labels)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(`
		UPDATE custom_products SET
			code = ?, product_name = ?, brands = ?, quantity = ?,
			nutriscore_grade = ?, nova_group = ?,
			categories = ?, countries = ?, allergens = ?, labels = ?,
			nutriments = ?, image_url = ?, status = ?, updated_at = ?
		WHERE id = ?`,
		p.Code, p.ProductName, p.Brands, p.Quantity,
		p.NutriscoreGrade, p.NovaGroup,
		string(cats), string(countries), string(allergens), string(labels),
		string(p.Nutriments), p.ImageURL, p.Status, now, p.ID,
	)
	return err
}

// DeleteProduct removes a custom product by ID.
func (db *SQLiteDB) DeleteProduct(id string) error {
	_, err := db.conn.Exec("DELETE FROM custom_products WHERE id = ?", id)
	return err
}

// GetProductByID fetches a custom product by its ID.
func (db *SQLiteDB) GetProductByID(id string) (*CustomProduct, error) {
	return db.scanProduct(db.conn.QueryRow(`
		SELECT id, code, product_name, brands, quantity,
			nutriscore_grade, nova_group, categories, countries, allergens, labels,
			nutriments, image_url, status, created_by, created_at, updated_at
		FROM custom_products WHERE id = ?`, id))
}

// GetProductByCode fetches a custom product by barcode.
func (db *SQLiteDB) GetProductByCode(code string) (*CustomProduct, error) {
	return db.scanProduct(db.conn.QueryRow(`
		SELECT id, code, product_name, brands, quantity,
			nutriscore_grade, nova_group, categories, countries, allergens, labels,
			nutriments, image_url, status, created_by, created_at, updated_at
		FROM custom_products WHERE code = ?`, code))
}

// ListProducts returns custom products with pagination.
func (db *SQLiteDB) ListProducts(search string, limit, offset int) ([]CustomProduct, int, error) {
	var total int
	var rows *sql.Rows
	var err error

	if search != "" {
		pattern := "%" + search + "%"
		db.conn.QueryRow(`SELECT count(*) FROM custom_products WHERE product_name LIKE ? OR brands LIKE ? OR code LIKE ?`,
			pattern, pattern, pattern).Scan(&total)
		rows, err = db.conn.Query(`
			SELECT id, code, product_name, brands, quantity,
				nutriscore_grade, nova_group, categories, countries, allergens, labels,
				nutriments, image_url, status, created_by, created_at, updated_at
			FROM custom_products
			WHERE product_name LIKE ? OR brands LIKE ? OR code LIKE ?
			ORDER BY updated_at DESC LIMIT ? OFFSET ?`,
			pattern, pattern, pattern, limit, offset)
	} else {
		db.conn.QueryRow(`SELECT count(*) FROM custom_products`).Scan(&total)
		rows, err = db.conn.Query(`
			SELECT id, code, product_name, brands, quantity,
				nutriscore_grade, nova_group, categories, countries, allergens, labels,
				nutriments, image_url, status, created_by, created_at, updated_at
			FROM custom_products ORDER BY updated_at DESC LIMIT ? OFFSET ?`,
			limit, offset)
	}
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var products []CustomProduct
	for rows.Next() {
		p, err := db.scanProductRow(rows)
		if err != nil {
			continue
		}
		products = append(products, *p)
	}
	if products == nil {
		products = []CustomProduct{}
	}
	return products, total, nil
}

// CustomProductCount returns the number of custom products.
func (db *SQLiteDB) CustomProductCount() int {
	var count int
	db.conn.QueryRow("SELECT count(*) FROM custom_products").Scan(&count)
	return count
}

func (db *SQLiteDB) scanProduct(row *sql.Row) (*CustomProduct, error) {
	var p CustomProduct
	var nutriscore sql.NullString
	var novaGroup sql.NullInt64
	var catsJSON, countriesJSON, allergensJSON, labelsJSON string
	var nutrimentsRaw sql.NullString

	err := row.Scan(
		&p.ID, &p.Code, &p.ProductName, &p.Brands, &p.Quantity,
		&nutriscore, &novaGroup,
		&catsJSON, &countriesJSON, &allergensJSON, &labelsJSON,
		&nutrimentsRaw, &p.ImageURL, &p.Status, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if nutriscore.Valid {
		p.NutriscoreGrade = nutriscore.String
	}
	if novaGroup.Valid {
		v := int(novaGroup.Int64)
		p.NovaGroup = &v
	}
	json.Unmarshal([]byte(catsJSON), &p.Categories)
	json.Unmarshal([]byte(countriesJSON), &p.Countries)
	json.Unmarshal([]byte(allergensJSON), &p.Allergens)
	json.Unmarshal([]byte(labelsJSON), &p.Labels)
	if nutrimentsRaw.Valid && json.Valid([]byte(nutrimentsRaw.String)) {
		p.Nutriments = json.RawMessage(nutrimentsRaw.String)
	}

	return &p, nil
}

func (db *SQLiteDB) scanProductRow(rows *sql.Rows) (*CustomProduct, error) {
	var p CustomProduct
	var nutriscore sql.NullString
	var novaGroup sql.NullInt64
	var catsJSON, countriesJSON, allergensJSON, labelsJSON string
	var nutrimentsRaw sql.NullString

	err := rows.Scan(
		&p.ID, &p.Code, &p.ProductName, &p.Brands, &p.Quantity,
		&nutriscore, &novaGroup,
		&catsJSON, &countriesJSON, &allergensJSON, &labelsJSON,
		&nutrimentsRaw, &p.ImageURL, &p.Status, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if nutriscore.Valid {
		p.NutriscoreGrade = nutriscore.String
	}
	if novaGroup.Valid {
		v := int(novaGroup.Int64)
		p.NovaGroup = &v
	}
	json.Unmarshal([]byte(catsJSON), &p.Categories)
	json.Unmarshal([]byte(countriesJSON), &p.Countries)
	json.Unmarshal([]byte(allergensJSON), &p.Allergens)
	json.Unmarshal([]byte(labelsJSON), &p.Labels)
	if nutrimentsRaw.Valid && json.Valid([]byte(nutrimentsRaw.String)) {
		p.Nutriments = json.RawMessage(nutrimentsRaw.String)
	}

	return &p, nil
}
