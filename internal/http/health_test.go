package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestHealthz_Returns200(t *testing.T) {
	// Liveness must not depend on the database: even with both dependencies
	// down, the process itself is alive.
	deps := testDeps()
	deps.Postgres = fakePinger{err: errors.New("db down")}
	deps.Redis = fakePinger{err: errors.New("redis down")}

	rec := do(t, NewRouter(deps), http.MethodGet, "/healthz", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestReadyz_Returns200WhenAllUp(t *testing.T) {
	rec := do(t, NewRouter(testDeps()), http.MethodGet, "/readyz", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "ok" || body.Checks["postgres"] != "ok" || body.Checks["redis"] != "ok" {
		t.Errorf("body = %+v", body)
	}
}

func TestReadyz_Returns503WhenDependencyDown(t *testing.T) {
	tests := []struct {
		name        string
		postgresErr error
		redisErr    error
		wantFailed  string
	}{
		{"database down", errors.New("connection refused"), nil, "postgres"},
		{"redis down", nil, errors.New("connection refused"), "redis"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := testDeps()
			deps.Postgres = fakePinger{err: tt.postgresErr}
			deps.Redis = fakePinger{err: tt.redisErr}

			rec := do(t, NewRouter(deps), http.MethodGet, "/readyz", "")

			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", rec.Code)
			}
			payload := decodeError(t, rec)
			if payload.Code != "DEPENDENCY_UNAVAILABLE" {
				t.Errorf("code = %q, want DEPENDENCY_UNAVAILABLE", payload.Code)
			}
			checks, _ := payload.Details["checks"].(map[string]any)
			if checks[tt.wantFailed] != "unavailable" {
				t.Errorf("details.checks = %v, want %s=unavailable", payload.Details["checks"], tt.wantFailed)
			}
		})
	}
}
