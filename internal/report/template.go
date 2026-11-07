package report

import (
	"bytes"
	"embed"
	"fmt"
	"math"
	"strings"
	"text/template"

	"github.com/xoBerlierxo/TieBreaker/internal/decision"
	"github.com/xoBerlierxo/TieBreaker/internal/money"
)

//go:embed templates/*.md.tmpl
var templateFS embed.FS

// Prose sources (stored in reports.prose_source).
const (
	SourceTemplate = "template"
	SourceLLM      = "llm"
)

// templateFuncs are the helpers the templates may use. They only format
// numbers that are already in the facts; templates never compute anything new.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"inr":  money.FormatINR,
		"lakh": money.FormatLakh,
		"num":  func(n int) string { return money.FormatNumber(float64(n)) },
		"km":   money.FormatNumber,
		"pct":  func(p float64) string { return fmt.Sprintf("%.0f%%", math.Round(p*100)) },
		"join": strings.Join,
		"plus": func(a, b int) int { return a + b },
	}
}

// RenderTemplate renders the facts as Markdown with the template for their
// kind. It always works and is the fallback for the LLM (SPEC §9.4).
func RenderTemplate(f Facts) (string, error) {
	name := map[decision.Kind]string{
		decision.KindWinner:   "winner.md.tmpl",
		decision.KindDeadlock: "deadlock.md.tmpl",
		decision.KindNoneFit:  "none_fit.md.tmpl",
	}[f.Kind]
	if name == "" {
		return "", fmt.Errorf("no template for report kind %q", f.Kind)
	}
	t, err := template.New(name).Funcs(templateFuncs()).ParseFS(templateFS, "templates/"+name, "templates/partials.md.tmpl")
	if err != nil {
		return "", fmt.Errorf("parsing report template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, f); err != nil {
		return "", fmt.Errorf("rendering report: %w", err)
	}
	return strings.TrimSpace(buf.String()) + "\n", nil
}
