// Package db provides a lazily-initialised SQLite connection singleton,
// configured from the DATABASE_URL environment variable.
//
// The main liz-whiteboard app moved from Prisma/Postgres to raw SQLite
// (data/app.db); this package reads that same file directly. The exported
// *DB wrapper keeps pgx-style method names (Query/QueryRow/Exec taking a
// context) so the data-access call sites stay unchanged.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo), registers "sqlite"
)

// DB wraps *sql.DB and exposes context-first Query/QueryRow/Exec methods that
// mirror the pgx pool API used throughout the data layer.
type DB struct{ sdb *sql.DB }

// Query runs a query that returns rows.
func (d *DB) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.sdb.QueryContext(ctx, query, args...)
}

// QueryRow runs a query expected to return at most one row.
func (d *DB) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return d.sdb.QueryRowContext(ctx, query, args...)
}

// Exec runs a query that returns no rows.
func (d *DB) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.sdb.ExecContext(ctx, query, args...)
}

var (
	instance *DB
	mu       sync.Mutex
)

// Connect opens the SQLite database referenced by DATABASE_URL. It is safe to
// call multiple times; the connection is created once.
func Connect(ctx context.Context) (*DB, error) {
	mu.Lock()
	defer mu.Unlock()

	if instance != nil {
		return instance, nil
	}

	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set")
	}

	sdb, err := sql.Open("sqlite", dsn(raw))
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}
	if err := sdb.PingContext(ctx); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	instance = &DB{sdb: sdb}
	return instance, nil
}

// Pool returns the initialised connection. It panics if Connect has not been
// called successfully first — this is a programmer error.
func Pool() *DB {
	mu.Lock()
	defer mu.Unlock()
	if instance == nil {
		panic("db.Pool() called before db.Connect()")
	}
	return instance
}

// Close shuts down the connection.
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if instance != nil {
		instance.sdb.Close()
		instance = nil
	}
}

// dsn normalises a DATABASE_URL into a modernc SQLite DSN. It accepts the SQLite
// file URL forms used by the main app (`file:./data/app.db`, `file:/abs/path`)
// as well as a bare path, and appends pragmas for safe concurrent access against
// the live (WAL-mode) database.
func dsn(raw string) string {
	path := strings.TrimPrefix(raw, "file://")
	path = strings.TrimPrefix(path, "file:")

	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	// busy_timeout: wait out the app's brief write locks instead of erroring.
	// foreign_keys: honour ON DELETE CASCADE for the lazy expired-session delete.
	return path + sep + "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
}
