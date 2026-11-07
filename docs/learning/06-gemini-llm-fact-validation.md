# 06 — Gemini behind an interface, and a fact validator

Milestone **M6**. The code is in [internal/llm](../../internal/llm/), [internal/report/llm.go](../../internal/report/llm.go) and [internal/report/validate.go](../../internal/report/validate.go).

---

## 1. The rule: the engine decides, the LLM only writes

Every number, ranking and verdict comes from the simulation. The LLM may only turn the facts into friendlier sentences, and even that is checked before anyone sees it. If anything is off, the template text is used instead. The app works fully with `LLM_ENABLED=false`, which is the default.

## 2. Interfaces make the outside world swappable

```go
type Client interface {
    GenerateText(ctx context.Context, req TextRequest) (string, error)
    GenerateJSON(ctx context.Context, req JSONRequest, out any) error
}
```

Three types implement it:

- `llm.Gemini`, the real thing;
- `llm.Fake`, scripted responses for tests;
- wrappers that themselves hold a `Client`: `Cached` and `Budget`.

The report code depends on the interface only, so its tests run in milliseconds with no network and no key.

## 3. The decorator pattern

```go
client := llm.Cached{Inner: llm.Budget{Inner: gemini, …}, …}
```

Each wrapper adds one behaviour and delegates the rest: *cache* (same input → stored answer), then *budget* (count calls per day, refuse over the cap), then the real call. The order matters: with `Cached` outermost, a cache hit never touches the budget. Same idea as HTTP middleware.

## 4. Calling Gemini with `google.golang.org/genai`

```go
client, _ := genai.NewClient(ctx, &genai.ClientConfig{APIKey: key, Backend: genai.BackendGeminiAPI})
resp, err := client.Models.GenerateContent(ctx, model, genai.Text(prompt), &genai.GenerateContentConfig{
    SystemInstruction: genai.NewContentFromText(system, ""),
    MaxOutputTokens:   1024,
    Temperature:       genai.Ptr[float32](0.2),
    ThinkingConfig:    &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelMinimal},
    HTTPOptions:       &genai.HTTPOptions{RetryOptions: &genai.HTTPRetryOptions{Attempts: genai.Ptr[int32](2), HTTPStatusCodes: []int32{429, 500, 502, 503, 504}}},
})
text := resp.Text()
```

- `genai.Ptr` is a tiny generic helper (`func Ptr[T any](v T) *T`). Optional fields are pointers so the SDK can tell "not set" from zero.
- We read the SDK *source* to learn that `Attempts` counts the first try (2 = one retry), and that nil retry options mean no retry at all.
- `genai.APIError` is returned **as a value**, so `errors.As(err, &apiErr)` needs `var apiErr genai.APIError`, not a pointer.

## 5. Testing an HTTP client without the internet

```go
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { … }))
llm.NewGemini(ctx, llm.GeminiConfig{BaseURL: srv.URL + "/", …})
```

`httptest.NewServer` starts a real HTTP server on localhost. Pointing the **real adapter** at it tests our code *and* the SDK's request building: the retry count, the JSON body (`"thinkingLevel":"MINIMAL"`), and the API key sent as a header rather than in the URL.

## 6. Validating free text with regular expressions

[validate.go](../../internal/report/validate.go) extracts numbers with one regexp that captures a hedge word, a currency, the digits and a unit:

```
(~|about|around|…)?\s*(₹|rs\.?|inr)?\s*(\d[\d,]*(?:\.\d+)?)\s*(lakhs?|crores?|%|percent|k|l|th…)?
```

The clever part is **precision from the written form**:

| Written | Value | Precision | Accepts ₹1,02,340? |
|---|---|---|---|
| "₹1,02,340" | 102340 | ₹10 (trailing zero) | ✅ |
| "₹1.02 lakh" | 102000 | ₹1,000 | ✅ (±500) |
| "about ₹1 lakh" | 100000 | ₹1,00,000, hedged | ✅ |
| "₹1.5 lakh" | 150000 | ₹10,000 | ❌ |

Car names are removed first, because "Alpha 1.2 MT" contains a number that is a name, not a fact. Then any *other* catalogue name left in the text is a violation.

## 7. Privacy by construction

The prompt is built from a separate, smaller struct (`llmFacts`) that simply has no fields for session IDs, the seed, the user's state or usage answers. You can't leak what you never put in. Logs record `sha256(prompt)[:8]` and the size, enough to correlate a problem but useless for reading the content.

## 8. Never hold a transaction while waiting on a network

The LLM call happens in the worker's analysis step, before the completion transaction opens. A 20-second network call inside a transaction would hold row locks and a pool connection for all that time.

---

## Questions you should be able to answer

1. **The LLM writes "Car B costs about ₹1 lakh" and the fact is ₹1,02,340. Walk through exactly how the validator decides to accept it. Would "₹1.5 lakh" pass? Why?**
   *Hint:* hedge word → full-unit tolerance; "1" lakh has a precision of 1 lakh; |102340 − 100000| ≤ 100000. For "1.5 lakh" the precision is 0.1 lakh, so |102340 − 150000| > 5000.

2. **Why is the client stack `Cached → Budget → Gemini` and not `Budget → Cached → Gemini`? What happens on the 501st identical request of the day in each order (daily cap 500)?**
   *Hint:* in the first order the cache answers before the budget is consulted; in the second the budget refuses even though the answer is cached.

3. **How do the tests prove that the adapter retries exactly once on HTTP 429, without calling Google?**
   *Hint:* an `httptest` server always answering 429, `BaseURL` override, an atomic call counter that must equal 2.
