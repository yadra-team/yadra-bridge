package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/yarda-team/yadra-bridge/internal/apierr"
)

type ctxKey int

const claimsKey ctxKey = 1

func JWTMiddleware(v *Validator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authz := r.Header.Get("Authorization")
			if !strings.HasPrefix(authz, "Bearer ") {
				apierr.WriteUnauthorized(w)
				return
			}
			token := strings.TrimPrefix(authz, "Bearer ")
			claims, err := v.Validate(token)
			if err != nil {
				apierr.WriteUnauthorized(w)
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func ClaimsFromContext(ctx context.Context) *Claims {
	c, _ := ctx.Value(claimsKey).(*Claims)
	return c
}
