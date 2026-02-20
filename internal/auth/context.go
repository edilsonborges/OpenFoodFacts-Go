package auth

import "context"

type contextKey string

const claimsKey contextKey = "claims"

// ContextWithClaims stores JWT claims in context.
func ContextWithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

// ClaimsFromContext retrieves JWT claims from context.
func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(claimsKey).(*Claims)
	return claims, ok
}
