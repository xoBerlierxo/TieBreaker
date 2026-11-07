package catalog

// Feature is one entry of the feature vocabulary: a key that may appear in
// Variant.KeyFeatures, and the label shown to users.
type Feature struct {
	Key   string
	Label string
}

// KnownFeatures is the vocabulary of "meaningful" features the interview may
// ask about (SPEC §8.1: one question per meaningful feature difference, max 2).
// Keys not in this list can still be stored on a variant, but are never asked
// about. It is a function so no caller can modify a shared slice.
//
// Changing this list changes which questions can be asked; add new keys at the
// end so the order (used to break ties) stays stable.
func KnownFeatures() []Feature {
	return []Feature{
		{"6_airbags", "six airbags"},
		{"esc", "electronic stability control (ESC)"},
		{"rear_camera", "a rear parking camera"},
		{"connected_car", "connected-car features"},
		{"sunroof", "a sunroof"},
		{"ventilated_seats", "ventilated front seats"},
		{"adas", "driver-assistance features (ADAS)"},
		{"auto_climate", "automatic climate control"},
		{"cruise_control", "cruise control"},
		{"360_camera", "a 360° camera"},
	}
}

// FeatureLabel returns the human label for a known feature key, and whether the
// key is in the vocabulary.
func FeatureLabel(key string) (string, bool) {
	for _, f := range KnownFeatures() {
		if f.Key == key {
			return f.Label, true
		}
	}
	return "", false
}
