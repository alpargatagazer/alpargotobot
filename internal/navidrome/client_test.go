package navidrome

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientPing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/ping", r.URL.Path)
		assert.Equal(t, "json", r.URL.Query().Get("f"))
		assert.Equal(t, "my-user", r.URL.Query().Get("u"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok","version":"1.16.1"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "my-user", "my-password", "1.16.1", "Music Library")
	err := client.Ping()
	assert.NoError(t, err)
}

func TestClientPingFailedAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":40,"message":"Wrong user or password"}}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "my-user", "wrong-password", "1.16.1", "Music Library")
	err := client.Ping()
	assert.ErrorIs(t, err, ErrAuth)
}

func TestClientGetMusicFolderID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/getMusicFolders", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok","version":"1.16.1","musicFolders":{"musicFolder":[{"id":"folder-id-123","name":"Music Library"}]}}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "my-user", "my-password", "1.16.1", "Music Library")
	folderID, err := client.GetMusicFolderID()
	assert.NoError(t, err)
	assert.Equal(t, "folder-id-123", folderID)
}

func TestLibrarySync(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sync-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	cacheFile := filepath.Join(tempDir, "albums_cache.json")
	scanMetaFile := filepath.Join(tempDir, "scan_status.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/rest/getMusicFolders" {
			_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok","version":"1.16.1","musicFolders":{"musicFolder":[{"id":123,"name":"Music Library"}]}}}`))
			return
		}

		if r.URL.Path == "/rest/getScanStatus" {
			_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok","version":"1.16.1","scanStatus":{"scanning":false,"count":1,"lastScan":"2026-05-21T12:00:00Z"}}}`))
			return
		}

		if r.URL.Path == "/rest/getAlbumList" {
			offset := r.URL.Query().Get("offset")
			if offset != "" && offset != "0" {
				_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok","version":"1.16.1","albumList":{}}}`))
			} else {
				_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok","version":"1.16.1","albumList":{"album":[{"id":"alb-1","name":"Album One","artist":"Artist One","year":2024,"songCount":1}]}}}`))
			}
			return
		}

		if r.URL.Path == "/rest/getAlbum" {
			assert.Equal(t, "alb-1", r.URL.Query().Get("id"))
			_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok","version":"1.16.1","album":{"id":"alb-1","name":"Album One","artist":"Artist One","year":2024,"song":[{"id":"song-1","title":"Song 1","size":1000}]}}}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, "my-user", "my-password", "1.16.1", "Music Library")

	// Perform sync
	albums, err := LibrarySync(client, cacheFile, scanMetaFile, true)
	require.NoError(t, err)
	require.Len(t, albums, 1)
	assert.Equal(t, "Album One", albums[0].Name)
	assert.Equal(t, int64(1000), albums[0].TotalSizeBytes)

	// Verify cache file was written
	assert.FileExists(t, cacheFile)
	cacheBytes, err := os.ReadFile(cacheFile)
	require.NoError(t, err)

	var cached []Album
	err = json.Unmarshal(cacheBytes, &cached)
	require.NoError(t, err)
	require.Len(t, cached, 1)
	assert.Equal(t, "Album One", cached[0].Name)
}
