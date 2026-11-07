-- Idempotency keys (SPEC §5.5). The primary key (session_id, key) is the
-- spec's unique constraint. A replayed request with the same key and the same
-- body gets the stored response; the same key with a different body gets 422.
-- response_json is text, not jsonb: jsonb re-orders keys and spacing, and a
-- replay must return exactly the bytes of the original response.

CREATE TABLE idempotency_keys (
    session_id      text NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    key             text NOT NULL,
    request_hash    text NOT NULL,
    response_status smallint,
    response_json   text,
    created_at      timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (session_id, key),
    CONSTRAINT idempotency_keys_key_length CHECK (length(key) BETWEEN 1 AND 128)
);

-- For periodic cleanup of old keys.
CREATE INDEX idempotency_keys_created_idx ON idempotency_keys (created_at);
