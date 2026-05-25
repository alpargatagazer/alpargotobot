// Package config handles loading configuration values from environment variables
// and Docker secrets, initializing paths, and decoding encryption keys.
package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds all configuration parameters for the application.
type Config struct {
	NavidromeURL      string   // URL of the Navidrome server (e.g. http://localhost:4533)
	NavidromeUser     string   // Default admin/user name for Navidrome API connections
	NavidromePassword string   // Password for Navidrome API connections
	TelegramToken     string   // Telegram Bot API Token from BotFather
	TelegramChatIDs   []string // List of Telegram chat IDs authorized to interact with the bot
	EncryptionKey     []byte   // 32-byte key used to encrypt/decrypt user credentials in the DB
	ScheduleTime      string   // Time of day to run the scheduled sync (e.g. "08:00")
	LogLevel          string   // Log level (DEBUG, INFO, WARN, ERROR)
	RunOnStartup      bool     // If true, trigger sync/scans immediately on startup
	Timezone          string   // Timezone database name for the scheduler (e.g. "Europe/Madrid")
	APIVersion        string   // Subsonic API version to report (e.g. "1.16.1")
	MusicFolderName   string   // Name of the music folder to sync
	DataDir           string   // Path to store application data files (SQLite, cache)
	DBPath            string   // Absolute path to the SQLite database
	CacheFile         string   // Absolute path to the JSON cache of albums
	ScanMetaFile      string   // Absolute path to the JSON scan status file
}

// GetSecret reads a secret from Docker secrets location (/run/secrets/<secret_name>)
// or falls back to an uppercase environment variable.
func GetSecret(secretName string, defaultValue string) string {
	secretPath := filepath.Join("/run/secrets", secretName)
	data, err := os.ReadFile(secretPath)
	if err == nil {
		return strings.TrimSpace(string(data))
	}

	envName := strings.ToUpper(secretName)
	if val, ok := os.LookupEnv(envName); ok {
		return strings.TrimSpace(val)
	}

	return defaultValue
}

// LoadConfig initializes the Config struct from Docker secrets and environment variables.
func LoadConfig() (*Config, error) {
	dataDir := GetSecret("data_dir", "/app/data")

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory %s: %w", dataDir, err)
	}

	// Load and decode encryption key
	keyHex := GetSecret("credentials_encryption_key", "")
	if keyHex == "" {
		return nil, fmt.Errorf("credentials_encryption_key not found in secrets or environment")
	}
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode credentials_encryption_key hex: %w", err)
	}
	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("credentials_encryption_key must be exactly 32 bytes (64 hex characters), got %d bytes", len(keyBytes))
	}

	// Parse Telegram Chat IDs
	chatIDSecret := GetSecret("telegram_chat_id", "")
	if chatIDSecret == "" {
		// Try plural variant
		chatIDSecret = GetSecret("telegram_chat_ids", "")
	}
	var chatIDs []string
	if chatIDSecret != "" {
		parts := strings.Split(chatIDSecret, ",")
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				chatIDs = append(chatIDs, trimmed)
			}
		}
	}

	runOnStartupStr := strings.ToLower(GetSecret("run_on_startup", "false"))
	runOnStartup := runOnStartupStr == "true" || runOnStartupStr == "1"

	return &Config{
		NavidromeURL:      GetSecret("navidrome_url", ""),
		NavidromeUser:     GetSecret("navidrome_user", ""),
		NavidromePassword: GetSecret("navidrome_password", ""),
		TelegramToken:     GetSecret("telegram_token", ""),
		TelegramChatIDs:   chatIDs,
		EncryptionKey:     keyBytes,
		ScheduleTime:      GetSecret("schedule_time", "08:00"),
		LogLevel:          GetSecret("logging", "INFO"),
		RunOnStartup:      runOnStartup,
		Timezone:          GetSecret("timezone", "UTC"),
		APIVersion:        GetSecret("navidrome_api_version", "1.16.1"),
		MusicFolderName:   GetSecret("navidrome_music_folder", "Music Library"),
		DataDir:           dataDir,
		DBPath:            filepath.Join(dataDir, "credentials.db"),
		CacheFile:         filepath.Join(dataDir, "albums_cache.json"),
		ScanMetaFile:      filepath.Join(dataDir, "scan_status.json"),
	}, nil
}
