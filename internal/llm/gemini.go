package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/genai"
)

// GeminiConfig configures the Gemini adapter.
type GeminiConfig struct {
	APIKey  string        // secret: never logged
	Model   string        // from GEMINI_MODEL; no default in code (SPEC §13)
	Timeout time.Duration // per call (default 20 s)
	// BaseURL overrides the API endpoint. Tests point it at an httptest server
	// so the real adapter runs without calling Google.
	BaseURL string
	// RetryInitialDelay is the backoff before the single retry (default 1 s).
	RetryInitialDelay time.Duration
}

// Gemini implements Client with google.golang.org/genai (never the deprecated
// github.com/google/generative-ai-go, SPEC §3).
type Gemini struct {
	client *genai.Client
	cfg    GeminiConfig
}

// retryableStatuses are retried once, with backoff (SPEC §13: at most 1 retry on 429/5xx).
func retryableStatuses() []int32 { return []int32{429, 500, 502, 503, 504} }

// NewGemini creates the client. The API key is passed explicitly, so the SDK
// never reads GOOGLE_API_KEY / GEMINI_API_KEY from the environment itself
// (config.Load refuses GOOGLE_API_KEY anyway, D-P-18).
func NewGemini(ctx context.Context, cfg GeminiConfig) (*Gemini, error) {
	if cfg.APIKey == "" || cfg.Model == "" {
		return nil, errors.New("gemini: API key and model are required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}
	if cfg.RetryInitialDelay <= 0 {
		cfg.RetryInitialDelay = time.Second
	}
	cc := &genai.ClientConfig{APIKey: cfg.APIKey, Backend: genai.BackendGeminiAPI}
	if cfg.BaseURL != "" {
		cc.HTTPOptions = genai.HTTPOptions{BaseURL: cfg.BaseURL}
	}
	client, err := genai.NewClient(ctx, cc)
	if err != nil {
		return nil, fmt.Errorf("creating gemini client: %w", err)
	}
	return &Gemini{client: client, cfg: cfg}, nil
}

func (g *Gemini) config(system string, maxTokens int32) *genai.GenerateContentConfig {
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	return &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(system, ""),
		MaxOutputTokens:   maxTokens,
		Temperature:       genai.Ptr[float32](0.2), // factual rephrasing, not creativity
		// Minimal thinking: we want a fast, cheap rewrite of given facts.
		ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelMinimal},
		HTTPOptions: &genai.HTTPOptions{RetryOptions: &genai.HTTPRetryOptions{
			Attempts:        genai.Ptr[int32](2), // the original call + at most 1 retry
			InitialDelay:    genai.Ptr(g.cfg.RetryInitialDelay.Seconds()),
			HTTPStatusCodes: retryableStatuses(),
		}},
	}
}

func (g *Gemini) generate(ctx context.Context, prompt string, cfg *genai.GenerateContentConfig) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, g.cfg.Timeout)
	defer cancel()
	resp, err := g.client.Models.GenerateContent(ctx, g.cfg.Model, genai.Text(prompt), cfg)
	if err != nil {
		var apiErr genai.APIError // returned as a value, not a pointer (D-P-18)
		if errors.As(err, &apiErr) {
			return "", fmt.Errorf("gemini API error %d %s: %w", apiErr.Code, apiErr.Status, err)
		}
		return "", fmt.Errorf("gemini call: %w", err)
	}
	if len(resp.Candidates) > 0 && resp.Candidates[0].FinishReason == genai.FinishReasonMaxTokens {
		return "", ErrOutputTruncated
	}
	text := strings.TrimSpace(resp.Text())
	if text == "" {
		return "", ErrEmptyOutput
	}
	return text, nil
}

// GenerateText implements Client.
func (g *Gemini) GenerateText(ctx context.Context, req TextRequest) (string, error) {
	return g.generate(ctx, req.Prompt, g.config(req.System, req.MaxOutputTokens))
}

// GenerateJSON implements Client using structured output: the response is
// constrained to req.Schema (ResponseJsonSchema) and decoded into out.
func (g *Gemini) GenerateJSON(ctx context.Context, req JSONRequest, out any) error {
	cfg := g.config(req.System, req.MaxOutputTokens)
	cfg.ResponseMIMEType = "application/json"
	cfg.ResponseJsonSchema = req.Schema
	text, err := g.generate(ctx, req.Prompt, cfg)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(text), out); err != nil {
		return fmt.Errorf("decoding gemini JSON: %w", err)
	}
	return nil
}
