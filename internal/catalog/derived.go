package catalog

// Segment groups body types for cost assumptions such as service and tyre
// costs, which differ between small and large cars.
type Segment string

const (
	SegmentSmall  Segment = "small"
	SegmentMedium Segment = "medium"
	SegmentLarge  Segment = "large"
)

// Segment maps the body type to a cost segment.
func (v Variant) Segment() Segment {
	switch v.BodyType {
	case BodyHatchback:
		return SegmentSmall
	case BodySedan, BodyCompactSUV:
		return SegmentMedium
	default:
		return SegmentLarge
	}
}

// IsAutomatic reports whether the car has any kind of automatic gearbox.
// AMT, CVT, DCT, torque converter and single-speed (EV) all count
// (DECISIONS.md D-P-*: "automatic" = anything except manual).
func (v Variant) IsAutomatic() bool {
	return v.Transmission != TransManual
}

// PowerToWeight returns power in PS per tonne of kerb weight, the measure the
// interview uses for "performance" (SPEC §8.1). ok is false if either value is
// missing.
func (v Variant) PowerToWeight() (psPerTonne float64, ok bool) {
	if v.PowerPS == nil || v.KerbWeightKg == nil || *v.KerbWeightKg <= 0 {
		return 0, false
	}
	return *v.PowerPS / (float64(*v.KerbWeightKg) / 1000), true
}

// Efficiency returns the claimed efficiency in the fuel's unit (km/l, km/kg or
// km/kWh). For an EV without an explicit figure, it is derived as claimed range
// divided by battery capacity. ok is false if it can't be determined.
func (v Variant) Efficiency() (value float64, ok bool) {
	if v.ClaimedEfficiency != nil {
		return *v.ClaimedEfficiency, true
	}
	if v.FuelType == FuelEV && v.ClaimedRangeKm != nil && v.BatteryKWh != nil && *v.BatteryKWh > 0 {
		return float64(*v.ClaimedRangeKm) / *v.BatteryKWh, true
	}
	return 0, false
}

// HasFeature reports whether the variant lists the given feature key.
func (v Variant) HasFeature(key string) bool {
	for _, f := range v.KeyFeatures {
		if f == key {
			return true
		}
	}
	return false
}
