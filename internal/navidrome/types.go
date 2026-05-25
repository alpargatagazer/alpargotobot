package navidrome

import (
	"encoding/json"
	"fmt"
)

// DateField handles Navidrome's polymorphic date format.
// It can parse either a JSON object like {"year":2024,"month":5,"day":17}
// or a simple string timestamp like "2024-05-17T00:00:00Z".
type DateField struct {
	Year  int    `json:"year,omitempty"`
	Month int    `json:"month,omitempty"`
	Day   int    `json:"day,omitempty"`
	Raw   string `json:"raw,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler.
func (d *DateField) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}

	// Check if it's an object first
	if len(b) > 0 && b[0] == '{' {
		var obj struct {
			Year  int `json:"year"`
			Month int `json:"month"`
			Day   int `json:"day"`
		}
		if err := json.Unmarshal(b, &obj); err == nil {
			d.Year = obj.Year
			d.Month = obj.Month
			d.Day = obj.Day
			return nil
		}
	}

	// Try string format
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		// If it's a number (sometimes year is just a number)
		var num int
		if errNum := json.Unmarshal(b, &num); errNum == nil {
			d.Year = num
			return nil
		}
		return err
	}
	d.Raw = str

	if len(str) >= 4 {
		// Try parsing YYYY-MM-DD
		_, _ = fmt.Sscanf(str, "%d-%d-%d", &d.Year, &d.Month, &d.Day)
	}

	return nil
}

// MarshalJSON implements json.Marshaler.
func (d DateField) MarshalJSON() ([]byte, error) {
	if d.Raw != "" {
		return json.Marshal(d.Raw)
	}
	if d.Year > 0 {
		return json.Marshal(struct {
			Year  int `json:"year"`
			Month int `json:"month"`
			Day   int `json:"day"`
		}{
			Year:  d.Year,
			Month: d.Month,
			Day:   d.Day,
		})
	}
	return []byte("null"), nil
}

// Format returns the date as a string (YYYY-MM-DD, YYYY-MM, or YYYY).
func (d DateField) Format() string {
	if d.Raw != "" {
		// If raw is an ISO-8601 string, truncate to just the date part if it contains time
		if len(d.Raw) >= 10 && d.Raw[4] == '-' && d.Raw[7] == '-' {
			return d.Raw[:10]
		}
		return d.Raw
	}
	if d.Year > 0 {
		if d.Month > 0 && d.Day > 0 {
			return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
		}
		if d.Month > 0 {
			return fmt.Sprintf("%04d-%02d", d.Year, d.Month)
		}
		return fmt.Sprintf("%04d", d.Year)
	}
	return ""
}

// Genre represents a musical genre returned by the Navidrome Subsonic API.
type Genre struct {
	Name  string `json:"value"`
	Count int    `json:"albumCount,omitempty"`
}

// Artist represents an artist returned by the Navidrome Subsonic API.
type Artist struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CoverArt   string  `json:"coverArt,omitempty"`
	AlbumCount int     `json:"albumCount,omitempty"`
	Album      []Album `json:"album,omitempty"`
}

// Album represents an album returned by the Navidrome Subsonic API.
type Album struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	Artist              string     `json:"artist"`
	Year                int        `json:"year,omitempty"`
	CoverArt            string     `json:"coverArt,omitempty"`
	Created             string     `json:"created,omitempty"`
	Genre               string     `json:"genre,omitempty"`
	Genres              []Genre    `json:"genres,omitempty"`
	SongCount           int        `json:"songCount,omitempty"`
	ReleaseDate         *DateField `json:"releaseDate,omitempty"`
	OriginalReleaseDate *DateField `json:"originalReleaseDate,omitempty"`
	ReleaseTypes        []string   `json:"releaseTypes,omitempty"`
	IsCompilation       bool       `json:"isCompilation,omitempty"`
	TotalSizeBytes      int64      `json:"total_size_bytes,omitempty"`
	FetchedAt           string     `json:"_fetched_at,omitempty"`
}

// Song represents a song returned by the Navidrome Subsonic API (for size calculation during enrichment).
type Song struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Size  int64  `json:"size,omitempty"`
}
