package database

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBFlow(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "bot-db-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := NewDB(dbPath)
	require.NoError(t, err)
	defer db.Close()

	// 1. Check migrations worked (table exists, no crash)
	assert.NotNil(t, db.Conn)

	// 2. Encryption key
	key := make([]byte, 32)
	_, err = rand.Read(key)
	require.NoError(t, err)

	// 3. Upsert credentials
	telegramID := int64(987654321)
	err = db.UpsertCredential("alice", "alice-password", &telegramID, key)
	require.NoError(t, err)

	// 4. Get by telegram ID
	username, err := db.GetNavidromeUserByTelegramID(telegramID)
	require.NoError(t, err)
	assert.Equal(t, "alice", username)

	// 5. Get credential and decrypt
	gotUsername, gotPassword, err := db.GetCredential("alice", key)
	require.NoError(t, err)
	assert.Equal(t, "alice", gotUsername)
	assert.Equal(t, "alice-password", gotPassword)

	// 6. List users
	users, err := db.ListUsers()
	require.NoError(t, err)
	assert.Equal(t, []string{"alice"}, users)

	// 7. Starred items flow
	starredItems := []map[string]any{
		{"id": "song-1", "title": "Song One", "artist": "Artist A"},
		{"id": "song-2", "title": "Song Two", "artist": "Artist B"},
	}

	err = db.UpsertStarredItems("alice", "song", starredItems)
	require.NoError(t, err)

	// 8. Get sync time
	syncTime, err := db.GetStarredSyncTime("alice")
	require.NoError(t, err)
	require.NotNil(t, syncTime)
	assert.WithinDuration(t, time.Now().UTC(), *syncTime, 5*time.Second)

	// 9. Get starred items
	gotStarred, err := db.GetStarredItems("alice", "song")
	require.NoError(t, err)
	require.Len(t, gotStarred, 2)
	assert.Equal(t, "song-1", gotStarred[0]["id"])
	assert.Equal(t, "Song One", gotStarred[0]["title"])

	// 10. Delete starred cache
	err = db.DeleteStarredItems("alice")
	require.NoError(t, err)
	gotStarredAfterDelete, err := db.GetStarredItems("alice", "song")
	require.NoError(t, err)
	assert.Empty(t, gotStarredAfterDelete)

	// 11. Delete user (and cascade starred cache)
	err = db.DeleteUser("alice")
	require.NoError(t, err)
	usersAfterDelete, err := db.ListUsers()
	require.NoError(t, err)
	assert.Empty(t, usersAfterDelete)
}
