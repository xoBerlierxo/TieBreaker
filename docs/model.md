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
| `service.default_interval_km` | `{"value": n}` | service interval used when a variant's own interval is unknown (a data warning is shown) | PLACEHOLDER |
| `general_inflation.rate` | `{"value": i}` | yearly escalation of service, tyre and repair costs | PLACEHOLDER |
| `repairs.model` | see `catalog.RepairModel` | Poisson repair-event rate (base by fuel, rising with age, odometer, condition, used) and log-normal cost per event; share covered inside warranty | PLACEHOLDER |
| `tyres.model` | `{"set_cost_inr": {segment: ₹}, "life_km": n}` | cost of a tyre set and how far it lasts | PLACEHOLDER |
| `resale.curve.<fuel>` | `{"fractions": [age1, age2, …]}` | baseline resale value as a share of purchase price by age | PLACEHOLDER |
| `resale.shock` | `{"market_sigma": σm, "idio_sigma": σi}` | log-normal resale noise: market-wide per fuel (shared by cars) and per car | PLACEHOLDER |

Cost segments come from body type: hatchback → small; sedan and compact SUV → medium; SUV and MUV → large (`catalog.Variant.Segment`).

## 3. Cost model (SPEC §10), as implemented in `internal/sim`

All money is in INR, discounted to today. Years are numbered t = 1…T, where T is the ownership horizon (1–15). Costs in year t are paid at the end of the year and discounted by d(t) = 1/(1+r)^t, with r = `DISCOUNT_RATE` (nominal, D-P-9).

```
OwnershipCost = Upfront
              + Σ_t d(t) × (Financing_t + Energy_t + Insurance_t + Service_t + Repairs_t + Tyres_t)
              − d(T) × Resale_T
NetCost       = OwnershipCost − PreferenceValue
```

| Term | Formula | Code |
|---|---|---|
| Upfront | on-road price (new) or asking price + transfer (used), minus the loan principal | `prepare` (`cashUp`) |
| Loan principal | min(loan amount, upfront price) | `prepare` |
| Financing | EMI = P·i·(1+i)^n / ((1+i)^n − 1), i = rate/12; EMIs grouped by year; any balance left at T is paid at T | `finance.go` |
| km driven | km_t = 12 × U(km/month range) × exp(σ_km·Z_t − σ_km²/2) | `buildPaths` |
| Price path | year 1 pays today's price; p(t+1) = p(t)·exp(μ + σZ) for each energy | `buildPaths` |
| Real efficiency | claimed × [c·U(city range) + (1−c)·U(highway range)]; the same U is used for every car of that fuel in a future; c = city share from U(city-share range) | `evaluateCar` |
| Energy (ICE, CNG, hybrid) | km_t / real efficiency × price_t | `evaluateCar` |
| Energy (EV) | km_t / (real km/kWh × charging efficiency) × tariff_t; tariff = home if home charging, otherwise public | `evaluateCar`, `prepare` |
| Insurance | year 1 is skipped when the upfront price is a sourced on-road quote that already includes first-year insurance; otherwise rate × IDV_t; IDV_t = basis × IDV(age_t) / (IDV(age at purchase) if used, else 1) | `evaluateCar` |
| Service | max(km_t / interval, minimum visits) × cost per visit × (1+i)^(t−1) | `evaluateCar` |
| Tyres | km_t / tyre life × set cost × (1+i)^(t−1) | `evaluateCar` |
| Repairs | N ~ Poisson(λ_t), λ_t = base × (1 + a·age_t) × (1 + b·odo_t/100k) × grade × (used mult. if used); each event costs exp(μ_r + σ_r·Z) × (1+i)^(t−1); inside warranty (age_t < years and odo_t < km), cost × (1 − coverage) | `evaluateCar` |
| Resale | basis × curve(age at purchase + T) / curve(age at purchase) × market shock(fuel) × own shock × (1 − wear)^T for EVs; clamped to [0, basis]; shocks are exp(σZ − σ²/2), mean 1 | `evaluateCar` |
| Preference value | Σ_a share_a(car) × V_a, where V_a is drawn by choosing a bracket uniformly, then a value uniformly inside it | `prefValues` |

Here age_t = age at purchase + t − 1 (age at the start of year t), and odo_t = odometer at purchase plus the km driven before year t. "Basis" is the ex-showroom price (new) or the asking price (used).

## 4. Randomness and determinism (SPEC §10.1)

```
chunk i   : rand.New(rand.NewPCG(masterSeed, uint64(i)))      futures [i·C, (i+1)·C)
future k  : futureSeed = chunkRNG.Uint64()
world     : PCG(futureSeed, 0x9E3779B97F4A7C15)
car c     : PCG(futureSeed, FNV64a("car:" + shortlist item ID))
pref a    : PCG(futureSeed, FNV64a("pref:" + attribute key))
```

The **world stream** always draws the same 87 values per future, in this order, whatever the inputs:

1. For each year 1…15: one N(0,1) per energy (petrol, diesel, CNG, electricity), then one N(0,1) for km variation (15 × 5 = 75 values).
2. U for km/month, then U for city share (2 values).
3. One U per fuel for the efficiency factor (5 values).
4. One N(0,1) per fuel for the resale market shock (5 values).

The **car stream** draws first the car's own resale shock (one N(0,1)), then the repair events. The **preference stream** always draws exactly two U per attribute.

As a result:

- **Shared futures:** every car sees the same prices, usage and market shocks within a future.
- **Common random numbers:** changing the horizon, adding or removing a car, or adding a preference never shifts any other random value. This is what makes the interview's bracket comparisons and the tipping-point sweeps stable (`TestCarStream_UnaffectedByAddingAnotherCar`, `TestPrefStream_AddingAttributeDoesNotShiftOthers`).
- **Worker-independent:** future k always writes sample slot k, so the result is bit-identical for 1, 4 and 16 workers (`TestRun_IdenticalAcrossWorkerCounts_1_4_16`).
- **Regression tripwire:** `TestRun_GoldenHash` fingerprints the output. A model change must bump `sim.ModelVersion`.

**Known limit:** bit-exactness is guaranteed on one CPU architecture. The Go spec allows fused multiply-add, which can change the last bits of a result between amd64 and arm64.
