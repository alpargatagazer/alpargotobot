package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// handleCallbackQuery processes callback queries from inline keyboards.
func (b *Bot) handleCallbackQuery(ctx context.Context, call *models.CallbackQuery) {
	if call == nil {
		return
	}

	data := call.Data

	if strings.HasPrefix(data, "genre:") {
		b.handleGenreCallback(ctx, call)
	} else if strings.HasPrefix(data, "year:") {
		b.handleYearCallback(ctx, call)
	} else if strings.HasPrefix(data, "rec_type:") {
		b.handleRecTypeCallback(ctx, call)
	} else if strings.HasPrefix(data, "rec_user:") {
		b.handleRecUserCallback(ctx, call)
	} else if strings.HasPrefix(data, "ticket_type:") {
		b.handleTicketTypeCallback(ctx, call)
	} else if strings.HasPrefix(data, "ticket_done:") {
		b.handleTicketDoneCallback(ctx, call)
	}
}

// handleGenreCallback processes the selection of a specific genre from the inline keyboard.
func (b *Bot) handleGenreCallback(ctx context.Context, call *models.CallbackQuery) {
	genre := strings.TrimPrefix(call.Data, "genre:")
	b.answerCallback(ctx, call.ID, fmt.Sprintf("Searching for %s albums...", genre))

	albums, err := b.navClient.GetAlbumsByGenre(genre)
	if err != nil {
		b.editMessage(ctx, call.Message.Message.Chat.ID, call.Message.Message.ID, fmt.Sprintf("❌ Error: %v", err), nil)
		return
	}

	if len(albums) == 0 {
		b.editMessage(ctx, call.Message.Message.Chat.ID, call.Message.Message.ID, fmt.Sprintf("❓ No albums found for genre '%s'.", genre), nil)
		return
	}

	// Shuffle and select up to 25 albums
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(albums), func(i, j int) {
		albums[i], albums[j] = albums[j], albums[i]
	})

	limit := 25
	if len(albums) < limit {
		limit = len(albums)
	}
	selected := albums[:limit]

	intro := fmt.Sprintf("🎸 Random albums from <b>%s</b>:", genre)
	if genre == "None" {
		intro = "🎸 Random albums with <b>no defined genre</b>:"
	}

	msgText := FormatAlbumList(selected, intro)
	if msgText != "" {
		b.deleteMessage(ctx, call.Message.Message.Chat.ID, call.Message.Message.ID)
		b.SendMessage(ctx, call.Message.Message.Chat.ID, msgText, models.ParseModeHTML, nil)
	}
}

// handleYearCallback processes the selection of a specific year or decade from the inline keyboard.
func (b *Bot) handleYearCallback(ctx context.Context, call *models.CallbackQuery) {
	arg := strings.TrimPrefix(call.Data, "year:")
	b.answerCallback(ctx, call.ID, fmt.Sprintf("Selecting %s...", arg))

	b.deleteMessage(ctx, call.Message.Message.Chat.ID, call.Message.Message.ID)
	b.processYearRequest(ctx, call.Message.Message.Chat.ID, arg)
}

// handleRecTypeCallback processes the recommendation type selection callback.
func (b *Bot) handleRecTypeCallback(ctx context.Context, call *models.CallbackQuery) {
	itemType := strings.TrimPrefix(call.Data, "rec_type:")
	b.answerCallback(ctx, call.ID, "Validating active users...")

	users, err := b.activityEngine.ValidateAndGetUsers(b.cfg.NavidromeURL)
	if err != nil || len(users) == 0 {
		msgText := "⚠️ No users have registered yet. To share your favorites, " +
			"send me a private message (DM) with: \n`/login username password`"
		b.editMessage(ctx, call.Message.Message.Chat.ID, call.Message.Message.ID, msgText, nil)
		return
	}

	var keyboard [][]models.InlineKeyboardButton
	keyboard = append(keyboard, []models.InlineKeyboardButton{
		{Text: "🎲 Random", CallbackData: fmt.Sprintf("rec_user:%s:__random__", itemType)},
	})

	var row []models.InlineKeyboardButton
	for _, u := range users {
		btn := models.InlineKeyboardButton{Text: "👤 " + u, CallbackData: fmt.Sprintf("rec_user:%s:%s", itemType, u)}
		row = append(row, btn)
		if len(row) == 2 {
			keyboard = append(keyboard, row)
			row = nil
		}
	}
	if len(row) > 0 {
		keyboard = append(keyboard, row)
	}

	typeLabels := map[string]string{
		"song":   "songs",
		"album":  "albums",
		"artist": "artists",
	}

	label := typeLabels[itemType]
	menuText := fmt.Sprintf("🎯 <b>%s Recommendations</b>\n\nWhose recommendations would you like to get?", cases.Title(language.English).String(label))

	markup := &models.InlineKeyboardMarkup{InlineKeyboard: keyboard}
	b.editMessage(ctx, call.Message.Message.Chat.ID, call.Message.Message.ID, menuText, markup)
}

// handleRecUserCallback processes recommendation callbacks when a user requests
// recommendations based on their own listening history (e.g., songs, albums, artists).
func (b *Bot) handleRecUserCallback(ctx context.Context, call *models.CallbackQuery) {
	parts := strings.SplitN(strings.TrimPrefix(call.Data, "rec_user:"), ":", 2)
	if len(parts) < 2 {
		return
	}
	itemType := parts[0]
	chosenUser := parts[1]

	b.answerCallback(ctx, call.ID, "⏳ Fetching recommendations...")
	b.deleteMessage(ctx, call.Message.Message.Chat.ID, call.Message.Message.ID)

	limits := map[string]int{
		"song":   20,
		"album":  10,
		"artist": 5,
	}
	limit := limits[itemType]

	var items []map[string]any
	var sourceUser string
	var err error

	if chosenUser == "__random__" {
		// Identify caller
		callerNDUser, _ := b.db.GetNavidromeUserByTelegramID(call.From.ID)
		result, errRec := b.activityEngine.GetRandomUserRecommendations(itemType, limit, b.cfg.NavidromeURL, callerNDUser)
		if errRec != nil {
			b.SendMessage(ctx, call.Message.Message.Chat.ID, fmt.Sprintf("❌ Error: %v", errRec), models.ParseModeHTML, nil)
			return
		}
		sourceUser = result.Username
		items = result.Items
	} else {
		sourceUser = chosenUser
		items, err = b.activityEngine.GetRecommendations(chosenUser, itemType, limit, b.cfg.NavidromeURL)
		if err != nil {
			b.SendMessage(ctx, call.Message.Message.Chat.ID, fmt.Sprintf("❌ Error: %v", err), models.ParseModeHTML, nil)
			return
		}
	}

	if len(items) == 0 {
		b.SendMessage(ctx, call.Message.Message.Chat.ID, fmt.Sprintf("📭 %s has no favorites of this type.", sourceUser), models.ParseModeHTML, nil)
		return
	}

	b.formatAndSendRecommendations(ctx, call.Message.Message.Chat.ID, sourceUser, itemType, items)
}

// formatAndSendRecommendations structures the recommendation results into a readable message
// and sends it back to the user.
func (b *Bot) formatAndSendRecommendations(ctx context.Context, chatID int64, sourceUser string, itemType string, items []map[string]any) {
	typeLabels := map[string]string{"song": "songs", "album": "albums", "artist": "artists"}
	typeEmojis := map[string]string{"song": "🎵", "album": "💿", "artist": "👤"}

	label := typeLabels[itemType]
	emoji := typeEmojis[itemType]

	header := fmt.Sprintf("🎯 <b>%s Recommendations</b>\n📌 Based on favorites by <b>%s</b>", cases.Title(language.English).String(label), sourceUser)

	switch itemType {
	case "song":
		var sb strings.Builder
		sb.WriteString(header)
		sb.WriteString("\n\n")

		for _, item := range items {
			title := b.getMapString(item, "title", "Unknown")
			artist := b.getMapString(item, "artist", "Unknown")
			album := b.getMapString(item, "album", "")
			genre := b.getMapString(item, "genre", "")

			fmt.Fprintf(&sb, "%s <b>%s</b> — %s", emoji, title, artist)
			if album != "" {
				fmt.Fprintf(&sb, " (%s)", album)
			}
			if genre != "" {
				fmt.Fprintf(&sb, " 🏷 %s", genre)
			}
			sb.WriteString("\n")
		}

		b.SendMessage(ctx, chatID, sb.String(), models.ParseModeHTML, nil)

	case "album":
		var lines []string
		lines = append(lines, header, "")

		var media []models.InputMedia
		for _, item := range items {
			name := b.getMapString(item, "name", b.getMapString(item, "album", "Unknown"))
			artist := b.getMapString(item, "artist", b.getMapString(item, "albumArtist", "Unknown"))
			var yearStr string
			if y, ok := item["year"]; ok {
				yearStr = fmt.Sprintf("%v", y)
			}
			genre := b.getMapString(item, "genre", "")
			coverID := b.getMapString(item, "coverArt", "")

			line := fmt.Sprintf("%s <b>%s</b> — %s", emoji, name, artist)
			if yearStr != "" {
				line += " 📅 " + yearStr
			}
			if genre != "" {
				line += " 🏷 " + genre
			}
			lines = append(lines, line)

			if coverID != "" {
				coverBytes, err := b.navClient.GetCoverArtBytes(coverID)
				if err == nil {
					photo := &models.InputMediaPhoto{
						Media:           "attach://" + name + ".jpg",
						MediaAttachment: strings.NewReader(string(coverBytes)),
					}
					media = append(media, photo)
				}
			}
		}

		fullCaption := strings.Join(lines, "\n")
		b.sendMediaOrText(ctx, chatID, media, fullCaption)

	case "artist":
		var lines []string
		lines = append(lines, header, "")

		var media []models.InputMedia
		for _, item := range items {
			name := b.getMapString(item, "name", "Unknown")
			artistID := b.getMapString(item, "id", "")

			var genres []string
			if artistID != "" {
				genres, _ = b.navClient.GetArtistGenres(artistID)
			}

			line := fmt.Sprintf("%s <b>%s</b>", emoji, name)
			if len(genres) > 0 {
				line += " 🏷 " + strings.Join(genres, ", ")
			}
			lines = append(lines, line)

			if artistID != "" {
				coverBytes, err := b.navClient.GetCoverArtBytes(artistID)
				if err == nil {
					photo := &models.InputMediaPhoto{
						Media:           "attach://" + name + ".jpg",
						MediaAttachment: strings.NewReader(string(coverBytes)),
					}
					media = append(media, photo)
				}
			}
		}

		fullCaption := strings.Join(lines, "\n")
		b.sendMediaOrText(ctx, chatID, media, fullCaption)
	}
}

// sendMediaOrText checks if the media (e.g., Album Art) is accessible. If so, it sends the media
// with the provided text as a caption. If the media URL is invalid or empty, it sends only the text message.
func (b *Bot) sendMediaOrText(ctx context.Context, chatID int64, media []models.InputMedia, caption string) {
	if len(media) > 0 {
		// Truncate caption if it exceeds Telegram's 1024 limit for media captions
		if len(caption) > 1024 {
			caption = caption[:1021] + "..."
		}

		// Assign the full caption to the FIRST item in the group
		if photo, ok := media[0].(*models.InputMediaPhoto); ok {
			photo.Caption = caption
			photo.ParseMode = models.ParseModeHTML
		}

		// Send in chunks of 10
		for k := 0; k < len(media); k += 10 {
			end := k + 10
			if end > len(media) {
				end = len(media)
			}
			params := &bot.SendMediaGroupParams{
				ChatID: chatID,
				Media:  media[k:end],
			}
			b.injectThreadID(ctx, params)
			_, err := b.api.SendMediaGroup(ctx, params)
			if err != nil {
				slog.Error("Failed to send media group", "error", err)
			}
		}
	} else {
		b.SendMessage(ctx, chatID, caption, models.ParseModeHTML, nil)
	}
}

// answerCallback notifies Telegram that the callback has been received and processed.
// This dismisses the loading state on the user's inline keyboard button.
func (b *Bot) answerCallback(ctx context.Context, callbackQueryID string, text string) {
	params := &bot.AnswerCallbackQueryParams{
		CallbackQueryID: callbackQueryID,
		Text:            text,
	}
	_, _ = b.api.AnswerCallbackQuery(ctx, params)
}

// editMessage updates the text or keyboard of an existing message.
func (b *Bot) editMessage(ctx context.Context, chatID int64, messageID int, text string, replyMarkup *models.InlineKeyboardMarkup) {
	params := &bot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: messageID,
		Text:      text,
		ParseMode: models.ParseModeHTML,
	}
	if replyMarkup != nil {
		params.ReplyMarkup = replyMarkup
	}
	_, _ = b.api.EditMessageText(ctx, params)
}
