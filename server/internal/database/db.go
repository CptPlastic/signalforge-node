// Package database provides PostgreSQL persistence for scanner calls.
package database

import (
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// DB wraps the SQL connection.
type DB struct {
	db               *sql.DB
	autoApproveUsers bool
}

// SetAutoApproveUsers controls whether new email sign-ups are created as active
// instead of pending approval (useful for closed/off-grid cells).
func (d *DB) SetAutoApproveUsers(enabled bool) {
	d.autoApproveUsers = enabled
}

// Open opens a PostgreSQL database connection and runs migrations.
func Open(dsn string) (*DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(10)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	d := &DB{db: db}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.db.Close()
}
