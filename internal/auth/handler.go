package auth

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/edilsonborges/openfoodfacts-go/internal/database"
)

// Handler handles authentication endpoints.
type Handler struct {
	db  *database.SQLiteDB
	jwt *JWTService
}

// NewHandler creates a new auth handler.
func NewHandler(db *database.SQLiteDB, jwt *JWTService) *Handler {
	return &Handler{db: db, jwt: jwt}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type registerRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	data, _ := json.Marshal(v)
	w.WriteHeader(status)
	w.Write(data)
}

// Login authenticates a user and returns JWT tokens.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Email == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password required"})
		return
	}

	user, err := h.db.Authenticate(req.Email, req.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "authentication failed"})
		return
	}
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid email or password"})
		return
	}

	tokens, err := h.jwt.GenerateTokens(user.ID, user.Email, user.Role)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate tokens"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user":   user,
		"tokens": tokens,
	})
}

// Register creates a new user. Requires admin authentication.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Email == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password required"})
		return
	}
	if len(req.Password) < 6 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be at least 6 characters"})
		return
	}

	claims, ok := ClaimsFromContext(r.Context())
	if !ok || claims.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only admins can create new users"})
		return
	}

	role := req.Role
	if role == "" {
		role = "editor"
	}

	user, err := h.db.CreateUser(uuid.New().String(), req.Email, req.Name, req.Password, role)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "user already exists or creation failed"})
		return
	}

	tokens, err := h.jwt.GenerateTokens(user.ID, user.Email, user.Role)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate tokens"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"user":   user,
		"tokens": tokens,
	})
}

// Refresh generates new tokens from a valid refresh token.
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	claims, err := h.jwt.ValidateToken(req.RefreshToken)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired refresh token"})
		return
	}

	user, err := h.db.GetUserByID(claims.UserID)
	if err != nil || user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "user not found"})
		return
	}

	tokens, err := h.jwt.GenerateTokens(user.ID, user.Email, user.Role)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate tokens"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user":   user,
		"tokens": tokens,
	})
}

// Me returns the current authenticated user.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := ClaimsFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}

	user, err := h.db.GetUserByID(claims.UserID)
	if err != nil || user == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}

	writeJSON(w, http.StatusOK, user)
}
