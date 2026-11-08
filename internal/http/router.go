package httpapi

import (
	"log/slog"
	"time"

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
	JWTSecret    string

	CORSAllowedOrigins []string // browser origins allowed to call the API
	ClientIPHeader     string   // header with the real client IP set by the platform proxy; "" = TCP peer

	Catalog     CatalogService    // nil: catalogue routes are not mounted
	Sessions    SessionService    // nil: session routes are not mounted
	Simulations SimulationService // nil: simulation routes are not mounted
	Reports     ReportService     // nil: report routes are not mounted
	Progress    ProgressSource
	Limiter     Limiter // nil: no rate limiting
	SimLimits   SimLimits
	APILimits   APILimits
}

// APILimits caps cheap-to-call but costly-to-serve public endpoints per client IP.
type APILimits struct {
	SessionsPerIPPerHour    int // POST /v1/sessions writes a row
	QuestionsPerIPPerMinute int // GET next-question runs up to ~64 quick simulations
}

// SimLimits caps simulation starts (SPEC §14), per session and per client IP.
type SimLimits struct {
	PerSession int
	PerIP      int
	Window     time.Duration
}

// NewRouter builds the Gin engine with the standard middleware and all routes.
// Call gin.SetMode before calling this (main does it once at startup).
func NewRouter(d Deps) *gin.Engine {
	r := gin.New()
	// By default ClientIP() is the TCP peer address, which a client can't
	// spoof with X-Forwarded-For. Behind a hosting proxy (Railway) every
	// request would then share the proxy's IP, so the platform's client-IP
	// header can be trusted instead (CLIENT_IP_HEADER).
	_ = r.SetTrustedProxies(nil)
	if d.ClientIPHeader != "" {
		r.TrustedPlatform = d.ClientIPHeader
	}

	r.Use(
		securityHeaders(),
		cors(d.CORSAllowedOrigins),
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

	v1 := r.Group("/v1")
	admin := r.Group("/admin/v1", requireAdmin(d.JWTSecret, d.Logger))

	if d.Catalog != nil {
		vh := variantHandler{logger: d.Logger, catalog: d.Catalog}
		v1.GET("/variants", vh.search)
		v1.GET("/variants/:id", vh.get)
		admin.POST("/variants", vh.create)
		admin.PUT("/variants/:id", vh.update)
		admin.DELETE("/variants/:id", vh.remove)
	}

	if d.Sessions != nil {
		sh := sessionHandler{logger: d.Logger, sessions: d.Sessions}
		createLimit := limited(d, Limit{Name: "sessions-ip", Max: d.APILimits.SessionsPerIPPerHour, Window: time.Hour, Key: byClientIP})
		questionLimit := limited(d, Limit{Name: "questions-ip", Max: d.APILimits.QuestionsPerIPPerMinute, Window: time.Minute, Key: byClientIP})
		v1.POST("/sessions", append(createLimit, sh.create)...)
		v1.GET("/sessions/:id", sh.get)
		v1.POST("/sessions/:id/cars", sh.addCar)
		v1.DELETE("/sessions/:id/cars/:itemId", sh.removeCar)
		v1.PUT("/sessions/:id/constraints", sh.setConstraints)
		v1.GET("/sessions/:id/next-question", append(questionLimit, sh.nextQuestion)...)
		v1.POST("/sessions/:id/answers", sh.submitAnswer)
	}

	if d.Simulations != nil {
		simh := simulationHandler{logger: d.Logger, sims: d.Simulations, progress: d.Progress}
		start := []gin.HandlerFunc{}
		if d.Limiter != nil {
			start = append(start, rateLimit(d.Limiter, d.Logger,
				Limit{Name: "sim-session", Max: d.SimLimits.PerSession, Window: d.SimLimits.Window, Key: bySessionParam},
				Limit{Name: "sim-ip", Max: d.SimLimits.PerIP, Window: d.SimLimits.Window, Key: byClientIP},
			))
		}
		v1.POST("/sessions/:id/simulations", append(start, simh.start)...)
		v1.GET("/simulations/:id", simh.get)
		v1.POST("/simulations/:id/cancel", simh.cancel)
		if d.Progress != nil {
			v1.GET("/simulations/:id/events", simh.events)
		}
	}

	if d.Reports != nil {
		rh := reportHandler{logger: d.Logger, reports: d.Reports}
		v1.GET("/sessions/:id/report", rh.forSession)
		r.GET("/r/:shareId", rh.shared)
	}

	return r
}

// limited returns the rate-limit middleware for one rule, or nothing when
// rate limiting isn't configured.
func limited(d Deps, l Limit) []gin.HandlerFunc {
	if d.Limiter == nil {
		return nil
	}
	return []gin.HandlerFunc{rateLimit(d.Limiter, d.Logger, l)}
}
