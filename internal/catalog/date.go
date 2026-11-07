package catalog

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

const dateLayout = "2006-01-02"

// Date is a calendar date with no time of day, such as a source's "checked on"
// date. In JSON it is written "2026-11-07", which is easier to type in seed
// files than a full timestamp. In Postgres it maps to a `date` column.
type Date struct{ time.Time }

// NewDate builds a Date in UTC.
func NewDate(year int, month time.Month, day int) Date {
	return Date{time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

// String formats the date as YYYY-MM-DD.
func (d Date) String() string { return d.Format(dateLayout) }

// MarshalJSON writes "YYYY-MM-DD".
func (d Date) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

// UnmarshalJSON reads "YYYY-MM-DD".
func (d *Date) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("date must be a string: %w", err)
	}
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return fmt.Errorf("date must be YYYY-MM-DD, got %q", s)
	}
	d.Time = t
	return nil
}

// Scan implements sql.Scanner for a Postgres date column.
func (d *Date) Scan(src any) error {
	switch v := src.(type) {
	case time.Time:
		d.Time = time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.UTC)
		return nil
	case string:
		return d.UnmarshalJSON([]byte(`"` + v + `"`))
	case nil:
		d.Time = time.Time{}
		return nil
	default:
		return fmt.Errorf("cannot scan %T into Date", src)
	}
}

// Value implements driver.Valuer.
func (d Date) Value() (driver.Value, error) { return d.String(), nil }
