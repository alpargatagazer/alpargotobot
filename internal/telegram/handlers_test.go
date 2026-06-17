package telegram

import (
	"testing"

	"github.com/alpargatagazer/alpargotobot/internal/config"
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
