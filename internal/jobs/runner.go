package jobs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xoBerlierxo/TieBreaker/internal/decision"
	"github.com/xoBerlierxo/TieBreaker/internal/sim"
)

// histogramBins is the resolution of the stored net-cost histograms.
const histogramBins = 30

// RunOutput is what SimRunner hands to the Finalizer through Outcome.Extra.
type RunOutput struct {
	Payload Payload
	Result  sim.Result
	Summary decision.Summary
	// Analysis is whatever the Analyze hook returned (M5: classification and
	// tipping points).
	Analysis any
}

// AnalyzeFunc runs extra pure computation after the main simulation (M5 uses
// it for tipping points). It reports progress 0–100 for its own share.
type AnalyzeFunc func(ctx context.Context, run Run, p Payload, res sim.Result, sum decision.Summary, progress func(pct int)) (any, error)

// SimRunner runs a stored full simulation: decode inputs_json, run the engine
// with the stored seed, summarise, and build the result rows.
type SimRunner struct {
	Workers int // engine workers; 0 = GOMAXPROCS
	Analyze AnalyzeFunc
}

// Run implements Runner.
func (r SimRunner) Run(ctx context.Context, run Run, progress func(int)) (Outcome, error) {
	var p Payload
	if err := json.Unmarshal(run.InputsJSON, &p); err != nil {
		return Outcome{}, fmt.Errorf("decoding stored inputs: %w", err)
	}

	// The main simulation takes 0–80 % of the progress bar when an analysis
	// step follows (tipping points), otherwise 0–99 %.
	simShare := 99
	if r.Analyze != nil {
		simShare = 80
	}
	res, err := sim.Run(ctx, p.Inputs, sim.Options{
		Futures: run.Futures, ChunkSize: run.ChunkSize, Workers: r.Workers, MasterSeed: run.Seed(),
		Progress: func(done, total int) { progress(done * simShare / total) },
	})
	if err != nil {
		return Outcome{}, err
	}
	sum := decision.Summarize(res)

	out := RunOutput{Payload: p, Result: res, Summary: sum}
	if r.Analyze != nil {
		out.Analysis, err = r.Analyze(ctx, run, p, res, sum, func(pct int) { progress(simShare + pct*(99-simShare)/100) })
		if err != nil {
			return Outcome{}, err
		}
	}

	rows, err := ResultRows(res, sum)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Results: rows, ResultsHash: decision.Fingerprint(res), Extra: out}, nil
}

// ResultRows turns a summary into sim_results rows (one per car).
func ResultRows(res sim.Result, sum decision.Summary) ([]ResultRow, error) {
	hists := decision.Histograms(res, histogramBins)
	rows := make([]ResultRow, len(sum.Cars))
	for i, c := range sum.Cars {
		breakdown, err := json.Marshal(c.Breakdown)
		if err != nil {
			return nil, fmt.Errorf("encoding breakdown: %w", err)
		}
		hist, err := json.Marshal(hists[i])
		if err != nil {
			return nil, fmt.Errorf("encoding histogram: %w", err)
		}
		rows[i] = ResultRow{
			ShortlistItemID: c.ID,
			PBest:           c.PBest, PCheapest: c.PCheapest,
			NetcostP10: c.NetCost.P10, NetcostP50: c.NetCost.P50, NetcostP90: c.NetCost.P90,
			OwnershipCostP10: c.OwnershipCost.P10, OwnershipCostP50: c.OwnershipCost.P50, OwnershipCostP90: c.OwnershipCost.P90,
			BreakdownJSON: breakdown, HistogramJSON: hist,
		}
	}
	return rows, nil
}
