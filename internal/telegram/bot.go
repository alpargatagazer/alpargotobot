// Package telegram implements the Telegram Bot handlers, routing, formatted responses,
// and interactive user commands using the go-telegram/bot API.
package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/alpargatagazer/alpargotobot/internal/activity"
	"github.com/alpargatagazer/alpargotobot/internal/config"
	"github.com/alpargatagazer/alpargotobot/internal/database"
	"github.com/alpargatagazer/alpargotobot/internal/navidrome"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type contextKey string

const threadIDKey = contextKey("threadID")

type authCacheEntry struct {
	isAuth    bool
	lastCheck time.Time
}

type loginState struct {
	state    int // 1: waiting for username, 2: waiting for password
	username string
}

// ticketStep represents the current step in the interactive ticket creation flow.
type ticketStep int

const (
	ticketStepTitle       ticketStep = 1 // waiting for the ticket title
	ticketStepDescription ticketStep = 2 // waiting for the description
)

// ticketState tracks the in-progress state of a user creating a new ticket.
type ticketState struct {
	step       ticketStep
	ticketType database.TicketType
	title      string
}

// Bot handles Telegram bot logic, command routing, and authorization middleware.
type Bot struct {
	api            *bot.Bot
	cfg            *config.Config
	db             *database.DB
	navClient      *navidrome.Client
	activityEngine *activity.Engine

	authCache   map[int64]authCacheEntry
	authCacheMu sync.RWMutex

	loginStates   map[int64]loginState
	loginStatesMu sync.Mutex

	ticketStates   map[int64]ticketState
	ticketStatesMu sync.Mutex
}

// NewBot initializes and returns a new Telegram Bot.
func NewBot(cfg *config.Config, db *database.DB, navClient *navidrome.Client, activityEngine *activity.Engine) (*Bot, error) {
	b := &Bot{
		cfg:            cfg,
		db:             db,
		navClient:      navClient,
		activityEngine: activityEngine,
		authCache:      make(map[int64]authCacheEntry),
		loginStates:    make(map[int64]loginState),
		ticketStates:   make(map[int64]ticketState),
	}

	opts := []bot.Option{
		bot.WithDefaultHandler(b.handleUpdate),
	}

	api, err := bot.New(cfg.TelegramToken, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot: %w", err)
	}
	b.api = api

	slog.Info("Telegram bot initialized")

	return b, nil
}

// Start starts updates long-polling loop.
func (b *Bot) Start(ctx context.Context) {
	slog.Info("Started Telegram updates long-polling loop")
	b.api.Start(ctx)
}

// SendMessage sends a message, automatically splitting it if it exceeds Telegram's 4096 character limit.
func (b *Bot) SendMessage(ctx context.Context, chatID any, text string, parseMode models.ParseMode, replyMarkup models.ReplyMarkup) {
	chunks := SplitMessage(text, 4096)
	for i, chunk := range chunks {
		params := &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      chunk,
			ParseMode: parseMode,
		}

		// Automatically route to the correct subchannel if we are in one
		if threadID, ok := ctx.Value(threadIDKey).(int); ok && threadID != 0 {
			params.MessageThreadID = threadID
		}

		// Only attach replyMarkup to the last chunk
		if i == len(chunks)-1 && replyMarkup != nil {
			params.ReplyMarkup = replyMarkup
		}
		if _, err := b.api.SendMessage(ctx, params); err != nil {
			slog.Error("Failed to send message", "chatID", chatID, "error", err)
		}
	}
}

// injectThreadID sets the MessageThreadID on a SendMessageParams if the context
// has a non-zero threadID value. Call this before any direct b.api.SendMessage.
func (b *Bot) injectThreadID(ctx context.Context, params *bot.SendMessageParams) {
	if threadID, ok := ctx.Value(threadIDKey).(int); ok && threadID != 0 {
		params.MessageThreadID = threadID
	}
}

// SendNotification sends a message to all configured chats, optionally within a specific topic.
func (b *Bot) SendNotification(ctx context.Context, text string, topicID int) {
	if len(b.cfg.TelegramChatIDs) == 0 {
		slog.Error("No authorized chat IDs configured for notifications.")
		return
	}

	for _, idStr := range b.cfg.TelegramChatIDs {
		var id int64
		if _, err := fmt.Sscanf(idStr, "%d", &id); err == nil {
			chunks := SplitMessage(text, 4096)
			for _, chunk := range chunks {
				params := &bot.SendMessageParams{
					ChatID:          id,
					MessageThreadID: topicID,
					Text:            chunk,
					ParseMode:       models.ParseModeHTML,
				}
				if _, err := b.api.SendMessage(ctx, params); err != nil {
					slog.Error("Failed to send notification", "chatID", id, "topicID", topicID, "error", err)
				}
			}
		} else {
			slog.Error("Failed to parse chat ID for notification", "idStr", idStr, "error", err)
		}
	}
}

func (b *Bot) handleUpdate(ctx context.Context, api *bot.Bot, update *models.Update) {
	if update.Message != nil {
		ctx = context.WithValue(ctx, threadIDKey, update.Message.MessageThreadID)
		b.handleMessage(ctx, update.Message)
	} else if update.CallbackQuery != nil {
		if update.CallbackQuery.Message.Message != nil {
			ctx = context.WithValue(ctx, threadIDKey, update.CallbackQuery.Message.Message.MessageThreadID)
		}
		b.handleCallbackQuery(ctx, update.CallbackQuery)
	}
}

// isAuthorized checks if the chat/user has permissions to use the bot.
func (b *Bot) isAuthorized(ctx context.Context, chatID int64, fromUser *models.User, replyToMsg *models.Message) bool {
	if len(b.cfg.TelegramChatIDs) == 0 {
		return false
	}

	// 1. Direct match with authorized groups
	chatIDStr := fmt.Sprintf("%d", chatID)
	for _, id := range b.cfg.TelegramChatIDs {
		if id == chatIDStr {
			return true
		}
	}

	// 2. For private chats, check if the user belongs to one of the authorized groups.
	if fromUser != nil {
		userID := fromUser.ID

		b.authCacheMu.RLock()
		cached, exists := b.authCache[userID]
		b.authCacheMu.RUnlock()

		if exists && time.Since(cached.lastCheck) < 1*time.Hour {
			if cached.isAuth {
				return true
			}
			b.sendUnauthorizedReply(ctx, chatID, replyToMsg)
			return false
		}

		isMember := false
		for _, groupIDStr := range b.cfg.TelegramChatIDs {
			var groupID int64
			if _, err := fmt.Sscanf(groupIDStr, "%d", &groupID); err != nil {
				continue
			}

			member, err := b.api.GetChatMember(ctx, &bot.GetChatMemberParams{
				ChatID: groupID,
				UserID: userID,
			})
			if err == nil {
				status := member.Type
				if status == models.ChatMemberTypeOwner || status == models.ChatMemberTypeAdministrator || status == models.ChatMemberTypeMember || status == models.ChatMemberTypeRestricted {
					isMember = true
					break
				}
			}
		}

		b.authCacheMu.Lock()
		b.authCache[userID] = authCacheEntry{
			isAuth:    isMember,
			lastCheck: time.Now(),
		}
		b.authCacheMu.Unlock()

		if isMember {
			return true
		}

		slog.Warn("Unauthorized DM attempt", "username", fromUser.Username, "userID", userID)
		b.sendUnauthorizedReply(ctx, chatID, replyToMsg)
		return false
	}

	slog.Warn("Unauthorized access attempt", "chatID", chatID)
	return false
}

// sendUnauthorizedReply sends a standard rejection message when a user interacts
// with the bot but is not authorized (i.e. not in the group, or we don't have their record).
// It includes a helpful tip explaining how they can get themselves synced into the bot's db.
func (b *Bot) sendUnauthorizedReply(ctx context.Context, chatID int64, replyToMsg *models.Message) {
	msgText := "⛔ Sorry, I can only interact with members of authorized groups.\n\n" +
		"*Tip*: If you are in the group, try sending any message in the group first so I can re-sync my user list, then try sending me a DM again."

	params := &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      msgText,
		ParseMode: models.ParseModeMarkdown,
	}
	if replyToMsg != nil {
		params.ReplyParameters = &models.ReplyParameters{
			MessageID: replyToMsg.ID,
		}
	}
	_, _ = b.api.SendMessage(ctx, params)
}
