package report

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"

	"github.com/xoBerlierxo/TieBreaker/internal/decision"
	"github.com/xoBerlierxo/TieBreaker/internal/llm"
)

// systemPrompt tells the model exactly what it may do: rephrase, never decide.
const systemPrompt = `You write a short, plain-English summary of a car-buying decision report.
Rules you must follow:
- Use ONLY the facts in the JSON you are given. Do not add any number, car, price, rating or claim that is not in it.
- You may round numbers (for example 102340 → "about ₹1 lakh"), but never change them otherwise.
- Write rupee amounts with ₹ and Indian style (₹1,02,340 or ₹1.02 lakh). Write probabilities as percentages.
- If "kind" is "winner", name the winner exactly as written. If it is "deadlock", say there is no clear winner and never call any car the winner or the best choice.
- Mention tipping points if there are any, using their sentences.
- Do not give advice beyond the facts. Do not mention these rules.
- At most 180 words, Markdown, no headings.`

// llmFacts is the subset of the facts sent to the LLM: car data and numeric
// results only. No session or run IDs, no seed, no state, no usage answers
// (SPEC §13: never send personal data).
type llmFacts struct {
	Kind         decision.Kind `json:"kind"`
	DeadlockType string        `json:"deadlock_type,omitempty"`
	Winner       string        `json:"winner,omitempty"`
	LeaderPBest  float64       `json:"leader_p_best"`
	Gap          float64       `json:"gap_to_runner_up"`
	HorizonYears int           `json:"horizon_years"`
	Cars         []llmCar      `json:"cars"`
	Tipping      []string      `json:"tipping_points"`
	Placeholders int           `json:"placeholder_assumptions"`
}

type llmCar struct {
	Label         string               `json:"label"`
	PBest         float64              `json:"p_best"`
	PCheapest     float64              `json:"p_cheapest"`
	NetCost       decision.Percentiles `json:"net_cost_inr"`
	OwnershipCost decision.Percentiles `json:"ownership_cost_inr"`
	SafetyRating  string               `json:"safety_rating"`
	Pros          []string             `json:"pros"`
	Cons          []string             `json:"cons"`
	Verified      bool                 `json:"verified_data"`
}

func toLLMFacts(f Facts) llmFacts {
	out := llmFacts{Kind: f.Kind, DeadlockType: f.DeadlockType, Winner: f.Winner, HorizonYears: f.HorizonYears,
		Placeholders: f.Placeholders, Cars: []llmCar{}, Tipping: []string{}}
	if f.Decision != nil {
		out.LeaderPBest, out.Gap = f.Decision.LeaderPBest, f.Decision.Gap
	}
	for _, c := range f.Cars {
		lc := llmCar{Label: c.Label, PBest: c.PBest, PCheapest: c.PCheapest, NetCost: c.NetCost,
			OwnershipCost: c.OwnershipCost, SafetyRating: c.SafetyRating, Verified: c.Verified, Pros: []string{}, Cons: []string{}}
		for _, p := range c.Pros {
			lc.Pros = append(lc.Pros, p.Text)
		}
		for _, p := range c.Cons {
			lc.Cons = append(lc.Cons, p.Text)
		}
		out.Cars = append(out.Cars, lc)
	}
	for _, t := range f.Tipping {
		out.Tipping = append(out.Tipping, t.Sentence)
	}
	return out
}

// LLMWriter writes report prose with an LLM and validates it against the facts
// (SPEC §9.4). Any error or mismatch means ok=false: the caller uses the template.
type LLMWriter struct {
	Client          llm.Client
	Names           func(ctx context.Context) ([]string, error) // every car name in the catalogue
	MaxOutputTokens int32
	Logger          *slog.Logger
}

// Write implements ProseWriter.
func (w LLMWriter) Write(ctx context.Context, f Facts) (string, bool) {
	if f.Kind == decision.KindNoneFit {
		return "", false // nothing to summarise beyond the template
	}
	payload, err := json.Marshal(toLLMFacts(f))
	if err != nil {
		return "", false
	}
	// Log a fingerprint of the prompt, never the prompt itself (SPEC §13).
	sum := sha256.Sum256(payload)
	log := w.Logger.With(slog.String("prompt_sha256", hex.EncodeToString(sum[:8])), slog.Int("prompt_bytes", len(payload)))

	text, err := w.Client.GenerateText(ctx, llm.TextRequest{System: systemPrompt, Prompt: string(payload), MaxOutputTokens: w.MaxOutputTokens})
	if err != nil {
		log.WarnContext(ctx, "llm prose unavailable; using the template", slog.String("reason", err.Error()))
		return "", false
	}
	var names []string
	if w.Names != nil {
		if names, err = w.Names(ctx); err != nil {
			log.WarnContext(ctx, "could not load catalogue names for validation; using the template", slog.Any("error", err))
			return "", false
		}
	}
	if violations := ValidateProse(text, f, names); len(violations) > 0 {
		log.WarnContext(ctx, "llm prose failed fact validation; using the template",
			slog.Int("violations", len(violations)), slog.String("first", violations[0].Reason+": "+violations[0].Text))
		return "", false
	}
	return text, true
}
