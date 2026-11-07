//go:build integration

package jobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
	"gorm.io/gorm"

	"github.com/xoBerlierxo/TieBreaker/internal/store"
	"github.com/xoBerlierxo/TieBreaker/internal/testutil"
)

var testDB *gorm.DB

func TestMain(m *testing.M) { os.Exit(runWithDB(m)) }

func runWithDB(m *testing.M) int {
	ctx := context.Background()
	dsn, stop, err := testutil.Postgres(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer stop()
	if err := store.MigrateUp(ctx, dsn); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	testDB, err = store.OpenPostgres(ctx, dsn, quiet())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

var seq atomic.Int64

// queueRun inserts a session in SIMULATING and a queued run for it.
func queueRun(t *testing.T) Run {
	t.Helper()
	n := seq.Add(1)
	sessionID := fmt.Sprintf("ts-%019d", n) // exactly 22 characters
	if err := testDB.Exec(`INSERT INTO sessions (id, state, user_state) VALUES (?, 'SIMULATING', 'TN')`, sessionID).Error; err != nil {
		t.Fatalf("insert session: %v", err)
	}
	run := Run{ID: fmt.Sprintf("run-%018d", n)[:22], SessionID: sessionID, Kind: KindFull, MasterSeed: n,
		Futures: 1000, ChunkSize: 1000, InputsHash: "h", InputsJSON: []byte(`{}`), ModelVersion: "test"}
	if err := Insert(testDB, &run); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	return run
}

func clearRuns(t *testing.T) {
	t.Helper()
	testDB.Exec("TRUNCATE sim_runs, sessions CASCADE")
}

func reload(t *testing.T, id string) Run {
	t.Helper()
	r, err := GetRun(testDB, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestInsert_OneActiveRunPerSession(t *testing.T) {
	clearRuns(t)
	r := queueRun(t)
	dup := r
	dup.ID = "duplicate-run-00000001"
	if err := Insert(testDB, &dup); err == nil {
		t.Fatal("a second active run for the same session was accepted")
	}
}

func TestClaim_ConcurrentClaimersNeverGetSameJob(t *testing.T) {
	clearRuns(t)
	const jobs = 20
	for range jobs {
		queueRun(t)
	}
	q := NewQueue(testDB, time.Minute, 3)
	var mu sync.Mutex
	claimed := map[string]int{}
	var wg sync.WaitGroup
	for w := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				run, err := q.Claim(context.Background(), fmt.Sprintf("w%d", w))
				if err != nil {
					t.Error(err)
					return
				}
				if run == nil {
					return
				}
				mu.Lock()
				claimed[run.ID]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(claimed) != jobs {
		t.Fatalf("claimed %d distinct jobs, want %d", len(claimed), jobs)
	}
	for id, n := range claimed {
		if n != 1 {
			t.Errorf("job %s claimed %d times", id, n)
		}
	}
}

func TestClaim_ReclaimsExpiredLease_AndFencesTheOldOwner(t *testing.T) {
	clearRuns(t)
	run := queueRun(t)
	q := NewQueue(testDB, time.Second, 3)

	first, _ := q.Claim(context.Background(), "worker-a")
	if first == nil || first.ID != run.ID {
		t.Fatalf("worker-a claimed %+v", first)
	}
	if again, _ := q.Claim(context.Background(), "worker-b"); again != nil {
		t.Fatal("a run with a live lease was claimed twice")
	}
	time.Sleep(1200 * time.Millisecond) // let the lease expire

	second, _ := q.Claim(context.Background(), "worker-b")
	if second == nil || second.ID != run.ID || second.Attempts != 2 {
		t.Fatalf("worker-b reclaim = %+v, want the same run on attempt 2", second)
	}
	// worker-a wakes up and tries to finish: every write is fenced.
	if _, err := q.ExtendLease(context.Background(), run.ID, "worker-a"); !errors.Is(err, ErrLeaseLost) {
		t.Errorf("old owner heartbeat err = %v, want ErrLeaseLost", err)
	}
	err := store.WithTx(context.Background(), testDB, func(tx *gorm.DB) error { return Complete(tx, run.ID, "worker-a", "x") })
	if !errors.Is(err, ErrLeaseLost) {
		t.Errorf("old owner Complete err = %v, want ErrLeaseLost", err)
	}
	if r := reload(t, run.ID); r.Status != StatusRunning || r.WorkerID == nil || *r.WorkerID != "worker-b" {
		t.Errorf("run after fenced write = %+v, want still running for worker-b", r)
	}
}

func TestClaim_MarksFailedAfterMaxAttempts(t *testing.T) {
	clearRuns(t)
	run := queueRun(t)
	q := NewQueue(testDB, time.Second, 1)
	if r, _ := q.Claim(context.Background(), "a"); r == nil {
		t.Fatal("first claim failed")
	}
	time.Sleep(1200 * time.Millisecond)
	if r, _ := q.Claim(context.Background(), "b"); r != nil {
		t.Fatalf("a run past max attempts was claimed again: %+v", r)
	}
	if r := reload(t, run.ID); r.Status != StatusFailed || r.Error == nil {
		t.Errorf("run = %+v, want failed with an error message", r)
	}
}

// fakeRunner counts how often each run is processed.
type fakeRunner struct {
	mu     sync.Mutex
	counts map[string]int
	delay  time.Duration
	block  bool // block until the job context is cancelled
}

func (f *fakeRunner) Run(ctx context.Context, run Run, progress func(int)) (Outcome, error) {
	f.mu.Lock()
	f.counts[run.ID]++
	f.mu.Unlock()
	progress(50)
	if f.block {
		<-ctx.Done()
		return Outcome{}, ctx.Err()
	}
	select {
	case <-time.After(f.delay):
	case <-ctx.Done():
		return Outcome{}, ctx.Err()
	}
	return Outcome{ResultsHash: "hash-" + run.ID}, nil
}

type recordingFinalizer struct {
	mu      sync.Mutex
	done    []string
	stopped map[string]Status
}

func (f *recordingFinalizer) Done(_ context.Context, _ *gorm.DB, run Run, _ Outcome) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.done = append(f.done, run.ID)
	return nil
}

func (f *recordingFinalizer) Stopped(_ context.Context, _ *gorm.DB, run Run, s Status) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped[run.ID] = s
	return nil
}

func newWorker(id string, q *Queue, r Runner, f Finalizer) *Worker {
	return &Worker{ID: id, DB: testDB, Queue: q, Runner: r, Finalizer: f, Publisher: NopPublisher{}, Logger: quiet(),
		PollInterval: 50 * time.Millisecond, Heartbeat: 200 * time.Millisecond, ShutdownGrace: 200 * time.Millisecond}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func countStatus(t *testing.T, s Status) int64 {
	t.Helper()
	var n int64
	testDB.Model(&Run{}).Where("status = ?", s).Count(&n)
	return n
}

func TestTwoWorkersTenJobs_EachProcessedExactlyOnce(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	clearRuns(t)
	for range 10 {
		queueRun(t)
	}
	q := NewQueue(testDB, 5*time.Second, 3)
	runner := &fakeRunner{counts: map[string]int{}, delay: 30 * time.Millisecond}
	fin := &recordingFinalizer{stopped: map[string]Status{}}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for _, id := range []string{"w1", "w2"} {
		wg.Add(1)
		go func() { defer wg.Done(); _ = newWorker(id, q, runner, fin).Run(ctx) }()
	}
	waitFor(t, 15*time.Second, func() bool { return countStatus(t, StatusDone) == 10 })
	cancel()
	wg.Wait()

	if len(runner.counts) != 10 {
		t.Fatalf("processed %d distinct jobs, want 10", len(runner.counts))
	}
	for id, n := range runner.counts {
		if n != 1 {
			t.Errorf("job %s processed %d times", id, n)
		}
	}
	if len(fin.done) != 10 {
		t.Errorf("finalizer ran %d times, want 10", len(fin.done))
	}
	var runs []Run
	testDB.Find(&runs)
	for _, r := range runs {
		if r.Attempts != 1 || r.Progress != 100 || r.ResultsHash == nil {
			t.Errorf("run %s = attempts %d progress %d hash %v", r.ID, r.Attempts, r.Progress, r.ResultsHash)
		}
	}
}

func TestWorkerCrash_LeaseExpiresAnotherWorkerCompletes(t *testing.T) {
	clearRuns(t)
	run := queueRun(t)
	q := NewQueue(testDB, 2*time.Second, 3)

	// "Crashed" worker: claims the job, then dies, so no heartbeat, no release.
	if r, _ := q.Claim(context.Background(), "crashed-worker"); r == nil {
		t.Fatal("crashed worker could not claim")
	}

	fin := &recordingFinalizer{stopped: map[string]Status{}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = newWorker("healthy-worker", q, &fakeRunner{counts: map[string]int{}, delay: 10 * time.Millisecond}, fin).Run(ctx)
	}()
	waitFor(t, 10*time.Second, func() bool { return reload(t, run.ID).Status == StatusDone })
	cancel()
	<-done

	r := reload(t, run.ID)
	if r.Attempts != 2 {
		t.Errorf("attempts = %d, want 2 (crash + recovery)", r.Attempts)
	}
	// If the crashed worker comes back, it can't overwrite anything.
	err := store.WithTx(context.Background(), testDB, func(tx *gorm.DB) error { return Complete(tx, run.ID, "crashed-worker", "late") })
	if !errors.Is(err, ErrLeaseLost) {
		t.Errorf("late Complete err = %v, want ErrLeaseLost", err)
	}
	if r := reload(t, run.ID); *r.ResultsHash != "hash-"+run.ID {
		t.Errorf("results hash = %s, the late worker overwrote the result", *r.ResultsHash)
	}
}

func TestWorkerShutdown_ReleasesInFlightJob(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	clearRuns(t)
	run := queueRun(t)
	q := NewQueue(testDB, 10*time.Second, 3)
	runner := &fakeRunner{counts: map[string]int{}, block: true}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = newWorker("w", q, runner, &recordingFinalizer{stopped: map[string]Status{}}).Run(ctx)
	}()
	waitFor(t, 5*time.Second, func() bool { return reload(t, run.ID).Status == StatusRunning })
	cancel() // shutdown: the blocking job outlives the 200 ms grace period
	<-done

	r := reload(t, run.ID)
	if r.Status != StatusQueued || r.Attempts != 0 || r.WorkerID != nil {
		t.Errorf("run after shutdown = status %s attempts %d worker %v, want queued/0/nil", r.Status, r.Attempts, r.WorkerID)
	}
}

func TestCancel_RunningRunStopsWithin2s(t *testing.T) {
	clearRuns(t)
	run := queueRun(t)
	q := NewQueue(testDB, 10*time.Second, 3)
	fin := &recordingFinalizer{stopped: map[string]Status{}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = newWorker("w", q, &fakeRunner{counts: map[string]int{}, block: true}, fin).Run(ctx)
	}()
	waitFor(t, 5*time.Second, func() bool { return reload(t, run.ID).Status == StatusRunning })

	start := time.Now()
	testDB.Model(&Run{}).Where("id = ?", run.ID).Update("cancel_requested", true)
	waitFor(t, 2*time.Second, func() bool { return reload(t, run.ID).Status == StatusCancelled })
	elapsed := time.Since(start)
	cancel()
	<-done

	if fin.stopped[run.ID] != StatusCancelled {
		t.Errorf("finalizer stopped = %v, want cancelled", fin.stopped)
	}
	t.Logf("cancel took %v", elapsed)
}

func TestWorker_RunnerErrorMarksFailed(t *testing.T) {
	clearRuns(t)
	run := queueRun(t)
	q := NewQueue(testDB, 10*time.Second, 3)
	fin := &recordingFinalizer{stopped: map[string]Status{}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = newWorker("w", q, SimRunner{}, fin).Run(ctx) }() // inputs '{}' are invalid
	waitFor(t, 5*time.Second, func() bool { return reload(t, run.ID).Status == StatusFailed })
	cancel()
	<-done
	if fin.stopped[run.ID] != StatusFailed || reload(t, run.ID).Error == nil {
		t.Errorf("want a failed run with an error and a Stopped call; got %v", fin.stopped)
	}
}
