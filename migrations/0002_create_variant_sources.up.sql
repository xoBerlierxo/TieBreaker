-- Where each car data value came from, and when it was checked (SPEC §5.1).
-- A variant can only be marked verified when every non-null field has at least
-- one source; that rule is enforced in internal/catalog (CanVerify).

CREATE TABLE variant_sources (
    id         bigserial PRIMARY KEY,
    variant_id uuid NOT NULL REFERENCES variants (id) ON DELETE CASCADE,
    field_name text NOT NULL,
    source_url text NOT NULL,
    checked_at date NOT NULL,
    note       text,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT variant_sources_url_not_empty CHECK (source_url <> ''),
    CONSTRAINT variant_sources_field_not_empty CHECK (field_name <> '')
);

CREATE INDEX variant_sources_variant_field_idx ON variant_sources (variant_id, field_name);
