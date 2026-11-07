package httpapi

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
)

// Deps is everything the router needs. It grows as milestones add services.
// Passing dependencies explicitly (instead of using globals) keeps handlers
// testable with fakes.
type Deps struct {
	Logger       *slog.Logger
	Postgres     Pinger
	Redis        Pinger
	MaxBodyBytes int64
}

// NewRouter builds the Gin engine with the standard middleware and all routes.
// Call gin.SetMode before calling this (main does it once at startup).
func NewRouter(d Deps) *gin.Engine {
	r := gin.New()
	// We don't run behind a trusted proxy by default; ClientIP() then uses the
	// TCP peer address, which a client can't spoof with X-Forwarded-For.
	_ = r.SetTrustedProxies(nil)

	r.Use(
		requestID(),
		accessLog(d.Logger),
		recovery(d.Logger),
		bodyLimit(d.Logger, d.MaxBodyBytes),
	)

	r.NoRoute(func(c *gin.Context) {
		WriteError(c, d.Logger, apperr.New(apperr.ErrNotFound, "route not found"))
	})

	health := healthHandler{
		logger: d.Logger,
		checks: []namedPinger{{"postgres", d.Postgres}, {"redis", d.Redis}},
	}
	r.GET("/healthz", health.healthz)
	r.GET("/readyz", health.readyz)

	return r
}
