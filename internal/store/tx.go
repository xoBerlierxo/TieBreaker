package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// WithTx runs fn inside a database transaction (SPEC §18: every transaction goes
// through this one helper).
//
//   - If fn returns nil, the transaction is committed.
//   - If fn returns an error (or the context is cancelled), it is rolled back and
//     the error is returned unchanged, so callers can still use errors.Is on it.
//
// fn must use the tx handle it is given, not the outer db; otherwise its queries
// run outside the transaction.
func WithTx(ctx context.Context, db *gorm.DB, fn func(tx *gorm.DB) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	return db.WithContext(ctx).Transaction(fn)
}
