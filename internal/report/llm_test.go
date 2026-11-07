package report

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/xoBerlierxo/TieBreaker/internal/llm"
)

func writer(fake *llm.Fake) LLMWriter {
	return LLMWriter{
		Client: fake, MaxOutputTokens: 512,
		Names:  func(context.Context) ([]string, error) { return catalogNames, nil },
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

const faithfulProse = "Test Car B is the best fit: it had the lowest net cost in 70% of the simulated futures, " +
	"with a median net cost of about ₹5.9 lakh. Above ~1,500 km/month, Test Car A would win instead."

func TestRenderLLM_ValidProseIsUsed(t *testing.T) {
	fake := &llm.Fake{Responses: []string{faithfulProse}}
	d, err := Analyzer{Writer: writer(fake)}.draft(context.Background(), BuildFacts(fixtureInput()))
	if err != nil {
		t.Fatal(err)
	}
	if d.ProseSource != SourceLLM || d.ProseMD != faithfulProse {
		t.Fatalf("draft = %s %q, want the LLM prose", d.ProseSource, d.ProseMD)
	}
}

func TestRenderLLM_InvalidProseFallsBackToTemplate(t *testing.T) {
	fake := &llm.Fake{Responses: []string{"Test Car B wins in 91% of futures and saves ₹3 lakh."}} // invented numbers
	d, _ := Analyzer{Writer: writer(fake)}.draft(context.Background(), BuildFacts(fixtureInput()))
	if d.ProseSource != SourceTemplate || !strings.HasPrefix(d.ProseMD, "# Test Car B is the best choice") {
		t.Fatalf("draft = %s, want the template", d.ProseSource)
	}
}

func TestRenderLLM_ErrorOrQuotaFallsBack(t *testing.T) {
	for _, err := range []error{llm.ErrQuotaExceeded, llm.ErrOutputTruncated, context.DeadlineExceeded} {
		d, _ := Analyzer{Writer: writer(&llm.Fake{Err: err})}.draft(context.Background(), BuildFacts(fixtureInput()))
		if d.ProseSource != SourceTemplate {
			t.Errorf("%v: draft source = %s, want template", err, d.ProseSource)
		}
	}
}

func TestRenderLLM_PromptHasNoPersonalOrRunFields(t *testing.T) {
	fake := &llm.Fake{Responses: []string{faithfulProse}}
	in := fixtureInput()
	in.Run.ID, in.Run.Seed = "run-should-not-leak", "1234567890123"
	writer(fake).Write(context.Background(), BuildFacts(in))
	prompt := fake.Texts[0].Prompt
	for _, forbidden := range []string{"run-should-not-leak", "1234567890123", "user_state", `"TN"`, "item-a", "km_per_month", "city_share", "financing"} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("prompt contains %q:\n%s", forbidden, prompt)
		}
	}
	if !strings.Contains(prompt, "Test Car B") || fake.Texts[0].MaxOutputTokens != 512 || fake.Texts[0].System == "" {
		t.Error("prompt is missing the facts, the token limit or the instructions")
	}
}

func TestRunner_LLMDisabledUsesTemplateAndNeverCallsClient(t *testing.T) {
	d, err := Analyzer{Writer: nil}.draft(context.Background(), BuildFacts(fixtureInput()))
	if err != nil || d.ProseSource != SourceTemplate {
		t.Fatalf("draft = %s, %v", d.ProseSource, err)
	}
	fake := &llm.Fake{Responses: []string{faithfulProse}}
	if _, ok := writer(fake).Write(context.Background(), BuildNoneFit("TN", nil)); ok || fake.Calls() != 0 {
		t.Error("none_fit reports must not call the LLM")
	}
}
