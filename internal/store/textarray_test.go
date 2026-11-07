package store

import (
	"slices"
	"testing"
)

func TestTextArray_ScanPostgresTextFormat(t *testing.T) {
	tests := []struct {
		name string
		src  any
		want []string
	}{
		{"nil becomes empty", nil, []string{}},
		{"empty array", "{}", []string{}},
		{"simple values", "{rear_camera,esc}", []string{"rear_camera", "esc"}},
		{"quoted value with comma", `{"a,b",c}`, []string{"a,b", "c"}},
		{"bytes input", []byte("{x,y}"), []string{"x", "y"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var a TextArray
			if err := a.Scan(tt.src); err != nil {
				t.Fatalf("Scan(%v): %v", tt.src, err)
			}
			if !slices.Equal([]string(a), tt.want) {
				t.Errorf("Scan(%v) = %q, want %q", tt.src, a, tt.want)
			}
		})
	}
}

func TestTextArray_ValueOfNilIsEmptySlice(t *testing.T) {
	v, err := TextArray(nil).Value()
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := v.([]string); !ok || len(s) != 0 {
		t.Errorf("Value() = %#v, want empty []string", v)
	}
}
