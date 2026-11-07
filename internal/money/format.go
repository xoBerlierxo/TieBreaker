// Package money formats rupee amounts the Indian way: ₹1,02,340 (lakh/crore
// digit grouping) and "₹1.02 lakh". It is shared by the interview questions
// and the reports, so the same number always looks the same.
package money

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// FormatINR formats a rupee amount with Indian digit grouping, rounded to the
// nearest rupee: 102340 → "₹1,02,340", 12345678 → "₹1,23,45,678".
// The first group from the right has three digits and every group after it two.
func FormatINR(amount float64) string {
	neg := amount < 0
	n := int64(math.Round(math.Abs(amount)))
	s := strconv.FormatInt(n, 10)
	if len(s) > 3 {
		head, tail := s[:len(s)-3], s[len(s)-3:]
		var groups []string
		for len(head) > 2 {
			groups = append([]string{head[len(head)-2:]}, groups...)
			head = head[:len(head)-2]
		}
		if head != "" {
			groups = append([]string{head}, groups...)
		}
		s = strings.Join(groups, ",") + "," + tail
	}
	if neg {
		return "−₹" + s
	}
	return "₹" + s
}

// FormatLakh formats an amount in lakhs or crores for prose:
// 102340 → "₹1.02 lakh", 25000 → "₹25,000", 12500000 → "₹1.25 crore".
// Amounts under ₹1 lakh are written in full.
func FormatLakh(amount float64) string {
	abs := math.Abs(amount)
	sign := ""
	if amount < 0 {
		sign = "−"
	}
	switch {
	case abs >= 1e7:
		return sign + "₹" + trimZeros(fmt.Sprintf("%.2f", abs/1e7)) + " crore"
	case abs >= 1e5:
		return sign + "₹" + trimZeros(fmt.Sprintf("%.2f", abs/1e5)) + " lakh"
	default:
		return FormatINR(amount)
	}
}

// FormatNumber formats a plain number with Indian grouping and no ₹ sign.
func FormatNumber(n float64) string {
	return strings.TrimPrefix(strings.TrimPrefix(FormatINR(n), "−"), "₹")
}

func trimZeros(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}
