-- Reports (SPEC §5.4, §9). facts_json is the single source of truth (§9.4);
-- prose_md is rendered from it by the template or, if enabled and validated,
-- by the LLM (prose_source says which). share_id is 128 random bits for the
-- public read-only link /r/:share_id. run_id is an addition (Plan.md D1) so a
-- share link can be reproduced from its run; none_fit reports have no run.

CREATE TABLE reports (
    id           bigserial PRIMARY KEY,
    session_id   text NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    run_id       text REFERENCES sim_runs (id) ON DELETE CASCADE,
    kind         text NOT NULL,
    facts_json   jsonb NOT NULL,
    prose_md     text NOT NULL,
    prose_source text NOT NULL,
    share_id     text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT reports_share_id_unique UNIQUE (share_id),
    CONSTRAINT reports_share_id_format CHECK (share_id ~ '^[A-Za-z0-9_-]{22}$'),
    CONSTRAINT reports_kind_check CHECK (kind IN ('winner', 'deadlock', 'none_fit')),
    CONSTRAINT reports_prose_source_check CHECK (prose_source IN ('template', 'llm')),
    CONSTRAINT reports_run_matches_kind CHECK ((kind = 'none_fit') = (run_id IS NULL))
);

CREATE UNIQUE INDEX reports_one_per_run ON reports (run_id) WHERE run_id IS NOT NULL;
CREATE INDEX reports_session_idx ON reports (session_id, created_at DESC);
