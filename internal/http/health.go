package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
)

// Pinger is anything whose health can be checked: the Postgres pool, the Redis
// client. Depending on this tiny interface, not on GORM or go-redis, lets the
// tests use fakes.
type Pinger interface {
	Ping(ctx context.Context) error
}

const readinessTimeout = 2 * time.Second

type healthHandler struct {
	logger *slog.Logger
	checks []namedPinger // fixed order, so the response is deterministic
}

type namedPinger struct {
	name   string
	pinger Pinger
}

// healthz is the liveness check: "is the process running?". It must never touch
// a dependency, otherwise an orchestrator would restart a healthy API just
// because the database is briefly down.
func (h healthHandler) healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// readyz is the readiness check: "can this instance serve traffic?". It pings
// Postgres and Redis and returns 503 if either is down (SPEC §12).
func (h healthHandler) readyz(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), readinessTimeout)
	defer cancel()

	checks := make(map[string]string, len(h.checks))
	ready := true
	for _, check := range h.checks {
		if err := check.pinger.Ping(ctx); err != nil {
			ready = false
			checks[check.name] = "unavailable"
			h.logger.WarnContext(ctx, "readiness check failed",
				slog.String("dependency", check.name), slog.Any("error", err))
			continue
		}
		checks[check.name] = "ok"
	}

	if !ready {
		WriteError(c, h.logger, apperr.New(apperr.ErrUnavailable, "not ready").WithDetails("checks", checks))
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "checks": checks})
}
