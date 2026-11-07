package httpapi

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
)

// cors lets the browser frontend (a different origin, e.g. the Vercel app)
// call the API. Only origins in CORS_ALLOWED_ORIGINS are allowed; the browser
// may read ETag (the session version) and must be allowed to send If-Match and
// Idempotency-Key. Preflight OPTIONS requests are answered here with 204.
func cors(allowed []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" || !slices.Contains(allowed, origin) {
			c.Next()
			return
		}
		h := c.Writer.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Add("Vary", "Origin")
		h.Set("Access-Control-Expose-Headers", "ETag, Idempotent-Replayed, Retry-After, Location, X-Request-ID")
		if c.Request.Method == http.MethodOptions && c.GetHeader("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type, If-Match, Idempotency-Key, Authorization")
			h.Set("Access-Control-Max-Age", "600")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
