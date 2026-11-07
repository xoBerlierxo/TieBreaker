# model.md — simulation assumptions and formulas

> **Data status: every assumption value is a PLACEHOLDER** (`verified=false`, `source_url="TODO"`). Placeholders are clearly marked, never presented as real data (DECISIONS.md D-P-6). Replace each one with a sourced value in `data/seed/assumptions/*.json`, set `source_url`, `checked_at` and `verified: true`, then re-run `go run ./scripts/seed`. The API, the reports and the UI show a "placeholder assumptions" warning until every key used by a run is verified.

Assumptions live in the `assumptions` table, keyed by `(key, model_version)`. The current model version is **`sim-v1`**. `catalog.BuildAssumptionSet` turns them into a typed, validated `AssumptionSet`. Each simulation run stores a snapshot of that set, so changing a value later never changes an old result (SPEC §5.1).

## 1. State-specific keys (`<ST>` ∈ TN, KA, MH, DL)

| Key | Value shape | Unit | Meaning | Status |
|---|---|---|---|---|
| `fuel_price.petrol.<ST>` | `{"inr_per_litre": n}` | ₹/litre | today's petrol price (also used by strong hybrids) | PLACEHOLDER |
| `fuel_price.diesel.<ST>` | `{"inr_per_litre": n}` | ₹/litre | today's diesel price | PLACEHOLDER |
| `fuel_price.cng.<ST>` | `{"inr_per_kg": n}` | ₹/kg | today's CNG price | PLACEHOLDER |
| `electricity_tariff.<ST>` | `{"inr_per_kwh": n}` | ₹/kWh | home charging tariff | PLACEHOLDER |
| `public_charging_tariff.<ST>` | `{"inr_per_kwh": n}` | ₹/kWh | public charging tariff (used without home charging) | PLACEHOLDER |
| `road_tax.<ST>.rule` | `{fuel: [{"up_to_inr": n\|null, "rate": r}, …]}` | share of ex-showroom | road-tax slabs per fuel type; the first slab covering the price applies | PLACEHOLDER |
| `registration_misc.<ST>` | `{"inr": n}` | ₹ | registration, number plates and other fixed on-road charges (insurance excluded, D-P-11) | PLACEHOLDER |
| `used_transfer.<ST>.rule` | `{"rate": r, "fixed_inr": n}` | share + ₹ | cost of transferring a used car: rate × price + fixed | PLACEHOLDER |

## 2. Common keys

| Key | Value shape | Meaning | Status |
|---|---|---|---|
| `price_trend.<petrol\|diesel\|cng\|electricity>` | `{"mu": μ, "sigma": σ}` | yearly log-normal price path: p(t+1) = p(t)·exp(μ + σZ) | PLACEHOLDER |
| `price_trend.sweep` | `{"mu_range": [lo, hi]}` | plausible range of the fuel-price trend μ, for tipping-point sweeps | PLACEHOLDER |
| `real_efficiency_factor.<fuel>` | `{"city": [lo, hi], "highway": [lo, hi]}` | real ÷ claimed efficiency, sampled uniformly and blended by city share | PLACEHOLDER |
| `ev.charging_efficiency` | `{"value": e}` | share of grid energy that reaches the battery | PLACEHOLDER |
| `ev.battery_wear` | `{"per_year": w}` | extra yearly resale reduction for EVs from battery wear | PLACEHOLDER |
| `usage.km_year_sigma` | `{"value": σ}` | year-to-year log-normal variation of distance driven | PLACEHOLDER |
| `insurance.premium_rate` | `{fuel: r}` | yearly premium as a share of the insured declared value (IDV) | PLACEHOLDER |
| `insurance.idv_schedule` | `{"factors": [f0, f1, …]}` | IDV as a share of the purchase value by car age (last value used for older cars) | PLACEHOLDER |
| `service.cost_per_visit` | `{segment: {fuel: ₹}}` | cost of one scheduled service | PLACEHOLDER |
| `service.min_visits_per_year` | `{"value": n}` | minimum services per year even at low mileage | PLACEHOLDER |
| `general_inflation.rate` | `{"value": i}` | yearly escalation of service, tyre and repair costs | PLACEHOLDER |
| `repairs.model` | see `catalog.RepairModel` | Poisson repair-event rate (base by fuel, rising with age, odometer, condition, used) and log-normal cost per event; share covered inside warranty | PLACEHOLDER |
| `tyres.model` | `{"set_cost_inr": {segment: ₹}, "life_km": n}` | cost of a tyre set and how far it lasts | PLACEHOLDER |
| `resale.curve.<fuel>` | `{"fractions": [age1, age2, …]}` | baseline resale value as a share of purchase price by age | PLACEHOLDER |
| `resale.shock` | `{"market_sigma": σm, "idio_sigma": σi}` | log-normal resale noise: market-wide per fuel (shared by cars) and per car | PLACEHOLDER |

Cost segments come from body type: hatchback → small; sedan and compact SUV → medium; SUV and MUV → large (`catalog.Variant.Segment`).

## 3. Formulas

The full cost model (SPEC §10) and the order in which random values are drawn are documented here in M2, alongside the engine code.
