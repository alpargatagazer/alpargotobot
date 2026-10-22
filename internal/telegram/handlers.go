package telegram

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alpargatagazer/alpargotobot/internal/navidrome"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// handleMessage routes incoming Telegram messages to the correct command handler or interactive state machine.
func (b *Bot) handleMessage(ctx context.Context, message *models.Message) {
	if message == nil {
		return
	}

	// 0. Handle new chat members joining the group
	if len(message.NewChatMembers) > 0 {
		b.handleNewChatMembers(ctx, message)
		return
	}

	// 1. Interactive login / np filter step-by-step flow (DMs only)
	if message.Chat.Type == models.ChatTypePrivate {
		// If user types any command, clear interactive states so they never get trapped
		if strings.HasPrefix(strings.TrimSpace(message.Text), "/") {
			b.loginStatesMu.Lock()
			delete(b.loginStates, message.From.ID)
			b.loginStatesMu.Unlock()

			b.npFilterStatesMu.Lock()
			delete(b.npFilterStates, message.From.ID)
			b.npFilterStatesMu.Unlock()
		} else {
			b.loginStatesMu.Lock()
			state, inLogin := b.loginStates[message.From.ID]
			b.loginStatesMu.Unlock()

			if inLogin {
				b.handleInteractiveLogin(ctx, message, state)
				return
			}

			b.npFilterStatesMu.Lock()
			npState, inNP := b.npFilterStates[message.From.ID]
			b.npFilterStatesMu.Unlock()

			if inNP {
				b.handleInteractiveNPFilter(ctx, message, npState)
				return
			}
		}
	}

	text := message.Text
	isCommand := false
	cmd := ""
	args := ""

	if strings.HasPrefix(text, "/") {
		isCommand = true
		parts := strings.SplitN(text, " ", 2)
		cmd = strings.TrimPrefix(parts[0], "/")
		// Handle bot mentions like /start@bot_username
		if idx := strings.Index(cmd, "@"); idx != -1 {
			cmd = cmd[:idx]
		}
		if len(parts) > 1 {
			args = parts[1]
		}
	}

	// 2. Command routing
	if !isCommand {
		// Intercept text replies from users currently in the ticket creation flow
		b.ticketStatesMu.Lock()
		tState, inTicket := b.ticketStates[message.From.ID]
		b.ticketStatesMu.Unlock()
		if inTicket {
			if !b.isAuthorized(ctx, message.Chat.ID, message.From, message) {
				return
			}
			b.handleTicketInput(ctx, message, tState)
			return
		}

		// If it's a reply to "What do you want to search for?"
		if message.ReplyToMessage != nil && strings.Contains(strings.ToLower(message.ReplyToMessage.Text), "what do you want to search for?") {
			if !b.isAuthorized(ctx, message.Chat.ID, message.From, message) {
				return
			}
			b.performSearch(ctx, message.Chat.ID, text)
			return
		}
		return
	}

	// Authorize commands (all commands require authorization)
	if !b.isAuthorized(ctx, message.Chat.ID, message.From, message) {
		return
	}

	// Command categorization
	isCommonCmd := cmd == "start" || cmd == "help"
	isPrivateCmd := cmd == "login" || cmd == "enable_np" || cmd == "disable_np" || cmd == "enablenp" || cmd == "disablenp"
	isGroupCmd := cmd == "stats" || cmd == "random" || cmd == "search" || cmd == "nowplaying" ||
		cmd == "genres" || cmd == "year" || cmd == "recent" || cmd == "recommend" ||
		cmd == "ticket" || cmd == "tickets" || cmd == "done"

	if message.Chat.Type == models.ChatTypePrivate {
		if isGroupCmd {
			b.SendMessage(ctx, message.Chat.ID, fmt.Sprintf("⚠️ The <code>/%s</code> command can only be used in the group chat, not in private messages.", cmd), models.ParseModeHTML, nil)
			return
		}
		if !isCommonCmd && !isPrivateCmd {
			b.SendMessage(ctx, message.Chat.ID, fmt.Sprintf("⚠️ Unknown command <code>/%s</code>. Use /help to see available commands.", cmd), models.ParseModeHTML, nil)
			return
		}
	}

	// Topic Routing Rules
	if message.Chat.Type != models.ChatTypePrivate {
		if !b.isCommandAllowedInTopic(cmd, message.MessageThreadID) {
			// Notify the user instead of silently ignoring
			b.SendMessage(ctx, message.Chat.ID, "⚠️ This command is not allowed in this subchannel.", models.ParseModeHTML, nil)
			return
		}
	}

	switch cmd {
	case "start", "help":
		b.handleHelp(ctx, message.Chat.ID)
	case "stats":
		b.handleStats(ctx, message.Chat.ID)
	case "random":
		b.handleRandom(ctx, message.Chat.ID)
	case "search":
		b.handleSearch(ctx, message, args)
	case "nowplaying":
		b.handleNowPlaying(ctx, message.Chat.ID)
	case "genres":
		b.handleGenres(ctx, message.Chat.ID)
	case "year":
		b.handleYear(ctx, message.Chat.ID, args)
	case "recent":
		b.handleRecent(ctx, message.Chat.ID)
	case "recommend":
		b.handleRecommend(ctx, message.Chat.ID)
	case "login":
		b.handleLogin(ctx, message, args)
	case "enable_np", "enablenp":
		b.handleEnableNP(ctx, message, args)
	case "disable_np", "disablenp":
		b.handleDisableNP(ctx, message, args)
	case "ticket":
		b.handleTicket(ctx, message)
	case "tickets":
		b.handleListTickets(ctx, message.Chat.ID)
	case "done":
		b.handleCloseTicket(ctx, message.Chat.ID, args)
	}
}

// handleNewChatMembers welcomes any new user joining the group chat,
// mentioning them and inviting them to recommend an album already on the platform.
func (b *Bot) handleNewChatMembers(ctx context.Context, message *models.Message) {
	// Only welcome in authorized groups
	if !b.isChatAuthorized(message.Chat.ID) {
		return
	}

	// If group has a configured General topic and message came with threadID 0, route to General
	if b.cfg.TopicGeneral != 0 {
		if tID, ok := ctx.Value(threadIDKey).(int); !ok || tID == 0 {
			ctx = context.WithValue(ctx, threadIDKey, b.cfg.TopicGeneral)
		}
	}

	for _, member := range message.NewChatMembers {
		// Ignore bot accounts
		if member.IsBot {
			continue
		}

		text := buildWelcomeMessage(member)
		b.SendMessage(ctx, message.Chat.ID, text, models.ParseModeHTML, nil)
	}
}

// buildWelcomeMessage constructs the personalized welcome greeting asking the member
// to recommend an album from the platform as per group tradition.
func buildWelcomeMessage(member models.User) string {
	var mention string
	if member.Username != "" {
		mention = "@" + member.Username
	} else {
		name := strings.TrimSpace(member.FirstName + " " + member.LastName)
		if name == "" {
			name = "there"
		}
		mention = fmt.Sprintf("<a href=\"tg://user?id=%d\">%s</a>", member.ID, html.EscapeString(name))
	}

	return fmt.Sprintf("👋 Welcome %s! As is tradition here, please recommend an album that is already on the platform 💿", mention)
}

// isCommandAllowedInTopic enforces the routing rules for subchannels.
// If topics are not configured, it defaults to allowing all commands.
func (b *Bot) isCommandAllowedInTopic(cmd string, threadID int) bool {
	cfg := b.cfg
	hasTopics := cfg.TopicGeneral != 0 || cfg.TopicIssues != 0 || cfg.TopicRecommendations != 0

	if !hasTopics {
		return true // Allow all if topics aren't configured
	}

	switch cmd {
	case "help", "start", "stats", "search", "login", "enable_np", "disable_np", "enablenp", "disablenp":
		return true // all channels
	case "year", "genres", "nowplaying":
		return threadID != cfg.TopicIssues
	case "recent", "random", "recommend":
		return threadID == cfg.TopicRecommendations
	case "ticket", "tickets", "done":
		return threadID == cfg.TopicIssues
	}
	return true
}

// handleInteractiveLogin runs the state machine for password privacy.
func (b *Bot) handleInteractiveLogin(ctx context.Context, message *models.Message, state loginState) {
	userID := message.From.ID
	chatID := message.Chat.ID

	// Delete user's message as fast as possible to prevent clear-text exposure
	b.deleteMessage(ctx, chatID, message.ID)

	switch state.state {
	case 1:
		username := strings.TrimSpace(message.Text)
		if username == "" {
			b.SendMessage(ctx, chatID, "❌ Username cannot be blank. Please try again:", models.ParseModeHTML, nil)
			return
		}

		b.loginStatesMu.Lock()
		b.loginStates[userID] = loginState{state: 2, username: username}
		b.loginStatesMu.Unlock()

		b.SendMessage(ctx, chatID, "🔑 Now enter your *password*:", models.ParseModeMarkdown, nil)
	case 2:
		password := strings.TrimSpace(message.Text)
		b.loginStatesMu.Lock()
		delete(b.loginStates, userID)
		b.loginStatesMu.Unlock()

		if password == "" {
			b.SendMessage(ctx, chatID, "❌ Password cannot be blank. Please start login again with /login", models.ParseModeHTML, nil)
			return
		}

		b.processLogin(ctx, chatID, message.From, state.username, password)
	}
}

// handleHelp sends a list of available Telegram bot commands to the chat.
func (b *Bot) handleHelp(ctx context.Context, chatID int64) {
	helpText := "👋 <b>Hello! I am the Navidrome Bot.</b>\n\n" +
		"Available commands:\n" +
		"• /search &lt;text&gt; - Search for an artist or album\n" +
		"• /year &lt;year&gt; - Discover albums from a specific year or decade\n" +
		"• /random - Suggest a random album\n" +
		"• /recent - Show recently added albums\n" +
		"• /nowplaying - Show who is listening to what\n" +
		"• /genres - Browse albums by genre\n" +
		"• /recommend - Get music recommendations from other users\n" +
		"• /stats - Show server statistics\n" +
		"• /help - Show this message\n\n" +
		"🎫 <b>Issues &amp; Improvements (issues channel only)</b>:\n" +
		"• /ticket - Submit a new issue or improvement\n" +
		"• /tickets - List all open tickets\n" +
		"• /done &lt;id&gt; - Mark a ticket as resolved\n\n" +
		"🔒 <b>Private Commands (DM only)</b>:\n" +
		"• /login &lt;user&gt; &lt;pass&gt; - Store credentials to share your favorites\n" +
		"• /disable_np - Hide your activity from /nowplaying\n" +
		"• /enable_np - Show your activity in /nowplaying\n"

	b.SendMessage(ctx, chatID, helpText, models.ParseModeHTML, nil)
}

// handleStats retrieves server stats (size, artists, albums, songs) and sends them to the chat.
func (b *Bot) handleStats(ctx context.Context, chatID int64) {
	b.SendMessage(ctx, chatID, "🔄 Fetching server statistics...", models.ParseModeHTML, nil)

	stats, err := navidrome.GetServerStats(b.navClient, b.cfg.CacheFile, b.cfg.ScanMetaFile)
	if err != nil {
		b.SendMessage(ctx, chatID, fmt.Sprintf("❌ Error: %v", err), models.ParseModeHTML, nil)
		return
	}

	sizeText := FormatSize(stats.SizeBytes)
	statsText := fmt.Sprintf("📊 <b>Navidrome Library Statistics</b>\n\n"+
		"💿 Albums: %d\n"+
		"👤 Artists: %d\n"+
		"🎵 Songs: %d\n"+
		"📦 Total Size: %s\n", stats.Albums, stats.Artists, stats.Songs, sizeText)

	b.SendMessage(ctx, chatID, statsText, models.ParseModeHTML, nil)
}

// handleRandom fetches a random album from Navidrome and prints its details to the chat.
func (b *Bot) handleRandom(ctx context.Context, chatID int64) {
	b.SendMessage(ctx, chatID, "🎲 Finding a random album...", models.ParseModeHTML, nil)

	album, err := b.navClient.GetRandomAlbum()
	if err != nil {
		b.SendMessage(ctx, chatID, fmt.Sprintf("❌ Error: %v", err), models.ParseModeHTML, nil)
		return
	}

	title := album.Name
	artist := album.Artist
	year := album.Year
	coverID := album.CoverArt
	typeTag := GetAlbumTypeTag(*album)

	caption := fmt.Sprintf("🎲 <b>Why not listen to this?</b>\n\n💿 <b>%s</b>%s\n👤 %s", title, typeTag, artist)
	if year > 0 {
		caption += fmt.Sprintf("\n📅 %d", year)
	}

	genreStr := album.Genre
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
	if genreStr != "" {
		caption += fmt.Sprintf("\n🏷 %s", genreStr)
	}

	if coverID != "" {
		coverBytes, err := b.navClient.GetCoverArtBytes(coverID)
		if err == nil {
			photoParams := &bot.SendPhotoParams{
				ChatID:    chatID,
				Photo:     &models.InputFileUpload{Filename: "cover.jpg", Data: strings.NewReader(string(coverBytes))},
				Caption:   caption,
				ParseMode: models.ParseModeHTML,
			}
			b.injectThreadID(ctx, photoParams)
			if _, err = b.api.SendPhoto(ctx, photoParams); err == nil {
				return
			}
		}
	}

	b.SendMessage(ctx, chatID, caption, models.ParseModeHTML, nil)
}

// handleSearch prompts the user if no query is provided, or initiates a search.
// If the user simply types "/search", it forces a reply to prompt for the search term.
func (b *Bot) handleSearch(ctx context.Context, message *models.Message, args string) {
	query := strings.TrimSpace(args)
	chatID := message.Chat.ID
	if query == "" {
		params := &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "🔎 What do you want to search for?",
			ReplyParameters: &models.ReplyParameters{
				MessageID: message.ID,
			},
			ReplyMarkup: &models.ForceReply{
				ForceReply: true,
				Selective:  true,
			},
		}
		b.injectThreadID(ctx, params)
		_, _ = b.api.SendMessage(ctx, params)
		return
	}

	b.performSearch(ctx, chatID, query)
}

// performSearch fetches search results from Navidrome and formats them into a message.
// It handles cases where no albums are found or API errors occur.
func (b *Bot) performSearch(ctx context.Context, chatID int64, query string) {
	b.SendMessage(ctx, chatID, fmt.Sprintf("🔎 Searching for '%s'...", query), models.ParseModeHTML, nil)

	albums, err := navidrome.SearchAlbums(b.navClient, b.cfg.CacheFile, b.cfg.ScanMetaFile, query, 50)
	if err != nil {
		b.SendMessage(ctx, chatID, fmt.Sprintf("❌ Error: %v", err), models.ParseModeHTML, nil)
		return
	}

	if len(albums) == 0 {
		b.SendMessage(ctx, chatID, fmt.Sprintf("❌ No albums found matching '%s'.", query), models.ParseModeHTML, nil)
		return
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "🔎 <b>Results for '%s':</b>\n\n", query)
	for _, album := range albums {
		typeTag := GetAlbumTypeTag(album)
		dateDisplay := ExtractBestDate(album)
		genreStr := album.Genre

		fmt.Fprintf(&sb, "• %s - <b>%s</b>%s", album.Artist, album.Name, typeTag)
		if dateDisplay != "" {
			fmt.Fprintf(&sb, " 📅 %s", dateDisplay)
		}
		if genreStr != "" {
			fmt.Fprintf(&sb, " 🏷 %s", genreStr)
		}
		sb.WriteString("\n")
	}

	b.SendMessage(ctx, chatID, sb.String(), models.ParseModeHTML, nil)
}

// handleNowPlaying retrieves active user sessions from Navidrome and displays currently playing tracks.
func (b *Bot) handleNowPlaying(ctx context.Context, chatID int64) {
	entries, err := b.navClient.GetNowPlaying()
	if err != nil {
		b.SendMessage(ctx, chatID, fmt.Sprintf("❌ Error: %v", err), models.ParseModeHTML, nil)
		return
	}

	if len(entries) == 0 {
		b.SendMessage(ctx, chatID, "🤫 Nobody is listening to music right now.", models.ParseModeHTML, nil)
		return
	}

	// Filter out users who have opted out of /nowplaying
	filters, err := b.db.GetNowPlayingFilteredUsers()
	if err != nil {
		slog.Error("Failed to fetch now playing filters", "error", err)
		filters = make(map[string]bool)
	}

	var visibleEntries []map[string]any
	for _, entry := range entries {
		u := strings.ToLower(strings.TrimSpace(b.getMapString(entry, "username", "")))
		if filters[u] {
			continue
		}
		visibleEntries = append(visibleEntries, entry)
	}

	if len(visibleEntries) == 0 {
		b.SendMessage(ctx, chatID, "🤫 Nobody is listening to music right now.", models.ParseModeHTML, nil)
		return
	}

	// Fetch full library for type tag lookup
	allAlbums, _ := navidrome.LibrarySync(b.navClient, b.cfg.CacheFile, b.cfg.ScanMetaFile, false)

	msg := "🎧 <b>Now Playing:</b>\n\n"
	for _, entry := range visibleEntries {
		username := b.getMapString(entry, "username", "Someone")
		title := b.getMapString(entry, "title", "Unknown")
		artist := b.getMapString(entry, "artist", "Unknown")
		album := b.getMapString(entry, "album", "Unknown")
		albumID := b.getMapString(entry, "albumId", "")

		var yearStr string
		if y, ok := entry["year"]; ok {
			yearStr = fmt.Sprintf("%v", y)
		} else {
			yearStr = "Unknown"
		}

		typeTag := ""
		if albumID != "" {
			for _, a := range allAlbums {
				if a.ID == albumID {
					typeTag = GetAlbumTypeTag(a)
					break
				}
			}
		}

		msg += fmt.Sprintf("👤 <b>%s</b> is listening to:\n🎵 %s - %s (%s%s, %s)\n\n", username, artist, title, album, typeTag, yearStr)
	}

	b.SendMessage(ctx, chatID, msg, models.ParseModeHTML, nil)
}

// handleGenres fetches all available genres from the Navidrome server and displays them
// as an interactive inline keyboard. Telegram limits the number of buttons, so it caps at 40 buttons.
func (b *Bot) handleGenres(ctx context.Context, chatID int64) {
	genres, err := b.navClient.GetGenres()
	if err != nil {
		b.SendMessage(ctx, chatID, fmt.Sprintf("❌ Error: %v", err), models.ParseModeHTML, nil)
		return
	}

	if len(genres) == 0 {
		b.SendMessage(ctx, chatID, "📭 No genres found.", models.ParseModeHTML, nil)
		return
	}

	var buttons [][]models.InlineKeyboardButton
	var row []models.InlineKeyboardButton

	for _, g := range genres {
		if g.Name == "" {
			continue
		}
		data := "genre:" + g.Name
		row = append(row, models.InlineKeyboardButton{Text: g.Name, CallbackData: data})
		if len(row) == 2 {
			buttons = append(buttons, row)
			row = nil
		}
	}
	if len(row) > 0 {
		buttons = append(buttons, row)
	}

	// Add No Genre option
	btnNone := models.InlineKeyboardButton{Text: "No Genre", CallbackData: "genre:None"}
	buttons = append(buttons, []models.InlineKeyboardButton{btnNone})

	// Limit number of buttons to avoid huge keyboards (Telegram limits)
	if len(buttons) > 40 {
		buttons = buttons[:40]
	}

	params := &bot.SendMessageParams{
		ChatID: chatID,
		Text:   "🎷 Select a genre to explore:",
		ReplyMarkup: &models.InlineKeyboardMarkup{
			InlineKeyboard: buttons,
		},
	}
	b.injectThreadID(ctx, params)
	_, _ = b.api.SendMessage(ctx, params)
}

// handleYear processes the "/year" command. If no year is provided, it returns an inline
// keyboard offering a selection of decades (50s, 60s, etc.) or the current year.
func (b *Bot) handleYear(ctx context.Context, chatID int64, args string) {
	arg := strings.TrimSpace(args)

	if arg == "" {
		// Show decades menu
		var buttons [][]models.InlineKeyboardButton
		decades := []string{"50s", "60s", "70s", "80s", "90s", "00s", "10s", "20s"}

		var row []models.InlineKeyboardButton
		for _, d := range decades {
			row = append(row, models.InlineKeyboardButton{Text: d, CallbackData: "year:" + d})
			if len(row) == 3 {
				buttons = append(buttons, row)
				row = nil
			}
		}
		if len(row) > 0 {
			buttons = append(buttons, row)
		}

		currentYear := time.Now().Year()
		btnCurrent := models.InlineKeyboardButton{Text: fmt.Sprintf("Current (%d)", currentYear), CallbackData: fmt.Sprintf("year:%d", currentYear)}
		buttons = append(buttons, []models.InlineKeyboardButton{btnCurrent})

		params := &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "📅 Select a decade or year:",
			ReplyMarkup: &models.InlineKeyboardMarkup{
				InlineKeyboard: buttons,
			},
		}
		b.injectThreadID(ctx, params)
		_, _ = b.api.SendMessage(ctx, params)
		return
	}

	b.processYearRequest(ctx, chatID, arg)
}

// processYearRequest parses the user's year input (e.g., "1994", "90s", "Current").
// It resolves the input into a startYear and endYear range, then searches Navidrome for albums in that range.
func (b *Bot) processYearRequest(ctx context.Context, chatID int64, arg string) {
	var startYear, endYear int
	displayStr := arg

	currentYear := time.Now().Year()

	if strings.ToLower(arg) == "current" {
		startYear = currentYear
		endYear = currentYear
		displayStr = strconv.Itoa(currentYear)
	} else if matched, _ := regexp.MatchString(`^\d{4}$`, arg); matched {
		y, err := strconv.Atoi(arg)
		if err == nil && y >= 1950 && y <= currentYear+1 {
			startYear = y
			endYear = y
		} else {
			b.SendMessage(ctx, chatID, "❌ Please provide a valid year between 1950 and now.", models.ParseModeHTML, nil)
			return
		}
	} else if matched, _ := regexp.MatchString(`^\d0s$`, arg); matched {
		decadePrefix, _ := strconv.Atoi(arg[:2])
		var base int
		if decadePrefix >= 50 && decadePrefix <= 99 {
			base = 1900 + decadePrefix
		} else if decadePrefix >= 0 && decadePrefix <= 40 {
			base = 2000 + decadePrefix
		} else {
			b.SendMessage(ctx, chatID, "❌ Invalid decade.", models.ParseModeHTML, nil)
			return
		}
		startYear = base
		endYear = base + 9
		displayStr = "the " + arg
	} else {
		b.SendMessage(ctx, chatID, "❌ Invalid format. Use `/year 1994` or `/year 90s`.", models.ParseModeHTML, nil)
		return
	}

	b.SendMessage(ctx, chatID, fmt.Sprintf("📅 Finding albums from %s...", displayStr), models.ParseModeHTML, nil)

	albums, err := navidrome.GetAlbumsByYear(b.navClient, b.cfg.CacheFile, b.cfg.ScanMetaFile, startYear, endYear, 40)
	if err != nil {
		b.SendMessage(ctx, chatID, fmt.Sprintf("❌ Error: %v", err), models.ParseModeHTML, nil)
		return
	}

	if len(albums) == 0 {
		b.SendMessage(ctx, chatID, fmt.Sprintf("❌ No albums found for %s.", displayStr), models.ParseModeHTML, nil)
		return
	}

	msg := FormatAlbumList(albums, fmt.Sprintf("📅 Random albums from <b>%s</b>:", displayStr))
	b.SendMessage(ctx, chatID, msg, models.ParseModeHTML, nil)
}

// handleRecent queries Navidrome for albums added in the last 30 days and displays
// the top 10 most recent ones.
func (b *Bot) handleRecent(ctx context.Context, chatID int64) {
	b.SendMessage(ctx, chatID, "🆕 Fetching recently added albums...", models.ParseModeHTML, nil)

	recent, err := navidrome.GetNewAlbums(b.navClient, b.cfg.CacheFile, b.cfg.ScanMetaFile, 24*30, false)
	if err != nil {
		b.SendMessage(ctx, chatID, fmt.Sprintf("❌ Error: %v", err), models.ParseModeHTML, nil)
		return
	}

	if len(recent) == 0 {
		b.SendMessage(ctx, chatID, "📭 No albums added in the last 30 days.", models.ParseModeHTML, nil)
		return
	}

	sort.Slice(recent, func(i, j int) bool {
		return recent[i].Created > recent[j].Created
	})

	if len(recent) > 10 {
		recent = recent[:10]
	}

	msg := FormatAlbumList(recent, "🆕 <b>Recently Added Albums:</b>")
	b.SendMessage(ctx, chatID, msg, models.ParseModeHTML, nil)
}

// handleRecommend presents the user with an inline keyboard to choose the type
// of recommendations they'd like (songs, albums, or artists).
func (b *Bot) handleRecommend(ctx context.Context, chatID int64) {
	params := &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      "🎯 <b>Recommendations</b>\n\nWhat type of recommendations do you want?",
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{Text: "🎵 Songs (20)", CallbackData: "rec_type:song"},
					{Text: "💿 Albums (10)", CallbackData: "rec_type:album"},
					{Text: "👤 Artists (5)", CallbackData: "rec_type:artist"},
				},
			},
		},
	}
	b.injectThreadID(ctx, params)
	_, _ = b.api.SendMessage(ctx, params)
}

// handleLogin manages user authentication.
// It verifies that it is invoked in a private chat for security, and handles both inline
// credentials (`/login user pass`) and interactive step-by-step logins.
func (b *Bot) handleLogin(ctx context.Context, message *models.Message, args string) {
	chatID := message.Chat.ID
	userID := message.From.ID

	if message.Chat.Type != models.ChatTypePrivate {
		b.SendMessage(ctx, chatID, "⚠️ The `/login` command can only be used in private messages (DM) with me for security.", models.ParseModeHTML, nil)
		return
	}

	parts := strings.Fields(args)

	// Case 1: Credentials provided in the command line
	if len(parts) >= 2 {
		// Delete message for password privacy
		b.deleteMessage(ctx, chatID, message.ID)

		username := parts[0]
		password := parts[1]
		b.processLogin(ctx, chatID, message.From, username, password)
		return
	}

	// Case 2: Interactive step-by-step mode
	b.loginStatesMu.Lock()
	b.loginStates[userID] = loginState{state: 1}
	b.loginStatesMu.Unlock()

	b.SendMessage(ctx, chatID, "👤 Please enter your Navidrome *username*:", models.ParseModeMarkdown, nil)
}

// processLogin validates provided credentials with the Navidrome server using a Ping request.
// Upon success, it encrypts and stores the credentials locally for future background syncing.
func (b *Bot) processLogin(ctx context.Context, chatID int64, fromUser *models.User, username, password string) {
	params := &bot.SendMessageParams{
		ChatID: chatID,
		Text:   "⏳ Verifying credentials with Navidrome...",
	}
	b.injectThreadID(ctx, params)
	botMsg, err := b.api.SendMessage(ctx, params)
	var editMsgID int
	if err == nil {
		editMsgID = botMsg.ID
	}

	client := navidrome.NewClient(b.cfg.NavidromeURL, username, password, b.cfg.APIVersion, b.cfg.MusicFolderName)
	errPing := client.Ping()
	if errPing != nil {
		errText := "❌ Invalid credentials. Please check your Navidrome username and password."
		if strings.Contains(strings.ToLower(errPing.Error()), "network") || strings.Contains(strings.ToLower(errPing.Error()), "connection") {
			errText = "❌ Connection error. The bot could not reach the Navidrome server."
		}

		if editMsgID != 0 {
			editParams := &bot.EditMessageTextParams{
				ChatID:    chatID,
				MessageID: editMsgID,
				Text:      errText,
			}
			_, _ = b.api.EditMessageText(ctx, editParams)
		} else {
			b.SendMessage(ctx, chatID, errText, models.ParseModeHTML, nil)
		}
		return
	}

	// Store credentials in the local database
	err = b.db.UpsertCredential(username, password, &fromUser.ID, b.cfg.EncryptionKey)
	if err != nil {
		slog.Error("Failed to store credentials", "username", username, "error", err)
		errText := "❌ Database error: failed to store credentials."
		if editMsgID != 0 {
			editParams := &bot.EditMessageTextParams{
				ChatID:    chatID,
				MessageID: editMsgID,
				Text:      errText,
			}
			_, _ = b.api.EditMessageText(ctx, editParams)
		} else {
			b.SendMessage(ctx, chatID, errText, models.ParseModeHTML, nil)
		}
		return
	}

	slog.Info("Credentials saved", "username", username, "telegram_id", fromUser.ID)

	// Sync favorites in the background
	go func() {
		// Use a background context for the sync operation since it might outlive the webhook/polling request context
		_, errSync := b.activityEngine.SyncUserStarred(username, password, b.cfg.NavidromeURL)
		if errSync != nil {
			slog.Error("Initial favorites sync failed", "username", username, "error", errSync)
		} else {
			b.SendMessage(context.Background(), chatID, "✅ Your favorites have been successfully synchronized.", models.ParseModeHTML, nil)
		}
	}()

	successText := fmt.Sprintf("✅ Credentials verified and saved for <b>%s</b>.\n\n"+
		"Your favorites are syncing in the background. You can now return to the group and use <code>/recommend</code>.", username)

	if editMsgID != 0 {
		editParams := &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: editMsgID,
			Text:      successText,
			ParseMode: models.ParseModeHTML,
		}
		_, _ = b.api.EditMessageText(ctx, editParams)
	} else {
		b.SendMessage(ctx, chatID, successText, models.ParseModeHTML, nil)
	}
}

// handleDisableNP initiates the flow to exclude a user from appearing in /nowplaying.
// Can only be used in private messages (DM).
func (b *Bot) handleDisableNP(ctx context.Context, message *models.Message, args string) {
	b.startNPFilterFlow(ctx, message, args, npActionDisable)
}

// handleEnableNP initiates the flow to re-include a user in /nowplaying.
// Can only be used in private messages (DM).
func (b *Bot) handleEnableNP(ctx context.Context, message *models.Message, args string) {
	b.startNPFilterFlow(ctx, message, args, npActionEnable)
}

// startNPFilterFlow resolves the Navidrome username and verifies whether
// the user is already filtered before asking for confirmation/password.
func (b *Bot) startNPFilterFlow(ctx context.Context, message *models.Message, args string, action npFilterAction) {
	chatID := message.Chat.ID
	userID := message.From.ID

	cmdName := "/disable_np"
	if action == npActionEnable {
		cmdName = "/enable_np"
	}

	if message.Chat.Type != models.ChatTypePrivate {
		b.SendMessage(ctx, chatID, fmt.Sprintf("⚠️ The <code>%s</code> command can only be used in private messages (DM) with me for security.", cmdName), models.ParseModeHTML, nil)
		return
	}

	parts := strings.Fields(args)
	var username, password string

	if len(parts) >= 1 {
		username = parts[0]
	}
	if len(parts) >= 2 {
		password = parts[1]
	}

	// If username was not passed in args, prompt for username interactively (like /login does)
	if username == "" {
		b.npFilterStatesMu.Lock()
		b.npFilterStates[userID] = npFilterState{action: action, state: 1}
		b.npFilterStatesMu.Unlock()

		b.SendMessage(ctx, chatID, "👤 Please enter your Navidrome <b>username</b>:", models.ParseModeHTML, nil)
		return
	}

	b.checkAndPromptNPPassword(ctx, chatID, userID, action, username, password, message.ID)
}

// checkAndPromptNPPassword checks whether the user is already filtered.
// If already in the target state, it informs the user. Otherwise, it prompts for password.
func (b *Bot) checkAndPromptNPPassword(ctx context.Context, chatID int64, userID int64, action npFilterAction, username, password string, msgIDToDelete int) {
	// Always clear interactive state unless transitioning to password prompt (state 2)
	defer func() {
		b.npFilterStatesMu.Lock()
		if s, ok := b.npFilterStates[userID]; ok && s.state != 2 {
			delete(b.npFilterStates, userID)
		}
		b.npFilterStatesMu.Unlock()
	}()

	filtered, err := b.db.IsNowPlayingFiltered(username)
	if err != nil {
		slog.Error("Failed to check now playing filter status", "username", username, "error", err)
		b.SendMessage(ctx, chatID, "❌ Database error: failed to check filter status.", models.ParseModeHTML, nil)
		return
	}

	if action == npActionDisable && filtered {
		b.SendMessage(ctx, chatID, fmt.Sprintf("ℹ️ <b>%s</b> is already filtered from now playing.", username), models.ParseModeHTML, nil)
		return
	}
	if action == npActionEnable && !filtered {
		b.SendMessage(ctx, chatID, fmt.Sprintf("ℹ️ <b>%s</b> is not currently filtered (already visible in now playing).", username), models.ParseModeHTML, nil)
		return
	}

	// If password was already provided inline (e.g. /disable_np username password)
	if password != "" {
		if msgIDToDelete != 0 {
			b.deleteMessage(ctx, chatID, msgIDToDelete)
		}
		b.processNPFilter(ctx, chatID, action, username, password)
		return
	}

	// Prompt for password interactively
	b.npFilterStatesMu.Lock()
	b.npFilterStates[userID] = npFilterState{action: action, state: 2, username: username}
	b.npFilterStatesMu.Unlock()

	actionDesc := "disable"
	if action == npActionEnable {
		actionDesc = "re-enable"
	}
	prompt := fmt.Sprintf("🔑 Please enter your Navidrome <b>password</b> for <b>%s</b> to %s now playing:", html.EscapeString(username), actionDesc)
	b.SendMessage(ctx, chatID, prompt, models.ParseModeHTML, nil)
}

// handleInteractiveNPFilter handles incoming messages from users in the nowplaying filter state machine.
func (b *Bot) handleInteractiveNPFilter(ctx context.Context, message *models.Message, state npFilterState) {
	userID := message.From.ID
	chatID := message.Chat.ID

	// Delete message for security
	b.deleteMessage(ctx, chatID, message.ID)

	switch state.state {
	case 1:
		username := strings.TrimSpace(message.Text)
		if username == "" {
			b.SendMessage(ctx, chatID, "❌ Username cannot be blank. Please try again:", models.ParseModeHTML, nil)
			return
		}
		b.checkAndPromptNPPassword(ctx, chatID, userID, state.action, username, "", 0)

	case 2:
		password := strings.TrimSpace(message.Text)
		b.npFilterStatesMu.Lock()
		delete(b.npFilterStates, userID)
		b.npFilterStatesMu.Unlock()

		if password == "" {
			b.SendMessage(ctx, chatID, "❌ Password cannot be blank. Please start again.", models.ParseModeHTML, nil)
			return
		}

		b.processNPFilter(ctx, chatID, state.action, state.username, password)
	}
}

// processNPFilter checks credentials with Navidrome and persists the filter preference in SQLite.
func (b *Bot) processNPFilter(ctx context.Context, chatID int64, action npFilterAction, username, password string) {
	params := &bot.SendMessageParams{
		ChatID: chatID,
		Text:   "⏳ Verifying credentials with Navidrome...",
	}
	b.injectThreadID(ctx, params)
	var editMsgID int
	if b.api != nil {
		botMsg, err := b.api.SendMessage(ctx, params)
		if err == nil {
			editMsgID = botMsg.ID
		}
	}

	client := navidrome.NewClient(b.cfg.NavidromeURL, username, password, b.cfg.APIVersion, b.cfg.MusicFolderName)
	if err := client.Ping(); err != nil {
		errText := "❌ Invalid credentials. Please check your Navidrome username and password."
		if strings.Contains(strings.ToLower(err.Error()), "network") || strings.Contains(strings.ToLower(err.Error()), "connection") {
			errText = "❌ Connection error. The bot could not reach the Navidrome server."
		}
		if editMsgID != 0 {
			_, _ = b.api.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID:    chatID,
				MessageID: editMsgID,
				Text:      errText,
			})
		} else {
			b.SendMessage(ctx, chatID, errText, models.ParseModeHTML, nil)
		}
		return
	}

	var resultText string
	if action == npActionDisable {
		if err := b.db.SetNowPlayingFilter(username, &chatID); err != nil {
			slog.Error("Failed to set nowplaying filter", "username", username, "error", err)
			resultText = "❌ Database error: failed to update now playing preference."
		} else {
			resultText = fmt.Sprintf("✅ <b>%s</b> is now filtered out from <code>/nowplaying</code>.", username)
		}
	} else {
		if err := b.db.RemoveNowPlayingFilter(username); err != nil {
			slog.Error("Failed to remove nowplaying filter", "username", username, "error", err)
			resultText = "❌ Database error: failed to update now playing preference."
		} else {
			resultText = fmt.Sprintf("✅ <b>%s</b> is now visible in <code>/nowplaying</code>.", username)
		}
	}

	if editMsgID != 0 && b.api != nil {
		_, _ = b.api.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: editMsgID,
			Text:      resultText,
			ParseMode: models.ParseModeHTML,
		})
	} else {
		b.SendMessage(ctx, chatID, resultText, models.ParseModeHTML, nil)
	}
}

// Helpers

// deleteMessage is a utility to immediately delete a user's message, primarily
// used to hide sensitive data like plaintext passwords or clutter.
func (b *Bot) deleteMessage(ctx context.Context, chatID int64, messageID int) {
	if b.api == nil {
		return
	}
	params := &bot.DeleteMessageParams{
		ChatID:    chatID,
		MessageID: messageID,
	}
	_, _ = b.api.DeleteMessage(ctx, params)
}

// getMapString safely extracts a string value from a generic map parsed from JSON.
// Returns the fallback string if the key does not exist or the value is not a string.
func (b *Bot) getMapString(m map[string]any, key string, fallback string) string {
	if val, ok := m[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return fallback
}
