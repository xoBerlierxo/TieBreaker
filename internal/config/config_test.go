package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// env builds a getenv function from a map, so tests never touch the real environment.
func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// required holds the minimum env vars needed for Load to succeed.
func required() map[string]string {
	return map[string]string{
		"DATABASE_URL": "postgres://tb:secretpw@localhost:5432/tiebreaker?sslmode=disable",
		"REDIS_URL":    "redis://:redispw@localhost:6379/0",
		"JWT_SECRET":   "jwt-secret-value-for-tests-only",
	}
}

func with(base map[string]string, kv ...string) map[string]string {
	out := make(map[string]string, len(base)+len(kv)/2)
	for k, v := range base {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i]] = kv[i+1]
	}
	return out
}

func TestLoad_AppliesSpecDefaults(t *testing.T) {
	cfg, err := Load(env(required()))
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	// Every default listed in SPEC §17.
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"AppEnv", cfg.AppEnv, "development"},
		{"HTTPAddr", cfg.HTTPAddr, ":8080"},
		{"LLM.Enabled", cfg.LLM.Enabled, false},
		{"Sim.FullFutures", cfg.Sim.FullFutures, 50000},
		{"Sim.QuickFutures", cfg.Sim.QuickFutures, 2000},
		{"Sim.ChunkSize", cfg.Sim.ChunkSize, 1000},
		{"Sim.Workers", cfg.Sim.Workers, 0},
		{"Sim.QuickTimeout", cfg.Sim.QuickTimeout, 2 * time.Second},
		{"Interview.MaxQuestions", cfg.Interview.MaxQuestions, 10},
		{"Interview.StopPBest", cfg.Interview.StopPBest, 0.65},
		{"Interview.MinInfoGain", cfg.Interview.MinInfoGain, 0.02},
		{"Decision.WinPBest", cfg.Decision.WinPBest, 0.55},
		{"Decision.WinGap", cfg.Decision.WinGap, 0.15},
		{"DiscountRate", cfg.DiscountRate, 0.07},
		{"Jobs.Lease", cfg.Jobs.Lease, 60 * time.Second},
		{"LLM.Timeout", cfg.LLM.Timeout, 20 * time.Second},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	wantLadder := []int64{0, 25_000, 50_000, 100_000, 200_000, 400_000}
	if len(cfg.Interview.Ladder) != len(wantLadder) {
		t.Fatalf("Interview.Ladder = %v, want %v", cfg.Interview.Ladder, wantLadder)
	}
	for i := range wantLadder {
		if cfg.Interview.Ladder[i] != wantLadder[i] {
			t.Fatalf("Interview.Ladder = %v, want %v", cfg.Interview.Ladder, wantLadder)
		}
	}
	if cfg.Interview.LadderCap != 800_000 {
		t.Errorf("Interview.LadderCap = %d, want 800000", cfg.Interview.LadderCap)
	}
}

func TestLoad_OverridesFromEnv(t *testing.T) {
	cfg, err := Load(env(with(required(),
		"APP_ENV", "production",
		"HTTP_ADDR", ":9090",
		"SIM_FULL_FUTURES", "20000",
		"SIM_WORKERS", "4",
		"INTERVIEW_STOP_PBEST", "0.7",
		"JOB_LEASE_SECONDS", "30",
		"INTERVIEW_LADDER", "0, 10000, 20000",
		"INTERVIEW_LADDER_CAP", "40000",
	)))
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if cfg.AppEnv != "production" || cfg.HTTPAddr != ":9090" {
		t.Errorf("AppEnv/HTTPAddr not overridden: %q %q", cfg.AppEnv, cfg.HTTPAddr)
	}
	if cfg.Sim.FullFutures != 20000 || cfg.Sim.Workers != 4 {
		t.Errorf("Sim overrides not applied: %+v", cfg.Sim)
	}
	if cfg.Interview.StopPBest != 0.7 {
		t.Errorf("StopPBest = %v, want 0.7", cfg.Interview.StopPBest)
	}
	if cfg.Jobs.Lease != 30*time.Second {
		t.Errorf("Jobs.Lease = %v, want 30s", cfg.Jobs.Lease)
	}
	if len(cfg.Interview.Ladder) != 3 || cfg.Interview.Ladder[2] != 20000 || cfg.Interview.LadderCap != 40000 {
		t.Errorf("ladder override not applied: %v cap %d", cfg.Interview.Ladder, cfg.Interview.LadderCap)
	}
}

func TestLoad_MissingRequiredListsAllMissing(t *testing.T) {
	_, err := Load(env(map[string]string{}))
	if err == nil {
		t.Fatal("Load: expected error for missing required vars, got nil")
	}
	for _, name := range []string{"DATABASE_URL", "REDIS_URL", "JWT_SECRET"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not mention %s", err.Error(), name)
		}
	}
}

func TestLoad_LLMEnabledRequiresKeyAndModel(t *testing.T) {
	_, err := Load(env(with(required(), "LLM_ENABLED", "true")))
	if err == nil {
		t.Fatal("expected error when LLM_ENABLED=true without key and model")
	}
	for _, name := range []string{"GEMINI_API_KEY", "GEMINI_MODEL"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not mention %s", err.Error(), name)
		}
	}

	cfg, err := Load(env(with(required(),
		"LLM_ENABLED", "true",
		"GEMINI_API_KEY", "test-key",
		"GEMINI_MODEL", "test-model",
	)))
	if err != nil {
		t.Fatalf("unexpected error with key and model set: %v", err)
	}
	if !cfg.LLM.Enabled || cfg.LLM.APIKey != "test-key" || cfg.LLM.Model != "test-model" {
		t.Errorf("LLM config not loaded: enabled=%v model=%q", cfg.LLM.Enabled, cfg.LLM.Model)
	}
}

func TestLoad_LLMEnabledRejectsGoogleAPIKey(t *testing.T) {
	// The genai SDK prefers GOOGLE_API_KEY over GEMINI_API_KEY when both are set (SPEC §13).
	_, err := Load(env(with(required(),
		"LLM_ENABLED", "true",
		"GEMINI_API_KEY", "test-key",
		"GEMINI_MODEL", "test-model",
		"GOOGLE_API_KEY", "other-key",
	)))
	if err == nil || !strings.Contains(err.Error(), "GOOGLE_API_KEY") {
		t.Fatalf("expected error mentioning GOOGLE_API_KEY, got %v", err)
	}
}

func TestLoad_LLMDisabledIgnoresMissingKey(t *testing.T) {
	cfg, err := Load(env(with(required(), "LLM_ENABLED", "false")))
	if err != nil {
		t.Fatalf("unexpected error with LLM disabled: %v", err)
	}
	if cfg.LLM.Enabled {
		t.Error("LLM.Enabled = true, want false")
	}
}

func TestLoad_InvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantMsg string
	}{
		{"non-numeric futures", "SIM_FULL_FUTURES", "lots", "SIM_FULL_FUTURES"},
		{"zero futures", "SIM_FULL_FUTURES", "0", "SIM_FULL_FUTURES"},
		{"negative quick futures", "SIM_QUICK_FUTURES", "-5", "SIM_QUICK_FUTURES"},
		{"zero chunk size", "SIM_CHUNK_SIZE", "0", "SIM_CHUNK_SIZE"},
		{"negative workers", "SIM_WORKERS", "-1", "SIM_WORKERS"},
		{"probability above one", "INTERVIEW_STOP_PBEST", "1.5", "INTERVIEW_STOP_PBEST"},
		{"probability not a number", "DECISION_WIN_PBEST", "high", "DECISION_WIN_PBEST"},
		{"negative info gain", "INTERVIEW_MIN_INFO_GAIN", "-0.1", "INTERVIEW_MIN_INFO_GAIN"},
		{"discount rate too high", "DISCOUNT_RATE", "1.2", "DISCOUNT_RATE"},
		{"zero lease", "JOB_LEASE_SECONDS", "0", "JOB_LEASE_SECONDS"},
		{"bad bool", "LLM_ENABLED", "maybe", "LLM_ENABLED"},
		{"unsorted ladder", "INTERVIEW_LADDER", "0,50000,25000", "INTERVIEW_LADDER"},
		{"ladder not starting at zero", "INTERVIEW_LADDER", "10000,20000", "INTERVIEW_LADDER"},
		{"ladder cap below top rung", "INTERVIEW_LADDER_CAP", "300000", "INTERVIEW_LADDER_CAP"},
		{"unknown app env", "APP_ENV", "staging-ish", "APP_ENV"},
		{"zero max questions", "INTERVIEW_MAX_QUESTIONS", "0", "INTERVIEW_MAX_QUESTIONS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(env(with(required(), tt.key, tt.value)))
			if err == nil {
				t.Fatalf("expected error for %s=%q, got nil", tt.key, tt.value)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error %q does not mention %s", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestConfig_LogValueRedactsSecrets(t *testing.T) {
	cfg, err := Load(env(with(required(),
		"LLM_ENABLED", "true",
		"GEMINI_API_KEY", "super-secret-gemini-key",
		"GEMINI_MODEL", "test-model",
	)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Info("config loaded", "config", cfg)
	out := buf.String()

	for _, secret := range []string{"super-secret-gemini-key", "jwt-secret-value-for-tests-only", "secretpw", "redispw"} {
		if strings.Contains(out, secret) {
			t.Errorf("log output leaks secret %q: %s", secret, out)
		}
	}
	// Non-secret values should still be visible, so the log is useful.
	for _, visible := range []string{"test-model", "localhost:5432", "50000"} {
		if !strings.Contains(out, visible) {
			t.Errorf("log output is missing non-secret value %q: %s", visible, out)
		}
	}
}
