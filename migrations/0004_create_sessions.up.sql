-- Anonymous decision sessions (SPEC §5.2). The id is 128 random bits encoded as
-- 22 base64url characters (DECISIONS.md D-P-17), unguessable like a password.
-- `version` implements optimistic locking: every change bumps it, and a write
-- with a stale If-Match version is rejected with 409.

CREATE TABLE sessions (
    id               text PRIMARY KEY,
    version          integer NOT NULL DEFAULT 1,
    state            text NOT NULL DEFAULT 'SHORTLISTING',
    user_state       char(2) NOT NULL,
    constraints_json jsonb,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT sessions_id_format CHECK (id ~ '^[A-Za-z0-9_-]{22}$'),
    CONSTRAINT sessions_version_positive CHECK (version >= 1),
    CONSTRAINT sessions_state_check CHECK (state IN ('SHORTLISTING', 'USAGE', 'INTERVIEW', 'SIMULATING', 'DONE'))
);
