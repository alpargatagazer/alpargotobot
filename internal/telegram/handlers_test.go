package telegram

import (
	"testing"

	"github.com/alpargatagazer/alpargotobot/internal/config"
	"github.com/go-telegram/bot/models"
	"github.com/stretchr/testify/require"
)

func TestIsCommandAllowedInTopic_NoTopics(t *testing.T) {
	b := &Bot{
		cfg: &config.Config{
			TopicGeneral:         0,
			TopicIssues:          0,
			TopicRecommendations: 0,
		},
	}

	// Should allow everything everywhere
	require.True(t, b.isCommandAllowedInTopic("search", 123))
	require.True(t, b.isCommandAllowedInTopic("ticket", 456))
	require.True(t, b.isCommandAllowedInTopic("recent", 0))
}

func TestIsCommandAllowedInTopic_WithTopics(t *testing.T) {
	b := &Bot{
		cfg: &config.Config{
			TopicGeneral:         1,
			TopicIssues:          10,
			TopicRecommendations: 20,
		},
	}

	// Global commands allowed everywhere
	for _, cmd := range []string{"help", "start", "stats", "search", "login"} {
		require.True(t, b.isCommandAllowedInTopic(cmd, 0))
		require.True(t, b.isCommandAllowedInTopic(cmd, 1))
		require.True(t, b.isCommandAllowedInTopic(cmd, 10))
		require.True(t, b.isCommandAllowedInTopic(cmd, 20))
	}

	// Commands allowed everywhere EXCEPT issues
	for _, cmd := range []string{"year", "genres", "nowplaying"} {
		require.True(t, b.isCommandAllowedInTopic(cmd, 0))
		require.True(t, b.isCommandAllowedInTopic(cmd, 1))
		require.False(t, b.isCommandAllowedInTopic(cmd, 10)) // Blocked in issues
		require.True(t, b.isCommandAllowedInTopic(cmd, 20))
	}

	// Commands allowed ONLY in recommendations
	for _, cmd := range []string{"recent", "random", "recommend"} {
		require.False(t, b.isCommandAllowedInTopic(cmd, 0))
		require.False(t, b.isCommandAllowedInTopic(cmd, 1))
		require.False(t, b.isCommandAllowedInTopic(cmd, 10))
		require.True(t, b.isCommandAllowedInTopic(cmd, 20)) // Allowed here
	}

	// Commands allowed ONLY in issues
	for _, cmd := range []string{"ticket", "tickets", "done"} {
		require.False(t, b.isCommandAllowedInTopic(cmd, 0))
		require.False(t, b.isCommandAllowedInTopic(cmd, 1))
		require.True(t, b.isCommandAllowedInTopic(cmd, 10)) // Allowed here
		require.False(t, b.isCommandAllowedInTopic(cmd, 20))
	}

	// Unknown commands are allowed by default
	require.True(t, b.isCommandAllowedInTopic("unknown", 0))
	require.True(t, b.isCommandAllowedInTopic("unknown", 10))
}

func TestBuildWelcomeMessage(t *testing.T) {
	// 1. With username
	userWithUsername := models.User{
		ID:        12345,
		FirstName: "Alice",
		LastName:  "Wonderland",
		Username:  "alicew",
	}
	msg := buildWelcomeMessage(userWithUsername)
	require.Contains(t, msg, "@alicew")
	require.Contains(t, msg, "recommend an album that is already on the platform")

	// 2. Without username (uses HTML link mention)
	userWithoutUsername := models.User{
		ID:        67890,
		FirstName: "Bob",
		LastName:  "Dylan",
	}
	msg2 := buildWelcomeMessage(userWithoutUsername)
	require.Contains(t, msg2, `<a href="tg://user?id=67890">Bob Dylan</a>`)
	require.Contains(t, msg2, "recommend an album that is already on the platform")

	// 3. Name with HTML characters needing escaping
	userWithHTML := models.User{
		ID:        99999,
		FirstName: "Cool <Musician> & DJ",
	}
	msg3 := buildWelcomeMessage(userWithHTML)
	require.Contains(t, msg3, `<a href="tg://user?id=99999">Cool &lt;Musician&gt; &amp; DJ</a>`)

	// 4. Blank name fallback
	userBlank := models.User{
		ID: 11111,
	}
	msg4 := buildWelcomeMessage(userBlank)
	require.Contains(t, msg4, `<a href="tg://user?id=11111">there</a>`)
}

func TestIsChatAuthorized(t *testing.T) {
	b := &Bot{
		cfg: &config.Config{
			TelegramChatIDs: []string{"-100123456789", "987654321"},
		},
	}

	require.True(t, b.isChatAuthorized(-100123456789))
	require.True(t, b.isChatAuthorized(987654321))
	require.False(t, b.isChatAuthorized(11111111))

	// Empty config allows nothing
	bEmpty := &Bot{
		cfg: &config.Config{
			TelegramChatIDs: nil,
		},
	}
	require.False(t, bEmpty.isChatAuthorized(-100123456789))
}
