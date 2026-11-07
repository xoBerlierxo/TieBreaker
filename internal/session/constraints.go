package session

import (
	"fmt"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
	"github.com/xoBerlierxo/TieBreaker/internal/catalog"
	"github.com/xoBerlierxo/TieBreaker/internal/money"
)

// Constraints are the buyer's hard requirements (SPEC §6.2). Nil means "no requirement".
type Constraints struct {
	MaxBudgetINR *int64  `json:"max_budget_inr"`
	MinSeats     *int    `json:"min_seats"`
	HomeCharging *bool   `json:"home_charging"` // asked only if an EV is shortlisted
	Transmission *string `json:"transmission"`  // "automatic" or "manual"
}

// Elimination records why a car failed a constraint and by how much.
type Elimination struct {
	Constraint string  `json:"constraint"`
	Required   string  `json:"required"`
	Actual     string  `json:"actual"`
	Shortfall  float64 `json:"shortfall"` // ₹ over budget, seats missing; 0 when not numeric
	Message    string  `json:"message"`
}

// Validate checks the constraint values (SPEC §14 range checks). hasEV says
// whether the home-charging question applies.
func (c Constraints) Validate(hasEV bool) error {
	if c.MaxBudgetINR != nil && (*c.MaxBudgetINR <= 0 || *c.MaxBudgetINR > maxPriceINR) {
		return apperr.Validation("max_budget_inr", "budget must be a positive amount")
	}
	if c.MinSeats != nil && (*c.MinSeats < 1 || *c.MinSeats > 9) {
		return apperr.Validation("min_seats", "minimum seats must be between 1 and 9")
	}
	if c.Transmission != nil && *c.Transmission != "automatic" && *c.Transmission != "manual" {
		return apperr.Validation("transmission", `transmission must be "automatic" or "manual" (or omitted)`)
	}
	if hasEV && c.HomeCharging == nil {
		return apperr.Validation("home_charging", "your shortlist has an EV: say whether you have home charging")
	}
	return nil
}

// CarToCheck is one car with what the constraints need to know.
type CarToCheck struct {
	Item    Item
	Variant catalog.Variant
	Price   Price
}

// Evaluate applies the constraints to every car and returns, for each, the
// list of failures (empty = the car survives). Home charging never eliminates
// a car: it changes the EV's energy cost instead (Plan.md D6).
func (c Constraints) Evaluate(cars []CarToCheck) [][]Elimination {
	out := make([][]Elimination, len(cars))
	for i, car := range cars {
		var fails []Elimination
		if c.MaxBudgetINR != nil {
			budget := float64(*c.MaxBudgetINR)
			if car.Price.BudgetINR > budget {
				what := "on-road price"
				if car.Item.Condition == ConditionUsed {
					what = "asking price"
				}
				fails = append(fails, Elimination{
					Constraint: "max_budget",
					Required:   "at most " + money.FormatINR(budget),
					Actual:     money.FormatINR(car.Price.BudgetINR),
					Shortfall:  car.Price.BudgetINR - budget,
					Message:    fmt.Sprintf("%s is %s over your budget", what, money.FormatINR(car.Price.BudgetINR-budget)),
				})
			}
		}
		if c.MinSeats != nil && car.Variant.Seats < *c.MinSeats {
			fails = append(fails, Elimination{
				Constraint: "min_seats",
				Required:   fmt.Sprintf("at least %d seats", *c.MinSeats),
				Actual:     fmt.Sprintf("%d seats", car.Variant.Seats),
				Shortfall:  float64(*c.MinSeats - car.Variant.Seats),
				Message:    fmt.Sprintf("has %d seats, %d fewer than you need", car.Variant.Seats, *c.MinSeats-car.Variant.Seats),
			})
		}
		if c.Transmission != nil {
			auto := car.Variant.IsAutomatic()
			if (*c.Transmission == "automatic") != auto {
				actual := "manual"
				if auto {
					actual = "automatic (" + string(car.Variant.Transmission) + ")"
				}
				fails = append(fails, Elimination{
					Constraint: "transmission",
					Required:   *c.Transmission,
					Actual:     actual,
					Message:    fmt.Sprintf("has a %s gearbox; you need %s", actual, *c.Transmission),
				})
			}
		}
		out[i] = fails
	}
	return out
}
