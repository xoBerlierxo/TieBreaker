package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xoBerlierxo/TieBreaker/internal/session"
)

func TestSecurityHeaders_OnEveryResponse(t *testing.T) {
	rec := do(t, NewRouter(testDeps()), http.MethodGet, "/healthz", "")
	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	}
	for h, v := range want {
		if got := rec.Header().Get(h); got != v {
			t.Errorf("%s = %q, want %q", h, got, v)
		}
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("Content-Security-Policy missing")
	}
}

// keyRecorder is a Limiter that records the keys it is asked about.
type keyRecorder struct{ keys []string }

func (k *keyRecorder) Allow(_ context.Context, key string, _ int, _ time.Duration) (bool, time.Duration, error) {
	k.keys = append(k.keys, key)
	return true, 0, nil
}

func TestClientIP_UsesPlatformHeaderOnlyWhenConfigured(t *testing.T) {
	for _, tt := range []struct {
		header string
		want   string
	}{
		{"", "sessions-ip:192.0.2.1"},            // default: the TCP peer, header ignored (can't be spoofed)
		{"X-Real-IP", "sessions-ip:203.0.113.7"}, // behind a platform proxy that sets X-Real-IP
	} {
		rec := &keyRecorder{}
		d := testDeps()
		d.Limiter, d.ClientIPHeader = rec, tt.header
		d.Sessions = stubSessions{}
		d.APILimits = APILimits{SessionsPerIPPerHour: 10}

		req := httptest.NewRequest(http.MethodPost, "/v1/sessions", nil)
		req.RemoteAddr = "192.0.2.1:5555"
		req.Header.Set("X-Real-IP", "203.0.113.7")
		NewRouter(d).ServeHTTP(httptest.NewRecorder(), req)

		if len(rec.keys) != 1 || rec.keys[0] != tt.want {
			t.Errorf("header %q: limiter keys = %v, want [%s]", tt.header, rec.keys, tt.want)
		}
	}
}

func TestRateLimit_NextQuestionAndSessionCreation(t *testing.T) {
	d := testDeps()
	d.Limiter = &fakeLimiter{allow: 0}
	d.Sessions = stubSessions{}
	d.APILimits = APILimits{SessionsPerIPPerHour: 1, QuestionsPerIPPerMinute: 1}
	r := NewRouter(d)
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/v1/sessions"},
		{http.MethodGet, "/v1/sessions/abc/next-question"},
	} {
		if rec := do(t, r, tc.method, tc.path, ""); rec.Code != http.StatusTooManyRequests {
			t.Errorf("%s %s: status %d, want 429", tc.method, tc.path, rec.Code)
		}
	}
}

// stubSessions satisfies SessionService; the rate-limit tests never reach it.
type stubSessions struct{}

func (stubSessions) Create(context.Context, string) (session.View, error) { return session.View{}, nil }
func (stubSessions) Get(context.Context, string) (session.View, error)    { return session.View{}, nil }
func (stubSessions) AddCar(context.Context, string, int, session.AddCarRequest) (session.View, error) {
	return session.View{}, nil
}
func (stubSessions) RemoveCar(context.Context, string, int, string) (session.View, error) {
	return session.View{}, nil
}
func (stubSessions) SetConstraints(context.Context, string, int, session.Constraints) (session.View, error) {
	return session.View{}, nil
}
func (stubSessions) NextQuestion(context.Context, string) (session.NextResponse, error) {
	return session.NextResponse{}, nil
}
func (stubSessions) SubmitAnswer(context.Context, string, int, string, session.AnswerRequest) (session.AnswerOutcome, error) {
	return session.AnswerOutcome{}, nil
}
