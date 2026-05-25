package telegram

import (
	"testing"

	"github.com/alpargatagazer/alpargotobot/internal/navidrome"
	"github.com/stretchr/testify/assert"
)

func TestGetAlbumTypeTag(t *testing.T) {
	tests := []struct {
		name     string
		album    navidrome.Album
		expected string
	}{
		{
			name: "Standard Album",
			album: navidrome.Album{
				Name: "Ok Computer",
			},
			expected: "",
		},
		{
			name: "EP via releaseType",
			album: navidrome.Album{
				Name:         "In Rainbows",
				ReleaseTypes: []string{"ep"},
			},
			expected: " [EP]",
		},
		{
			name: "Compilation via flag",
			album: navidrome.Album{
				Name:          "Greatest Hits",
				IsCompilation: true,
			},
			expected: " [Compilation]",
		},
		{
			name: "Compilation already in title",
			album: navidrome.Album{
				Name:          "Greatest Hits [Compilation]",
				IsCompilation: true,
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, GetAlbumTypeTag(tt.album))
		})
	}
}

func TestExtractBestDate(t *testing.T) {
	album := navidrome.Album{
		Year: 1997,
		OriginalReleaseDate: &navidrome.DateField{
			Year:  1997,
			Month: 5,
			Day:   21,
		},
	}
	assert.Equal(t, "1997-05-21", ExtractBestDate(album))

	albumNoOrig := navidrome.Album{
		Year: 1997,
		ReleaseDate: &navidrome.DateField{
			Year:  1997,
			Month: 6,
		},
	}
	assert.Equal(t, "1997-06", ExtractBestDate(albumNoOrig))

	albumYearOnly := navidrome.Album{
		Year: 1997,
	}
	assert.Equal(t, "1997", ExtractBestDate(albumYearOnly))
}

func TestFormatSize(t *testing.T) {
	assert.Equal(t, "0 B", FormatSize(0))
	assert.Equal(t, "1.00 KB", FormatSize(1024))
	assert.Equal(t, "1.50 MB", FormatSize(1.5*1024*1024))
}

func TestSplitMessage(t *testing.T) {
	text := "album1\n\nalbum2\n\nalbum3"
	chunks := SplitMessage(text, 15)
	assert.Len(t, chunks, 3)
	assert.Equal(t, "album1", chunks[0])
	assert.Equal(t, "album2", chunks[1])
	assert.Equal(t, "album3", chunks[2])
}
