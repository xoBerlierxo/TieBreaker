// Command demo walks through the whole Tiebreaker flow against a running API:
// create a session, shortlist 2–4 cars matching -q, set constraints, answer
// the usage and trade-off questions, run the full simulation while streaming
// its progress, and print the report and its share link.
//
//	go run ./scripts/demo                       # against http://localhost:8080
//	go run ./scripts/demo -base https://api.example.com
//
// It is written in Go (not bash + jq) so it runs the same on Windows, macOS
// and Linux with nothing but the Go toolchain.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	base := flag.String("base", "http://localhost:8080", "API base URL")
	query := flag.String("q", "", "catalogue search used to pick 2–4 cars (default: the first 4 in the catalogue)")
	flag.Parse()
	c := &client{base: strings.TrimRight(*base, "/"), http: &http.Client{Timeout: 30 * time.Second}}
	if err := run(c, *query); err != nil {
		fmt.Fprintln(os.Stderr, "demo failed:", err)
		os.Exit(1)
	}
}

type client struct {
	base    string
	http    *http.Client
	version string // the session's current ETag
}

func (c *client) do(method, path string, body any, headers map[string]string, out any) (int, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.base+path, rd)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if etag := res.Header.Get("ETag"); etag != "" {
		c.version = etag
	}
	if res.StatusCode >= 300 {
		return res.StatusCode, fmt.Errorf("%s %s → %d: %s", method, path, res.StatusCode, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return res.StatusCode, fmt.Errorf("decoding %s: %w", path, err)
		}
	}
	return res.StatusCode, nil
}

func newKey() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func step(format string, args ...any) { fmt.Printf("\n▶ "+format+"\n", args...) }

func run(c *client, query string) error {
	step("Health check")
	if _, err := c.do("GET", "/readyz", nil, nil, nil); err != nil {
		return err
	}

	step("Create a session in Tamil Nadu")
	var sess struct {
		ID string `json:"id"`
	}
	if _, err := c.do("POST", "/v1/sessions", map[string]string{"user_state": "TN"}, nil, &sess); err != nil {
		return err
	}
	fmt.Println("  session", sess.ID)

	step("Search the catalogue for %q and shortlist up to four cars", query)
	var found struct {
		Variants []struct {
			ID          string   `json:"id"`
			DisplayName string   `json:"display_name"`
			Warnings    []string `json:"warnings"`
		} `json:"variants"`
	}
	if _, err := c.do("GET", "/v1/variants?limit=4&q="+url.QueryEscape(query), nil, nil, &found); err != nil {
		return err
	}
	if len(found.Variants) < 2 {
		return fmt.Errorf("search %q found %d cars, need at least 2; run the seed loader (go run ./scripts/seed) or pass -q", query, len(found.Variants))
	}
	for _, v := range found.Variants {
		fmt.Printf("  + %s  warnings=%v\n", v.DisplayName, v.Warnings)
		if _, err := c.do("POST", "/v1/sessions/"+sess.ID+"/cars", map[string]string{"variant_id": v.ID, "condition": "new"},
			map[string]string{"If-Match": c.version}, nil); err != nil {
			return err
		}
	}

	step("Set hard constraints (budget ₹20 lakh, home charging available)")
	if _, err := c.do("PUT", "/v1/sessions/"+sess.ID+"/constraints", map[string]any{"max_budget_inr": 2_000_000, "home_charging": true},
		map[string]string{"If-Match": c.version}, nil); err != nil {
		return err
	}

	step("Answer the usage questions")
	usage := map[string]any{
		"usage:km_per_month":    map[string]any{"km_per_month": []int{800, 1200}},
		"usage:city_share":      map[string]any{"city_share_pct": []int{50, 70}},
		"usage:ownership_years": map[string]any{"ownership_years": 5},
		"usage:financing":       map[string]any{"mode": "cash"},
		"usage:parking":         map[string]any{"parking": "covered"},
	}
	yes := true
	for {
		var next struct {
			Done     bool   `json:"done"`
			Reason   string `json:"reason"`
			Question *struct {
				ID    string `json:"question_id"`
				Phase string `json:"phase"`
				Text  string `json:"text"`
			} `json:"question"`
			PBest []struct {
				Label string  `json:"label"`
				PBest float64 `json:"p_best"`
			} `json:"p_best"`
		}
		if _, err := c.do("GET", "/v1/sessions/"+sess.ID+"/next-question", nil, nil, &next); err != nil {
			return err
		}
		if next.Done {
			fmt.Printf("  interview finished: %s\n", next.Reason)
			for _, p := range next.PBest {
				fmt.Printf("    current P(best) %-45s %.0f%%\n", p.Label, p.PBest*100)
			}
			break
		}
		q := next.Question
		var answer any
		if q.Phase == "usage" {
			answer = usage[q.ID]
		} else {
			answer = map[string]bool{"yes": yes}
			yes = !yes
		}
		fmt.Printf("  Q: %s\n  A: %v\n", q.Text, answer)
		if _, err := c.do("POST", "/v1/sessions/"+sess.ID+"/answers", map[string]any{"question_id": q.ID, "answer": answer},
			map[string]string{"If-Match": c.version, "Idempotency-Key": newKey()}, nil); err != nil {
			return err
		}
	}

	step("Start the full simulation")
	var started struct {
		RunID     string `json:"run_id"`
		EventsURL string `json:"events_url"`
	}
	if _, err := c.do("POST", "/v1/sessions/"+sess.ID+"/simulations", nil, map[string]string{"If-Match": c.version}, &started); err != nil {
		return err
	}
	fmt.Println("  run", started.RunID)

	step("Stream progress (server-sent events)")
	if err := c.stream(started.EventsURL); err != nil {
		return err
	}

	step("The report")
	var rep struct {
		Kind        string `json:"kind"`
		SharePath   string `json:"share_path"`
		ProseMD     string `json:"prose_md"`
		ProseSource string `json:"prose_source"`
	}
	if _, err := c.do("GET", "/v1/sessions/"+sess.ID+"/report", nil, nil, &rep); err != nil {
		return err
	}
	fmt.Printf("  kind=%s prose_source=%s\n\n%s\n", rep.Kind, rep.ProseSource, rep.ProseMD)
	fmt.Printf("Share link: %s%s\n", c.base, rep.SharePath)
	fmt.Printf("Reproduce:  tiebreaker verify-run -run-id %s\n", started.RunID)
	return nil
}

// stream prints SSE progress events until the run finishes.
func (c *client) stream(path string) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	res, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		var ev struct {
			Status   string `json:"status"`
			Progress int    `json:"progress"`
			Error    string `json:"error"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil {
			continue
		}
		fmt.Printf("  %-9s %3d%%\n", ev.Status, ev.Progress)
		switch ev.Status {
		case "done":
			return nil
		case "failed", "cancelled":
			return fmt.Errorf("run %s: %s", ev.Status, ev.Error)
		}
	}
	return sc.Err()
}
