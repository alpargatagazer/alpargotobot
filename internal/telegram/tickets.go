package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/alpargatagazer/alpargotobot/internal/database"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// handleTicket starts the interactive ticket creation flow.
// The user first selects the type (issue or improvement) via inline keyboard,
// then provides a title and description via sequential ForceReply prompts.
func (b *Bot) handleTicket(ctx context.Context, message *models.Message) {
	params := &bot.SendMessageParams{
		ChatID:    message.Chat.ID,
		Text:      "🎫 <b>New Ticket</b>\n\nWhat kind of ticket is this?",
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{Text: "🐛 Issue", CallbackData: "ticket_type:issue"},
					{Text: "✨ Improvement", CallbackData: "ticket_type:improvement"},
				},
			},
		},
	}
	b.injectThreadID(ctx, params)
	_, _ = b.api.SendMessage(ctx, params)
}

// handleTicketInput processes free-text messages from users in the ticket creation flow.
// It is called from handleMessage whenever a user has an active ticketState.
func (b *Bot) handleTicketInput(ctx context.Context, message *models.Message, state ticketState) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := strings.TrimSpace(message.Text)

	if text == "" {
		b.SendMessage(ctx, chatID, "❌ Input cannot be empty. Please try again.", models.ParseModeHTML, nil)
		return
	}

	switch state.step {
	case ticketStepTitle:
		// Store the title, ask for description
		b.ticketStatesMu.Lock()
		b.ticketStates[userID] = ticketState{
			step:       ticketStepDescription,
			ticketType: state.ticketType,
			title:      text,
		}
		b.ticketStatesMu.Unlock()

		params := &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      "📝 Now describe the problem or improvement in detail. Include as much context as possible:",
			ParseMode: models.ParseModeHTML,
			ReplyMarkup: &models.ForceReply{
				ForceReply: true,
			},
		}
		b.injectThreadID(ctx, params)
		_, _ = b.api.SendMessage(ctx, params)

	case ticketStepDescription:
		// We have all the data — save the ticket
		b.ticketStatesMu.Lock()
		delete(b.ticketStates, userID)
		b.ticketStatesMu.Unlock()

		authorName := message.From.FirstName
		if message.From.Username != "" {
			authorName = "@" + message.From.Username
		}

		id, err := b.db.AddTicket(state.ticketType, state.title, text, authorName)
		if err != nil {
			b.SendMessage(ctx, chatID, fmt.Sprintf("❌ Failed to save ticket: %v", err), models.ParseModeHTML, nil)
			return
		}

		typeEmoji := "🐛"
		if state.ticketType == database.TicketTypeImprovement {
			typeEmoji = "✨"
		}
		b.SendMessage(ctx, chatID,
			fmt.Sprintf("%s <b>Ticket #%d created!</b>\n\n<b>%s</b>\n%s\n\n<i>By %s</i>",
				typeEmoji, id, state.title, text, authorName),
			models.ParseModeHTML, nil)
	}
}

// handleListTickets lists all open tickets in the pool.
func (b *Bot) handleListTickets(ctx context.Context, chatID int64) {
	tickets, err := b.db.ListTickets()
	if err != nil {
		b.SendMessage(ctx, chatID, fmt.Sprintf("❌ Failed to fetch tickets: %v", err), models.ParseModeHTML, nil)
		return
	}

	if len(tickets) == 0 {
		b.SendMessage(ctx, chatID, "✅ No open tickets. Everything is fine!", models.ParseModeHTML, nil)
		return
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "📋 <b>Open Tickets (%d)</b>\n\n", len(tickets))
	for _, t := range tickets {
		typeEmoji := "🐛"
		if t.Type == database.TicketTypeImprovement {
			typeEmoji = "✨"
		}
		fmt.Fprintf(&sb, "%s <b>#%d — %s</b>\n%s\n<i>By %s on %s</i>\n\n",
			typeEmoji, t.ID, t.Title, t.Description, t.AuthorName, t.CreatedAt.Format("Jan 02 2006"))
	}
	fmt.Fprintf(&sb, "Use /done &lt;id&gt; to mark a ticket as resolved.")

	b.SendMessage(ctx, chatID, sb.String(), models.ParseModeHTML, nil)
}

// handleCloseTicket marks a ticket as done (deletes it from the pool).
// Usage: /done <id>
func (b *Bot) handleCloseTicket(ctx context.Context, chatID int64, args string) {
	args = strings.TrimSpace(args)
	if args == "" {
		b.SendMessage(ctx, chatID, "⚠️ Usage: /done &lt;ticket_id&gt;", models.ParseModeHTML, nil)
		return
	}

	var id int64
	if _, err := fmt.Sscanf(args, "%d", &id); err != nil || id <= 0 {
		b.SendMessage(ctx, chatID, "❌ Invalid ticket ID. Use /tickets to see the list.", models.ParseModeHTML, nil)
		return
	}

	removed, err := b.db.CloseTicket(id)
	if err != nil {
		b.SendMessage(ctx, chatID, fmt.Sprintf("❌ Error: %v", err), models.ParseModeHTML, nil)
		return
	}
	if !removed {
		b.SendMessage(ctx, chatID, fmt.Sprintf("⚠️ Ticket #%d not found.", id), models.ParseModeHTML, nil)
		return
	}

	b.SendMessage(ctx, chatID, fmt.Sprintf("✅ Ticket #%d marked as done and removed.", id), models.ParseModeHTML, nil)
}

// handleTicketTypeCallback handles the inline button press when the user selects
// "Issue" or "Improvement" after /ticket. It saves the ticket type to ticketStates
// and prompts for the title using a ForceReply message.
func (b *Bot) handleTicketTypeCallback(ctx context.Context, call *models.CallbackQuery) {
	typeStr := strings.TrimPrefix(call.Data, "ticket_type:")
	b.answerCallback(ctx, call.ID, "")

	var tType database.TicketType
	var label string
	switch typeStr {
	case "issue":
		tType = database.TicketTypeIssue
		label = "🐛 Issue"
	case "improvement":
		tType = database.TicketTypeImprovement
		label = "✨ Improvement"
	default:
		return
	}

	chatID := call.Message.Message.Chat.ID
	userID := call.From.ID

	b.ticketStatesMu.Lock()
	b.ticketStates[userID] = ticketState{
		step:       ticketStepTitle,
		ticketType: tType,
	}
	b.ticketStatesMu.Unlock()

	// Delete the type selection message and prompt for title
	b.deleteMessage(ctx, chatID, call.Message.Message.ID)

	params := &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      fmt.Sprintf("%s selected.\n\nPlease enter a short <b>title</b> for this ticket:", label),
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: &models.ForceReply{
			ForceReply: true,
		},
	}
	b.injectThreadID(ctx, params)
	_, _ = b.api.SendMessage(ctx, params)
}
