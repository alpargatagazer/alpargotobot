package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alpargatagazer/alpargotobot/internal/config"
	"github.com/alpargatagazer/alpargotobot/internal/database"
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
	for _, cmd := range []string{"help", "start", "stats", "search", "login", "enable_np", "disable_np"} {
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

func TestNowPlayingFilteringLogic(t *testing.T) {
	b := &Bot{}

	entries := []map[string]any{
		{"username": "Alice", "title": "Song 1"},
		{"username": "Bob", "title": "Song 2"},
		{"username": "Charlie", "title": "Song 3"},
	}

	filters := map[string]bool{
		"bob": true,
	}

	var visible []map[string]any
	for _, entry := range entries {
		u := b.getMapString(entry, "username", "")
		if filters[strings.ToLower(strings.TrimSpace(u))] {
			continue
		}
		visible = append(visible, entry)
	}

	require.Len(t, visible, 2)
	require.Equal(t, "Alice", visible[0]["username"])
	require.Equal(t, "Charlie", visible[1]["username"])
}

func TestCommandClassification(t *testing.T) {
	isPrivateCmd := func(cmd string) bool {
		return cmd == "login" || cmd == "enable_np" || cmd == "disable_np" || cmd == "enablenp" || cmd == "disablenp"
	}
	isGroupCmd := func(cmd string) bool {
		return cmd == "stats" || cmd == "random" || cmd == "search" || cmd == "nowplaying" ||
			cmd == "genres" || cmd == "year" || cmd == "recent" || cmd == "recommend" ||
			cmd == "ticket" || cmd == "tickets" || cmd == "done"
	}
	isCommonCmd := func(cmd string) bool {
		return cmd == "start" || cmd == "help"
	}

	// disable_nb is a typo and should be classified as neither private, group, nor common
	require.False(t, isPrivateCmd("disable_nb"))
	require.False(t, isGroupCmd("disable_nb"))
	require.False(t, isCommonCmd("disable_nb"))

	// Real commands
	require.True(t, isPrivateCmd("disable_np"))
	require.True(t, isGroupCmd("search"))
	require.True(t, isCommonCmd("help"))
}

func newTestBot(t *testing.T, navURL string) (*Bot, *database.DB, *[]string) {
	tmpDir := t.TempDir()
	db, err := database.NewDB(filepath.Join(tmpDir, "test.db"))
	require.NoError(t, err)

	var sentMessages []string
	b := &Bot{
		cfg: &config.Config{
			TelegramChatIDs: []string{"-100123456789"},
			NavidromeURL:    navURL,
			APIVersion:      "1.16.1",
			MusicFolderName: "Music",
		},
		db:             db,
		authCache:      make(map[int64]authCacheEntry),
		loginStates:    make(map[int64]loginState),
		ticketStates:   make(map[int64]ticketState),
		npFilterStates: make(map[int64]npFilterState),
		onSendMessage: func(ctx context.Context, chatID any, text string, parseMode models.ParseMode) {
			sentMessages = append(sentMessages, text)
		},
	}

	return b, db, &sentMessages
}

func TestHandleMessage_NPFilter_FullFlow(t *testing.T) {
	// Mock Navidrome ping server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok","version":"1.16.1"}}`))
	}))
	defer server.Close()

	b, db, sentMsgs := newTestBot(t, server.URL)
	defer func() { _ = db.Close() }()

	fromUser := &models.User{ID: 100, FirstName: "Alice"}
	chat := models.Chat{ID: 100, Type: models.ChatTypePrivate}
	b.authCache[100] = authCacheEntry{isAuth: true, lastCheck: time.Now()}

	ctx := context.Background()

	// 1. User sends /disable_np
	b.handleMessage(ctx, &models.Message{From: fromUser, Chat: chat, Text: "/disable_np"})
	require.NotEmpty(t, *sentMsgs)
	require.Contains(t, (*sentMsgs)[len(*sentMsgs)-1], "Please enter your Navidrome <b>username</b>:")
	require.Equal(t, 1, b.npFilterStates[100].state)

	// 2. User sends username "alice"
	b.handleMessage(ctx, &models.Message{From: fromUser, Chat: chat, Text: "alice"})
	require.Contains(t, (*sentMsgs)[len(*sentMsgs)-1], "password")
	require.Contains(t, (*sentMsgs)[len(*sentMsgs)-1], "alice")
	require.Equal(t, 2, b.npFilterStates[100].state)
	require.Equal(t, "alice", b.npFilterStates[100].username)

	// 3. User sends password "secret"
	b.handleMessage(ctx, &models.Message{From: fromUser, Chat: chat, Text: "secret"})
	require.Contains(t, (*sentMsgs)[len(*sentMsgs)-1], "filtered out from <code>/nowplaying</code>")
	require.Empty(t, b.npFilterStates[100])

	// Verify DB state
	filtered, err := db.IsNowPlayingFiltered("alice")
	require.NoError(t, err)
	require.True(t, filtered)

	// 4. User sends /enable_np
	b.handleMessage(ctx, &models.Message{From: fromUser, Chat: chat, Text: "/enable_np"})
	require.Contains(t, (*sentMsgs)[len(*sentMsgs)-1], "Please enter your Navidrome <b>username</b>:")
	require.Equal(t, 1, b.npFilterStates[100].state)

	// 5. User sends username "alice"
	b.handleMessage(ctx, &models.Message{From: fromUser, Chat: chat, Text: "alice"})
	require.Contains(t, (*sentMsgs)[len(*sentMsgs)-1], "re-enable now playing")
	require.Equal(t, 2, b.npFilterStates[100].state)

	// 6. User sends password "secret"
	b.handleMessage(ctx, &models.Message{From: fromUser, Chat: chat, Text: "secret"})
	require.Contains(t, (*sentMsgs)[len(*sentMsgs)-1], "now visible in <code>/nowplaying</code>")
	require.Empty(t, b.npFilterStates[100])

	// Verify DB state removed
	filtered, err = db.IsNowPlayingFiltered("alice")
	require.NoError(t, err)
	require.False(t, filtered)
}

func TestHandleMessage_NPFilter_TrapReproduction(t *testing.T) {
	b, db, sentMsgs := newTestBot(t, "http://localhost")
	defer func() { _ = db.Close() }()

	fromUser := &models.User{ID: 100, FirstName: "Alice"}
	chat := models.Chat{ID: 100, Type: models.ChatTypePrivate}
	b.authCache[100] = authCacheEntry{isAuth: true, lastCheck: time.Now()}

	ctx := context.Background()

	// Step 1: User runs /enable_np
	b.handleMessage(ctx, &models.Message{From: fromUser, Chat: chat, Text: "/enable_np"})
	require.Contains(t, (*sentMsgs)[len(*sentMsgs)-1], "Please enter your Navidrome <b>username</b>:")
	require.Equal(t, 1, b.npFilterStates[100].state)

	// Step 2: User sends "sadsad" (user that is not in exclusion list)
	b.handleMessage(ctx, &models.Message{From: fromUser, Chat: chat, Text: "sadsad"})
	require.Contains(t, (*sentMsgs)[len(*sentMsgs)-1], "is not currently filtered (already visible in now playing)")

	// CRITICAL ASSERTION: State MUST be cleared, not trapped in state 1
	require.Empty(t, b.npFilterStates[100])

	// Step 3: User immediately sends /enable_np again
	b.handleMessage(ctx, &models.Message{From: fromUser, Chat: chat, Text: "/enable_np"})

	// It must prompt for username again, NOT say "/enable_np is already visible in now playing"
	require.Contains(t, (*sentMsgs)[len(*sentMsgs)-1], "Please enter your Navidrome <b>username</b>:")
	require.NotContains(t, (*sentMsgs)[len(*sentMsgs)-1], "/enable_np is already visible")
}

func TestHandleMessage_SlashCommandCancelsState(t *testing.T) {
	b, db, sentMsgs := newTestBot(t, "http://localhost")
	defer func() { _ = db.Close() }()

	fromUser := &models.User{ID: 100, FirstName: "Alice"}
	chat := models.Chat{ID: 100, Type: models.ChatTypePrivate}
	b.authCache[100] = authCacheEntry{isAuth: true, lastCheck: time.Now()}

	ctx := context.Background()

	// Start /disable_np
	b.handleMessage(ctx, &models.Message{From: fromUser, Chat: chat, Text: "/disable_np"})
	require.Equal(t, 1, b.npFilterStates[100].state)

	// User types a slash command like /help
	b.handleMessage(ctx, &models.Message{From: fromUser, Chat: chat, Text: "/help"})

	// State must be completely wiped
	require.Empty(t, b.npFilterStates[100])

	// Help text was sent
	require.Contains(t, (*sentMsgs)[len(*sentMsgs)-1], "Available commands")
}

func TestHandleMessage_PrivateCommandTypo(t *testing.T) {
	b, db, sentMsgs := newTestBot(t, "http://localhost")
	defer func() { _ = db.Close() }()

	fromUser := &models.User{ID: 100, FirstName: "Alice"}
	chat := models.Chat{ID: 100, Type: models.ChatTypePrivate}
	b.authCache[100] = authCacheEntry{isAuth: true, lastCheck: time.Now()}

	ctx := context.Background()

	// Typo /disable_nb in DM
	b.handleMessage(ctx, &models.Message{From: fromUser, Chat: chat, Text: "/disable_nb"})

	// Must say Unknown command, NOT "can only be used in the group chat"
	require.Contains(t, (*sentMsgs)[len(*sentMsgs)-1], "Unknown command <code>/disable_nb</code>")
	require.NotContains(t, (*sentMsgs)[len(*sentMsgs)-1], "can only be used in the group chat")

	// Known group command in DM
	b.handleMessage(ctx, &models.Message{From: fromUser, Chat: chat, Text: "/nowplaying"})
	require.Contains(t, (*sentMsgs)[len(*sentMsgs)-1], "The <code>/nowplaying</code> command can only be used in the group chat")
}
