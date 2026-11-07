//go:build integration

package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/xoBerlierxo/TieBreaker/internal/testutil"
)

// Shared by every test in this package; set once in TestMain.
var (
	testDSN      string
	testRedisURL string
)

func TestMain(m *testing.M) {
	os.Exit(runWithContainers(m))
}

// runWithContainers is separate from TestMain so the deferred terminate calls
// run before os.Exit (os.Exit skips deferred functions).
func runWithContainers(m *testing.M) int {
	ctx := context.Background()

	dsn, stopPG, err := testutil.Postgres(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer stopPG()

	redisURL, stopRedis, err := testutil.Redis(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer stopRedis()

	testDSN, testRedisURL = dsn, redisURL
	return m.Run()
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := OpenPostgres(context.Background(), testDSN, quietLogger())
	if err != nil {
		t.Fatalf("OpenPostgres: %v", err)
	}
	t.Cleanup(func() { _ = ClosePostgres(db) })
	return db
}

// createScratchTable makes a table unique to this test, so tests don't interfere.
func createScratchTable(t *testing.T, db *gorm.DB) string {
	t.Helper()
	name := fmt.Sprintf("scratch_%d", time.Now().UnixNano())
	if err := db.Exec("CREATE TABLE " + name + " (id int PRIMARY KEY)").Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() { db.Exec("DROP TABLE IF EXISTS " + name) })
	return name
}

func countRows(t *testing.T, db *gorm.DB, table string) int64 {
	t.Helper()
	var n int64
	if err := db.Table(table).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestOpenPostgres_PingsRealPostgres(t *testing.T) {
	db := openTestDB(t)
	if err := (PostgresPinger{DB: db}).Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	var version string
	if err := db.Raw("SHOW server_version").Scan(&version).Error; err != nil {
		t.Fatalf("server_version: %v", err)
	}
	if len(version) < 2 || version[:2] != "16" {
		t.Errorf("server_version = %q, want 16.x (SPEC §3)", version)
	}
}

func TestOpenPostgres_UnreachableDatabaseFailsFast(t *testing.T) {
	start := time.Now()
	_, err := OpenPostgres(context.Background(),
		"postgres://nobody:nothing@127.0.0.1:1/none?sslmode=disable&connect_timeout=2", quietLogger())
	if err == nil {
		t.Fatal("OpenPostgres to a closed port: want error, got nil")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("took %v to fail, want a fast failure", elapsed)
	}
}

func TestWithTx_CommitsOnNil(t *testing.T) {
	db := openTestDB(t)
	table := createScratchTable(t, db)

	err := WithTx(context.Background(), db, func(tx *gorm.DB) error {
		return tx.Exec("INSERT INTO " + table + " (id) VALUES (1), (2)").Error
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	if n := countRows(t, db, table); n != 2 {
		t.Errorf("rows after commit = %d, want 2", n)
	}
}

func TestWithTx_RollsBackOnError(t *testing.T) {
	db := openTestDB(t)
	table := createScratchTable(t, db)
	errBusinessRule := errors.New("business rule failed")

	err := WithTx(context.Background(), db, func(tx *gorm.DB) error {
		if err := tx.Exec("INSERT INTO " + table + " (id) VALUES (1)").Error; err != nil {
			return err
		}
		return fmt.Errorf("validating: %w", errBusinessRule)
	})

	if !errors.Is(err, errBusinessRule) {
		t.Fatalf("WithTx error = %v, want it to wrap errBusinessRule", err)
	}
	if n := countRows(t, db, table); n != 0 {
		t.Errorf("rows after rollback = %d, want 0", n)
	}
}

func TestWithTx_CancelledContextDoesNotRunFn(t *testing.T) {
	db := openTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	err := WithTx(ctx, db, func(*gorm.DB) error {
		called = true
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("WithTx error = %v, want context.Canceled", err)
	}
	if called {
		t.Error("fn was called with an already-cancelled context")
	}
}

func TestOpenRedis_PingsRealRedis(t *testing.T) {
	client, err := OpenRedis(context.Background(), testRedisURL)
	if err != nil {
		t.Fatalf("OpenRedis: %v", err)
	}
	defer client.Close()

	if err := (RedisPinger{Client: client}).Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if err := client.Set(context.Background(), "it-works", "yes", time.Minute).Err(); err != nil {
		t.Fatalf("SET: %v", err)
	}
}

func TestOpenRedis_InvalidURLReturnsError(t *testing.T) {
	if _, err := OpenRedis(context.Background(), "not-a-url"); err == nil {
		t.Fatal("OpenRedis(not-a-url): want error, got nil")
	}
}
