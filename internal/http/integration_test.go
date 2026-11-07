//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/xoBerlierxo/TieBreaker/internal/catalog"
	"github.com/xoBerlierxo/TieBreaker/internal/interview"
	"github.com/xoBerlierxo/TieBreaker/internal/session"
	"github.com/xoBerlierxo/TieBreaker/internal/store"
	"github.com/xoBerlierxo/TieBreaker/internal/testutil"
)

// Full-stack integration tests: real router, real services, real Postgres and
// Redis (SPEC §15 "Integration").

var (
	itRouter http.Handler
	itDB     *gorm.DB
	itDeps   Deps
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(runIntegration(m))
}

func runIntegration(m *testing.M) int {
	ctx := context.Background()
	dsn, stopPG, err := testutil.Postgres(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer stopPG()
	redisURL, stopRedis, err := testutil.Redis(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer stopRedis()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := store.MigrateUp(ctx, dsn); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	db, err := store.OpenPostgres(ctx, dsn, logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rdb, err := store.OpenRedis(ctx, redisURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if _, err := catalog.LoadDir(ctx, db, "../../data/seed"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	itDB = db
	cat := catalog.NewService(db)
	sessions := session.NewService(db, cat, session.Config{
		Interview: interview.Config{
			MaxQuestions: 10, StopPBest: 0.65, MinInfoGain: 0.02,
			Ladder: interview.Ladder{Rungs: []int64{0, 25_000, 50_000, 100_000, 200_000, 400_000}, Cap: 800_000},
		},
		QuickFutures: 2000, ChunkSize: 1000, QuickTimeout: 10 * time.Second, DiscountRate: 0.07,
	}, store.QuickSimCache{Client: rdb, TTL: time.Hour, Logger: logger}, logger)

	itDeps = Deps{
		Logger: logger, Postgres: store.PostgresPinger{DB: db}, Redis: store.RedisPinger{Client: rdb},
		MaxBodyBytes: 1 << 20, JWTSecret: testJWTSecret, Catalog: cat, Sessions: sessions,
	}
	itRouter = NewRouter(itDeps)
	return m.Run()
}

// --- tiny HTTP client helpers ----------------------------------------------

type resp struct {
	code int
	body []byte
	hdr  http.Header
}

func (r resp) json(t *testing.T, dst any) {
	t.Helper()
	if err := json.Unmarshal(r.body, dst); err != nil {
		t.Fatalf("decoding %s: %v", r.body, err)
	}
}

func (r resp) errCode(t *testing.T) string {
	var e ErrorBody
	r.json(t, &e)
	return e.Error.Code
}

func call(t *testing.T, method, path, body string, headers map[string]string) resp {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	itRouter.ServeHTTP(rec, req)
	return resp{code: rec.Code, body: rec.Body.Bytes(), hdr: rec.Header()}
}

func version(r resp) string { return strings.Trim(r.hdr.Get("ETag"), `"`) }

func variantID(t *testing.T, q string) string {
	t.Helper()
	r := call(t, http.MethodGet, "/v1/variants?q="+q, "", nil)
	var body struct {
		Variants []VariantDTO `json:"variants"`
	}
	r.json(t, &body)
	if len(body.Variants) != 1 {
		t.Fatalf("search %q returned %d variants", q, len(body.Variants))
	}
	return body.Variants[0].ID
}

func newSession(t *testing.T) (string, string) {
	t.Helper()
	r := call(t, http.MethodPost, "/v1/sessions", `{"user_state": "TN"}`, nil)
	if r.code != http.StatusCreated {
		t.Fatalf("create session: %d %s", r.code, r.body)
	}
	var v session.View
	r.json(t, &v)
	return v.ID, version(r)
}

func addCar(t *testing.T, id, ver, body string) resp {
	t.Helper()
	return call(t, http.MethodPost, "/v1/sessions/"+id+"/cars", body, map[string]string{"If-Match": `"` + ver + `"`})
}

// sessionInUsage creates a session with the three sample cars, sets the
// constraints, and returns it in state USAGE with its current version.
func sessionInUsage(t *testing.T) (string, string) {
	t.Helper()
	id, ver := newSession(t)
	for _, q := range []string{"alpha", "beta", "gamma"} {
		r := addCar(t, id, ver, fmt.Sprintf(`{"variant_id": %q, "condition": "new"}`, variantID(t, q)))
		if r.code != http.StatusCreated {
			t.Fatalf("add %s: %d %s", q, r.code, r.body)
		}
		ver = version(r)
	}
	r := call(t, http.MethodPut, "/v1/sessions/"+id+"/constraints", `{"home_charging": true}`, map[string]string{"If-Match": ver})
	if r.code != http.StatusOK {
		t.Fatalf("constraints: %d %s", r.code, r.body)
	}
	var v session.View
	r.json(t, &v)
	if v.State != session.StateUsage {
		t.Fatalf("state = %s, want USAGE", v.State)
	}
	return id, version(r)
}

func answer(t *testing.T, id, ver, key, body string) resp {
	t.Helper()
	return call(t, http.MethodPost, "/v1/sessions/"+id+"/answers", body, map[string]string{"If-Match": ver, "Idempotency-Key": key})
}

func countRows(t *testing.T, table, sessionID string) int64 {
	t.Helper()
	var n int64
	if err := itDB.Table(table).Where("session_id = ?", sessionID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

const kmAnswer = `{"question_id": "usage:km_per_month", "answer": {"km_per_month": [800, 1200]}}`

// --- SPEC §15 integration scenarios ------------------------------------------

func TestStaleIfMatch_Returns409(t *testing.T) {
	id, ver := newSession(t)
	if r := addCar(t, id, ver, fmt.Sprintf(`{"variant_id": %q, "condition": "new"}`, variantID(t, "alpha"))); r.code != http.StatusCreated {
		t.Fatalf("first add: %d %s", r.code, r.body)
	}
	// Same (now stale) version again.
	r := addCar(t, id, ver, fmt.Sprintf(`{"variant_id": %q, "condition": "new"}`, variantID(t, "beta")))
	if r.code != http.StatusConflict || r.errCode(t) != "SESSION_VERSION_CONFLICT" {
		t.Fatalf("stale If-Match: %d %s, want 409 SESSION_VERSION_CONFLICT", r.code, r.body)
	}
}

func TestMissingIfMatch_Returns412(t *testing.T) {
	id, _ := newSession(t)
	r := call(t, http.MethodPost, "/v1/sessions/"+id+"/cars", `{"variant_id": "x", "condition": "new"}`, nil)
	if r.code != http.StatusPreconditionRequired {
		t.Fatalf("missing If-Match: %d, want 412", r.code)
	}
}

func TestIdempotency_SameKeySameBodyReturnsSameResponseOneRow(t *testing.T) {
	id, ver := sessionInUsage(t)
	first := answer(t, id, ver, "key-1", kmAnswer)
	if first.code != http.StatusCreated {
		t.Fatalf("first answer: %d %s", first.code, first.body)
	}
	second := answer(t, id, ver, "key-1", kmAnswer)
	if second.code != http.StatusCreated || string(second.body) != string(first.body) || second.hdr.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: %d %s (replayed=%q), want identical 201", second.code, second.body, second.hdr.Get("Idempotent-Replayed"))
	}
	if n := countRows(t, "answers", id); n != 1 {
		t.Errorf("answers rows = %d, want 1", n)
	}
	if n := countRows(t, "idempotency_keys", id); n != 1 {
		t.Errorf("idempotency rows = %d, want 1", n)
	}
}

func TestIdempotency_SameKeyDifferentBody422(t *testing.T) {
	id, ver := sessionInUsage(t)
	if r := answer(t, id, ver, "key-1", kmAnswer); r.code != http.StatusCreated {
		t.Fatalf("first: %d %s", r.code, r.body)
	}
	other := `{"question_id": "usage:km_per_month", "answer": {"km_per_month": [100, 200]}}`
	r := answer(t, id, ver, "key-1", other)
	if r.code != http.StatusUnprocessableEntity || r.errCode(t) != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("different body: %d %s, want 422", r.code, r.body)
	}
}

func TestIdempotency_ReplayAfterVersionMovedReturnsStored(t *testing.T) {
	id, ver := sessionInUsage(t)
	first := answer(t, id, ver, "km-key", kmAnswer)
	second := answer(t, id, version(first), "city-key", `{"question_id": "usage:city_share", "answer": {"city_share_pct": [50, 70]}}`)
	if second.code != http.StatusCreated {
		t.Fatalf("second answer: %d %s", second.code, second.body)
	}
	// Retry the first request with its original (now stale) If-Match.
	replay := answer(t, id, ver, "km-key", kmAnswer)
	if replay.code != http.StatusCreated || string(replay.body) != string(first.body) {
		t.Fatalf("replay after version moved: %d %s, want the stored 201", replay.code, replay.body)
	}
}

func TestConcurrentAnswers_ExactlyOneSucceeds(t *testing.T) {
	id, ver := sessionInUsage(t)
	var wg sync.WaitGroup
	start := make(chan struct{})
	codes := make([]int, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release both requests at the same moment
			codes[i] = answer(t, id, ver, "concurrent-"+strconv.Itoa(i), kmAnswer).code
		}()
	}
	close(start)
	wg.Wait()

	created, conflicts := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicts++
		}
	}
	if created != 1 || conflicts != 1 {
		t.Fatalf("status codes = %v, want exactly one 201 and one 409", codes)
	}
	if n := countRows(t, "answers", id); n != 1 {
		t.Errorf("answers rows = %d, want 1", n)
	}
}

func TestConcurrentSameKey_OneRowBothSameResponse(t *testing.T) {
	id, ver := sessionInUsage(t)
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]resp, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i] = answer(t, id, ver, "same-key", kmAnswer)
		}()
	}
	close(start)
	wg.Wait()
	for _, r := range results {
		if r.code != http.StatusCreated {
			t.Fatalf("status = %d %s, want 201 for both", r.code, r.body)
		}
	}
	if string(results[0].body) != string(results[1].body) {
		t.Errorf("bodies differ: %s vs %s", results[0].body, results[1].body)
	}
	if n := countRows(t, "answers", id); n != 1 {
		t.Errorf("answers rows = %d, want 1", n)
	}
}

func TestAnswers_MissingIdempotencyKey400(t *testing.T) {
	id, ver := sessionInUsage(t)
	r := call(t, http.MethodPost, "/v1/sessions/"+id+"/answers", kmAnswer, map[string]string{"If-Match": ver})
	if r.code != http.StatusBadRequest {
		t.Fatalf("missing Idempotency-Key: %d, want 400", r.code)
	}
}

func TestShortlist_RulesOverHTTP(t *testing.T) {
	id, ver := newSession(t)
	alpha := fmt.Sprintf(`{"variant_id": %q, "condition": "new"}`, variantID(t, "alpha"))
	r := addCar(t, id, ver, alpha)
	ver = version(r)

	if r := addCar(t, id, ver, alpha); r.code != http.StatusConflict {
		t.Errorf("duplicate car: %d %s, want 409", r.code, r.body)
	}
	if r := call(t, http.MethodPut, "/v1/sessions/"+id+"/constraints", `{}`, map[string]string{"If-Match": ver}); r.code != http.StatusConflict {
		t.Errorf("constraints with one car: %d %s, want 409", r.code, r.body)
	}
	used := fmt.Sprintf(`{"variant_id": %q, "condition": "used"}`, variantID(t, "gamma"))
	if r := addCar(t, id, ver, used); r.code != http.StatusBadRequest {
		t.Errorf("used car without details: %d %s, want 400", r.code, r.body)
	}
	usedOK := fmt.Sprintf(`{"variant_id": %q, "condition": "used", "asking_price_inr": 500000, "year": 2022, "odometer_km": 30000}`, variantID(t, "gamma"))
	if r := addCar(t, id, ver, usedOK); r.code != http.StatusCreated {
		t.Errorf("used car: %d %s, want 201", r.code, r.body)
	}
}

func TestConstraints_AllEliminatedMovesToDone(t *testing.T) {
	id, ver := newSession(t)
	for _, q := range []string{"alpha", "gamma"} {
		ver = version(addCar(t, id, ver, fmt.Sprintf(`{"variant_id": %q, "condition": "new"}`, variantID(t, q))))
	}
	r := call(t, http.MethodPut, "/v1/sessions/"+id+"/constraints", `{"max_budget_inr": 100000, "min_seats": 7}`, map[string]string{"If-Match": ver})
	var v session.View
	r.json(t, &v)
	if v.State != session.StateDone {
		t.Fatalf("state = %s, want DONE", v.State)
	}
	for _, c := range v.Cars {
		if !c.Eliminated || len(c.Eliminations) != 2 || c.Eliminations[0].Shortfall <= 0 {
			t.Errorf("car %s eliminations = %+v, want budget + seats with shortfalls", c.Label, c.Eliminations)
		}
	}
}

// TestInterview_FullFlowReachesDone walks the whole interview over HTTP:
// usage questions in order, then trade-off questions until the engine says done.
func TestInterview_FullFlowReachesDone(t *testing.T) {
	id, ver := sessionInUsage(t)
	usage := []string{
		kmAnswer,
		`{"question_id": "usage:city_share", "answer": {"city_share_pct": [50, 70]}}`,
		`{"question_id": "usage:ownership_years", "answer": {"ownership_years": 5}}`,
		`{"question_id": "usage:financing", "answer": {"mode": "cash"}}`,
		`{"question_id": "usage:parking", "answer": {"skipped": true}}`,
	}
	for i, body := range usage {
		r := answer(t, id, ver, "usage-"+strconv.Itoa(i), body)
		if r.code != http.StatusCreated {
			t.Fatalf("usage answer %d: %d %s", i, r.code, r.body)
		}
		ver = version(r)
	}

	// Which attributes may legitimately be asked about: real differences only.
	r := call(t, http.MethodGet, "/v1/sessions/"+id, "", nil)
	var view session.View
	r.json(t, &view)
	if view.State != session.StateInterview {
		t.Fatalf("state after usage = %s, want INTERVIEW", view.State)
	}

	asked := 0
	for {
		nr := call(t, http.MethodGet, "/v1/sessions/"+id+"/next-question", "", nil)
		if nr.code != http.StatusOK {
			t.Fatalf("next-question: %d %s", nr.code, nr.body)
		}
		var next session.NextResponse
		nr.json(t, &next)
		if next.Done {
			if next.Reason == "" {
				t.Error("done without a reason")
			}
			break
		}
		q := next.Question
		if q.Phase != "tradeoff" || q.Attribute == interview.KeySafety {
			t.Fatalf("unexpected question %+v (safety must not be asked: one sample car is unrated)", q)
		}
		yes := asked%2 == 0
		ar := answer(t, id, ver, "tradeoff-"+strconv.Itoa(asked), fmt.Sprintf(`{"question_id": %q, "answer": {"yes": %v}}`, q.ID, yes))
		if ar.code != http.StatusCreated {
			t.Fatalf("trade-off answer: %d %s", ar.code, ar.body)
		}
		ver = version(ar)
		asked++
		if asked > 10 {
			t.Fatal("more than 10 trade-off questions were asked")
		}
	}

	r = call(t, http.MethodGet, "/v1/sessions/"+id, "", nil)
	r.json(t, &view)
	if view.QuestionsAsked != asked {
		t.Errorf("questions_asked = %d, want %d", view.QuestionsAsked, asked)
	}
	for _, tr := range view.Tradeoffs {
		if tr.Bracket.Max <= tr.Bracket.Min {
			t.Errorf("trade-off %s has an empty bracket %+v", tr.Attribute, tr.Bracket)
		}
	}
}
