package activity

import (
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/alpargatagazer/alpargotobot/internal/database"
	"github.com/alpargatagazer/alpargotobot/internal/navidrome"
)

const (
	// StarredCacheTTL is the duration for which starred cache is considered fresh (24 hours).
	StarredCacheTTL = 24 * time.Hour
	// MaxRecentMemorized limits the memory of recently picked users for recommendations.
	MaxRecentMemorized = 3
)

// Engine handles favorites sync, recommendations, and user validation.
type Engine struct {
	db                  *database.DB
	encryptionKey       []byte
	recentPickedUsers   []string
	recentPickedUsersMu sync.Mutex
}

// NewEngine creates a new user activity Engine instance.
func NewEngine(db *database.DB, encryptionKey []byte) *Engine {
	return &Engine{
		db:            db,
		encryptionKey: encryptionKey,
	}
}

// SyncUserStarred fetches a user's starred songs, albums, and artists, caching them in the local database.
func (e *Engine) SyncUserStarred(username, password, baseURL string) (bool, error) {
	client := navidrome.NewClient(baseURL, username, password, "1.16.1", "Music Library")
	starred, err := client.GetStarred()
	if err != nil {
		return false, fmt.Errorf("failed to fetch starred items: %w", err)
	}

	for _, itemType := range []string{"song", "album", "artist"} {
		items := starred[itemType]
		if err := e.db.UpsertStarredItems(username, itemType, items); err != nil {
			return false, fmt.Errorf("failed to cache starred %s items: %w", itemType, err)
		}
	}

	slog.Info("Synced starred items successfully",
		"username", username,
		"songs", len(starred["song"]),
		"albums", len(starred["album"]),
		"artists", len(starred["artist"]),
	)
	return true, nil
}

// EnsureUserSynced verifies if a user's favorites cache is fresh, and performs sync if expired.
func (e *Engine) EnsureUserSynced(username, baseURL string) (bool, error) {
	lastSync, err := e.db.GetStarredSyncTime(username)
	if err != nil {
		slog.Warn("Failed to get starred sync time, proceeding to sync", "username", username, "error", err)
	}

	if lastSync != nil {
		age := time.Since(*lastSync)
		if age < StarredCacheTTL {
			slog.Debug("Starred cache is fresh, skipping sync", "username", username, "age", age)
			return true, nil
		}
	}

	// Retrieve credentials to sync
	credUser, credPass, err := e.db.GetCredential(username, e.encryptionKey)
	if err != nil {
		return false, fmt.Errorf("credentials not found: %w", err)
	}

	return e.SyncUserStarred(credUser, credPass, baseURL)
}

// GetRecommendations retrieves random items from a specific user's favorites.
func (e *Engine) GetRecommendations(fromUsername, itemType string, limit int, baseURL string) ([]map[string]any, error) {
	if _, err := e.EnsureUserSynced(fromUsername, baseURL); err != nil {
		return nil, fmt.Errorf("failed to ensure user synced: %w", err)
	}

	items, err := e.db.GetStarredItems(fromUsername, itemType)
	if err != nil {
		return nil, fmt.Errorf("failed to get starred items: %w", err)
	}

	if len(items) == 0 {
		return []map[string]any{}, nil
	}

	// Shuffle and limit
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(items), func(i, j int) {
		items[i], items[j] = items[j], items[i]
	})

	if len(items) > limit {
		items = items[:limit]
	}

	return items, nil
}

// ValidateAndGetUsers pings the server for all saved credentials.
// If a user gets ErrAuth, they are removed from the database.
func (e *Engine) ValidateAndGetUsers(baseURL string) ([]string, error) {
	usernames, err := e.db.ListUsers()
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}

	var validUsers []string
	for _, username := range usernames {
		credUser, credPass, err := e.db.GetCredential(username, e.encryptionKey)
		if err != nil {
			slog.Warn("Failed to retrieve credentials for validation, skipping", "username", username, "error", err)
			continue
		}

		client := navidrome.NewClient(baseURL, credUser, credPass, "1.16.1", "Music Library")
		errPing := client.Ping()
		if errPing != nil {
			if errors.Is(errPing, navidrome.ErrAuth) {
				slog.Warn("Validation failed with auth error, deleting user from DB", "username", username)
				if errDel := e.db.DeleteUser(username); errDel != nil {
					slog.Error("Failed to delete unauthorized user from DB", "username", username, "error", errDel)
				}
			} else {
				slog.Warn("Validation ping network error, assuming user remains valid", "username", username, "error", errPing)
				validUsers = append(validUsers, username)
			}
		} else {
			validUsers = append(validUsers, username)
		}
	}

	return validUsers, nil
}

// RecommendationResult holds the output of a random user recommendation request.
type RecommendationResult struct {
	Username string
	Items    []map[string]any
}

// GetRandomUserRecommendations selects a random user (excluding caller and recently picked ones) and draws recommendations from their favorites.
func (e *Engine) GetRandomUserRecommendations(itemType string, limit int, baseURL string, excludeUsername string) (*RecommendationResult, error) {
	users, err := e.ValidateAndGetUsers(baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get valid users list: %w", err)
	}
	if len(users) == 0 {
		return nil, errors.New("no valid registered users found")
	}

	e.recentPickedUsersMu.Lock()
	memoLimit := len(users) - 1
	if memoLimit > MaxRecentMemorized {
		memoLimit = MaxRecentMemorized
	}

	// Exclude recently picked users
	var pool []string
	recentMap := make(map[string]bool)
	for i := 0; i < memoLimit && i < len(e.recentPickedUsers); i++ {
		recentMap[e.recentPickedUsers[i]] = true
	}

	for _, u := range users {
		if !recentMap[u] {
			pool = append(pool, u)
		}
	}

	// Exclude the caller if provided
	if excludeUsername != "" {
		var filteredPool []string
		for _, u := range pool {
			if u != excludeUsername {
				filteredPool = append(filteredPool, u)
			}
		}
		pool = filteredPool
	}

	// Fallback to all valid users (minus caller) if pool is empty
	if len(pool) == 0 {
		if excludeUsername != "" {
			for _, u := range users {
				if u != excludeUsername {
					pool = append(pool, u)
				}
			}
		} else {
			pool = users
		}
	}
	e.recentPickedUsersMu.Unlock()

	if len(pool) == 0 {
		return nil, errors.New("no users available for recommendation after exclusions")
	}

	// Pick random user
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	chosen := pool[r.Intn(len(pool))]

	// Update recently picked list
	e.recentPickedUsersMu.Lock()
	var updatedRecent []string
	updatedRecent = append(updatedRecent, chosen)
	for _, u := range e.recentPickedUsers {
		if u != chosen {
			updatedRecent = append(updatedRecent, u)
		}
	}
	if len(updatedRecent) > MaxRecentMemorized {
		updatedRecent = updatedRecent[:MaxRecentMemorized]
	}
	e.recentPickedUsers = updatedRecent
	e.recentPickedUsersMu.Unlock()

	// Get recommendations
	items, err := e.GetRecommendations(chosen, itemType, limit, baseURL)
	if err != nil {
		return nil, err
	}

	// If chosen user has no items of this type, try one more user from pool
	if len(items) == 0 {
		var remaining []string
		for _, u := range pool {
			if u != chosen {
				remaining = append(remaining, u)
			}
		}

		if len(remaining) > 0 {
			chosen = remaining[r.Intn(len(remaining))]
			items, err = e.GetRecommendations(chosen, itemType, limit, baseURL)
			if err != nil {
				return nil, err
			}
		}
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("no starred %s items found in favorite users", itemType)
	}

	return &RecommendationResult{
		Username: chosen,
		Items:    items,
	}, nil
}

// PurgeInactiveUsers removes credentials of users who haven't logged into Navidrome in maxDays.
func (e *Engine) PurgeInactiveUsers(adminClient *navidrome.Client, maxDays int) ([]string, error) {
	usernames, err := e.db.ListUsers()
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	if len(usernames) == 0 {
		return nil, nil
	}

	var purged []string
	cutoff := time.Now().AddDate(0, 0, -maxDays)

	for _, username := range usernames {
		lastLogin, errLastLogin := adminClient.GetUserLastLogin(username)
		if errLastLogin != nil {
			slog.Warn("Could not check native last login, skipping purge check", "username", username, "error", errLastLogin)
			continue
		}

		if lastLogin == nil {
			// User does not exist anymore or has never logged in (and native API returned nothing for them)
			slog.Info("User not found or never logged into Navidrome, purging credentials", "username", username)
			if errDel := e.db.DeleteUser(username); errDel != nil {
				slog.Error("Failed to purge user", "username", username, "error", errDel)
			} else {
				purged = append(purged, username)
			}
			continue
		}

		if lastLogin.Before(cutoff) {
			slog.Info("User has been inactive for too long, purging credentials", "username", username, "last_login", lastLogin)
			if errDel := e.db.DeleteUser(username); errDel != nil {
				slog.Error("Failed to purge user", "username", username, "error", errDel)
			} else {
				purged = append(purged, username)
			}
		}
	}

	return purged, nil
}
