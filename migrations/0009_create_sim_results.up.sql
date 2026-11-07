-- Per-car results of a finished run (SPEC §5.4). histogram_json is an addition
-- (Plan.md D1) for the results page's net-cost histogram.

CREATE TABLE sim_results (
    run_id                 text NOT NULL REFERENCES sim_runs (id) ON DELETE CASCADE,
    shortlist_item_id      uuid NOT NULL REFERENCES shortlist_items (id) ON DELETE CASCADE,
    p_best                 double precision NOT NULL,
    p_cheapest             double precision NOT NULL,
    netcost_p10            double precision NOT NULL,
    netcost_p50            double precision NOT NULL,
    netcost_p90            double precision NOT NULL,
    ownership_cost_p10     double precision NOT NULL,
    ownership_cost_p50     double precision NOT NULL,
    ownership_cost_p90     double precision NOT NULL,
    breakdown_json         jsonb NOT NULL,
    histogram_json         jsonb NOT NULL,

    PRIMARY KEY (run_id, shortlist_item_id),
    CONSTRAINT sim_results_probabilities CHECK (p_best BETWEEN 0 AND 1 AND p_cheapest BETWEEN 0 AND 1),
    CONSTRAINT sim_results_netcost_order CHECK (netcost_p10 <= netcost_p50 AND netcost_p50 <= netcost_p90),
    CONSTRAINT sim_results_ownership_order CHECK (ownership_cost_p10 <= ownership_cost_p50 AND ownership_cost_p50 <= ownership_cost_p90)
);
