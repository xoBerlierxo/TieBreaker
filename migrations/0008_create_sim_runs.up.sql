-- Simulation runs (SPEC §5.4) double as the job queue (SPEC §11). Workers claim
-- a row with SELECT … FOR UPDATE SKIP LOCKED, hold a lease that their heartbeat
-- extends, and may only write to a run they still own (worker_id fencing).
--
-- Columns beyond the spec (Plan.md D1): chunk_size, inputs_json (the full
-- resolved inputs, including the assumption snapshot, so the run can be
-- reproduced from this row alone), cancel_requested, worker_id,
-- lease_expires_at, attempts, results_hash, started_at, finished_at.

CREATE TABLE sim_runs (
    id               text PRIMARY KEY,
    session_id       text NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    kind             text NOT NULL,
    master_seed      bigint NOT NULL,   -- the uint64 seed stored bit-for-bit as int64
    futures          integer NOT NULL,
    chunk_size       integer NOT NULL,
    inputs_hash      text NOT NULL,
    inputs_json      jsonb NOT NULL,
    model_version    text NOT NULL,
    status           text NOT NULL DEFAULT 'queued',
    progress         smallint NOT NULL DEFAULT 0,
    error            text,
    cancel_requested boolean NOT NULL DEFAULT false,
    worker_id        text,
    lease_expires_at timestamptz,
    attempts         smallint NOT NULL DEFAULT 0,
    results_hash     text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    started_at       timestamptz,
    finished_at      timestamptz,
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT sim_runs_kind_check CHECK (kind IN ('quick', 'full')),
    CONSTRAINT sim_runs_status_check CHECK (status IN ('queued', 'running', 'done', 'failed', 'cancelled')),
    CONSTRAINT sim_runs_progress_range CHECK (progress BETWEEN 0 AND 100),
    CONSTRAINT sim_runs_positive CHECK (futures > 0 AND chunk_size > 0 AND attempts >= 0),
    CONSTRAINT sim_runs_running_has_lease CHECK (status <> 'running' OR (worker_id IS NOT NULL AND lease_expires_at IS NOT NULL))
);

-- Workers look for the oldest queued run…
CREATE INDEX sim_runs_queued_idx ON sim_runs (created_at) WHERE status = 'queued';
-- …or a running run whose lease has expired (its worker crashed).
CREATE INDEX sim_runs_running_lease_idx ON sim_runs (lease_expires_at) WHERE status = 'running';
-- At most one active full run per session (blocks double submits at the database level).
CREATE UNIQUE INDEX sim_runs_one_active_per_session ON sim_runs (session_id)
    WHERE kind = 'full' AND status IN ('queued', 'running');
CREATE INDEX sim_runs_session_idx ON sim_runs (session_id, created_at);
