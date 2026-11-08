-- Sourced on-road prices per state (an addition to SPEC §5.1, DECISIONS
-- D-DATA-2). When a session's state has a sourced on-road price for a new car,
-- it is used instead of the computed estimate (ex-showroom + placeholder road
-- tax). includes_insurance records whether the quoted on-road price already
-- contains first-year insurance, so the simulation doesn't charge it twice.

CREATE TABLE variant_prices (
    id                 bigserial PRIMARY KEY,
    variant_id         uuid NOT NULL REFERENCES variants (id) ON DELETE CASCADE,
    state              char(2) NOT NULL,
    city               text,
    on_road_price_inr  bigint NOT NULL,
    includes_insurance boolean NOT NULL DEFAULT true,
    source_url         text NOT NULL,
    checked_at         date NOT NULL,
    note               text,
    created_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT variant_prices_one_per_state UNIQUE (variant_id, state),
    CONSTRAINT variant_prices_positive CHECK (on_road_price_inr > 0),
    CONSTRAINT variant_prices_source_not_empty CHECK (source_url <> '')
);
