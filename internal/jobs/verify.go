package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"

	"gorm.io/gorm"

	"github.com/xoBerlierxo/TieBreaker/internal/decision"
	"github.com/xoBerlierxo/TieBreaker/internal/sim"
)

// Verification is the outcome of re-running a stored run.
type Verification struct {
	RunID    string `json:"run_id"`
	Stored   string `json:"stored_results_hash"`
	Computed string `json:"computed_results_hash"`
	Match    bool   `json:"match"`
	Arch     string `json:"arch"` // bit-exactness is guaranteed per CPU architecture
}

// VerifyRun re-runs a finished simulation from its stored inputs and seed and
// compares the fingerprint of every sample with the stored one (SPEC §6.7:
// re-running with the stored seed reproduces the result exactly).
func VerifyRun(ctx context.Context, db *gorm.DB, runID string) (Verification, error) {
	run, err := GetRun(db.WithContext(ctx), runID)
	if err != nil {
		return Verification{}, err
	}
	if run.Status != StatusDone || run.ResultsHash == nil {
		return Verification{}, fmt.Errorf("run %s is %s, not done", runID, run.Status)
	}
	var p Payload
	if err := json.Unmarshal(run.InputsJSON, &p); err != nil {
		return Verification{}, fmt.Errorf("decoding stored inputs: %w", err)
	}
	res, err := sim.Run(ctx, p.Inputs, sim.Options{Futures: run.Futures, ChunkSize: run.ChunkSize, MasterSeed: run.Seed()})
	if err != nil {
		return Verification{}, err
	}
	got := decision.Fingerprint(res)
	return Verification{RunID: runID, Stored: *run.ResultsHash, Computed: got, Match: got == *run.ResultsHash, Arch: runtime.GOARCH}, nil
}
