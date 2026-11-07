# 05 — Decisions, tipping points and reports

Milestone **M5**. The code is in [internal/decision](../../internal/decision/) (classify, tipping) and [internal/report](../../internal/report/).

---

## 1. From probabilities to a verdict

`decision.Classify` is two lines of policy (SPEC §9.1): a winner if the leader has P(best) ≥ 55 % *or* leads the runner-up by ≥ 15 points, otherwise a deadlock. It returns *why* (`p_best_threshold`, `gap_threshold`, …) so the report can explain itself. Keeping policy in a tiny pure function makes it trivial to table-test (`TestClassify_Table`).

## 2. Tipping points: grid search + bisection

"Which single factor would change the answer?"

1. **Grid:** try 9 values of one input (say km/month from 400 to 2,400) and run a quick simulation at each. Note the leader at each point.
2. **Bracket:** if the leader is car A at 1,150 and car B at 1,400, the switch is somewhere in between.
3. **Bisection:** test the midpoint. Same leader as the low end → the switch is in the upper half; otherwise the lower half. Eight halvings shrink the interval 256 times.

That's the same idea as `sort.Search` or `git bisect`: if you can say "before or after the change?", you can find the change in log₂(n) steps. For whole-number inputs (years, yes/no) we stop when the two ends are neighbours.

**Why the same seed everywhere:** every grid point simulates the *same* futures (common random numbers). If the leader changes, it's because of the input, not because one run happened to draw unluckier repair bills.

## 3. Closures that return modified copies

```go
Apply: func(in sim.Inputs, x float64) sim.Inputs {
    prefs := slices.Clone(in.Prefs)   // never touch the caller's slice
    prefs[idx].Brackets = []sim.Range{{Lo: x, Hi: x}}
    in.Prefs = prefs
    return in
}
```

`sim.Inputs` is passed **by value**, so changing `in.HorizonYears` is safe. But a struct copy is *shallow*: the `Prefs` slice header is copied while the backing array is shared. Writing `in.Prefs[idx] = …` without cloning would silently change the base inputs used by other sweeps running in parallel. `TestTipping_PrefSweepDoesNotModifyBase` guards this.

## 4. Facts first, prose second

[internal/report/facts.go](../../internal/report/facts.go): `BuildFacts` computes **everything** the report will say (numbers, ranked pros and cons, rounded tipping sentences, warnings) into one struct. The templates may only format what's there.

Why bother?

- One source of truth: the JSON in `reports.facts_json` *is* the report; prose is just one rendering of it.
- The UI can draw charts straight from the facts.
- In M6 the LLM gets the facts and nothing else, and a validator checks that every number in its prose appears in them.

## 5. `text/template` + `embed`

```go
//go:embed templates/*.md.tmpl
var templateFS embed.FS

t, _ := template.New(name).Funcs(templateFuncs()).ParseFS(templateFS, "templates/"+name, "templates/partials.md.tmpl")
```

- `{{define "car"}}…{{end}}` in a partials file plus `{{template "car" .}}` gives reusable blocks.
- A `FuncMap` adds helpers such as `inr` (₹1,02,340) and `pct` (62 %).
- `{{range $i, $c := .Breakdown}}{{if $i}} · {{end}}…{{end}}` is the idiom for "join with a separator".
- `text/template`, not `html/template`, because the output is Markdown; the frontend sanitises when it renders HTML.

## 6. Golden-file tests

```go
if *update { os.WriteFile(path, got, 0o600) }
want, _ := os.ReadFile(path)
if got != string(want) { t.Errorf(…) }
```

The expected output lives in `testdata/winner.golden`. When you change a template on purpose, run `go test ./internal/report -run Golden -update` and **review the diff in git**. Unintended changes, such as "₹800 km" or a warning printed twice, then show up as an obvious diff instead of hiding in a long string assertion.

## 7. Atomic hooks across packages

Two places must write a report *in the same transaction* as a state change:

- **Run finished** → `session.Finalizer{OnDone: report.OnRunDone}`, wired in `cmd/tiebreaker/worker.go`.
- **Every car eliminated** → `sessions.SetNoneFitHook(report.OnNoneFit)`, wired in `serve.go`.

`report` imports `session` (for its types); `session` doesn't import `report`. Function-typed hooks injected at startup keep the dependency graph acyclic, which Go requires.

## 8. Long work outside, short transaction inside

Tipping points (hundreds of ms) and later the LLM (seconds) run in the analysis step, **before** the completion transaction opens. The transaction then only does fast inserts. An open transaction holds row locks and a pooled connection, so keeping slow work outside it is a basic scalability rule.

## 9. Proving reproducibility

`tiebreaker verify-run -run-id X` re-runs the stored inputs with the stored seed and compares a SHA-256 over every sample with the stored `results_hash`. It's the strongest possible test of determinism: not "similar numbers" but identical bits.

---

## Questions you should be able to answer

1. **Explain how the tipping-point search finds "above ~1,500 km/month car A wins". How many quick simulations does one sweep with a single crossover take, and why must they all use the same seed?**
   *Hint:* 9 grid points + up to 8 bisection steps ≈ 17; common random numbers make leader changes attributable to the input.

2. **`Apply` receives `sim.Inputs` by value. Why is changing `in.HorizonYears` safe but `in.Prefs[i].Brackets = …` not, and how does the code avoid the bug?**
   *Hint:* shallow copy; a shared backing array; `slices.Clone`.

3. **Why are tipping points and prose rendered *before* the completion transaction, and what would go wrong if `OnRunDone` computed them inside it?**
   *Hint:* row locks and a pool connection held for seconds; lease/heartbeat timing; contention with API requests.
