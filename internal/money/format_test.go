package money

import "testing"

func TestFormatINR_IndianGrouping(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "₹0"},
		{999, "₹999"},
		{1000, "₹1,000"},
		{99_999, "₹99,999"},
		{100_000, "₹1,00,000"},
		{102_340, "₹1,02,340"},
		{1_234_567, "₹12,34,567"},
		{12_345_678, "₹1,23,45,678"},
		{102_339.6, "₹1,02,340"},
		{-25_000, "−₹25,000"},
	}
	for _, tt := range tests {
		if got := FormatINR(tt.in); got != tt.want {
			t.Errorf("FormatINR(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatLakh(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{25_000, "₹25,000"},
		{100_000, "₹1 lakh"},
		{102_340, "₹1.02 lakh"},
		{250_000, "₹2.5 lakh"},
		{12_500_000, "₹1.25 crore"},
		{-150_000, "−₹1.5 lakh"},
	}
	for _, tt := range tests {
		if got := FormatLakh(tt.in); got != tt.want {
			t.Errorf("FormatLakh(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatNumber(t *testing.T) {
	if got := FormatNumber(1_100); got != "1,100" {
		t.Errorf("FormatNumber(1100) = %q", got)
	}
}
