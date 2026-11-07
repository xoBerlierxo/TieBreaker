// Package jobs runs full simulations in the background (SPEC §11): a
// Postgres-backed queue (SELECT … FOR UPDATE SKIP LOCKED with leases), a
// worker loop with heartbeats and cancellation, and progress published on
// Redis pub/sub channel run:{id}.
package jobs

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/xoBerlierxo/TieBreaker/internal/sim"
)

// Status is a run's lifecycle state (SPEC §5.4).
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusDone      Status = "done"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Terminal reports whether the run can no longer change.
func (s Status) Terminal() bool {
	return s == StatusDone || s == StatusFailed || s == StatusCancelled
}

// KindFull is a full simulation (quick sims run in-process and are not stored).
const KindFull = "full"

// ErrLeaseLost means the worker no longer owns the run: its lease expired and
// another worker may have taken it over. Every write is fenced on worker_id,
// so a worker that lost its lease can never overwrite the new owner's work.
var ErrLeaseLost = errors.New("lease lost")

// Run is one row of sim_runs.
type Run struct {
	ID              string          `gorm:"column:id;primaryKey" json:"id"`
	SessionID       string          `gorm:"column:session_id" json:"session_id"`
	Kind            string          `gorm:"column:kind" json:"kind"`
	MasterSeed      int64           `gorm:"column:master_seed" json:"-"` // uint64 stored bit-for-bit
	Futures         int             `gorm:"column:futures" json:"futures"`
	ChunkSize       int             `gorm:"column:chunk_size" json:"chunk_size"`
	InputsHash      string          `gorm:"column:inputs_hash" json:"inputs_hash"`
	InputsJSON      json.RawMessage `gorm:"column:inputs_json;type:jsonb" json:"-"`
	ModelVersion    string          `gorm:"column:model_version" json:"model_version"`
	Status          Status          `gorm:"column:status" json:"status"`
	Progress        int             `gorm:"column:progress" json:"progress"`
	Error           *string         `gorm:"column:error" json:"error,omitempty"`
	CancelRequested bool            `gorm:"column:cancel_requested" json:"cancel_requested"`
	WorkerID        *string         `gorm:"column:worker_id" json:"-"`
	LeaseExpiresAt  *time.Time      `gorm:"column:lease_expires_at" json:"-"`
	Attempts        int             `gorm:"column:attempts" json:"attempts"`
	ResultsHash     *string         `gorm:"column:results_hash" json:"results_hash,omitempty"`
	CreatedAt       time.Time       `gorm:"column:created_at" json:"created_at"`
	StartedAt       *time.Time      `gorm:"column:started_at" json:"started_at,omitempty"`
	FinishedAt      *time.Time      `gorm:"column:finished_at" json:"finished_at,omitempty"`
	UpdatedAt       time.Time       `gorm:"column:updated_at" json:"updated_at"`
}

// TableName tells GORM the table name explicitly.
func (Run) TableName() string { return "sim_runs" }

// Seed returns the master seed as the engine's uint64.
func (r Run) Seed() uint64 { return uint64(r.MasterSeed) } // #nosec G115 -- bit-for-bit round trip by design

// SeedToDB stores a uint64 seed in a bigint column, bit for bit.
func SeedToDB(seed uint64) int64 { return int64(seed) } // #nosec G115 -- bit-for-bit round trip by design

// Payload is what inputs_json holds: the engine inputs (with every assumption
// value resolved, so the run is reproducible from this row alone) and an
// opaque context the report builder needs (car labels, warnings, provenance).
type Payload struct {
	Inputs  sim.Inputs      `json:"inputs"`
	Context json.RawMessage `json:"context"`
}

// ResultRow is one row of sim_results.
type ResultRow struct {
	RunID            string          `gorm:"column:run_id;primaryKey" json:"run_id"`
	ShortlistItemID  string          `gorm:"column:shortlist_item_id;primaryKey;type:uuid" json:"shortlist_item_id"`
	PBest            float64         `gorm:"column:p_best" json:"p_best"`
	PCheapest        float64         `gorm:"column:p_cheapest" json:"p_cheapest"`
	NetcostP10       float64         `gorm:"column:netcost_p10" json:"netcost_p10"`
	NetcostP50       float64         `gorm:"column:netcost_p50" json:"netcost_p50"`
	NetcostP90       float64         `gorm:"column:netcost_p90" json:"netcost_p90"`
	OwnershipCostP10 float64         `gorm:"column:ownership_cost_p10" json:"ownership_cost_p10"`
	OwnershipCostP50 float64         `gorm:"column:ownership_cost_p50" json:"ownership_cost_p50"`
	OwnershipCostP90 float64         `gorm:"column:ownership_cost_p90" json:"ownership_cost_p90"`
	BreakdownJSON    json.RawMessage `gorm:"column:breakdown_json;type:jsonb" json:"breakdown"`
	HistogramJSON    json.RawMessage `gorm:"column:histogram_json;type:jsonb" json:"histogram"`
}

// TableName tells GORM the table name explicitly.
func (ResultRow) TableName() string { return "sim_results" }

// ProgressEvent is published on run:{id} and sent to SSE clients.
type ProgressEvent struct {
	RunID    string `json:"run_id"`
	Status   Status `json:"status"`
	Progress int    `json:"progress"`
	Error    string `json:"error,omitempty"`
}

// Channel is the Redis pub/sub channel for a run (SPEC §11).
func Channel(runID string) string { return "run:" + runID }
