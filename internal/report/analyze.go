package report

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/xoBerlierxo/TieBreaker/internal/decision"
	"github.com/xoBerlierxo/TieBreaker/internal/jobs"
	"github.com/xoBerlierxo/TieBreaker/internal/session"
	"github.com/xoBerlierxo/TieBreaker/internal/sim"
)

// Draft is a finished report waiting to be stored.
type Draft struct {
	Facts       Facts
	ProseMD     string
	ProseSource string
}

// ProseWriter rephrases facts in plain English (M6: Gemini, validated). It
// returns ok=false whenever the template should be used instead.
type ProseWriter interface {
	Write(ctx context.Context, f Facts) (prose string, ok bool)
}

// Analyzer is the worker's post-simulation step (jobs.AnalyzeFunc): classify,
// look for tipping points, build the facts and the prose. It runs outside any
// database transaction, because tipping points and the LLM take time.
type Analyzer struct {
	Thresholds   decision.Thresholds
	QuickFutures int
	ChunkSize    int
	Writer       ProseWriter // nil: template only
}

// Analyze implements jobs.AnalyzeFunc.
func (a Analyzer) Analyze(ctx context.Context, run jobs.Run, p jobs.Payload, res sim.Result, sum decision.Summary, progress func(int)) (any, error) {
	var rc session.RunContext
	if err := json.Unmarshal(p.Context, &rc); err != nil {
		return nil, fmt.Errorf("decoding run context: %w", err)
	}
	pBest := make([]float64, len(sum.Cars))
	for i, c := range sum.Cars {
		pBest[i] = c.PBest
	}
	cls := decision.Classify(pBest, a.Thresholds)

	// Tipping points are computed for every report: deadlock reports are built
	// around them, and winner reports list what could change the answer.
	sweeps := decision.BuildSweeps(p.Inputs, decision.SweepConfig{
		FuelTrendLo: rc.FuelTrendSweep[0], FuelTrendHi: rc.FuelTrendSweep[1], PrefCap: float64(rc.Ladder.Cap),
	})
	tipping, err := decision.FindTippingPoints(ctx, p.Inputs, sweeps,
		decision.QuickOptions{Futures: a.QuickFutures, ChunkSize: a.ChunkSize, Seed: run.Seed()}, progress)
	if err != nil {
		return nil, fmt.Errorf("finding tipping points: %w", err)
	}

	facts := BuildFacts(Input{
		Inputs: p.Inputs, Context: rc, Summary: sum, Classification: cls, Tipping: tipping, Thresholds: a.Thresholds,
		Run: RunFacts{ID: run.ID, ModelVersion: run.ModelVersion, Seed: strconv.FormatUint(run.Seed(), 10),
			Futures: run.Futures, ResultsHash: decision.Fingerprint(res)},
	})
	return a.draft(ctx, facts)
}

// draft renders prose: the LLM if enabled and its text passes the fact
// validator, otherwise the template (SPEC §9.4).
func (a Analyzer) draft(ctx context.Context, f Facts) (Draft, error) {
	tmpl, err := RenderTemplate(f)
	if err != nil {
		return Draft{}, err
	}
	d := Draft{Facts: f, ProseMD: tmpl, ProseSource: SourceTemplate}
	if a.Writer != nil {
		if prose, ok := a.Writer.Write(ctx, f); ok {
			d.ProseMD, d.ProseSource = prose, SourceLLM
		}
	}
	return d, nil
}
