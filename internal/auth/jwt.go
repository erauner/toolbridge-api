package auth

import (
	"context"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

type ctxKey string

const CtxUserID ctxKey = "uid"

// JWTCfg holds JWT authentication configuration
type JWTCfg struct {
	HS256Secret string // HMAC secret for HS256 tokens
	DevMode     bool   // Allow X-Debug-Sub header (DANGEROUS: only for local dev)
}

// Middleware creates HTTP middleware for JWT authentication
// Supports two modes:
// 1. Production: Bearer token with JWT validation
// 2. Development: X-Debug-Sub header (ONLY when DevMode=true)
func Middleware(db *pgxpool.Pool, cfg JWTCfg) func(http.Handler) http.Handler {
	// Log warning if dev mode is enabled
	if cfg.DevMode {
		log.Warn().Msg("SECURITY WARNING: DevMode enabled - X-Debug-Sub header will bypass JWT authentication")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract token from Authorization header
			tok := ""
			if h := r.Header.Get("Authorization"); len(h) > 7 && h[:7] == "Bearer " {
				tok = h[7:]
			}

			sub := ""

			// Development mode: accept X-Debug-Sub ONLY if DevMode is enabled and no token present
			if cfg.DevMode && tok == "" {
				sub = r.Header.Get("X-Debug-Sub")
				if sub != "" {
					log.Debug().Str("sub", sub).Msg("using X-Debug-Sub header (dev mode)")
				}
			}

			// Validate JWT token if present
			if tok != "" {
				claims := jwt.MapClaims{}
				t, err := jwt.ParseWithClaims(tok, claims, func(t *jwt.Token) (any, error) {
					// Verify signing method
					if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, jwt.ErrSignatureInvalid
					}
					return []byte(cfg.HS256Secret), nil
				})

				if err != nil || !t.Valid {
					log.Warn().Err(err).Msg("jwt validation failed")
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}

				// Extract subject from claims
				if s, ok := claims["sub"].(string); ok {
					sub = s
				}
			}

			// Require subject (either from JWT or debug header)
			if sub == "" {
				log.Warn().Msg("missing subject (no JWT sub or X-Debug-Sub header)")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			// Upsert app_user by subject (creates user on first auth)
			var userID string
			if err := db.QueryRow(r.Context(),
				`INSERT INTO app_user (sub) VALUES ($1)
				 ON CONFLICT (sub) DO UPDATE SET sub = excluded.sub
				 RETURNING id`, sub).Scan(&userID); err != nil {
				log.Error().Err(err).Str("sub", sub).Msg("failed to upsert user")
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}

			// Add user ID to request context
			ctx := context.WithValue(r.Context(), CtxUserID, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserID extracts the authenticated user ID from request context
// Returns empty string if not authenticated (should never happen after middleware)
func UserID(ctx context.Context) string {
	if v := ctx.Value(CtxUserID); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
