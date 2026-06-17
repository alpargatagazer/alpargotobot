// Package database manages SQLite database storage for bot state,
// encrypted credentials, and cached starred items.
package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps the sql.DB connection pool.
type DB struct {
	Conn *sql.DB
}

// NewDB opens a SQLite database, configures WAL mode, enables foreign keys, and initializes schemas.
func NewDB(dbPath string) (*DB, error) {
	// Ensure parent directory exists
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory %s: %w", dbDir, err)
	}

	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %s: %w", dbPath, err)
	}

	// SQLite connection pool limits
	// Since SQLite is file-based and serializes writes, limit to 1 open connection to avoid busy-locking
	conn.SetMaxOpenConns(1)

	// Enable WAL (Write-Ahead Logging) and Foreign Keys
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}
	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	db := &DB{Conn: conn}
	if err := db.initSchema(); err != nil {
		_ = conn.Close()
		return nil, err
	}

	return db, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	if db.Conn != nil {
		return db.Conn.Close()
	}
	return nil
}

func (db *DB) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS credentials (
		username        TEXT PRIMARY KEY,
		encrypted_password BLOB NOT NULL,
		nonce           BLOB NOT NULL,
		updated_at      TEXT NOT NULL,
		telegram_id     INTEGER
	);

	CREATE TABLE IF NOT EXISTS starred_cache (
		username    TEXT NOT NULL,
		item_type   TEXT NOT NULL CHECK(item_type IN ('song', 'album', 'artist')),
		item_id     TEXT NOT NULL,
		item_data   TEXT NOT NULL,
		synced_at   TEXT NOT NULL,
		PRIMARY KEY (username, item_type, item_id),
		FOREIGN KEY (username) REFERENCES credentials(username) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS tickets (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		type        TEXT NOT NULL CHECK(type IN ('issue', 'improvement')),
		title       TEXT NOT NULL,
		description TEXT NOT NULL,
		author_name TEXT NOT NULL,
		created_at  TEXT NOT NULL
	);`

	if _, err := db.Conn.Exec(schema); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	// Migration: Add telegram_id column if it doesn't exist
	// In Go, we can execute the ALTER TABLE statement. If the column already exists, SQLite will return an error
	// containing "duplicate column name" which we can ignore safely.
	_, err := db.Conn.Exec("ALTER TABLE credentials ADD COLUMN telegram_id INTEGER")
	if err != nil {
		errStr := strings.ToLower(err.Error())
		if !strings.Contains(errStr, "duplicate column name") {
			return fmt.Errorf("failed to run migration alter table: %w", err)
		}
	}

	return nil
}
