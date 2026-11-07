// Command llm-smoke checks a real Gemini key and model end to end: it sends a
// small fictional report's facts, prints the prose, and says whether it passed
// the fact validator (which decides between "llm" and "template" prose).
//
//	GEMINI_API_KEY=... GEMINI_MODEL=gemini-3.5-flash-lite go run ./scripts/llm-smoke
//
// The facts are made up and contain no personal data.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/xoBerlierxo/TieBreaker/internal/decision"
	"github.com/xoBerlierxo/TieBreaker/internal/llm"
	"github.com/xoBerlierxo/TieBreaker/internal/report"
)

func main() {
	key, model := os.Getenv("GEMINI_API_KEY"), os.Getenv("GEMINI_MODEL")
	if key == "" || model == "" {
		fmt.Fprintln(os.Stderr, "set GEMINI_API_KEY and GEMINI_MODEL (and do not set GOOGLE_API_KEY)")
		os.Exit(2)
	}
	if os.Getenv("GOOGLE_API_KEY") != "" {
		fmt.Fprintln(os.Stderr, "GOOGLE_API_KEY is set; unset it: the SDK would prefer it over GEMINI_API_KEY")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	g, err := llm.NewGemini(ctx, llm.GeminiConfig{APIKey: key, Model: model})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	facts := report.Facts{
		Kind: decision.KindWinner, Winner: "Sample Motors Beta EV", HorizonYears: 5, Placeholders: 35,
		Decision:   &decision.Classification{Kind: decision.KindWinner, LeaderPBest: 0.71, Gap: 0.48},
		Thresholds: decision.Thresholds{WinPBest: 0.55, WinGap: 0.15},
		Cars: []report.CarFacts{
			{Label: "Sample Motors Beta EV", Make: "Sample Motors", Model: "Beta EV", PBest: 0.71, PCheapest: 0.40, SafetyRating: "5★",
				NetCost: decision.Percentiles{P10: 880_000, P50: 940_000, P90: 1_010_000},
				Pros:    []report.Point{{Source: "cost", Key: "energy", Text: "Lower fuel/energy cost: about ₹2.1 lakh less over 5 years (median, today's money)", ValueINR: 210_000}}},
			{Label: "Sample Motors Alpha", Make: "Sample Motors", Model: "Alpha", PBest: 0.23, PCheapest: 0.55, SafetyRating: "3★",
				NetCost: decision.Percentiles{P10: 930_000, P50: 1_000_000, P90: 1_090_000}},
		},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	w := report.LLMWriter{Client: g, MaxOutputTokens: 1024, Logger: logger,
		Names: func(context.Context) ([]string, error) { return []string{"Sample Motors", "Beta EV", "Alpha"}, nil }}

	prose, ok := w.Write(ctx, facts)
	if !ok {
		fmt.Println("RESULT: LLM prose unavailable or rejected; the app would use the template (see the warning above).")
		os.Exit(1)
	}
	fmt.Printf("RESULT: LLM prose accepted by the fact validator.\n\n%s\n", prose)
}
