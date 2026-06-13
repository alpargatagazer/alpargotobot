package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alpargatagazer/alpargotobot/internal/activity"
	"github.com/alpargatagazer/alpargotobot/internal/config"
	"github.com/alpargatagazer/alpargotobot/internal/database"
	"github.com/alpargatagazer/alpargotobot/internal/navidrome"
	"github.com/alpargatagazer/alpargotobot/internal/scheduler"
	"github.com/alpargatagazer/alpargotobot/internal/telegram"
)

func main() {
	// 1. Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	// 2. Setup Logging
	var level slog.Level
	switch cfg.LogLevel {
	case "DEBUG":
		level = slog.LevelDebug
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)

	slog.Info("Navidrome Telegram Bot Starting...")

	// 3. Initialize Database
	db, err := database.NewDB(cfg.DBPath)
	if err != nil {
		slog.Error("Failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("Failed to close database", "error", err)
		}
	}()

	// 4. Initialize Navidrome Client (System/Admin)
	systemNavClient := navidrome.NewClient(
		cfg.NavidromeURL,
		cfg.NavidromeUser,
		cfg.NavidromePassword,
		cfg.APIVersion,
		cfg.MusicFolderName,
	)

	// Ping to verify
	if err := systemNavClient.Ping(); err != nil {
		slog.Warn("Failed to ping Navidrome on startup, will continue", "error", err)
	} else {
		slog.Info("Connected to Navidrome server successfully")
	}

	// 5. Initialize Activity Engine
	activityEngine := activity.NewEngine(db, cfg.EncryptionKey)

	// 6. Initialize Telegram Bot
	bot, err := telegram.NewBot(cfg, db, systemNavClient, activityEngine)
	if err != nil {
		slog.Error("Failed to initialize Telegram bot", "error", err)
		os.Exit(1)
	}

	// 7. Initialize Scheduler
	sched, err := scheduler.NewScheduler(cfg.Timezone)
	if err != nil {
		slog.Error("Failed to initialize scheduler", "error", err)
		os.Exit(1)
	}

	// Helper for the daily check job
	runDailyJob := func() {
		slog.Info("Starting scheduled daily check...")

		// 1. New Albums (Last 24h)
		slog.Info("Checking for new albums...")
		newAlbums, err := navidrome.GetNewAlbums(systemNavClient, cfg.CacheFile, cfg.ScanMetaFile, 24, false)
		if err != nil {
			slog.Error("Error checking new albums", "error", err)
		} else if len(newAlbums) > 0 {
			slog.Info("Found new albums", "count", len(newAlbums))
			msg := telegram.FormatAlbumList(newAlbums, "🆕 Freshly Added Albums (Last 24h)")
			if msg != "" {
				bot.SendNotification(context.Background(), msg, cfg.TopicRecommendations)
			}
		} else {
			slog.Info("No new albums found.")
		}

		// 2. Anniversaries (Same Day, Same Month)
		slog.Info("Checking for anniversaries...")
		now := time.Now()
		anniversaries, err := navidrome.GetAnniversaryAlbums(systemNavClient, cfg.CacheFile, cfg.ScanMetaFile, now.Day(), int(now.Month()), false)
		if err != nil {
			slog.Error("Error checking anniversaries", "error", err)
		} else if len(anniversaries) > 0 {
			slog.Info("Found anniversaries", "count", len(anniversaries))
			msg := telegram.FormatAlbumList(anniversaries, fmt.Sprintf("🎂 On this day (%s) in music history", now.Format("January 02")))
			if msg != "" {
				bot.SendNotification(context.Background(), msg, cfg.TopicRecommendations)
			}
		} else {
			slog.Info("No anniversaries found.")
		}

		slog.Info("Daily check completed.")
	}

	// Helper for the inactive user purge job
	runPurgeJob := func() {
		slog.Info("Starting scheduled inactive user purge...")
		purged, err := activityEngine.PurgeInactiveUsers(systemNavClient, 30)
		if err != nil {
			slog.Error("Error during user purge", "error", err)
		} else if len(purged) > 0 {
			slog.Info("Purged inactive users", "count", len(purged), "users", purged)
		} else {
			slog.Info("No inactive users to purge.")
		}
	}

	// 8. Schedule Jobs
	err = sched.ScheduleDailyJob(cfg.ScheduleTime, runDailyJob)
	if err != nil {
		slog.Error("Failed to schedule daily job", "error", err)
		os.Exit(1)
	}

	err = sched.SchedulePurgeJob(runPurgeJob)
	if err != nil {
		slog.Error("Failed to schedule purge job", "error", err)
		os.Exit(1)
	}

	// Start scheduler
	sched.Start()
	defer sched.Stop()

	// Optional: Run daily job once on startup
	if cfg.RunOnStartup {
		slog.Info("Running daily job on startup (RUN_ON_STARTUP=true)...")
		go runDailyJob()
	}

	// 9. Setup OS Signal Handling for Graceful Shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start bot polling loop in background
	go bot.Start(ctx)

	slog.Info("Navidrome Telegram Bot is up and running")

	// Block until signal received
	sig := <-sigChan
	slog.Info("Received shutdown signal, stopping services...", "signal", sig)

	cancel() // Cancel context to stop bot polling

	// Wait a moment for goroutines to clean up
	time.Sleep(1 * time.Second)
	slog.Info("Shutdown complete.")
}
