// Package navidrome provides a client to interface with the Navidrome Subsonic API,
// supporting sync, search, authentication, and metadata enrichment.
package navidrome

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// ErrAuth is returned when the Subsonic API returns authentication error (code 40).
	ErrAuth = errors.New("navidrome authentication failed (code 40)")
)

// Client handles communication with the Navidrome Subsonic and Native APIs.
type Client struct {
	baseURL         string
	username        string
	password        string
	clientName      string
	apiVersion      string
	musicFolderName string
	musicFolderID   string
	httpClient      *http.Client
	mu              sync.RWMutex
}

// NewClient creates a new Navidrome client using configuration parameters.
func NewClient(baseURL, username, password, apiVersion, musicFolderName string) *Client {
	return &Client{
		baseURL:         strings.TrimSuffix(baseURL, "/"),
		username:        username,
		password:        password,
		clientName:      "telegram-bot",
		apiVersion:      apiVersion,
		musicFolderName: musicFolderName,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetHTTPClient allows overriding the default http.Client (useful for testing).
func (c *Client) SetHTTPClient(client *http.Client) {
	c.httpClient = client
}

// request performs a GET request to the Navidrome Subsonic API and returns the subsonic-response object.
func (c *Client) request(endpoint string, params url.Values) (json.RawMessage, error) {
	if params == nil {
		params = url.Values{}
	}

	authParams := BuildAuthParams(c.username, c.password, c.clientName, c.apiVersion)
	for k, vals := range authParams {
		for _, v := range vals {
			params.Add(k, v)
		}
	}

	if c.baseURL == "" {
		return nil, errors.New("navidrome URL not configured")
	}

	reqURL := fmt.Sprintf("%s/rest/%s?%s", c.baseURL, endpoint, params.Encode())

	resp, err := c.httpClient.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("http request error: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http error: status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var root struct {
		Response json.RawMessage `json:"subsonic-response"`
	}
	if err := json.Unmarshal(bodyBytes, &root); err != nil {
		return nil, fmt.Errorf("failed to unmarshal subsonic-response: %w", err)
	}

	var statusCheck struct {
		Status string `json:"status"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(root.Response, &statusCheck); err == nil && statusCheck.Status == "failed" && statusCheck.Error != nil {
		if statusCheck.Error.Code == 40 {
			return nil, ErrAuth
		}
		return nil, fmt.Errorf("navidrome API error: %s (code %d)", statusCheck.Error.Message, statusCheck.Error.Code)
	}

	return root.Response, nil
}

// Ping pings the server to verify credentials.
func (c *Client) Ping() error {
	_, err := c.request("ping", nil)
	return err
}

// GetMusicFolderID retrieves and caches the ID of the music folder configured in musicFolderName.
func (c *Client) GetMusicFolderID() (string, error) {
	c.mu.RLock()
	if c.musicFolderID != "" {
		c.mu.RUnlock()
		return c.musicFolderID, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double check
	if c.musicFolderID != "" {
		return c.musicFolderID, nil
	}

	raw, err := c.request("getMusicFolders", nil)
	if err != nil {
		return "", err
	}

	var foldersResp struct {
		MusicFolders struct {
			MusicFolder []struct {
				ID   json.RawMessage `json:"id"` // ID can be string or number in Subsonic
				Name string          `json:"name"`
			} `json:"musicFolder"`
		} `json:"musicFolders"`
	}

	if err := json.Unmarshal(raw, &foldersResp); err != nil {
		return "", fmt.Errorf("failed to parse music folders: %w", err)
	}

	for _, folder := range foldersResp.MusicFolders.MusicFolder {
		if folder.Name == c.musicFolderName {
			// strip quotes if it marshaled as string
			idStr := string(folder.ID)
			idStr = strings.Trim(idStr, "\"")
			c.musicFolderID = idStr
			return c.musicFolderID, nil
		}
	}

	return "", fmt.Errorf("music folder '%s' not found", c.musicFolderName)
}

// CheckScanStatus gets the current library scan status.
type ScanStatus struct {
	Scanning bool   `json:"scanning"`
	Count    int64  `json:"count"`
	LastScan string `json:"lastScan"`
}

func (c *Client) CheckScanStatus() (*ScanStatus, error) {
	raw, err := c.request("getScanStatus", nil)
	if err != nil {
		return nil, err
	}

	var statusResp struct {
		ScanStatus ScanStatus `json:"scanStatus"`
	}
	if err := json.Unmarshal(raw, &statusResp); err != nil {
		return nil, fmt.Errorf("failed to parse scan status: %w", err)
	}

	return &statusResp.ScanStatus, nil
}

// FetchAlbumDetails retrieves enriched metadata for an album and calculates its total size in bytes.
// It returns the album struct without songs to save memory.
func (c *Client) FetchAlbumDetails(albumID string) (*Album, error) {
	params := url.Values{}
	params.Set("id", albumID)

	raw, err := c.request("getAlbum", params)
	if err != nil {
		return nil, err
	}

	var albumResp struct {
		Album struct {
			Album
			Song []Song `json:"song"`
		} `json:"album"`
	}

	if err := json.Unmarshal(raw, &albumResp); err != nil {
		return nil, fmt.Errorf("failed to parse album details: %w", err)
	}

	alb := albumResp.Album.Album
	var totalSize int64
	for _, s := range albumResp.Album.Song {
		totalSize += s.Size
	}
	alb.TotalSizeBytes = totalSize

	return &alb, nil
}

// GetArtist fetches detailed information for a single artist.
func (c *Client) GetArtist(artistID string) (*Artist, error) {
	params := url.Values{}
	params.Set("id", artistID)

	raw, err := c.request("getArtist", params)
	if err != nil {
		return nil, err
	}

	var artistResp struct {
		Artist Artist `json:"artist"`
	}
	if err := json.Unmarshal(raw, &artistResp); err != nil {
		return nil, fmt.Errorf("failed to parse artist details: %w", err)
	}

	return &artistResp.Artist, nil
}

// GetArtistGenres retrieves and sorts unique genres for an artist by inspecting their albums.
func (c *Client) GetArtistGenres(artistID string) ([]string, error) {
	artist, err := c.GetArtist(artistID)
	if err != nil {
		return nil, err
	}

	genreMap := make(map[string]bool)
	for _, album := range artist.Album {
		if album.Genre != "" {
			genreMap[album.Genre] = true
		}
	}

	var genres []string
	for g := range genreMap {
		genres = append(genres, g)
	}
	sort.Strings(genres)
	return genres, nil
}

// GetRandomAlbum retrieves a single random album.
func (c *Client) GetRandomAlbum() (*Album, error) {
	params := url.Values{}
	params.Set("type", "random")
	params.Set("size", "1")

	if folderID, err := c.GetMusicFolderID(); err == nil && folderID != "" {
		params.Set("musicFolderId", folderID)
	}

	raw, err := c.request("getAlbumList2", params)
	if err != nil {
		return nil, err
	}

	var listResp struct {
		AlbumList2 struct {
			Album []Album `json:"album"`
		} `json:"albumList2"`
	}

	if err := json.Unmarshal(raw, &listResp); err != nil {
		return nil, fmt.Errorf("failed to parse random album response: %w", err)
	}

	if len(listResp.AlbumList2.Album) == 0 {
		return nil, errors.New("no random album returned by server")
	}

	return &listResp.AlbumList2.Album[0], nil
}

// GetNowPlaying retrieves currently playing tracks.
func (c *Client) GetNowPlaying() ([]map[string]any, error) {
	raw, err := c.request("getNowPlaying", nil)
	if err != nil {
		return nil, err
	}

	var npResp struct {
		NowPlaying struct {
			Entry []map[string]any `json:"entry"`
		} `json:"nowPlaying"`
	}

	if err := json.Unmarshal(raw, &npResp); err != nil {
		return nil, fmt.Errorf("failed to parse now playing: %w", err)
	}

	return npResp.NowPlaying.Entry, nil
}

// GetGenres retrieves all genres from the Navidrome server.
func (c *Client) GetGenres() ([]Genre, error) {
	raw, err := c.request("getGenres", nil)
	if err != nil {
		return nil, err
	}

	var genresResp struct {
		Genres struct {
			Genre []Genre `json:"genre"`
		} `json:"genres"`
	}

	if err := json.Unmarshal(raw, &genresResp); err != nil {
		return nil, fmt.Errorf("failed to parse genres: %w", err)
	}

	return genresResp.Genres.Genre, nil
}

// GetAlbumsByGenre retrieves albums matching a specific genre.
func (c *Client) GetAlbumsByGenre(genre string) ([]Album, error) {
	params := url.Values{}
	params.Set("type", "byGenre")
	if genre != "None" {
		params.Set("genre", genre)
	} else {
		params.Set("genre", "")
	}
	params.Set("size", "500") // large size for randomization in caller

	if folderID, err := c.GetMusicFolderID(); err == nil && folderID != "" {
		params.Set("musicFolderId", folderID)
	}

	raw, err := c.request("getAlbumList2", params)
	if err != nil {
		return nil, err
	}

	var listResp struct {
		AlbumList2 struct {
			Album []Album `json:"album"`
		} `json:"albumList2"`
	}

	if err := json.Unmarshal(raw, &listResp); err != nil {
		return nil, fmt.Errorf("failed to parse byGenre albums: %w", err)
	}

	return listResp.AlbumList2.Album, nil
}

// GetStarred retrieves starred items (songs, albums, artists) for the user.
func (c *Client) GetStarred() (map[string][]map[string]any, error) {
	raw, err := c.request("getStarred2", nil)
	if err != nil {
		return nil, err
	}

	var starredResp struct {
		Starred2 struct {
			Song   []map[string]any `json:"song"`
			Album  []map[string]any `json:"album"`
			Artist []map[string]any `json:"artist"`
		} `json:"starred2"`
	}

	if err := json.Unmarshal(raw, &starredResp); err != nil {
		return nil, fmt.Errorf("failed to parse starred items: %w", err)
	}

	res := map[string][]map[string]any{
		"song":   starredResp.Starred2.Song,
		"album":  starredResp.Starred2.Album,
		"artist": starredResp.Starred2.Artist,
	}
	return res, nil
}

// GetCoverArtURL returns an authenticated URL to retrieve cover art.
func (c *Client) GetCoverArtURL(coverArtID string) string {
	if coverArtID == "" {
		return ""
	}

	authParams := BuildAuthParams(c.username, c.password, c.clientName, c.apiVersion)
	authParams.Set("id", coverArtID)

	return fmt.Sprintf("%s/rest/getCoverArt?%s", c.baseURL, authParams.Encode())
}

// GetCoverArtBytes downloads the cover art image binary.
func (c *Client) GetCoverArtBytes(coverArtID string) ([]byte, error) {
	if coverArtID == "" {
		return nil, errors.New("empty cover art ID")
	}

	reqURL := c.GetCoverArtURL(coverArtID)

	resp, err := c.httpClient.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download cover art: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http error: status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// NativeUser represents a user response structure from the Navidrome Native API.
type NativeUser struct {
	UserName    string `json:"userName"`
	LastLoginAt string `json:"lastLoginAt"`
}

// GetUserLastLogin retrieves a user's last login date from the Native API using HTTP Basic Authentication.
func (c *Client) GetUserLastLogin(username string) (*time.Time, error) {
	if c.baseURL == "" {
		return nil, errors.New("navidrome URL not configured")
	}

	reqURL := fmt.Sprintf("%s/api/user?_end=100&_order=ASC&_sort=userName&_start=0", c.baseURL)

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	// Native API uses HTTP Basic Auth
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http native API request error: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http native API error: status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read native API response body: %w", err)
	}

	var users []NativeUser
	if err := json.Unmarshal(bodyBytes, &users); err != nil {
		return nil, fmt.Errorf("failed to parse native users list: %w", err)
	}

	for _, u := range users {
		if u.UserName == username {
			if u.LastLoginAt == "" {
				return nil, nil
			}
			t, err := time.Parse(time.RFC3339, u.LastLoginAt)
			if err != nil {
				return nil, fmt.Errorf("failed to parse lastLoginAt timestamp %s: %w", u.LastLoginAt, err)
			}
			return &t, nil
		}
	}

	return nil, nil // user not found
}
