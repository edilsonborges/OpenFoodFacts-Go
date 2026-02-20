package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/edilsonborges/openfoodfacts-go/internal/auth"
	"github.com/edilsonborges/openfoodfacts-go/internal/database"
)

// CustomProductHandler handles CRUD for user-managed products.
type CustomProductHandler struct {
	sqlite *database.SQLiteDB
	duckdb *database.DB
}

// NewCustomProductHandler creates a new CRUD handler.
func NewCustomProductHandler(sqlite *database.SQLiteDB, duckdb *database.DB) *CustomProductHandler {
	return &CustomProductHandler{sqlite: sqlite, duckdb: duckdb}
}

type createProductRequest struct {
	Code            string          `json:"code"`
	ProductName     string          `json:"product_name"`
	Brands          string          `json:"brands"`
	Quantity        string          `json:"quantity"`
	NutriscoreGrade string          `json:"nutriscore_grade"`
	NovaGroup       *int            `json:"nova_group"`
	Categories      []string        `json:"categories"`
	Countries       []string        `json:"countries"`
	Allergens       []string        `json:"allergens"`
	Labels          []string        `json:"labels"`
	Nutriments      json.RawMessage `json:"nutriments"`
	ImageURL        string          `json:"image_url"`
	Status          string          `json:"status"`
}

// Create handles POST /api/v1/products
func (h *CustomProductHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Code == "" {
		JSON(w, http.StatusBadRequest, map[string]string{"error": "barcode (code) is required"})
		return
	}
	if req.ProductName == "" {
		JSON(w, http.StatusBadRequest, map[string]string{"error": "product_name is required"})
		return
	}

	status := req.Status
	if status == "" {
		status = "draft"
	}

	createdBy := ""
	if claims, ok := auth.ClaimsFromContext(r.Context()); ok {
		createdBy = claims.UserID
	}

	p := &database.CustomProduct{
		ID:              uuid.New().String(),
		Code:            req.Code,
		ProductName:     req.ProductName,
		Brands:          req.Brands,
		Quantity:        req.Quantity,
		NutriscoreGrade: req.NutriscoreGrade,
		NovaGroup:       req.NovaGroup,
		Categories:      req.Categories,
		Countries:       req.Countries,
		Allergens:       req.Allergens,
		Labels:          req.Labels,
		Nutriments:      req.Nutriments,
		ImageURL:        req.ImageURL,
		Status:          status,
		CreatedBy:       createdBy,
	}

	if err := h.sqlite.CreateProduct(p); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			JSON(w, http.StatusConflict, map[string]string{"error": "product with this barcode already exists"})
			return
		}
		JSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create product"})
		return
	}

	created, _ := h.sqlite.GetProductByID(p.ID)
	JSON(w, http.StatusCreated, created)
}

// List handles GET /api/v1/products
func (h *CustomProductHandler) List(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("q")
	limit := 20
	offset := 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}

	products, total, err := h.sqlite.ListProducts(search, limit, offset)
	if err != nil {
		JSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list products"})
		return
	}

	JSON(w, http.StatusOK, map[string]any{
		"products": products,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

// Get handles GET /api/v1/products/{id}
func (h *CustomProductHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := h.sqlite.GetProductByID(id)
	if err != nil {
		JSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch product"})
		return
	}
	if p == nil {
		JSON(w, http.StatusNotFound, map[string]string{"error": "product not found"})
		return
	}
	JSON(w, http.StatusOK, p)
}

// Update handles PUT /api/v1/products/{id}
func (h *CustomProductHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	existing, err := h.sqlite.GetProductByID(id)
	if err != nil || existing == nil {
		JSON(w, http.StatusNotFound, map[string]string{"error": "product not found"})
		return
	}

	var req createProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	existing.Code = req.Code
	existing.ProductName = req.ProductName
	existing.Brands = req.Brands
	existing.Quantity = req.Quantity
	existing.NutriscoreGrade = req.NutriscoreGrade
	existing.NovaGroup = req.NovaGroup
	existing.Categories = req.Categories
	existing.Countries = req.Countries
	existing.Allergens = req.Allergens
	existing.Labels = req.Labels
	existing.Nutriments = req.Nutriments
	existing.ImageURL = req.ImageURL
	if req.Status != "" {
		existing.Status = req.Status
	}

	if err := h.sqlite.UpdateProduct(existing); err != nil {
		JSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update product"})
		return
	}

	updated, _ := h.sqlite.GetProductByID(id)
	JSON(w, http.StatusOK, updated)
}

// Delete handles DELETE /api/v1/products/{id}
func (h *CustomProductHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	existing, err := h.sqlite.GetProductByID(id)
	if err != nil || existing == nil {
		JSON(w, http.StatusNotFound, map[string]string{"error": "product not found"})
		return
	}

	if err := h.sqlite.DeleteProduct(id); err != nil {
		JSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete product"})
		return
	}

	JSON(w, http.StatusOK, map[string]string{"message": "product deleted"})
}

// UnifiedLookup handles GET /api/v1/product/{barcode} — checks custom products first, then OFF.
func (h *CustomProductHandler) UnifiedLookup(w http.ResponseWriter, r *http.Request) {
	barcode := r.PathValue("barcode")
	if barcode == "" {
		JSON(w, http.StatusBadRequest, map[string]string{"error": "barcode required"})
		return
	}

	// 1. Check custom products first
	custom, err := h.sqlite.GetProductByCode(barcode)
	if err == nil && custom != nil {
		// Convert to the same Product format for consistency
		JSON(w, http.StatusOK, map[string]any{
			"source":  "custom",
			"product": custom,
		})
		return
	}

	// 2. Fallback to Open Food Facts (DuckDB)
	product, err := h.duckdb.GetByBarcode(barcode)
	if err != nil {
		JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if product == nil {
		JSON(w, http.StatusNotFound, map[string]any{
			"code":    barcode,
			"found":   false,
			"message": "Product not found. You can add it as a custom product.",
		})
		return
	}

	JSON(w, http.StatusOK, map[string]any{
		"source":  "openfoodfacts",
		"product": product,
	})
}
