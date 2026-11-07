-- Interview answers (SPEC §5.3). One row per question; a trade-off ladder step
-- is its own question ("tradeoff:<attribute>:<step>"). The unique constraint is
-- a second guard against double submissions, besides optimistic locking.

CREATE TABLE answers (
    id          bigserial PRIMARY KEY,
    session_id  text NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    question_id text NOT NULL,
    answer_json jsonb NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT answers_unique_question UNIQUE (session_id, question_id),
    CONSTRAINT answers_question_format CHECK (question_id ~ '^(usage|tradeoff):[a-z0-9_:]+$')
);

CREATE INDEX answers_session_idx ON answers (session_id, id);
