package catalog

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
)

// OnRoadPrice is a sourced on-road price for a new car in one state. It
// replaces the computed estimate (ex-showroom + road tax + registration) when
// a session is in that state (DECISIONS.md D-DATA-2).
type OnRoadPrice struct {
	ID                int64   `gorm:"column:id;primaryKey" json:"-"`
	VariantID         string  `gorm:"column:variant_id;type:uuid" json:"-"`
	State             string  `gorm:"column:state" json:"state"`
	City              *string `gorm:"column:city" json:"city,omitempty"`
	OnRoadPriceINR    int64   `gorm:"column:on_road_price_inr" json:"on_road_price_inr"`
	IncludesInsurance bool    `gorm:"column:includes_insurance" json:"includes_insurance"`
	SourceURL         string  `gorm:"column:source_url" json:"source_url"`
	CheckedAt         Date    `gorm:"column:checked_at;type:date" json:"checked_at"`
	Note              *string `gorm:"column:note" json:"note,omitempty"`
}

// TableName tells GORM the table name explicitly.
func (OnRoadPrice) TableName() string { return "variant_prices" }

// OnRoadPriceFor returns the sourced on-road price for state, if any.
func (v Variant) OnRoadPriceFor(state string) (OnRoadPrice, bool) {
	for _, p := range v.OnRoadPrices {
		if p.State == state {
			return p, true
		}
	}
	return OnRoadPrice{}, false
}

func validateOnRoadPrices(prices []OnRoadPrice) error {
	seen := map[string]bool{}
	for _, p := range prices {
		switch {
		case !IsSupportedState(p.State):
			return apperr.Validation("on_road_prices", fmt.Sprintf("state %q is not supported", p.State))
		case seen[p.State]:
			return apperr.Validation("on_road_prices", fmt.Sprintf("two on-road prices for %s", p.State))
		case p.OnRoadPriceINR <= 0:
			return apperr.Validation("on_road_prices", "on-road price must be positive")
		case p.SourceURL == "" || p.CheckedAt.IsZero():
			return apperr.Validation("on_road_prices", "every on-road price needs source_url and checked_at")
		}
		seen[p.State] = true
	}
	return nil
}

// replaceOnRoadPrices stores v's on-road prices, replacing any old ones.
func replaceOnRoadPrices(tx *gorm.DB, variantID string, prices []OnRoadPrice) error {
	if err := tx.Where("variant_id = ?", variantID).Delete(&OnRoadPrice{}).Error; err != nil {
		return fmt.Errorf("deleting old on-road prices: %w", err)
	}
	if len(prices) == 0 {
		return nil
	}
	for i := range prices {
		prices[i].ID = 0
		prices[i].VariantID = variantID
	}
	if err := tx.Create(&prices).Error; err != nil {
		return fmt.Errorf("inserting on-road prices: %w", err)
	}
	return nil
}
