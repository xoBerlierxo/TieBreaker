package store

import (
	"database/sql/driver"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

// TextArray maps a Go []string to a Postgres text[] column.
//
// GORM has no built-in array type, and database/sql can't scan an array into a
// plain []string. TextArray implements sql.Scanner (DB → Go) and driver.Valuer
// (Go → DB) by delegating to pgx's own array codec, so we don't hand-write a
// Postgres array parser.
type TextArray []string

// Scan implements sql.Scanner.
func (a *TextArray) Scan(src any) error {
	if src == nil {
		*a = TextArray{}
		return nil
	}
	var out []string
	if err := pgtype.NewMap().SQLScanner(&out).Scan(src); err != nil {
		return fmt.Errorf("scanning text[]: %w", err)
	}
	if out == nil {
		out = []string{}
	}
	*a = out
	return nil
}

// Value implements driver.Valuer. The pgx database/sql driver accepts a
// []string directly and encodes it as text[].
func (a TextArray) Value() (driver.Value, error) {
	if a == nil {
		return []string{}, nil
	}
	return []string(a), nil
}

// GormDataType tells GORM which SQL type to use for this Go type.
func (TextArray) GormDataType() string { return "text[]" }
