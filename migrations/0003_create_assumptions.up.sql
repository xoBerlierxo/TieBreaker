-- Versioned, sourced model assumptions: fuel prices, tariffs, tax rules,
-- depreciation curves, repair rates (SPEC §5.1, §10). Each simulation run
-- snapshots the assumptions it used, so old results stay reproducible after a
-- value here changes.
--
-- `verified` is an addition to the spec (Plan.md D1): seed values are
-- placeholders until the developer replaces them with sourced values.

CREATE TABLE assumptions (
    id            bigserial PRIMARY KEY,
    key           text NOT NULL,
    value_json    jsonb NOT NULL,
    source_url    text NOT NULL,
    checked_at    date,
    model_version text NOT NULL,
    verified      boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT assumptions_key_version_unique UNIQUE (key, model_version),
    CONSTRAINT assumptions_key_format CHECK (key ~ '^[a-z0-9_]+(\.[A-Za-z0-9_]+)+$'),
    CONSTRAINT assumptions_verified_needs_source CHECK (NOT verified OR (source_url <> 'TODO' AND checked_at IS NOT NULL))
);

-- Prefix lookups such as key LIKE 'fuel_price.%'.
CREATE INDEX assumptions_key_prefix_idx ON assumptions (key text_pattern_ops);
