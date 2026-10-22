package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SetNowPlayingFilter adds a Navidrome username to the now playing exclusion list.
// Comparisons on username are case-insensitive due to the COLLATE NOCASE column constraint.
func (db *DB) SetNowPlayingFilter(username string, telegramID *int64) error {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" {
		return errors.New("username cannot be empty")
	}

	now := time.Now().UTC().Format(time.RFC3339)

	var sqlTelegramID sql.NullInt64
	if telegramID != nil {
		sqlTelegramID = sql.NullInt64{Int64: *telegramID, Valid: true}
	}

	query := `
		INSERT INTO nowplaying_filters (username, telegram_id, created_at)
		VALUES (?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET
			telegram_id = COALESCE(excluded.telegram_id, nowplaying_filters.telegram_id),
			created_at = excluded.created_at
	`

	_, err := db.Conn.Exec(query, trimmed, sqlTelegramID, now)
	if err != nil {
		return fmt.Errorf("failed to set nowplaying filter for %s: %w", trimmed, err)
	}

	return nil
}

// RemoveNowPlayingFilter removes a Navidrome username from the now playing exclusion list.
func (db *DB) RemoveNowPlayingFilter(username string) error {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" {
		return errors.New("username cannot be empty")
	}

	query := "DELETE FROM nowplaying_filters WHERE username = ? COLLATE NOCASE"
	_, err := db.Conn.Exec(query, trimmed)
	if err != nil {
		return fmt.Errorf("failed to remove nowplaying filter for %s: %w", trimmed, err)
	}

	return nil
}

// IsNowPlayingFiltered checks if a Navidrome username is currently filtered out from now playing.
// The check is case-insensitive.
func (db *DB) IsNowPlayingFiltered(username string) (bool, error) {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" {
		return false, nil
	}

	query := "SELECT 1 FROM nowplaying_filters WHERE username = ? COLLATE NOCASE"
	var dummy int
	err := db.Conn.QueryRow(query, trimmed).Scan(&dummy)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check nowplaying filter for %s: %w", trimmed, err)
	}

	return true, nil
}

// GetNowPlayingFilteredUsers returns a set (as map[string]bool) of all lowercase usernames
// that have opted out of appearing in the now playing output.
func (db *DB) GetNowPlayingFilteredUsers() (map[string]bool, error) {
	query := "SELECT username FROM nowplaying_filters"
	rows, err := db.Conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query nowplaying filters: %w", err)
	}
	defer func() { _ = rows.Close() }()

	filters := make(map[string]bool)
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, fmt.Errorf("failed to scan nowplaying filter username: %w", err)
		}
		filters[strings.ToLower(strings.TrimSpace(u))] = true
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("nowplaying filters row iteration error: %w", err)
	}

	return filters, nil
}
