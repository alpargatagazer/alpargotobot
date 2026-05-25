package navidrome

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/sync/errgroup"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// LibrarySync performs library synchronization, updating the local album cache file.
// It leverages scan status cache optimizations, incremental diffs, and concurrent enrichment.
func LibrarySync(client *Client, cacheFilePath, scanMetaPath string, force bool) ([]Album, error) {
	// 0. Check Scan Status before doing anything heavy
	if !force {
		status, err := client.CheckScanStatus()
		if err == nil && status != nil && !status.Scanning {
			count := status.Count
			lastScan := status.LastScan

			// Try to read saved status
			var saved struct {
				Count    int64  `json:"count"`
				LastScan string `json:"lastScan"`
			}
			savedBytes, errRead := os.ReadFile(scanMetaPath)
			if errRead == nil {
				_ = json.Unmarshal(savedBytes, &saved)
			}

			if saved.Count == count && saved.LastScan == lastScan {
				// Check if cache file exists
				cacheBytes, errCache := os.ReadFile(cacheFilePath)
				if errCache == nil {
					var cachedAlbums []Album
					if errUnmarshal := json.Unmarshal(cacheBytes, &cachedAlbums); errUnmarshal == nil {
						// Migration check: verify first few albums have size metadata
						needsMigration := false
						checkCount := 20
						if len(cachedAlbums) < checkCount {
							checkCount = len(cachedAlbums)
						}
						for i := 0; i < checkCount; i++ {
							if cachedAlbums[i].TotalSizeBytes == 0 && cachedAlbums[i].SongCount > 0 {
								needsMigration = true
								break
							}
						}

						if !needsMigration {
							slog.Info("Scan status unchanged and cache exists. Skipping full sync.")
							return cachedAlbums, nil
						}
						slog.Info("Cache is missing size metadata. Triggering enrichment sync.")
					}
				}
			}

			// Save current status for next time if we're about to sync
			statusBytes, _ := json.Marshal(saved)
			if len(statusBytes) > 0 {
				_ = os.WriteFile(scanMetaPath, statusBytes, 0644)
			}
		} else if err != nil {
			slog.Warn("Optimization check failed", "error", err)
		}
	}

	// 1. Load Cache
	cachedAlbums := make(map[string]Album)
	if !force {
		cacheBytes, err := os.ReadFile(cacheFilePath)
		if err == nil {
			var albumsList []Album
			if errUnmarshal := json.Unmarshal(cacheBytes, &albumsList); errUnmarshal == nil {
				for _, alb := range albumsList {
					if alb.ID != "" {
						cachedAlbums[alb.ID] = alb
					}
				}
			}
		}
	}

	// 2. Fetch full list from API (Lightweight)
	var currentAPIAlbums []Album
	offset := 0
	size := 500

	slog.Info("Fetching full album list (IDs) from Navidrome...")
	for {
		params := url.Values{}
		params.Set("type", "alphabeticalByArtist")
		params.Set("size", fmt.Sprintf("%d", size))
		params.Set("offset", fmt.Sprintf("%d", offset))

		if folderID, err := client.GetMusicFolderID(); err == nil && folderID != "" {
			params.Set("musicFolderId", folderID)
		}

		raw, err := client.request("getAlbumList", params)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch album list: %w", err)
		}

		var listResp struct {
			AlbumList struct {
				Album []Album `json:"album"`
			} `json:"albumList"`
		}
		if err := json.Unmarshal(raw, &listResp); err != nil {
			return nil, fmt.Errorf("failed to parse album list batch: %w", err)
		}

		batch := listResp.AlbumList.Album
		if len(batch) == 0 {
			break
		}

		currentAPIAlbums = append(currentAPIAlbums, batch...)
		offset += size

		if offset%2000 == 0 {
			slog.Info("Fetching in progress...", "count", offset)
		}
	}

	// 3. Diff
	currentIDs := make(map[string]Album)
	for _, a := range currentAPIAlbums {
		if a.ID != "" {
			currentIDs[a.ID] = a
		}
	}

	newIDs := make(map[string]bool)
	deletedIDs := make(map[string]bool)
	expiredIDs := make(map[string]bool)

	for id := range currentIDs {
		if _, ok := cachedAlbums[id]; !ok {
			newIDs[id] = true
		}
	}

	for id := range cachedAlbums {
		if _, ok := currentIDs[id]; !ok {
			deletedIDs[id] = true
		}
	}

	// 4. Check for expired items in cache
	expiryDays := 7
	now := time.Now().UTC()

	if !force {
		for id, album := range cachedAlbums {
			if deletedIDs[id] {
				continue
			}

			isExpired := true
			if album.FetchedAt != "" {
				fetchedAt, err := time.Parse(time.RFC3339, album.FetchedAt)
				if err == nil {
					age := now.Sub(fetchedAt)
					if age.Hours() < float64(expiryDays*24) {
						isExpired = false
					}
				}
			}

			// Expired if too old, or missing total_size_bytes (and has songs)
			if isExpired || (album.TotalSizeBytes == 0 && album.SongCount > 0) {
				expiredIDs[id] = true
			}
		}
	}

	// Combine new and expired IDs to fetch
	idsToFetch := make(map[string]bool)
	for id := range newIDs {
		idsToFetch[id] = true
	}
	for id := range expiredIDs {
		idsToFetch[id] = true
	}

	slog.Info("Sync Status",
		"total", len(currentAPIAlbums),
		"new", len(newIDs),
		"deleted", len(deletedIDs),
		"expired", len(expiredIDs),
		"to_fetch", len(idsToFetch),
	)

	// 5. Enrich New & Expired Albums concurrently
	var newEnrichedAlbums []Album
	var mu sync.Mutex

	if len(idsToFetch) > 0 {
		slog.Info("Enriching albums...", "count", len(idsToFetch))

		g, _ := errgroup.WithContext(context.Background())
		sem := make(chan struct{}, 10) // Semaphore to limit concurrency to 10 workers

		count := 0
		total := len(idsToFetch)

		for id := range idsToFetch {
			id := id // capture range variable
			g.Go(func() error {
				sem <- struct{}{}
				defer func() { <-sem }()

				details, err := client.FetchAlbumDetails(id)
				if err != nil {
					slog.Error("Error enriching album, using fallback", "id", id, "error", err)
					// Fallback to basic info from currentAPIAlbums list
					if fallback, ok := currentIDs[id]; ok {
						fallback.FetchedAt = time.Now().UTC().Format(time.RFC3339)
						mu.Lock()
						newEnrichedAlbums = append(newEnrichedAlbums, fallback)
						mu.Unlock()
					}
				} else {
					details.FetchedAt = time.Now().UTC().Format(time.RFC3339)
					mu.Lock()
					newEnrichedAlbums = append(newEnrichedAlbums, *details)
					mu.Unlock()
				}

				mu.Lock()
				count++
				if count%50 == 0 {
					slog.Info("Enrichment progress...", "progress", fmt.Sprintf("%d/%d", count, total))
				}
				mu.Unlock()

				return nil
			})
		}

		_ = g.Wait()
	}

	// 6. Reconstruct Final List and Cache
	var finalLibrary []Album

	// Add preserved cached items (excluding deleted and expired)
	for id, album := range cachedAlbums {
		if !deletedIDs[id] && !expiredIDs[id] {
			finalLibrary = append(finalLibrary, album)
		}
	}

	// Add newly enriched items
	finalLibrary = append(finalLibrary, newEnrichedAlbums...)

	// Save cache
	if len(finalLibrary) > 0 {
		if err := os.MkdirAll(filepath.Dir(cacheFilePath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create cache directory: %w", err)
		}

		cacheBytes, err := json.Marshal(finalLibrary)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal final library cache: %w", err)
		}

		if err := os.WriteFile(cacheFilePath, cacheBytes, 0644); err != nil {
			return nil, fmt.Errorf("failed to write cache file: %w", err)
		}

		slog.Info("Updated library cache file", "count", len(finalLibrary))
	}

	// Save scan status meta
	status, err := client.CheckScanStatus()
	if err == nil && status != nil {
		statusBytes, _ := json.Marshal(struct {
			Count    int64  `json:"count"`
			LastScan string `json:"lastScan"`
		}{
			Count:    status.Count,
			LastScan: status.LastScan,
		})
		if len(statusBytes) > 0 {
			_ = os.WriteFile(scanMetaPath, statusBytes, 0644)
		}
	}

	return finalLibrary, nil
}

// GetNewAlbums filters the synchronized library for albums added in the last N hours.
func GetNewAlbums(client *Client, cacheFilePath, scanMetaPath string, hours int, force bool) ([]Album, error) {
	allAlbums, err := LibrarySync(client, cacheFilePath, scanMetaPath, force)
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	var newAlbums []Album

	for _, album := range allAlbums {
		if album.Created == "" {
			continue
		}

		// Parse created timestamp (Navidrome uses ISO-8601 strings ending in Z or with offsets)
		createdStr := album.Created
		if strings.HasSuffix(createdStr, "Z") {
			createdStr = createdStr[:len(createdStr)-1] + "+00:00"
		}

		createdTime, err := time.Parse(time.RFC3339, createdStr)
		if err != nil {
			// Try without offset if standard RFC3339 fails (e.g. YYYY-MM-DDTHH:MM:SS)
			createdTime, err = time.Parse("2006-01-02T15:04:05", createdStr)
			if err != nil {
				continue
			}
		}

		if createdTime.After(cutoff) {
			newAlbums = append(newAlbums, album)
		}
	}

	return newAlbums, nil
}

// GetAnniversaryAlbums scans the library for albums released on the specified day and month.
func GetAnniversaryAlbums(client *Client, cacheFilePath, scanMetaPath string, day, month int, force bool) ([]Album, error) {
	allAlbums, err := LibrarySync(client, cacheFilePath, scanMetaPath, force)
	if err != nil {
		return nil, err
	}

	var matches []Album
	for _, album := range allAlbums {
		// Prioritize originalReleaseDate, releaseDate, then simple year
		var releaseDate *DateField
		if album.OriginalReleaseDate != nil && album.OriginalReleaseDate.Year > 0 {
			releaseDate = album.OriginalReleaseDate
		} else if album.ReleaseDate != nil && album.ReleaseDate.Year > 0 {
			releaseDate = album.ReleaseDate
		}

		if releaseDate != nil {
			if releaseDate.Month == month && releaseDate.Day == day {
				matches = append(matches, album)
			}
		}
	}

	return matches, nil
}

// normalize strips accents, converts to lowercase, and prepares string for comparison.
func normalize(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, _ := transform.String(t, s)
	return strings.ToLower(result)
}

// SearchAlbums searches for albums using a local lookup over the cached/synchronized library.
func SearchAlbums(client *Client, cacheFilePath, scanMetaPath string, query string, limit int) ([]Album, error) {
	normalizedQuery := normalize(query)
	if normalizedQuery == "" {
		return []Album{}, nil
	}

	allAlbums, err := LibrarySync(client, cacheFilePath, scanMetaPath, false)
	if err != nil {
		return nil, err
	}

	var matches []Album
	for _, album := range allAlbums {
		artistNorm := normalize(album.Artist)
		nameNorm := normalize(album.Name)

		if strings.Contains(artistNorm, normalizedQuery) || strings.Contains(nameNorm, normalizedQuery) {
			matches = append(matches, album)
		}
	}

	// Sort alphabetically by artist, then album name
	sort.Slice(matches, func(i, j int) bool {
		artistI := strings.ToLower(matches[i].Artist)
		artistJ := strings.ToLower(matches[j].Artist)
		if artistI != artistJ {
			return artistI < artistJ
		}
		return strings.ToLower(matches[i].Name) < strings.ToLower(matches[j].Name)
	})

	if len(matches) > limit {
		matches = matches[:limit]
	}

	slog.Info("Local search completed", "query", query, "matches", len(matches))
	return matches, nil
}

// getSortableDate formats DateField/Year into a comparable string.
func getSortableDate(album Album) string {
	if album.OriginalReleaseDate != nil {
		if fmtDate := album.OriginalReleaseDate.Format(); fmtDate != "" {
			return fmtDate
		}
	}
	if album.ReleaseDate != nil {
		if fmtDate := album.ReleaseDate.Format(); fmtDate != "" {
			return fmtDate
		}
	}
	if album.Year > 0 {
		return fmt.Sprintf("%04d", album.Year)
	}
	return "0000"
}

// GetAlbumsByYear retrieves random albums released within a specific year range.
func GetAlbumsByYear(client *Client, cacheFilePath, scanMetaPath string, startYear, endYear int, limit int) ([]Album, error) {
	allAlbums, err := LibrarySync(client, cacheFilePath, scanMetaPath, false)
	if err != nil {
		return nil, err
	}

	var matches []Album
	for _, album := range allAlbums {
		if album.Year >= startYear && album.Year <= endYear {
			matches = append(matches, album)
		}
	}

	if len(matches) == 0 {
		return []Album{}, nil
	}

	// Shuffle first to get random selection
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(matches), func(i, j int) {
		matches[i], matches[j] = matches[j], matches[i]
	})

	selected := matches
	if len(selected) > limit {
		selected = selected[:limit]
	}

	// Sort by release date (ascending)
	sort.Slice(selected, func(i, j int) bool {
		return getSortableDate(selected[i]) < getSortableDate(selected[j])
	})

	return selected, nil
}

// ServerStats represents statistics of the Navidrome library.
type ServerStats struct {
	Albums    int   `json:"albums"`
	Artists   int   `json:"artists"`
	Songs     int   `json:"songs"`
	SizeBytes int64 `json:"size_bytes"`
}

// GetServerStats retrieves server stats (album count, artist count, song count, total size).
func GetServerStats(client *Client, cacheFilePath, scanMetaPath string) (*ServerStats, error) {
	allAlbums, err := LibrarySync(client, cacheFilePath, scanMetaPath, false)
	if err != nil {
		return nil, err
	}

	artists := make(map[string]bool)
	var totalSongs int
	var totalSize int64

	for _, album := range allAlbums {
		if album.Artist != "" {
			artists[album.Artist] = true
		}
		totalSongs += album.SongCount
		totalSize += album.TotalSizeBytes
	}

	return &ServerStats{
		Albums:    len(allAlbums),
		Artists:   len(artists),
		Songs:     totalSongs,
		SizeBytes: totalSize,
	}, nil
}
