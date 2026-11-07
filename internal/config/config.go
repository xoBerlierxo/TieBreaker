// Package config loads all runtime configuration from environment variables
// exactly once, at startup, into a typed Config struct (SPEC §3, §17).
//
// Load takes a getenv function instead of calling os.Getenv directly. Production
// code passes os.Getenv; tests pass a map lookup. This keeps the package free of
// globals and makes every setting testable.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Config is the complete, validated application configuration.
type Config struct {
	AppEnv      string // development | test | production
	HTTPAddr    string
	DatabaseURL string
	RedisURL    string
	JWTSecret   string
	// MigrateOnStart makes `serve` apply pending migrations before listening
	// (for platforms without a reliable pre-deploy step; safe with several
	// instances because golang-migrate takes an advisory lock).
	MigrateOnStart bool

	HTTP      HTTPConfig
	LLM       LLMConfig
	Sim       SimConfig
	Interview InterviewConfig
	Decision  DecisionConfig
	Jobs      JobsConfig
	RateLimit RateLimitConfig

	// DiscountRate is the nominal yearly opportunity-cost rate used to discount
	// future cash flows (DECISIONS.md D-P-9).
	DiscountRate float64
}

// HTTPConfig holds server-level limits.
type HTTPConfig struct {
	MaxBodyBytes       int64
	ShutdownGrace      time.Duration
	CORSAllowedOrigins []string
}

// LLMConfig configures the optional Gemini integration (SPEC §13).
type LLMConfig struct {
	Enabled         bool
	APIKey          string // secret: never logged
	Model           string // no default in code; see .env.example
	Timeout         time.Duration
	MaxOutputTokens int
	DailyMaxCalls   int
	CacheTTL        time.Duration
}

// SimConfig configures the Monte Carlo engine (SPEC §10.1, §11).
type SimConfig struct {
	FullFutures  int
	QuickFutures int
	ChunkSize    int
	Workers      int // 0 means runtime.GOMAXPROCS(0)
	QuickTimeout time.Duration
}

// InterviewConfig configures the adaptive interview (SPEC §8).
type InterviewConfig struct {
	MaxQuestions int
	StopPBest    float64
	MinInfoGain  float64
	Ladder       []int64 // rupee rungs, strictly increasing, starting at 0
	LadderCap    int64   // upper bound used for "more than the top rung"
}

// DecisionConfig holds the winner thresholds (SPEC §9.1).
type DecisionConfig struct {
	WinPBest float64
	WinGap   float64
}

// JobsConfig configures the background job queue (SPEC §11).
type JobsConfig struct {
	Lease        time.Duration
	PollInterval time.Duration
	Heartbeat    time.Duration
	MaxAttempts  int
}

// RateLimitConfig caps expensive requests (SPEC §14), per hour.
type RateLimitConfig struct {
	SimPerSession int
	SimPerIP      int
	Window        time.Duration
}

var validAppEnvs = []string{"development", "test", "production"}

// Load reads every setting through getenv, applies the defaults from SPEC §17,
// and validates the result. It reports all problems at once (joined), so a
// misconfigured deployment can be fixed in one pass.
func Load(getenv func(string) string) (Config, error) {
	p := parser{getenv: getenv}

	cfg := Config{
		AppEnv:         p.str("APP_ENV", "development"),
		HTTPAddr:       p.str("HTTP_ADDR", defaultAddr(getenv("PORT"))),
		DatabaseURL:    p.required("DATABASE_URL"),
		RedisURL:       p.required("REDIS_URL"),
		JWTSecret:      p.required("JWT_SECRET"),
		MigrateOnStart: p.boolean("MIGRATE_ON_START", false),
		HTTP: HTTPConfig{
			MaxBodyBytes:       p.int64Min("HTTP_MAX_BODY_BYTES", 1<<20, 1),
			ShutdownGrace:      p.seconds("SHUTDOWN_GRACE_SECONDS", 20),
			CORSAllowedOrigins: p.list("CORS_ALLOWED_ORIGINS", []string{"http://localhost:3000"}),
		},
		LLM: LLMConfig{
			Enabled:         p.boolean("LLM_ENABLED", false),
			APIKey:          p.str("GEMINI_API_KEY", ""),
			Model:           p.str("GEMINI_MODEL", ""),
			Timeout:         p.seconds("LLM_TIMEOUT_SECONDS", 20),
			MaxOutputTokens: p.intMin("LLM_MAX_OUTPUT_TOKENS", 1024, 64),
			DailyMaxCalls:   p.intMin("LLM_DAILY_MAX_CALLS", 500, 0),
			CacheTTL:        time.Duration(p.intMin("LLM_CACHE_TTL_HOURS", 24, 1)) * time.Hour,
		},
		Sim: SimConfig{
			FullFutures:  p.intMin("SIM_FULL_FUTURES", 50000, 1),
			QuickFutures: p.intMin("SIM_QUICK_FUTURES", 2000, 1),
			ChunkSize:    p.intMin("SIM_CHUNK_SIZE", 1000, 1),
			Workers:      p.intMin("SIM_WORKERS", 0, 0),
			QuickTimeout: p.millis("SIM_QUICK_TIMEOUT_MS", 2000),
		},
		Interview: InterviewConfig{
			MaxQuestions: p.intMin("INTERVIEW_MAX_QUESTIONS", 10, 1),
			StopPBest:    p.probability("INTERVIEW_STOP_PBEST", 0.65),
			MinInfoGain:  p.floatRange("INTERVIEW_MIN_INFO_GAIN", 0.02, 0, 10),
			Ladder:       p.ladder("INTERVIEW_LADDER", []int64{0, 25_000, 50_000, 100_000, 200_000, 400_000}),
			LadderCap:    p.int64Min("INTERVIEW_LADDER_CAP", 800_000, 1),
		},
		Decision: DecisionConfig{
			WinPBest: p.probability("DECISION_WIN_PBEST", 0.55),
			WinGap:   p.probability("DECISION_WIN_GAP", 0.15),
		},
		Jobs: JobsConfig{
			Lease:        p.seconds("JOB_LEASE_SECONDS", 60),
			PollInterval: p.millis("JOB_POLL_INTERVAL_MS", 1000),
			Heartbeat:    p.millis("JOB_HEARTBEAT_MS", 1000),
			MaxAttempts:  p.intMin("JOB_MAX_ATTEMPTS", 3, 1),
		},
		RateLimit: RateLimitConfig{
			SimPerSession: p.intMin("RATE_LIMIT_SIM_PER_SESSION_PER_HOUR", 10, 0),
			SimPerIP:      p.intMin("RATE_LIMIT_SIM_PER_IP_PER_HOUR", 30, 0),
			Window:        time.Hour,
		},
		DiscountRate: p.floatRange("DISCOUNT_RATE", 0.07, 0, 0.99),
	}

	// Cross-field rules.
	if !slices.Contains(validAppEnvs, cfg.AppEnv) {
		p.fail("APP_ENV must be one of %v, got %q", validAppEnvs, cfg.AppEnv)
	}
	if cfg.Jobs.Heartbeat >= cfg.Jobs.Lease {
		p.fail("JOB_HEARTBEAT_MS must be shorter than JOB_LEASE_SECONDS, or leases expire between heartbeats")
	}
	if cfg.AppEnv == "production" && len(cfg.JWTSecret) < 32 {
		p.fail("JWT_SECRET must be at least 32 characters in production")
	}
	if n := len(cfg.Interview.Ladder); n > 0 && cfg.Interview.LadderCap <= cfg.Interview.Ladder[n-1] {
		p.fail("INTERVIEW_LADDER_CAP (%d) must be greater than the top ladder rung (%d)",
			cfg.Interview.LadderCap, cfg.Interview.Ladder[n-1])
	}
	if cfg.LLM.Enabled {
		if cfg.LLM.APIKey == "" {
			p.fail("GEMINI_API_KEY is required when LLM_ENABLED=true")
		}
		if cfg.LLM.Model == "" {
			p.fail("GEMINI_MODEL is required when LLM_ENABLED=true (there is no default model in code)")
		}
		// The genai SDK reads both variables and prefers GOOGLE_API_KEY, which
		// would silently override GEMINI_API_KEY (SPEC §13).
		if getenv("GOOGLE_API_KEY") != "" {
			p.fail("GOOGLE_API_KEY must not be set when LLM_ENABLED=true; use GEMINI_API_KEY only")
		}
	}

	if err := errors.Join(p.errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

// LogValue implements slog.LogValuer. Secrets are never included, and passwords
// inside connection URLs are redacted (SPEC §13: never log the API key).
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("app_env", c.AppEnv),
		slog.String("http_addr", c.HTTPAddr),
		slog.String("database_url", redactURL(c.DatabaseURL)),
		slog.String("redis_url", redactURL(c.RedisURL)),
		slog.Bool("llm_enabled", c.LLM.Enabled),
		slog.String("gemini_model", c.LLM.Model),
		slog.Bool("gemini_api_key_set", c.LLM.APIKey != ""),
		slog.Int("sim_full_futures", c.Sim.FullFutures),
		slog.Int("sim_quick_futures", c.Sim.QuickFutures),
		slog.Int("sim_chunk_size", c.Sim.ChunkSize),
		slog.Int("sim_workers", c.Sim.Workers),
		slog.Int("interview_max_questions", c.Interview.MaxQuestions),
		slog.Float64("interview_stop_pbest", c.Interview.StopPBest),
		slog.Float64("interview_min_info_gain", c.Interview.MinInfoGain),
		slog.Float64("decision_win_pbest", c.Decision.WinPBest),
		slog.Float64("decision_win_gap", c.Decision.WinGap),
		slog.Float64("discount_rate", c.DiscountRate),
		// Durations as strings ("1m0s"): slog's JSON handler would print nanoseconds.
		slog.String("job_lease", c.Jobs.Lease.String()),
		slog.String("sim_quick_timeout", c.Sim.QuickTimeout.String()),
		slog.String("llm_timeout", c.LLM.Timeout.String()),
	)
}

// defaultAddr listens on $PORT when a platform (Railway, Heroku-style) sets it.
func defaultAddr(port string) string {
	if port = strings.TrimSpace(port); port != "" {
		return ":" + port
	}
	return ":8080"
}

// redactURL replaces the password in a URL with "xxxxx". If the string isn't a
// parseable URL, it is replaced entirely, to be safe.
func redactURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "[unparseable, redacted]"
	}
	return u.Redacted()
}

// parser reads env vars, applies defaults and collects every error instead of
// stopping at the first one.
type parser struct {
	getenv func(string) string
	errs   []error
}

func (p *parser) fail(format string, args ...any) {
	p.errs = append(p.errs, fmt.Errorf(format, args...))
}

// lookup returns the trimmed value and whether it was set to something non-empty.
func (p *parser) lookup(key string) (string, bool) {
	v := strings.TrimSpace(p.getenv(key))
	return v, v != ""
}

func (p *parser) str(key, def string) string {
	if v, ok := p.lookup(key); ok {
		return v
	}
	return def
}

func (p *parser) required(key string) string {
	v, ok := p.lookup(key)
	if !ok {
		p.fail("%s is required", key)
	}
	return v
}

func (p *parser) boolean(key string, def bool) bool {
	v, ok := p.lookup(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		p.fail("%s must be true or false, got %q", key, v)
		return def
	}
	return b
}

func (p *parser) intMin(key string, def, minValue int) int {
	v, ok := p.lookup(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		p.fail("%s must be an integer, got %q", key, v)
		return def
	}
	if n < minValue {
		p.fail("%s must be >= %d, got %d", key, minValue, n)
		return def
	}
	return n
}

func (p *parser) int64Min(key string, def, minValue int64) int64 {
	v, ok := p.lookup(key)
	if !ok {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		p.fail("%s must be an integer, got %q", key, v)
		return def
	}
	if n < minValue {
		p.fail("%s must be >= %d, got %d", key, minValue, n)
		return def
	}
	return n
}

func (p *parser) floatRange(key string, def, lo, hi float64) float64 {
	v, ok := p.lookup(key)
	if !ok {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		p.fail("%s must be a number, got %q", key, v)
		return def
	}
	if f < lo || f > hi {
		p.fail("%s must be between %g and %g, got %g", key, lo, hi, f)
		return def
	}
	return f
}

func (p *parser) probability(key string, def float64) float64 {
	return p.floatRange(key, def, 0, 1)
}

func (p *parser) seconds(key string, def int) time.Duration {
	return time.Duration(p.intMin(key, def, 1)) * time.Second
}

func (p *parser) millis(key string, def int) time.Duration {
	return time.Duration(p.intMin(key, def, 1)) * time.Millisecond
}

// list parses a comma-separated list of strings (empty items dropped).
func (p *parser) list(key string, def []string) []string {
	v, ok := p.lookup(key)
	if !ok {
		return slices.Clone(def)
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ladder parses a comma-separated list of rupee rungs. The rungs must start at
// 0 and be strictly increasing (SPEC §8.3).
func (p *parser) ladder(key string, def []int64) []int64 {
	v, ok := p.lookup(key)
	if !ok {
		return slices.Clone(def)
	}
	parts := strings.Split(v, ",")
	rungs := make([]int64, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil {
			p.fail("%s must be comma-separated integers, got %q", key, v)
			return slices.Clone(def)
		}
		rungs = append(rungs, n)
	}
	if len(rungs) < 2 || rungs[0] != 0 {
		p.fail("%s must have at least 2 rungs and start at 0, got %q", key, v)
		return slices.Clone(def)
	}
	for i := 1; i < len(rungs); i++ {
		if rungs[i] <= rungs[i-1] {
			p.fail("%s must be strictly increasing, got %q", key, v)
			return slices.Clone(def)
		}
	}
	return rungs
}
