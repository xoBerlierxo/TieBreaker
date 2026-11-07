package session

import (
	"fmt"
	"strings"
	"time"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
	"github.com/xoBerlierxo/TieBreaker/internal/catalog"
)

// AddCarRequest is the body of POST /v1/sessions/:id/cars.
type AddCarRequest struct {
	VariantID              string  `json:"variant_id"`
	Condition              string  `json:"condition"`
	AskingPriceINR         *int64  `json:"asking_price_inr,omitempty"`
	Year                   *int    `json:"year,omitempty"`
	OdometerKm             *int    `json:"odometer_km,omitempty"`
	OwnersCount            *int    `json:"owners_count,omitempty"`
	ConditionGrade         *string `json:"condition_grade,omitempty"`
	OnRoadPriceOverrideINR *int64  `json:"on_road_price_override_inr,omitempty"`
}

// Limits on used-car inputs (SPEC §14: explicit range checks).
const (
	maxOdometerKm = 1_000_000
	maxOwners     = 10
	maxPriceINR   = 1_000_000_000 // ₹100 crore: anything above is a typo
)

// BuildItem validates an add-car request against the variant and returns the
// item to store (SPEC §6.1). Used cars need asking price, year and odometer;
// owners default to 1 and condition grade to "fair" (DECISIONS.md).
func BuildItem(req AddCarRequest, v catalog.Variant, now time.Time) (Item, error) {
	if _, ok := v.Efficiency(); !ok {
		return Item{}, apperr.Validation("variant_id", "this variant has no efficiency data, so its running cost can't be simulated")
	}
	item := Item{VariantID: v.ID, Condition: Condition(req.Condition)}

	switch item.Condition {
	case ConditionNew:
		if req.AskingPriceINR != nil || req.Year != nil || req.OdometerKm != nil || req.OwnersCount != nil || req.ConditionGrade != nil {
			return Item{}, apperr.Validation("condition", "a new car has no asking price, year, odometer, owners or condition grade")
		}
		if req.OnRoadPriceOverrideINR != nil {
			if p := *req.OnRoadPriceOverrideINR; p <= 0 || p > maxPriceINR {
				return Item{}, apperr.Validation("on_road_price_override_inr", "on-road price must be a positive amount")
			}
			item.OnRoadPriceOverrideINR = req.OnRoadPriceOverrideINR
		} else if v.ExShowroomPriceINR == nil {
			return Item{}, apperr.Validation("on_road_price_override_inr", "this variant has no ex-showroom price; give an on-road price (excluding insurance)")
		}
		if v.DiscontinuedYear != nil && *v.DiscontinuedYear < now.Year() {
			return Item{}, apperr.Validation("condition", fmt.Sprintf("this variant was discontinued in %d; add it as a used car", *v.DiscontinuedYear))
		}

	case ConditionUsed:
		if req.OnRoadPriceOverrideINR != nil {
			return Item{}, apperr.Validation("on_road_price_override_inr", "an on-road price only applies to new cars")
		}
		if req.AskingPriceINR == nil || req.Year == nil || req.OdometerKm == nil {
			return Item{}, apperr.Validation("asking_price_inr", "a used car needs asking_price_inr, year and odometer_km")
		}
		if p := *req.AskingPriceINR; p <= 0 || p > maxPriceINR {
			return Item{}, apperr.Validation("asking_price_inr", "asking price must be a positive amount")
		}
		last := now.Year()
		if v.DiscontinuedYear != nil {
			last = *v.DiscontinuedYear
		}
		if y := *req.Year; y < v.LaunchYear || y > last {
			return Item{}, apperr.Validation("year", fmt.Sprintf("this variant was made from %d to %d", v.LaunchYear, last))
		}
		if km := *req.OdometerKm; km < 0 || km > maxOdometerKm {
			return Item{}, apperr.Validation("odometer_km", "odometer must be between 0 and 10,00,000 km")
		}
		owners := 1
		if req.OwnersCount != nil {
			owners = *req.OwnersCount
		}
		if owners < 1 || owners > maxOwners {
			return Item{}, apperr.Validation("owners_count", "owners must be between 1 and 10")
		}
		grade := "fair"
		if req.ConditionGrade != nil {
			grade = strings.ToLower(*req.ConditionGrade)
		}
		if grade != "good" && grade != "fair" && grade != "poor" {
			return Item{}, apperr.Validation("condition_grade", "condition grade must be good, fair or poor")
		}
		item.AskingPriceINR, item.Year, item.OdometerKm = req.AskingPriceINR, req.Year, req.OdometerKm
		item.OwnersCount, item.ConditionGrade = &owners, &grade

	default:
		return Item{}, apperr.Validation("condition", `condition must be "new" or "used"`)
	}
	return item, nil
}

// Price is what a car costs to buy, as used by the budget check and the engine.
type Price struct {
	// UpfrontINR: on-road price (new, excluding insurance, DECISIONS.md D-P-11)
	// or asking price + transfer costs (used).
	UpfrontINR float64 `json:"upfront_inr"`
	// BudgetINR is compared with the buyer's budget: on-road (new) or asking (used) (SPEC §6.2).
	BudgetINR float64 `json:"budget_inr"`
	// ValueBasisINR is what insurance and resale are based on: ex-showroom (new) or asking (used).
	ValueBasisINR float64 `json:"value_basis_inr"`
	RoadTaxINR    float64 `json:"road_tax_inr,omitempty"`
	Computed      bool    `json:"computed"` // false when the user gave an on-road override
}

// PriceOf computes a car's price in the session's state.
func PriceOf(item Item, v catalog.Variant, set catalog.AssumptionSet) (Price, error) {
	if item.Condition == ConditionUsed {
		asking := float64(*item.AskingPriceINR)
		transfer := set.UsedTransfer.Rate*asking + set.UsedTransfer.FixedINR
		return Price{UpfrontINR: asking + transfer, BudgetINR: asking, ValueBasisINR: asking, Computed: true}, nil
	}
	if item.OnRoadPriceOverrideINR != nil {
		onRoad := float64(*item.OnRoadPriceOverrideINR)
		basis := onRoad
		if v.ExShowroomPriceINR != nil {
			basis = float64(*v.ExShowroomPriceINR)
		}
		return Price{UpfrontINR: onRoad, BudgetINR: onRoad, ValueBasisINR: basis}, nil
	}
	if v.ExShowroomPriceINR == nil {
		return Price{}, apperr.Validation("on_road_price_override_inr", v.DisplayName()+" has no ex-showroom price")
	}
	ex := float64(*v.ExShowroomPriceINR)
	tax := set.RoadTaxINR(v.FuelType, ex)
	onRoad := ex + tax + set.RegistrationMiscINR
	return Price{UpfrontINR: onRoad, BudgetINR: onRoad, ValueBasisINR: ex, RoadTaxINR: tax, Computed: true}, nil
}

// CarLabel is the name shown for a car: the variant name, plus "(used, 2019)" for used cars.
func CarLabel(item Item, v catalog.Variant) string {
	if item.Condition == ConditionUsed && item.Year != nil {
		return fmt.Sprintf("%s (used, %d)", v.DisplayName(), *item.Year)
	}
	return v.DisplayName()
}
