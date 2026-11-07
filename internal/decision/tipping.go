package decision

import (
	"context"
	"math"
	"runtime"
	"slices"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/xoBerlierxo/TieBreaker/internal/sim"
)

// Tipping-point search settings (SPEC §9.2).
const (
	gridPoints       = 9
	bisectIterations = 8
)

// Sweep is one input the tipping-point search varies, everything else fixed.
type Sweep struct {
	Key       string                                    // "km_per_month", "ownership_years", "fuel_trend", "home_charging", "pref:<attr>"
	Lo, Hi    float64                                   // plausible range to sweep
	Integer   bool                                      // only whole values (years, yes/no)
	UserLo    float64                                   // the user's own answer, to say whether
	UserHi    float64                                   // the crossover is inside it
	Apply     func(in sim.Inputs, x float64) sim.Inputs // returns a copy with the input set to x
	MaxPoints int                                       // 0 = gridPoints (home charging has 2)
}

// GridPoint is one sampled value and who led there.
type GridPoint struct {
	X      float64   `json:"x"`
	Leader int       `json:"leader"`
	PBest  []float64 `json:"p_best"`
}

// TippingPoint is a crossover: below it one car leads, above it another.
type TippingPoint struct {
	Input           string      `json:"input"`
	Crossover       float64     `json:"crossover"`
	Below           int         `json:"below"` // car index leading below the crossover
	Above           int         `json:"above"` // car index leading above it
	InsideUserRange bool        `json:"inside_user_range"`
	Grid            []GridPoint `json:"grid"`
}

// Tipping is the result of the search.
type Tipping struct {
	Points []TippingPoint `json:"points"`
	// GenuinelyClose is true when no single input changes the leader (SPEC §9.2):
	// a valid, useful answer, not a failure.
	GenuinelyClose bool `json:"genuinely_close"`
}

// SweepConfig is what BuildSweeps needs beyond the inputs.
type SweepConfig struct {
	FuelTrendLo, FuelTrendHi float64 // plausible range of the fuel-price trend μ (assumption price_trend.sweep)
	PrefCap                  float64 // top of the ₹ ladder, e.g. 8L
}

// BuildSweeps lists the inputs to vary (SPEC §9.2): km/month, ownership years,
// the fuel-price trend, home charging (only if an EV is shortlisted), and each
// priced preference.
func BuildSweeps(in sim.Inputs, cfg SweepConfig) []Sweep {
	km := in.Usage.KmPerMonth
	T := float64(in.HorizonYears)
	sweeps := []Sweep{
		{
			Key: "km_per_month", Lo: math.Max(100, km.Lo/2), Hi: math.Min(20_000, math.Max(2*km.Hi, 200)),
			UserLo: km.Lo, UserHi: km.Hi,
			Apply: func(in sim.Inputs, x float64) sim.Inputs { in.Usage.KmPerMonth = sim.Range{Lo: x, Hi: x}; return in },
		},
		{
			Key: "ownership_years", Lo: math.Max(1, T-4), Hi: math.Min(sim.MaxHorizonYears, T+4), Integer: true,
			UserLo: T, UserHi: T,
			Apply: func(in sim.Inputs, x float64) sim.Inputs { in.HorizonYears = int(math.Round(x)); return in },
		},
		{
			Key: "fuel_trend", Lo: cfg.FuelTrendLo, Hi: cfg.FuelTrendHi,
			UserLo: in.World.PetrolTrend.Mu, UserHi: in.World.PetrolTrend.Mu,
			Apply: func(in sim.Inputs, x float64) sim.Inputs {
				in.World.PetrolTrend.Mu, in.World.DieselTrend.Mu, in.World.CNGTrend.Mu = x, x, x
				return in
			},
		},
	}
	hasEV := slices.ContainsFunc(in.Cars, func(c sim.Car) bool { return c.Fuel == sim.FuelEV })
	if hasEV {
		home := 0.0
		if in.Usage.HomeCharging {
			home = 1
		}
		sweeps = append(sweeps, Sweep{
			Key: "home_charging", Lo: 0, Hi: 1, Integer: true, MaxPoints: 2, UserLo: home, UserHi: home,
			Apply: func(in sim.Inputs, x float64) sim.Inputs { in.Usage.HomeCharging = x >= 0.5; return in },
		})
	}
	for i, p := range in.Prefs {
		idx := i
		b := p.Brackets[0]
		sweeps = append(sweeps, Sweep{
			Key: "pref:" + p.Key, Lo: 0, Hi: cfg.PrefCap, UserLo: b.Lo, UserHi: b.Hi,
			Apply: func(in sim.Inputs, x float64) sim.Inputs {
				prefs := slices.Clone(in.Prefs) // never modify the caller's slice
				prefs[idx].Brackets = []sim.Range{{Lo: x, Hi: x}}
				in.Prefs = prefs
				return in
			},
		})
	}
	return sweeps
}

// QuickOptions controls the quick simulations used by the search.
type QuickOptions struct {
	Futures   int
	ChunkSize int
	Seed      uint64 // one seed for every point: the comparison is on identical futures
}

// FindTippingPoints sweeps each input over 9 grid points with quick
// simulations; wherever the leader changes between neighbouring points, it
// bisects to find the crossover (SPEC §9.2). Sweeps run in parallel; progress
// reports 0–100.
func FindTippingPoints(ctx context.Context, base sim.Inputs, sweeps []Sweep, o QuickOptions, progress func(pct int)) (Tipping, error) {
	results := make([][]TippingPoint, len(sweeps))
	var (
		mu   sync.Mutex // serialises the progress counter and callback
		done int
	)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.GOMAXPROCS(0))
	for i, sw := range sweeps {
		g.Go(func() error {
			points, err := sweepOne(gctx, base, sw, o)
			if err != nil {
				return err
			}
			results[i] = points
			mu.Lock()
			done++
			if progress != nil {
				progress(done * 100 / len(sweeps))
			}
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return Tipping{}, err
	}
	var t Tipping
	for _, pts := range results { // in sweep order: deterministic
		t.Points = append(t.Points, pts...)
	}
	t.GenuinelyClose = len(t.Points) == 0
	return t, nil
}

func sweepOne(ctx context.Context, base sim.Inputs, sw Sweep, o QuickOptions) ([]TippingPoint, error) {
	leaderAt := func(x float64) (int, []float64, error) {
		res, err := sim.Run(ctx, sw.Apply(base, x), sim.Options{Futures: o.Futures, ChunkSize: o.ChunkSize, Workers: 1, MasterSeed: o.Seed})
		if err != nil {
			return 0, nil, err
		}
		p := PBest(res)
		return Leader(p), p, nil
	}

	xs := gridValues(sw)
	grid := make([]GridPoint, len(xs))
	for i, x := range xs {
		l, p, err := leaderAt(x)
		if err != nil {
			return nil, err
		}
		grid[i] = GridPoint{X: x, Leader: l, PBest: p}
	}

	var out []TippingPoint
	for i := 1; i < len(grid); i++ {
		if grid[i].Leader == grid[i-1].Leader {
			continue
		}
		lo, hi := grid[i-1], grid[i]
		for range bisectIterations {
			if sw.Integer && hi.X-lo.X <= 1 {
				break
			}
			mid := (lo.X + hi.X) / 2
			if sw.Integer {
				mid = math.Floor(mid)
			}
			l, p, err := leaderAt(mid)
			if err != nil {
				return nil, err
			}
			if l == lo.Leader {
				lo = GridPoint{X: mid, Leader: l, PBest: p}
			} else {
				hi = GridPoint{X: mid, Leader: l, PBest: p}
			}
		}
		crossover := (lo.X + hi.X) / 2
		if sw.Integer {
			crossover = hi.X // the first whole value at which the new leader wins
		}
		out = append(out, TippingPoint{
			Input: sw.Key, Crossover: crossover, Below: lo.Leader, Above: hi.Leader,
			InsideUserRange: crossover >= sw.UserLo && crossover <= sw.UserHi || nearUserValue(sw, crossover),
			Grid:            grid,
		})
	}
	return out, nil
}

// nearUserValue treats a crossover within one grid step of a single-valued
// answer (years, trend) as relevant to the user.
func nearUserValue(sw Sweep, x float64) bool {
	if sw.UserLo != sw.UserHi {
		return false
	}
	step := (sw.Hi - sw.Lo) / float64(gridPoints-1)
	return math.Abs(x-sw.UserLo) <= step
}

// gridValues spreads up to 9 points evenly over [Lo, Hi] (whole numbers for
// integer inputs, without duplicates).
func gridValues(sw Sweep) []float64 {
	n := gridPoints
	if sw.MaxPoints > 0 {
		n = sw.MaxPoints
	}
	var xs []float64
	for i := range n {
		x := sw.Lo + float64(i)*(sw.Hi-sw.Lo)/float64(n-1)
		if sw.Integer {
			x = math.Round(x)
		}
		if len(xs) == 0 || x != xs[len(xs)-1] {
			xs = append(xs, x)
		}
	}
	return xs
}

// IsPrefSweep reports whether a tipping point is about a preference value.
func IsPrefSweep(input string) (attribute string, ok bool) {
	return strings.CutPrefix(input, "pref:")
}
