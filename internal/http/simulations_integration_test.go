//go:build integration

package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xoBerlierxo/TieBreaker/internal/jobs"
	"github.com/xoBerlierxo/TieBreaker/internal/session"
)

// sessionReadyToSimulate returns a session in INTERVIEW (usage answered).
func sessionReadyToSimulate(t *testing.T) (string, string) {
	t.Helper()
	id, ver := sessionInUsage(t)
	for i, body := range []string{
		kmAnswer,
		`{"question_id": "usage:city_share", "answer": {"city_share_pct": [50, 70]}}`,
		`{"question_id": "usage:ownership_years", "answer": {"ownership_years": 5}}`,
		`{"question_id": "usage:financing", "answer": {"mode": "loan", "principal_inr": 400000, "annual_rate_pct": 9, "tenure_months": 36}}`,
		`{"question_id": "usage:parking", "answer": {"parking": "covered"}}`,
	} {
		r := answer(t, id, ver, "ready-"+strconv.Itoa(i), body)
		if r.code != http.StatusCreated {
			t.Fatalf("usage %d: %d %s", i, r.code, r.body)
		}
		ver = version(r)
	}
	return id, ver
}

func startSim(t *testing.T, id, ver string) resp {
	t.Helper()
	return call(t, http.MethodPost, "/v1/sessions/"+id+"/simulations", "", map[string]string{"If-Match": ver})
}

// startWorker runs a real worker until the returned stop function is called.
func startWorker(t *testing.T) func() {
	t.Helper()
	w := &jobs.Worker{
		ID: "test-worker-" + session.NewID()[:6], DB: itDB, Queue: jobs.NewQueue(itDB, 10*time.Second, 3),
		Runner: jobs.SimRunner{Workers: 2}, Finalizer: session.Finalizer{}, Publisher: jobs.RedisPublisher{Client: itRedis},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), PollInterval: 50 * time.Millisecond,
		Heartbeat: 200 * time.Millisecond, ShutdownGrace: time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = w.Run(ctx) }()
	return func() { cancel(); <-done }
}

func runStatus(t *testing.T, runID string) session.RunView {
	t.Helper()
	r := call(t, http.MethodGet, "/v1/simulations/"+runID, "", nil)
	if r.code != http.StatusOK {
		t.Fatalf("GET run: %d %s", r.code, r.body)
	}
	var v session.RunView
	r.json(t, &v)
	return v
}

func waitRun(t *testing.T, runID string, want jobs.Status) session.RunView {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if v := runStatus(t, runID); v.Status == want {
			return v
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("run %s did not reach %s", runID, want)
	return session.RunView{}
}

func runIDFrom(t *testing.T, r resp) string {
	t.Helper()
	if r.code != http.StatusAccepted {
		t.Fatalf("start simulation: %d %s, want 202", r.code, r.body)
	}
	var body struct {
		RunID string `json:"run_id"`
	}
	r.json(t, &body)
	return body.RunID
}

func sessionState(t *testing.T, id string) session.State {
	t.Helper()
	var v session.View
	call(t, http.MethodGet, "/v1/sessions/"+id, "", nil).json(t, &v)
	return v.State
}

func TestStartSimulation_Returns202AndWorkerCompletesIt(t *testing.T) {
	stop := startWorker(t)
	defer stop()
	id, ver := sessionReadyToSimulate(t)
	runID := runIDFrom(t, startSim(t, id, ver))

	v := waitRun(t, runID, jobs.StatusDone)
	if len(v.Results) != 3 || v.Progress != 100 || v.ResultsHash == nil || v.Seed == "" {
		t.Fatalf("done run = %+v", v)
	}
	sum := 0.0
	for _, r := range v.Results {
		sum += r.PBest
	}
	if sum < 0.999 || sum > 1.001 {
		t.Errorf("P(best) sums to %v, want 1", sum)
	}
	if st := sessionState(t, id); st != session.StateDone {
		t.Errorf("session state = %s, want DONE", st)
	}
}

func TestStartSimulation_DoubleSubmitOneRun409(t *testing.T) {
	id, ver := sessionReadyToSimulate(t)
	first := startSim(t, id, ver)
	runIDFrom(t, first)
	// Retry with the same (now stale) version, and with a fresh version.
	if r := startSim(t, id, ver); r.code != http.StatusConflict {
		t.Errorf("stale double submit: %d, want 409", r.code)
	}
	var v session.View
	call(t, http.MethodGet, "/v1/sessions/"+id, "", nil).json(t, &v)
	if r := startSim(t, id, strconv.Itoa(v.Version)); r.code != http.StatusConflict {
		t.Errorf("double submit with fresh version: %d, want 409", r.code)
	}
	var n int64
	itDB.Model(&jobs.Run{}).Where("session_id = ?", id).Count(&n)
	if n != 1 {
		t.Errorf("runs = %d, want 1", n)
	}
}

func TestStartSimulation_SnapshotsAssumptions(t *testing.T) {
	id, ver := sessionReadyToSimulate(t)
	runID := runIDFrom(t, startSim(t, id, ver))
	before := runPetrolPrice(t, runID)

	itDB.Exec(`UPDATE assumptions SET value_json = '{"inr_per_litre": 999}' WHERE key = 'fuel_price.petrol.TN'`)
	defer itDB.Exec(`UPDATE assumptions SET value_json = '{"inr_per_litre": 100}' WHERE key = 'fuel_price.petrol.TN'`)

	if after := runPetrolPrice(t, runID); after != before || before == 999 {
		t.Errorf("stored petrol price changed from %v to %v after editing assumptions", before, after)
	}
}

func runPetrolPrice(t *testing.T, runID string) float64 {
	t.Helper()
	var run jobs.Run
	itDB.First(&run, "id = ?", runID)
	var p jobs.Payload
	if err := json.Unmarshal(run.InputsJSON, &p); err != nil {
		t.Fatal(err)
	}
	return p.Inputs.World.PetrolPriceINR
}

func TestCancel_QueuedRunBecomesCancelledAndSessionReturnsToInterview(t *testing.T) {
	id, ver := sessionReadyToSimulate(t)
	runID := runIDFrom(t, startSim(t, id, ver)) // no worker: stays queued
	r := call(t, http.MethodPost, "/v1/simulations/"+runID+"/cancel", "", nil)
	if r.code != http.StatusOK {
		t.Fatalf("cancel: %d %s", r.code, r.body)
	}
	if v := runStatus(t, runID); v.Status != jobs.StatusCancelled {
		t.Errorf("status = %s, want cancelled", v.Status)
	}
	if st := sessionState(t, id); st != session.StateInterview {
		t.Errorf("session state = %s, want INTERVIEW", st)
	}
	if r := call(t, http.MethodPost, "/v1/simulations/"+runID+"/cancel", "", nil); r.code != http.StatusConflict {
		t.Errorf("cancel a finished run: %d, want 409", r.code)
	}
}

func TestRateLimit_SimStartsPerSession429(t *testing.T) {
	id, ver := sessionReadyToSimulate(t)
	for range 3 { // the limit in TestMain is 3 per session per hour
		runID := runIDFrom(t, startSim(t, id, ver))
		call(t, http.MethodPost, "/v1/simulations/"+runID+"/cancel", "", nil)
		var v session.View
		call(t, http.MethodGet, "/v1/sessions/"+id, "", nil).json(t, &v)
		ver = strconv.Itoa(v.Version)
	}
	r := startSim(t, id, ver)
	if r.code != http.StatusTooManyRequests || r.hdr.Get("Retry-After") == "" {
		t.Fatalf("4th start: %d (Retry-After %q), want 429 with Retry-After", r.code, r.hdr.Get("Retry-After"))
	}
}

func TestSSE_UnknownRun404(t *testing.T) {
	if r := call(t, http.MethodGet, "/v1/simulations/does-not-exist-000000/events", "", nil); r.code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", r.code)
	}
}

// TestSSE_SendsCurrentThenUpdatesThenCloses streams a real run over a real
// HTTP connection: the first event is the current (queued) state, then
// progress arrives from the worker, and the stream ends after "done".
func TestSSE_SendsCurrentThenUpdatesThenCloses(t *testing.T) {
	srv := httptest.NewServer(itRouter)
	defer srv.Close()
	id, ver := sessionReadyToSimulate(t)
	runID := runIDFrom(t, startSim(t, id, ver))

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/simulations/"+runID+"/events", nil)
	res, err := http.DefaultClient.Do(req) //nolint:bodyclose // closed by the defer below; the body is read in a goroutine
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}

	var (
		mu     sync.Mutex
		events []jobs.ProgressEvent
	)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			line := sc.Text()
			if data, ok := strings.CutPrefix(line, "data: "); ok {
				var ev jobs.ProgressEvent
				if json.Unmarshal([]byte(data), &ev) == nil {
					mu.Lock()
					events = append(events, ev)
					mu.Unlock()
				}
			}
		}
	}()

	time.Sleep(300 * time.Millisecond) // the first (queued) event arrives before any work starts
	stop := startWorker(t)
	defer stop()

	select {
	case <-readDone: // the server closed the stream after the terminal event
	case <-time.After(30 * time.Second):
		t.Fatal("stream did not end after the run finished")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) < 2 || events[0].Status != jobs.StatusQueued || events[len(events)-1].Status != jobs.StatusDone {
		t.Fatalf("events = %+v, want queued first and done last", events)
	}
	for i := 1; i < len(events); i++ {
		if events[i].Progress < events[i-1].Progress && events[i].Status == events[i-1].Status {
			t.Errorf("progress went backwards: %+v", events)
		}
	}
}

func TestSSE_ReconnectCatchesUpFromDatabase(t *testing.T) {
	stop := startWorker(t)
	defer stop()
	id, ver := sessionReadyToSimulate(t)
	runID := runIDFrom(t, startSim(t, id, ver))
	waitRun(t, runID, jobs.StatusDone)

	// A client connecting after the run finished gets the final state at once.
	srv := httptest.NewServer(itRouter)
	defer srv.Close()
	res, err := http.Get(srv.URL + "/v1/simulations/" + runID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), `"status":"done"`) || !strings.Contains(string(body), `"progress":100`) {
		t.Fatalf("stream = %s, want the final done event", body)
	}
}
