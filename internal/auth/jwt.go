package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims represents the JWT claims.
type Claims struct {
	UserID string `json:"uid"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// TokenPair holds access and refresh tokens.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

// JWTService generates and validates JWT tokens.
type JWTService struct {
	secret        []byte
	accessExpiry  time.Duration
	refreshExpiry time.Duration
}

// NewJWTService creates a new JWT service.
func NewJWTService(secret string) *JWTService {
	return &JWTService{
		secret:        []byte(secret),
		accessExpiry:  24 * time.Hour,
		refreshExpiry: 7 * 24 * time.Hour,
	}
}

// GenerateTokens creates an access/refresh token pair.
func (s *JWTService) GenerateTokens(userID, email, role string) (*TokenPair, error) {
	now := time.Now()

	accessClaims := Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessExpiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			Subject:   userID,
		},
	}
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(s.secret)
	if err != nil {
		return nil, err
	}

	refreshClaims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.refreshExpiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			Subject:   userID,
		},
	}
	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(s.secret)
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(s.accessExpiry.Seconds()),
	}, nil
}

// ValidateToken parses and validates a JWT token.
func (s *JWTService) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		return s.secret, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, jwt.ErrSignatureInvalid
	}
	return claims, nil
}
