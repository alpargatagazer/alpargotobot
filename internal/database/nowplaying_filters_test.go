package database

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNowPlayingFilters(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_np.db")

	db, err := NewDB(dbPath)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// 1. Initial state: not filtered
	filtered, err := db.IsNowPlayingFiltered("alice")
	require.NoError(t, err)
	require.False(t, filtered)

	filters, err := db.GetNowPlayingFilteredUsers()
	require.NoError(t, err)
	require.Empty(t, filters)

	// 2. Set filter with mixed case
	tgID := int64(123456)
	err = db.SetNowPlayingFilter("Alice", &tgID)
	require.NoError(t, err)

	// Case-insensitive check
	filtered, err = db.IsNowPlayingFiltered("alice")
	require.NoError(t, err)
	require.True(t, filtered)

	filteredUpper, err := db.IsNowPlayingFiltered("ALICE")
	require.NoError(t, err)
	require.True(t, filteredUpper)

	// Check set of users
	filters, err = db.GetNowPlayingFilteredUsers()
	require.NoError(t, err)
	require.True(t, filters["alice"])

	// 3. Add second user
	err = db.SetNowPlayingFilter("bob", nil)
	require.NoError(t, err)

	filters, err = db.GetNowPlayingFilteredUsers()
	require.NoError(t, err)
	require.Len(t, filters, 2)
	require.True(t, filters["alice"])
	require.True(t, filters["bob"])

	// 4. Remove filter (case-insensitive)
	err = db.RemoveNowPlayingFilter("ALICE")
	require.NoError(t, err)

	filtered, err = db.IsNowPlayingFiltered("alice")
	require.NoError(t, err)
	require.False(t, filtered)

	filters, err = db.GetNowPlayingFilteredUsers()
	require.NoError(t, err)
	require.Len(t, filters, 1)
	require.False(t, filters["alice"])
	require.True(t, filters["bob"])
}
