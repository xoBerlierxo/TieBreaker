//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/xoBerlierxo/TieBreaker/internal/jobs"
	"github.com/xoBerlierxo/TieBreaker/internal/report"
)

func getReport(t *testing.T, id string) (report.View, resp) {
	t.Helper()
	r := call(t, http.MethodGet, "/v1/sessions/"+id+"/report", "", nil)
	var v report.View
	if r.code == http.StatusOK {
		r.json(t, &v)
	}
	return v, r
}

func TestReport_AfterRunIsWinnerOrDeadlockWithFacts(t *testing.T) {
	stop := startWorker(t)
	defer stop()
	id, ver := sessionReadyToSimulate(t)
	runID := runIDFrom(t, startSim(t, id, ver))
	waitRun(t, runID, jobs.StatusDone)

	v, r := getReport(t, id)
	if r.code != http.StatusOK {
		t.Fatalf("GET report: %d %s", r.code, r.body)
	}
	if v.Kind != "winner" && v.Kind != "deadlock" {
		t.Fatalf("kind = %q", v.Kind)
	}
	if v.ProseSource != report.SourceTemplate || !strings.HasPrefix(v.ProseMD, "# ") {
		t.Errorf("prose_source = %s, prose starts %q", v.ProseSource, v.ProseMD[:min(20, len(v.ProseMD))])
	}
	var f report.Facts
	if err := json.Unmarshal(v.Facts, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Cars) != 3 || f.Run == nil || f.Run.ID != runID || f.Placeholders == 0 {
		t.Errorf("facts = cars %d run %+v placeholders %d", len(f.Cars), f.Run, f.Placeholders)
	}
	for _, c := range f.Cars {
		if c.Label == "" || len(c.Breakdown) != 8 {
			t.Errorf("car facts incomplete: %+v", c)
		}
		if strings.Contains(c.Label, "Gamma") && c.SafetyRating != "not rated" {
			t.Errorf("unrated sample car shows %q", c.SafetyRating)
		}
	}
	if len(v.Results) != 3 {
		t.Errorf("results (with histograms) = %d, want 3", len(v.Results))
	}
}

func TestReport_NoneFitWithoutRun(t *testing.T) {
	id, ver := newSession(t)
	for _, q := range []string{"alpha", "gamma"} {
		ver = version(addCar(t, id, ver, fmt.Sprintf(`{"variant_id": %q, "condition": "new"}`, variantID(t, q))))
	}
	call(t, http.MethodPut, "/v1/sessions/"+id+"/constraints", `{"max_budget_inr": 100000}`, map[string]string{"If-Match": ver})

	v, r := getReport(t, id)
	if r.code != http.StatusOK || v.Kind != "none_fit" || v.RunID != nil {
		t.Fatalf("report = %d %+v", r.code, v)
	}
	if !strings.Contains(v.ProseMD, "over your budget") {
		t.Errorf("none_fit prose lacks the shortfall: %s", v.ProseMD)
	}
}

func TestReport_BeforeRunDone409(t *testing.T) {
	id, ver := sessionReadyToSimulate(t)
	runIDFrom(t, startSim(t, id, ver)) // no worker: stays queued, session SIMULATING
	if _, r := getReport(t, id); r.code != http.StatusConflict {
		t.Fatalf("report while simulating: %d, want 409", r.code)
	}
	fresh, _ := newSession(t)
	if _, r := getReport(t, fresh); r.code != http.StatusNotFound {
		t.Fatalf("report before any run: %d, want 404", r.code)
	}
}

func TestShareLink_PublicReadOnlyNoSessionID(t *testing.T) {
	stop := startWorker(t)
	defer stop()
	id, ver := sessionReadyToSimulate(t)
	waitRun(t, runIDFrom(t, startSim(t, id, ver)), jobs.StatusDone)
	v, _ := getReport(t, id)

	r := call(t, http.MethodGet, v.SharePath, "", nil)
	if r.code != http.StatusOK {
		t.Fatalf("GET %s: %d", v.SharePath, r.code)
	}
	if strings.Contains(string(r.body), id) {
		t.Error("the public share view leaks the session ID")
	}
	var shared report.View
	r.json(t, &shared)
	if shared.ProseMD != v.ProseMD || shared.Kind != v.Kind {
		t.Error("share view differs from the session's report")
	}
	if r := call(t, http.MethodPost, v.SharePath, "{}", nil); r.code != http.StatusNotFound {
		t.Errorf("POST to a share link: %d, want 404 (read-only)", r.code)
	}
}

func TestShareLink_Unknown404(t *testing.T) {
	if r := call(t, http.MethodGet, "/r/AAAAAAAAAAAAAAAAAAAAAA", "", nil); r.code != http.StatusNotFound {
		t.Fatalf("unknown share id: %d, want 404", r.code)
	}
}

func TestRerunWithStoredSeed_ReproducesExactly(t *testing.T) {
	stop := startWorker(t)
	defer stop()
	id, ver := sessionReadyToSimulate(t)
	runID := runIDFrom(t, startSim(t, id, ver))
	waitRun(t, runID, jobs.StatusDone)

	v, err := jobs.VerifyRun(context.Background(), itDB, runID)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Match {
		t.Fatalf("re-run did not reproduce: stored %s, computed %s", v.Stored, v.Computed)
	}
}

func TestVerifyRun_DetectsTamperedInputs(t *testing.T) {
	stop := startWorker(t)
	defer stop()
	id, ver := sessionReadyToSimulate(t)
	runID := runIDFrom(t, startSim(t, id, ver))
	waitRun(t, runID, jobs.StatusDone)

	itDB.Exec(`UPDATE sim_runs SET inputs_json = jsonb_set(inputs_json, '{inputs,world,petrol_price_inr}', '150') WHERE id = ?`, runID)
	v, err := jobs.VerifyRun(context.Background(), itDB, runID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Match {
		t.Fatal("tampered inputs still matched the stored results hash")
	}
}
