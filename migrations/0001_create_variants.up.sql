-- Car variants: one specific version of a model (SPEC §5.1).
-- Nullable columns are optional data: e.g. a car may have no safety rating, and
-- that must be shown as "not rated", never treated as zero (SPEC §7).

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE variants (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug                  text NOT NULL,
    make                  text NOT NULL,
    model                 text NOT NULL,
    variant_name          text NOT NULL,
    body_type             text NOT NULL,
    fuel_type             text NOT NULL,
    ex_showroom_price_inr bigint,
    launch_year           smallint NOT NULL,
    discontinued_year     smallint,
    claimed_efficiency    double precision,
    efficiency_unit       text,
    battery_kwh           double precision,
    claimed_range_km      integer,
    safety_rating_stars   smallint,
    safety_rating_body    text,
    airbags               smallint,
    boot_litres           integer,
    ground_clearance_mm   integer,
    power_ps              double precision,
    kerb_weight_kg        integer,
    transmission          text NOT NULL,
    seats                 smallint NOT NULL,
    service_interval_km   integer,
    warranty_years        smallint,
    warranty_km           integer,
    key_features          text[] NOT NULL DEFAULT '{}',
    verified              boolean NOT NULL DEFAULT false,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT variants_slug_unique UNIQUE (slug),
    CONSTRAINT variants_slug_format CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    CONSTRAINT variants_body_type_check CHECK (body_type IN ('hatchback', 'sedan', 'compact_suv', 'suv', 'muv')),
    CONSTRAINT variants_fuel_type_check CHECK (fuel_type IN ('petrol', 'diesel', 'cng', 'ev', 'strong_hybrid')),
    CONSTRAINT variants_transmission_check CHECK (transmission IN ('manual', 'amt', 'cvt', 'dct', 'torque_converter', 'single_speed')),
    CONSTRAINT variants_efficiency_unit_check CHECK (efficiency_unit IN ('km/l', 'km/kg', 'km/kWh')),
    CONSTRAINT variants_price_positive CHECK (ex_showroom_price_inr > 0),
    CONSTRAINT variants_years_check CHECK (launch_year BETWEEN 1950 AND 2100),
    CONSTRAINT variants_discontinued_after_launch CHECK (discontinued_year >= launch_year),
    CONSTRAINT variants_stars_range CHECK (safety_rating_stars BETWEEN 0 AND 5),
    CONSTRAINT variants_rating_body_with_stars CHECK ((safety_rating_stars IS NULL) = (safety_rating_body IS NULL)),
    CONSTRAINT variants_seats_range CHECK (seats BETWEEN 1 AND 9),
    CONSTRAINT variants_positive_physical CHECK (
        (claimed_efficiency IS NULL OR claimed_efficiency > 0) AND
        (battery_kwh IS NULL OR battery_kwh > 0) AND
        (claimed_range_km IS NULL OR claimed_range_km > 0) AND
        (airbags IS NULL OR airbags >= 0) AND
        (boot_litres IS NULL OR boot_litres >= 0) AND
        (ground_clearance_mm IS NULL OR ground_clearance_mm > 0) AND
        (power_ps IS NULL OR power_ps > 0) AND
        (kerb_weight_kg IS NULL OR kerb_weight_kg > 0) AND
        (service_interval_km IS NULL OR service_interval_km > 0) AND
        (warranty_years IS NULL OR warranty_years >= 0) AND
        (warranty_km IS NULL OR warranty_km >= 0)
    ),
    -- Efficiency value and unit come together.
    CONSTRAINT variants_efficiency_pair CHECK ((claimed_efficiency IS NULL) = (efficiency_unit IS NULL)),
    -- EVs need a battery and use km/kWh; non-EVs have no battery or range.
    CONSTRAINT variants_ev_battery CHECK (fuel_type <> 'ev' OR battery_kwh IS NOT NULL),
    CONSTRAINT variants_ev_unit CHECK (fuel_type <> 'ev' OR efficiency_unit IS NULL OR efficiency_unit = 'km/kWh'),
    CONSTRAINT variants_non_ev_no_battery CHECK (fuel_type = 'ev' OR (battery_kwh IS NULL AND claimed_range_km IS NULL)),
    CONSTRAINT variants_ice_unit CHECK (
        efficiency_unit IS NULL OR
        (fuel_type IN ('petrol', 'diesel', 'strong_hybrid') AND efficiency_unit = 'km/l') OR
        (fuel_type = 'cng' AND efficiency_unit = 'km/kg') OR
        (fuel_type = 'ev' AND efficiency_unit = 'km/kWh')
    )
);

-- Catalogue search matches a free-text query against make + model + variant.
-- A trigram GIN index makes ILIKE '%query%' fast.
CREATE INDEX variants_search_trgm ON variants
    USING gin ((make || ' ' || model || ' ' || variant_name) gin_trgm_ops);

CREATE INDEX variants_fuel_type_idx ON variants (fuel_type);
