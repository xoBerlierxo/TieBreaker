// Package sim is the pure Monte Carlo ownership-cost engine (SPEC §10).
//
// "Pure" means: no database, no HTTP, no globals, no clock. Plain structs go in,
// plain structs come out, and the same Inputs + Options always give exactly the
// same Result, whatever the number of workers. golangci-lint's depguard rule
// forbids importing the I/O packages from here.
//
// Inputs are fully resolved before they reach the engine: the caller (the
// session package) has already turned catalogue data and assumptions into
// per-car numbers. The engine never looks anything up.
package sim

// ModelVersion identifies the cost model. Bump it whenever a change to the
// engine would change results for the same inputs and seed; TestRun_GoldenHash
// fails until you do.
const ModelVersion = "sim-v1"

// MaxHorizonYears is the longest ownership horizon. The engine always draws
// random values for this many years, even for shorter horizons, so changing
// the horizon never shifts any other random draw (common random numbers).
const MaxHorizonYears = 15

// Fuel is the car's fuel type.
type Fuel string

const (
	FuelPetrol       Fuel = "petrol"
	FuelDiesel       Fuel = "diesel"
	FuelCNG          Fuel = "cng"
	FuelEV           Fuel = "ev"
	FuelStrongHybrid Fuel = "strong_hybrid"
)

// numFuels is the number of fuel types; fuel-indexed arrays have this length.
const numFuels = 5

// index maps a fuel to a stable array index (-1 if unknown).
func (f Fuel) index() int {
	switch f {
	case FuelPetrol:
		return 0
	case FuelDiesel:
		return 1
	case FuelCNG:
		return 2
	case FuelEV:
		return 3
	case FuelStrongHybrid:
		return 4
	}
	return -1
}

// Energy is what the car buys to move: a price path exists per energy.
type Energy int

const (
	EnergyPetrol Energy = iota
	EnergyDiesel
	EnergyCNG
	EnergyElectricity
	numEnergies
)

// energy maps a fuel to the energy whose price it pays. Strong hybrids burn petrol.
func (f Fuel) energy() Energy {
	switch f {
	case FuelDiesel:
		return EnergyDiesel
	case FuelCNG:
		return EnergyCNG
	case FuelEV:
		return EnergyElectricity
	default:
		return EnergyPetrol
	}
}

// Range is a closed interval [Lo, Hi].
type Range struct {
	Lo float64 `json:"lo"`
	Hi float64 `json:"hi"`
}

// at maps u ∈ [0,1) linearly into the range.
func (r Range) at(u float64) float64 { return r.Lo + u*(r.Hi-r.Lo) }

// PriceTrend parameterises a yearly log-normal price path:
// p(t+1) = p(t) · exp(Mu + Sigma·Z), Z ~ N(0,1).
type PriceTrend struct {
	Mu    float64 `json:"mu"`
	Sigma float64 `json:"sigma"`
}

// EfficiencyFactor gives the ranges of real ÷ claimed efficiency.
type EfficiencyFactor struct {
	City    Range `json:"city"`
	Highway Range `json:"highway"`
}

// Inputs is everything one simulation needs. It is stored as JSON in each run,
// so a run can be reproduced exactly from its row (SPEC §6.7).
type Inputs struct {
	Cars         []Car           `json:"cars"`
	Usage        Usage           `json:"usage"`
	World        World           `json:"world"`
	Prefs        []PrefAttribute `json:"prefs"`
	HorizonYears int             `json:"horizon_years"`
	DiscountRate float64         `json:"discount_rate"`
}

// Car holds one shortlisted car's resolved, car-specific parameters.
type Car struct {
	// ID is the stable shortlist-item ID. It keys the car's own random stream,
	// so adding or removing another car never changes this car's randomness.
	ID   string `json:"id"`
	Name string `json:"name"` // display only; never used in calculations
	Fuel Fuel   `json:"fuel"`

	// UpfrontINR is the full purchase cost at t=0: on-road price for a new car,
	// asking price + transfer costs for a used one (SPEC §10).
	UpfrontINR float64 `json:"upfront_inr"`
	// ValueBasisINR is the value that insurance (IDV) and resale are based on:
	// ex-showroom price (new) or asking price (used). Resale is clamped to it.
	ValueBasisINR float64 `json:"value_basis_inr"`

	Used       bool    `json:"used"`
	AgeYears   int     `json:"age_years"`   // car age at purchase (0 for new)
	OdometerKm float64 `json:"odometer_km"` // at purchase

	ClaimedEfficiency float64          `json:"claimed_efficiency"` // km per litre / kg / kWh
	EfficiencyFactor  EfficiencyFactor `json:"efficiency_factor"`

	ServiceIntervalKm   float64 `json:"service_interval_km"`
	ServiceCostPerVisit float64 `json:"service_cost_per_visit_inr"`
	TyreSetCostINR      float64 `json:"tyre_set_cost_inr"`
	InsuranceRate       float64 `json:"insurance_rate"` // yearly premium ÷ IDV

	RepairBaseRate        float64 `json:"repair_base_rate"`        // events per year for a new car
	RepairGradeMultiplier float64 `json:"repair_grade_multiplier"` // condition grade (1 for new)
	WarrantyYears         float64 `json:"warranty_years"`          // from first registration
	WarrantyKm            float64 `json:"warranty_km"`

	// ResaleCurve[k] is the resale value as a share of the new price at age k+1.
	ResaleCurve []float64 `json:"resale_curve"`
}

// Usage describes how the buyer will use the car (SPEC §6.3).
type Usage struct {
	KmPerMonth   Range `json:"km_per_month"`
	CityShare    Range `json:"city_share"` // share of km driven in the city, 0–1
	HomeCharging bool  `json:"home_charging"`
	Loan         *Loan `json:"loan"` // nil means cash
}

// Loan is the buyer's financing. The principal is capped at each car's
// upfront price.
type Loan struct {
	PrincipalINR float64 `json:"principal_inr"`
	AnnualRate   float64 `json:"annual_rate"` // e.g. 0.09 for 9 %
	TenureMonths int     `json:"tenure_months"`
}

// World holds the parameters shared by every car (SPEC §10, assumptions).
type World struct {
	PetrolPriceINR  float64 `json:"petrol_price_inr"`
	DieselPriceINR  float64 `json:"diesel_price_inr"`
	CNGPriceINR     float64 `json:"cng_price_inr"`
	HomeTariffINR   float64 `json:"home_tariff_inr"`
	PublicTariffINR float64 `json:"public_tariff_inr"`

	PetrolTrend      PriceTrend `json:"petrol_trend"`
	DieselTrend      PriceTrend `json:"diesel_trend"`
	CNGTrend         PriceTrend `json:"cng_trend"`
	ElectricityTrend PriceTrend `json:"electricity_trend"`

	KmYearSigma        float64 `json:"km_year_sigma"`
	ChargingEfficiency float64 `json:"charging_efficiency"`

	IDVSchedule      []float64 `json:"idv_schedule"` // IDV ÷ new value by age (last value repeats)
	ServiceMinVisits float64   `json:"service_min_visits"`
	GeneralInflation float64   `json:"general_inflation"`

	RepairAgeSlope        float64 `json:"repair_age_slope"`
	RepairOdoSlopePer100k float64 `json:"repair_odo_slope_per_100k"`
	RepairUsedMultiplier  float64 `json:"repair_used_multiplier"`
	RepairCostMu          float64 `json:"repair_cost_mu"`
	RepairCostSigma       float64 `json:"repair_cost_sigma"`
	WarrantyCoverage      float64 `json:"warranty_coverage"`

	TyreLifeKm float64 `json:"tyre_life_km"`

	ResaleMarketSigma    float64 `json:"resale_market_sigma"`
	ResaleIdioSigma      float64 `json:"resale_idio_sigma"`
	EVBatteryWearPerYear float64 `json:"ev_battery_wear_per_year"`
}

// PrefAttribute is one priced preference (SPEC §8.2). The buyer's ₹ value for
// the difference between the best and worst car on this attribute is sampled
// each future; each car receives Shares[i] of it (1 = best, 0 = worst).
//
// Brackets holds one ₹ range for an answered question. Several brackets mean an
// equal-weight mixture: pick a bracket uniformly, then a value uniformly within
// it (used by the interview to price a not-yet-asked question).
type PrefAttribute struct {
	Key      string    `json:"key"`
	Shares   []float64 `json:"shares"`
	Brackets []Range   `json:"brackets"`
}

// Options controls a run, but never changes what is being simulated.
type Options struct {
	Futures    int
	ChunkSize  int
	Workers    int // 0 means runtime.GOMAXPROCS(0)
	MasterSeed uint64
	// Progress, if not nil, is called after each chunk with the number of
	// completed chunks and the total. Calls are serialised and monotonic.
	Progress func(doneChunks, totalChunks int)
}

// Category is one bucket of the cost breakdown (SPEC §5.4 breakdown_json).
type Category int

const (
	CatUpfront Category = iota
	CatFinancing
	CatEnergy
	CatInsurance
	CatService
	CatRepairs
	CatTyres
	CatResale // negative: money back at the end
	NumCategories
)

// String names the category for JSON and reports.
func (c Category) String() string {
	return [...]string{"upfront", "financing", "energy", "insurance", "service", "repairs", "tyres", "resale"}[c]
}

// Result holds every sample, per car, indexed by future: sample k of every
// series belongs to the same simulated future.
type Result struct {
	Futures int
	Cars    []CarResult // same order as Inputs.Cars
}

// CarResult holds one car's samples. All money is discounted to today (₹).
type CarResult struct {
	ID            string
	NetCost       []float64 // OwnershipCost − PrefValue
	OwnershipCost []float64
	PrefValue     []float64
	Categories    [NumCategories][]float64 // they sum to OwnershipCost
}
