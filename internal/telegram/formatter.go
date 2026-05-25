package telegram

import (
	"fmt"
	"math"
	"strings"

	"github.com/alpargatagazer/alpargotobot/internal/navidrome"
)

// GetAlbumTypeTag extracts and formats the album release type tag.
func GetAlbumTypeTag(album navidrome.Album) string {
	typeMap := map[string]string{
		"ep":          "EP",
		"single":      "Single",
		"live":        "Live",
		"compilation": "Compilation",
		"soundtrack":  "Soundtrack",
		"other":       "Other",
	}

	detectedType := ""

	// 1. Check standard OpenSubsonic releaseTypes (list of strings)
	for _, t := range album.ReleaseTypes {
		tLower := strings.ToLower(t)
		if label, ok := typeMap[tLower]; ok {
			detectedType = label
			break
		}
	}

	// 2. Fallback to standard Subsonic isCompilation flag
	if detectedType == "" && album.IsCompilation {
		detectedType = "Compilation"
	}

	// 3. Heuristic: Check if title already contains keywords
	title := album.Name
	if detectedType == "" {
		titleLower := strings.ToLower(title)
		compilationKeywords := []string{"compilation", "anthology", "collection", "complete", "hits", "best of", "essentials", "box set"}

		for key, label := range typeMap {
			if strings.Contains(titleLower, " "+key) || strings.Contains(titleLower, "("+key) || strings.Contains(titleLower, "["+key) || strings.HasPrefix(titleLower, key+" ") {
				detectedType = label
				break
			}
		}

		if detectedType == "" {
			for _, word := range compilationKeywords {
				if strings.Contains(titleLower, " "+word) || strings.Contains(titleLower, "("+word) || strings.Contains(titleLower, "["+word) || strings.HasPrefix(titleLower, word+" ") {
					detectedType = "Compilation"
					break
				}
			}
		}
	}

	if detectedType != "" {
		tag := "[" + detectedType + "]"
		titleStripped := strings.TrimSpace(title)

		// Check if title already ends with this tag (in any bracket style)
		if strings.HasSuffix(titleStripped, " "+tag) ||
			strings.HasSuffix(titleStripped, " ["+strings.ToLower(detectedType)+"]") ||
			strings.HasSuffix(titleStripped, " ("+detectedType+")") ||
			strings.HasSuffix(titleStripped, " ("+strings.ToLower(detectedType)+")") {
			return ""
		}

		return " " + tag
	}

	return ""
}

// ExtractBestDate extracts the most detailed date string available from the album metadata.
func ExtractBestDate(album navidrome.Album) string {
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
	return ""
}

// FormatSize formats a size in bytes into a human-readable string (MB, GB, TB).
func FormatSize(sizeBytes int64) string {
	if sizeBytes <= 0 {
		return "0 B"
	}
	sizeNames := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"}
	i := int(math.Floor(math.Log(float64(sizeBytes)) / math.Log(1024)))
	if i >= len(sizeNames) {
		i = len(sizeNames) - 1
	}
	p := math.Pow(1024, float64(i))
	s := float64(sizeBytes) / p
	return fmt.Sprintf("%.2f %s", s, sizeNames[i])
}

// FormatAlbumList formats a list of album dictionaries into a readable HTML message.
func FormatAlbumList(albums []navidrome.Album, introText string) string {
	if len(albums) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("<b>")
	sb.WriteString(introText)
	sb.WriteString("</b>\n\n")

	for _, album := range albums {
		title := album.Name
		if title == "" {
			title = "Unknown Album"
		}
		artist := album.Artist
		if artist == "" {
			artist = "Unknown Artist"
		}
		typeTag := GetAlbumTypeTag(album)
		dateDisplay := ExtractBestDate(album)

		// Tags (Genres)
		var genreStr string
		if len(album.Genres) > 0 {
			var names []string
			for _, g := range album.Genres {
				if g.Name != "" {
					names = append(names, g.Name)
				}
			}
			if len(names) > 0 {
				genreStr = strings.Join(names, ", ")
			}
		}

		if genreStr == "" {
			genreStr = album.Genre
		}

		sb.WriteString("💿 <b>")
		sb.WriteString(title)
		sb.WriteString("</b>")
		sb.WriteString(typeTag)
		sb.WriteString("\n👤 ")
		sb.WriteString(artist)
		sb.WriteString("\n📅 ")
		sb.WriteString(dateDisplay)
		sb.WriteString("\n")
		if genreStr != "" {
			sb.WriteString("🏷 ")
			sb.WriteString(genreStr)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// SplitMessage splits a message into chunks that fit within Telegram's character limit.
func SplitMessage(text string, maxLength int) []string {
	if len(text) <= maxLength {
		return []string{text}
	}

	var chunks []string
	albums := strings.Split(text, "\n\n")
	var currentChunk strings.Builder

	for _, album := range albums {
		if album == "" {
			continue
		}
		var testChunk string
		if currentChunk.Len() > 0 {
			testChunk = currentChunk.String() + album + "\n\n"
		} else {
			testChunk = album + "\n\n"
		}

		if len(testChunk) > maxLength {
			if currentChunk.Len() > 0 {
				chunks = append(chunks, strings.TrimSpace(currentChunk.String()))
				currentChunk.Reset()
				currentChunk.WriteString(album + "\n\n")
			} else {
				// Single album is too long, force split it
				chunks = append(chunks, album[:maxLength])
			}
		} else {
			currentChunk.WriteString(album + "\n\n")
		}
	}

	if currentChunk.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(currentChunk.String()))
	}

	return chunks
}
