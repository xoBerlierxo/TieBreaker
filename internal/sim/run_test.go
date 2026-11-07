package sim

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"strconv"
	"sync"
	"testing"
	"time"
)

// sameBits compares two results exactly, bit for bit.
func sameBits(t *testing.T, a, b Result, label string) {
	t.Helper()
	if a.Futures != b.Futures || len(a.Cars) != len(b.Cars) {
		t.Fatalf("%s: shapes differ", label)
	}
	for c := range a.Cars {
		series := [][2][]float64{{a.Cars[c].NetCost, b.Cars[c].NetCost}, {a.Cars[c].OwnershipCost, b.Cars[c].OwnershipCost}, {a.Cars[c].PrefValue, b.Cars[c].PrefValue}}
		for cat := range NumCategories {
			series = append(series, [2][]float64{a.Cars[c].Categories[cat], b.Cars[c].Categories[cat]})
		}
		for _, s := range series {
			for k := range s[0] {
				if math.Float64bits(s[0][k]) != math.Float64bits(s[1][k]) {
					t.Fatalf("%s: car %s future %d differs: %v vs %v", label, a.Cars[c].ID, k, s[0][k], s[1][k])
				}
			}
		}
	}
}

func TestRun_IdenticalAcrossWorkerCounts_1_4_16(t *testing.T) {
	in := stochasticInputs()
	one := runOrFail(t, in, opts(20_000, 1, 2026))
	for _, w := range []int{4, 16} {
		sameBits(t, one, runOrFail(t, in, opts(20_000, w, 2026)), "workers=1 vs workers="+strconv.Itoa(w))
	}
}

func TestRun_FuturesNotMultipleOfChunkSize(t *testing.T) {
	in := stochasticInputs()
	a := runOrFail(t, in, Options{Futures: 2_537, ChunkSize: 300, Workers: 1, MasterSeed: 9})
	b := runOrFail(t, in, Options{Futures: 2_537, ChunkSize: 300, Workers: 7, MasterSeed: 9})
	if len(a.Cars[0].NetCost) != 2_537 {
		t.Fatalf("got %d samples, want 2537", len(a.Cars[0].NetCost))
	}
	sameBits(t, a, b, "uneven chunks")
}

func TestRun_DifferentSeedsGiveDifferentResults(t *testing.T) {
	in := stochasticInputs()
	a := runOrFail(t, in, opts(1000, 2, 1))
	b := runOrFail(t, in, opts(1000, 2, 2))
	if a.Cars[0].NetCost[0] == b.Cars[0].NetCost[0] && a.Cars[0].NetCost[1] == b.Cars[0].NetCost[1] {
		t.Fatal("different seeds produced identical samples")
	}
}

// resultHash fingerprints every sample, so any change to the model or the
// random-number layout is noticed.
func resultHash(r Result) string {
	h := sha256.New()
	var buf [8]byte
	for _, c := range r.Cars {
		for _, s := range [][]float64{c.NetCost, c.OwnershipCost, c.PrefValue} {
			for _, x := range s {
				binary.LittleEndian.PutUint64(buf[:], math.Float64bits(x))
				h.Write(buf[:])
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// goldenHash is the fingerprint of stochasticInputs() with seed 42 and 5,000
// futures under ModelVersion "sim-v1". If this test fails, results changed:
// if the change is intended, bump ModelVersion and update the hash.
// Note: bit-exactness is guaranteed on one CPU architecture (amd64 here); the
// Go spec allows fused multiply-add on others (DECISIONS.md).
const goldenHash = "91c5bbc149966949e1edd3de770018aacb01e49319d137249a69bf163ee0e93b"

func TestRun_GoldenHash(t *testing.T) {
	got := resultHash(runOrFail(t, stochasticInputs(), opts(5000, 4, 42)))
	if got != goldenHash {
		t.Errorf("result hash = %s\nwant          %s\n(model output changed: bump ModelVersion if intended)", got, goldenHash)
	}
}

func TestRun_CancelMidRunReturnsCanceledPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var once sync.Once
	var cancelledAt time.Time
	o := Options{Futures: 2_000_000, ChunkSize: 1000, Workers: 4, MasterSeed: 1,
		Progress: func(done, total int) {
			once.Do(func() { cancelledAt = time.Now(); cancel() })
		}}

	_, err := Run(ctx, stochasticInputs(), o)
	elapsed := time.Since(cancelledAt)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed > 250*time.Millisecond {
		t.Errorf("Run returned %v after cancellation, want promptly (< 250ms)", elapsed)
	}
	// goleak (TestMain) verifies no goroutine is left behind.
}

func TestRun_PreCancelledContextDoesNoWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	o := opts(10_000, 2, 1)
	o.Progress = func(int, int) { called = true }
	if _, err := Run(ctx, stochasticInputs(), o); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if called {
		t.Error("progress was reported for a pre-cancelled run")
	}
}

func TestRun_DeadlineExceededIsReported(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err := Run(ctx, stochasticInputs(), opts(5_000_000, 2, 1))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestRun_ProgressCalledOncePerChunkMonotonic(t *testing.T) {
	var got []int
	total := -1
	o := opts(10_500, 8, 3) // 11 chunks
	o.Progress = func(done, tot int) { got = append(got, done); total = tot }
	runOrFail(t, stochasticInputs(), o)
	if total != 11 || len(got) != 11 {
		t.Fatalf("progress calls = %v (total %d), want 1..11", got, total)
	}
	for i, d := range got {
		if d != i+1 {
			t.Fatalf("progress not monotonic: %v", got)
		}
	}
}

func TestRun_InvalidInputsRejected(t *testing.T) {
	in := stochasticInputs()
	in.HorizonYears = 0
	if _, err := Run(context.Background(), in, opts(10, 1, 1)); !errors.Is(err, ErrInvalidInputs) {
		t.Fatalf("err = %v, want ErrInvalidInputs", err)
	}
	if _, err := Run(context.Background(), stochasticInputs(), Options{Futures: 0, ChunkSize: 1}); !errors.Is(err, ErrInvalidInputs) {
		t.Fatalf("zero futures: err = %v, want ErrInvalidInputs", err)
	}
}

func TestInputsValidate_Table(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Inputs)
	}{
		{"no cars", func(in *Inputs) { in.Cars = nil }},
		{"horizon 16", func(in *Inputs) { in.HorizonYears = 16 }},
		{"negative discount", func(in *Inputs) { in.DiscountRate = -0.1 }},
		{"zero price", func(in *Inputs) { in.Cars[0].UpfrontINR = 0 }},
		{"zero efficiency", func(in *Inputs) { in.Cars[0].ClaimedEfficiency = 0 }},
		{"duplicate car id", func(in *Inputs) { in.Cars[1].ID = in.Cars[0].ID }},
		{"unknown fuel", func(in *Inputs) { in.Cars[0].Fuel = "steam" }},
		{"inverted bracket", func(in *Inputs) { in.Prefs[0].Brackets = []Range{{10, 5}} }},
		{"share above 1", func(in *Inputs) { in.Prefs[0].Shares[0] = 1.5 }},
		{"shares length", func(in *Inputs) { in.Prefs[0].Shares = []float64{1} }},
		{"city share above 1", func(in *Inputs) { in.Usage.CityShare = Range{0.5, 1.2} }},
		{"zero tenure", func(in *Inputs) { in.Usage.Loan.TenureMonths = 0 }},
		{"empty resale curve", func(in *Inputs) { in.Cars[0].ResaleCurve = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := stochasticInputs()
			tt.mut(&in)
			if err := in.Validate(); !errors.Is(err, ErrInvalidInputs) {
				t.Errorf("Validate() = %v, want ErrInvalidInputs", err)
			}
		})
	}
	if err := stochasticInputs().Validate(); err != nil {
		t.Fatalf("valid inputs rejected: %v", err)
	}
}
