// Package database manages SQLite database storage for bot state,
// encrypted credentials, and cached starred items.
package database

import (
	"fmt"
	"time"
)

// TicketType represents the category of a ticket.
type TicketType string

const (
	TicketTypeIssue       TicketType = "issue"
	TicketTypeImprovement TicketType = "improvement"
)

// Ticket represents a single issue or improvement submitted by a user.
type Ticket struct {
	ID          int64
	Type        TicketType
	Title       string
	Description string
	AuthorName  string
	CreatedAt   time.Time
}

// AddTicket inserts a new ticket into the database.
func (db *DB) AddTicket(ticketType TicketType, title, description, authorName string) (int64, error) {
	result, err := db.Conn.Exec(
		`INSERT INTO tickets (type, title, description, author_name, created_at) VALUES (?, ?, ?, ?, ?)`,
		string(ticketType), title, description, authorName, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert ticket: %w", err)
	}
	return result.LastInsertId()
}

// ListTickets returns all open (not done) tickets ordered by creation date.
func (db *DB) ListTickets() ([]Ticket, error) {
	rows, err := db.Conn.Query(
		`SELECT id, type, title, description, author_name, created_at FROM tickets ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list tickets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tickets []Ticket
	for rows.Next() {
		var t Ticket
		var createdStr string
		var typeStr string
		if err := rows.Scan(&t.ID, &typeStr, &t.Title, &t.Description, &t.AuthorName, &createdStr); err != nil {
			return nil, fmt.Errorf("failed to scan ticket row: %w", err)
		}
		t.Type = TicketType(typeStr)
		t.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		tickets = append(tickets, t)
	}
	return tickets, rows.Err()
}

// CloseTicket removes a ticket from the pool by ID.
// Returns false if no ticket with that ID existed.
func (db *DB) CloseTicket(id int64) (bool, error) {
	result, err := db.Conn.Exec(`DELETE FROM tickets WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("failed to close ticket: %w", err)
	}
	n, _ := result.RowsAffected()
	return n > 0, nil
}
