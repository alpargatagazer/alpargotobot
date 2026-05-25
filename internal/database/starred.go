package database

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// UpsertStarredItems deletes existing cached items of the given type for the user,
// and inserts the new list of items within a single database transaction.
func (db *DB) UpsertStarredItems(username, itemType string, items []map[string]any) error {
	if itemType != "song" && itemType != "album" && itemType != "artist" {
		return fmt.Errorf("invalid item_type: %s", itemType)
	}

	tx, err := db.Conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// 1. Delete existing items of this type for this user
	deleteQuery := "DELETE FROM starred_cache WHERE username = ? AND item_type = ?"
	if _, err := tx.Exec(deleteQuery, username, itemType); err != nil {
		return fmt.Errorf("failed to delete old starred cache: %w", err)
	}

	// 2. Insert new items
	insertQuery := `
		INSERT INTO starred_cache (username, item_type, item_id, item_data, synced_at)
		VALUES (?, ?, ?, ?, ?)
	`
	now := time.Now().UTC().Format(time.RFC3339)

	stmt, err := tx.Prepare(insertQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, item := range items {
		itemIDVal, ok := item["id"]
		if !ok {
			continue
		}
		itemID, ok := itemIDVal.(string)
		if !ok || itemID == "" {
			continue
		}

		itemData, err := json.Marshal(item)
		if err != nil {
			return fmt.Errorf("failed to marshal item data to JSON: %w", err)
		}

		if _, err := stmt.Exec(username, itemType, itemID, string(itemData), now); err != nil {
			return fmt.Errorf("failed to insert starred item: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetStarredItems retrieves the cached starred items of the given type for a user.
func (db *DB) GetStarredItems(username, itemType string) ([]map[string]any, error) {
	if itemType != "song" && itemType != "album" && itemType != "artist" {
		return nil, fmt.Errorf("invalid item_type: %s", itemType)
	}

	query := "SELECT item_data FROM starred_cache WHERE username = ? AND item_type = ?"
	rows, err := db.Conn.Query(query, username, itemType)
	if err != nil {
		return nil, fmt.Errorf("failed to query starred items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []map[string]any
	for rows.Next() {
		var itemDataStr string
		if err := rows.Scan(&itemDataStr); err != nil {
			return nil, fmt.Errorf("failed to scan item data: %w", err)
		}

		var item map[string]any
		if err := json.Unmarshal([]byte(itemDataStr), &item); err != nil {
			return nil, fmt.Errorf("failed to unmarshal item data: %w", err)
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return items, nil
}

// GetStarredSyncTime returns the most recent synchronization timestamp for a user's starred cache.
func (db *DB) GetStarredSyncTime(username string) (*time.Time, error) {
	query := "SELECT MAX(synced_at) FROM starred_cache WHERE username = ?"
	var syncedAt sql.NullString
	err := db.Conn.QueryRow(query, username).Scan(&syncedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query max synced_at: %w", err)
	}

	if !syncedAt.Valid || syncedAt.String == "" {
		return nil, nil
	}

	t, err := time.Parse(time.RFC3339, syncedAt.String)
	if err != nil {
		// Fallback to try parsing common SQLite date/time formats if needed
		return nil, fmt.Errorf("failed to parse synced_at timestamp %s: %w", syncedAt.String, err)
	}

	return &t, nil
}

// DeleteStarredItems clears all cached starred items for a user.
func (db *DB) DeleteStarredItems(username string) error {
	query := "DELETE FROM starred_cache WHERE username = ?"
	_, err := db.Conn.Exec(query, username)
	if err != nil {
		return fmt.Errorf("failed to clear starred cache for %s: %w", username, err)
	}
	return nil
}
