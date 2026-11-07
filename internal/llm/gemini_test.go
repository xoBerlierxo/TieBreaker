package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeGemini is an httptest server speaking just enough of the Gemini API.
type fakeGemini struct {
	srv      *httptest.Server
	calls    atomic.Int32
	lastBody atomic.Value // string
	lastURL  atomic.Value // string
	lastKey  atomic.Value // string
}

func newFakeGemini(t *testing.T, handle func(call int32, w http.ResponseWriter)) *fakeGemini {
	t.Helper()
	f := &fakeGemini{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.lastBody.Store(string(b))
		f.lastURL.Store(r.URL.String())
		f.lastKey.Store(r.Header.Get("x-goog-api-key"))
		handle(f.calls.Add(1), w)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func okText(w http.ResponseWriter, text, finish string) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{"candidates": []any{map[string]any{
		"content":      map[string]any{"role": "model", "parts": []any{map[string]any{"text": text}}},
		"finishReason": finish,
	}}}
	_ = json.NewEncoder(w).Encode(resp)
}

func apiError(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"error": {"code": ` + strconv.Itoa(code) + `, "message": "simulated", "status": "SIMULATED"}}`))
}

func client(t *testing.T, f *fakeGemini, timeout time.Duration) *Gemini {
	t.Helper()
	g, err := NewGemini(context.Background(), GeminiConfig{
		APIKey: "test-key-not-real", Model: "test-model", Timeout: timeout,
		BaseURL: f.srv.URL + "/", RetryInitialDelay: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestGemini_SendsModelLimitsAndThinkingLevel(t *testing.T) {
	f := newFakeGemini(t, func(_ int32, w http.ResponseWriter) { okText(w, "Hello.", "STOP") })
	out, err := client(t, f, 5*time.Second).GenerateText(context.Background(), TextRequest{System: "be brief", Prompt: "facts", MaxOutputTokens: 321})
	if err != nil || out != "Hello." {
		t.Fatalf("GenerateText = %q, %v", out, err)
	}
	url, body := f.lastURL.Load().(string), f.lastBody.Load().(string)
	if !strings.Contains(url, "models/test-model:generateContent") {
		t.Errorf("URL = %s", url)
	}
	for _, want := range []string{`"maxOutputTokens":321`, `"thinkingLevel":"MINIMAL"`, `"temperature":0.2`, `"be brief"`, `"facts"`} {
		if !strings.Contains(body, want) {
			t.Errorf("request body lacks %s: %s", want, body)
		}
	}
}

func TestGemini_APIKeyInHeaderNotURL(t *testing.T) {
	f := newFakeGemini(t, func(_ int32, w http.ResponseWriter) { okText(w, "ok", "STOP") })
	if _, err := client(t, f, 5*time.Second).GenerateText(context.Background(), TextRequest{Prompt: "x"}); err != nil {
		t.Fatal(err)
	}
	if f.lastKey.Load().(string) != "test-key-not-real" {
		t.Error("API key not sent in the x-goog-api-key header")
	}
	if strings.Contains(f.lastURL.Load().(string), "test-key-not-real") {
		t.Error("API key leaked into the URL (it would end up in proxy logs)")
	}
}

func TestGemini_RetriesOnceOn503ThenSucceeds(t *testing.T) {
	f := newFakeGemini(t, func(call int32, w http.ResponseWriter) {
		if call == 1 {
			apiError(w, http.StatusServiceUnavailable)
			return
		}
		okText(w, "second try", "STOP")
	})
	out, err := client(t, f, 5*time.Second).GenerateText(context.Background(), TextRequest{Prompt: "x"})
	if err != nil || out != "second try" || f.calls.Load() != 2 {
		t.Fatalf("out=%q err=%v calls=%d, want success on the 2nd call", out, err, f.calls.Load())
	}
}

func TestGemini_GivesUpAfterOneRetryOn429(t *testing.T) {
	f := newFakeGemini(t, func(_ int32, w http.ResponseWriter) { apiError(w, http.StatusTooManyRequests) })
	_, err := client(t, f, 5*time.Second).GenerateText(context.Background(), TextRequest{Prompt: "x"})
	if err == nil {
		t.Fatal("want an error after persistent 429s")
	}
	if n := f.calls.Load(); n != 2 {
		t.Errorf("calls = %d, want exactly 2 (one retry)", n)
	}
}

func TestGemini_NoRetryOn400(t *testing.T) {
	f := newFakeGemini(t, func(_ int32, w http.ResponseWriter) { apiError(w, http.StatusBadRequest) })
	if _, err := client(t, f, 5*time.Second).GenerateText(context.Background(), TextRequest{Prompt: "x"}); err == nil {
		t.Fatal("want an error for 400")
	}
	if n := f.calls.Load(); n != 1 {
		t.Errorf("calls = %d, want 1 (client errors are not retried)", n)
	}
}

func TestGemini_TimeoutHonoured(t *testing.T) {
	f := newFakeGemini(t, func(_ int32, w http.ResponseWriter) {
		time.Sleep(500 * time.Millisecond)
		okText(w, "too late", "STOP")
	})
	start := time.Now()
	_, err := client(t, f, 100*time.Millisecond).GenerateText(context.Background(), TextRequest{Prompt: "x"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
	if time.Since(start) > 400*time.Millisecond {
		t.Error("the call did not stop at the timeout")
	}
}

func TestGemini_MaxTokensIsError(t *testing.T) {
	f := newFakeGemini(t, func(_ int32, w http.ResponseWriter) { okText(w, "cut off mid", "MAX_TOKENS") })
	if _, err := client(t, f, 5*time.Second).GenerateText(context.Background(), TextRequest{Prompt: "x"}); !errors.Is(err, ErrOutputTruncated) {
		t.Fatalf("err = %v, want ErrOutputTruncated", err)
	}
}

func TestGemini_GenerateJSONDecodesIntoOut(t *testing.T) {
	f := newFakeGemini(t, func(_ int32, w http.ResponseWriter) { okText(w, `{"km_low": 800, "km_high": 1200}`, "STOP") })
	var out struct {
		KmLow  int `json:"km_low"`
		KmHigh int `json:"km_high"`
	}
	schema := map[string]any{"type": "object", "properties": map[string]any{"km_low": map[string]any{"type": "integer"}}}
	if err := client(t, f, 5*time.Second).GenerateJSON(context.Background(), JSONRequest{Prompt: "parse", Schema: schema}, &out); err != nil {
		t.Fatal(err)
	}
	if out.KmLow != 800 || out.KmHigh != 1200 {
		t.Errorf("decoded %+v", out)
	}
	body := f.lastBody.Load().(string)
	if !strings.Contains(body, `"responseMimeType":"application/json"`) || !strings.Contains(body, `"responseJsonSchema"`) {
		t.Errorf("structured-output config missing: %s", body)
	}
}

func TestNewGemini_RequiresKeyAndModel(t *testing.T) {
	if _, err := NewGemini(context.Background(), GeminiConfig{Model: "m"}); err == nil {
		t.Error("missing key accepted")
	}
	if _, err := NewGemini(context.Background(), GeminiConfig{APIKey: "k"}); err == nil {
		t.Error("missing model accepted")
	}
}

func TestFake_ReturnsScriptedResponsesAndRecordsCalls(t *testing.T) {
	f := &Fake{Responses: []string{"one", "two"}}
	a, _ := f.GenerateText(context.Background(), TextRequest{Prompt: "p1"})
	b, _ := f.GenerateText(context.Background(), TextRequest{Prompt: "p2"})
	c, _ := f.GenerateText(context.Background(), TextRequest{Prompt: "p3"})
	if a != "one" || b != "two" || c != "two" || f.Calls() != 3 || f.Texts[1].Prompt != "p2" {
		t.Fatalf("got %q %q %q calls=%d", a, b, c, f.Calls())
	}
	f.Err = ErrQuotaExceeded
	if _, err := f.GenerateText(context.Background(), TextRequest{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("scripted error not returned: %v", err)
	}
}
