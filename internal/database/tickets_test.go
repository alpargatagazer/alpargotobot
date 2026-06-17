package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTickets(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "bot-db-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := NewDB(dbPath)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Initial list should be empty
	tickets, err := db.ListTickets()
	require.NoError(t, err)
	require.Empty(t, tickets)

	// Add an issue
	id1, err := db.AddTicket(TicketTypeIssue, "Bug in login", "Can't login with space in name", "@user1")
	require.NoError(t, err)
	require.Greater(t, id1, int64(0))

	// Add an improvement
	id2, err := db.AddTicket(TicketTypeImprovement, "Add search by year", "Would be nice to search by year", "@user2")
	require.NoError(t, err)
	require.Greater(t, id2, int64(0))

	// List tickets
	tickets, err = db.ListTickets()
	require.NoError(t, err)
	require.Len(t, tickets, 2)

	require.Equal(t, id1, tickets[0].ID)
	require.Equal(t, TicketTypeIssue, tickets[0].Type)
	require.Equal(t, "Bug in login", tickets[0].Title)
	require.Equal(t, "Can't login with space in name", tickets[0].Description)
	require.Equal(t, "@user1", tickets[0].AuthorName)
	require.False(t, tickets[0].CreatedAt.IsZero())

	require.Equal(t, id2, tickets[1].ID)
	require.Equal(t, TicketTypeImprovement, tickets[1].Type)

	// Close a ticket
	removed, err := db.CloseTicket(id1)
	require.NoError(t, err)
	require.True(t, removed)

	// List again, should have 1 left
	tickets, err = db.ListTickets()
	require.NoError(t, err)
	require.Len(t, tickets, 1)
	require.Equal(t, id2, tickets[0].ID)

	// Close non-existent ticket
	removed, err = db.CloseTicket(999)
	require.NoError(t, err)
	require.False(t, removed)
}
