-- Cars on a session's shortlist (SPEC §5.1 ShortlistItem). Used-car fields are
-- required exactly when condition = 'used'. Eliminations from the hard
-- constraints are recorded on the item with the reason and the shortfall.

CREATE TABLE shortlist_items (
    id                         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id                 text NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    variant_id                 uuid NOT NULL REFERENCES variants (id) ON DELETE RESTRICT,
    position                   smallint NOT NULL,
    condition                  text NOT NULL,
    asking_price_inr           bigint,
    year                       smallint,
    odometer_km                integer,
    owners_count               smallint,
    condition_grade            text,
    on_road_price_override_inr bigint,
    eliminated                 boolean NOT NULL DEFAULT false,
    elimination_json           jsonb,
    created_at                 timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT shortlist_items_unique_car UNIQUE (session_id, variant_id, condition),
    CONSTRAINT shortlist_items_unique_position UNIQUE (session_id, position),
    CONSTRAINT shortlist_items_condition_check CHECK (condition IN ('new', 'used')),
    CONSTRAINT shortlist_items_used_fields CHECK (
        (condition = 'used') = (asking_price_inr IS NOT NULL AND year IS NOT NULL AND odometer_km IS NOT NULL)
    ),
    CONSTRAINT shortlist_items_new_fields CHECK (
        condition = 'used' OR (owners_count IS NULL AND condition_grade IS NULL)
    ),
    CONSTRAINT shortlist_items_override_new_only CHECK (condition = 'new' OR on_road_price_override_inr IS NULL),
    CONSTRAINT shortlist_items_grade_check CHECK (condition_grade IN ('good', 'fair', 'poor')),
    CONSTRAINT shortlist_items_positive CHECK (
        (asking_price_inr IS NULL OR asking_price_inr > 0) AND
        (on_road_price_override_inr IS NULL OR on_road_price_override_inr > 0) AND
        (odometer_km IS NULL OR odometer_km >= 0) AND
        (owners_count IS NULL OR owners_count >= 1) AND
        position >= 0
    )
);

CREATE INDEX shortlist_items_session_idx ON shortlist_items (session_id);
