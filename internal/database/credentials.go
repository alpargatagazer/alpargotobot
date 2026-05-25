package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/alpargatagazer/alpargotobot/internal/crypto"
)

// Credential represents a user credential record in the database.
type Credential struct {
	Username          string
	EncryptedPassword []byte
	Nonce             []byte
	UpdatedAt         string
	TelegramID        sql.NullInt64
}

// UpsertCredential inserts or updates a user's Navidrome credentials.
func (db *DB) UpsertCredential(username, password string, telegramID *int64, key []byte) error {
	ciphertext, nonce, err := crypto.Encrypt(key, password)
	if err != nil {
		return fmt.Errorf("failed to encrypt password: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	var sqlTelegramID sql.NullInt64
	if telegramID != nil {
		sqlTelegramID = sql.NullInt64{Int64: *telegramID, Valid: true}
	}

	query := `
		INSERT INTO credentials (username, encrypted_password, nonce, updated_at, telegram_id)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET
			encrypted_password = excluded.encrypted_password,
			nonce = excluded.nonce,
			updated_at = excluded.updated_at,
			telegram_id = COALESCE(excluded.telegram_id, credentials.telegram_id)
	`

	_, err = db.Conn.Exec(query, username, ciphertext, nonce, now, sqlTelegramID)
	if err != nil {
		return fmt.Errorf("failed to upsert credential for %s: %w", username, err)
	}

	return nil
}

// GetNavidromeUserByTelegramID retrieves the Navidrome username associated with a Telegram user ID.
func (db *DB) GetNavidromeUserByTelegramID(telegramID int64) (string, error) {
	query := "SELECT username FROM credentials WHERE telegram_id = ?"
	var username string
	err := db.Conn.QueryRow(query, telegramID).Scan(&username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("failed to get navidrome user by telegram ID: %w", err)
	}
	return username, nil
}

// GetCredential retrieves and decrypts the password for a user.
func (db *DB) GetCredential(username string, key []byte) (string, string, error) {
	query := "SELECT username, encrypted_password, nonce FROM credentials WHERE username = ?"
	var record struct {
		username          string
		encryptedPassword []byte
		nonce             []byte
	}

	err := db.Conn.QueryRow(query, username).Scan(&record.username, &record.encryptedPassword, &record.nonce)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", fmt.Errorf("credential not found for user %s: %w", username, err)
		}
		return "", "", fmt.Errorf("failed to query credential for %s: %w", username, err)
	}

	decrypted, err := crypto.Decrypt(key, record.encryptedPassword, record.nonce)
	if err != nil {
		return "", "", fmt.Errorf("failed to decrypt password for %s: %w", username, err)
	}

	return record.username, decrypted, nil
}

// ListUsers lists all usernames that have stored credentials.
func (db *DB) ListUsers() ([]string, error) {
	query := "SELECT username FROM credentials ORDER BY username"
	rows, err := db.Conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query usernames: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var usernames []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, fmt.Errorf("failed to scan username row: %w", err)
		}
		usernames = append(usernames, u)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return usernames, nil
}

// DeleteUser deletes a user's credentials and all their cached starred data.
// Due to ON DELETE CASCADE, starred_cache rows are automatically removed.
func (db *DB) DeleteUser(username string) error {
	query := "DELETE FROM credentials WHERE username = ?"
	_, err := db.Conn.Exec(query, username)
	if err != nil {
		return fmt.Errorf("failed to delete user %s: %w", username, err)
	}
	return nil
}
