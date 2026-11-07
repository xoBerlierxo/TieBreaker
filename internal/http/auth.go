package httpapi

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
)

// Admin tokens (SPEC §14): HS256 JWTs with role=admin, short-lived, signed
// with JWT_SECRET. There is no login flow; tokens are minted with
// `tiebreaker admin-token`.
const (
	adminTokenIssuer   = "tiebreaker"
	adminTokenAudience = "tiebreaker-admin"
	// MaxAdminTokenTTL caps how long an admin token may live. Tokens claiming a
	// longer life are rejected even if correctly signed.
	MaxAdminTokenTTL = time.Hour
	clockSkew        = 30 * time.Second
)

type adminClaims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// MintAdminToken creates a signed admin token valid for ttl from now.
func MintAdminToken(secret string, ttl time.Duration, now time.Time) (string, error) {
	if ttl <= 0 || ttl > MaxAdminTokenTTL {
		return "", fmt.Errorf("admin token TTL must be between 1s and %s", MaxAdminTokenTTL)
	}
	claims := adminClaims{
		Role: "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    adminTokenIssuer,
			Audience:  jwt.ClaimStrings{adminTokenAudience},
			Subject:   "admin-cli",
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-clockSkew)),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// requireAdmin rejects requests without a valid admin token.
//
//   - 401: no token, bad signature, expired, wrong issuer/audience, wrong algorithm
//   - 403: a valid token that doesn't carry role=admin
//
// Only HS256 is accepted, which blocks the classic "alg: none" and
// algorithm-confusion attacks.
func requireAdmin(secret string, logger *slog.Logger) gin.HandlerFunc {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(adminTokenIssuer),
		jwt.WithAudience(adminTokenAudience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(clockSkew),
	)
	return func(c *gin.Context) {
		raw, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(raw) == "" {
			WriteError(c, logger, apperr.New(apperr.ErrUnauthorized, "missing bearer token"))
			return
		}

		var claims adminClaims
		_, err := parser.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) { return []byte(secret), nil })
		if err != nil {
			WriteError(c, logger, apperr.New(apperr.ErrUnauthorized, "invalid or expired token"))
			return
		}
		if claims.IssuedAt == nil || claims.ExpiresAt.Sub(claims.IssuedAt.Time) > MaxAdminTokenTTL {
			WriteError(c, logger, apperr.New(apperr.ErrUnauthorized, "token lifetime too long"))
			return
		}
		if claims.Role != "admin" {
			WriteError(c, logger, apperr.New(apperr.ErrForbidden, "admin role required"))
			return
		}
		c.Next()
	}
}
