# Alpargotobot: Navidrome Telegram Bot

Alpargotobot is a Telegram bot for a Navidrome community server. Users can search for music, get random recommendations from each other's favorites, explore by genre and year, see who is listening, and submit issues and improvements — all from within Telegram.

This is the Go port of the original Python bot, featuring significantly lower memory usage, faster execution, native concurrency, and robust Docker integration.

## Features

- **Search** — Find artists and albums instantly.
- **Discover** — Random albums, recent additions, and anniversary releases.
- **Recommendations** — Pull random albums/songs/artists from other users' Navidrome favorites.
- **Now Playing** — See who is currently listening to what.
- **Scheduler** — Automatically posts daily new releases and anniversaries to the right subchannel, and purges inactive users nightly.
- **Ticket System** — Users can file issues and improvement requests in a dedicated subchannel, and mark them done with a single button press.
- **Topics Support** — Commands and scheduled messages are automatically routed to their correct Telegram subchannels.
- **Security** — AES-256-GCM encrypted local credential storage, fully compatible with the legacy Python database.

## Prerequisites

- **Docker** and **Docker Compose**
- A **Navidrome** server
- A **Telegram Bot Token** (from BotFather)
- A **Telegram Group/Chat ID** (your authorized group)

## Setup and Configuration

### 1. Secrets

Create a `secrets/` directory containing the following plain-text files (no trailing newlines):

| File | Description |
|------|-------------|
| `navidrome_url.txt` | Your Navidrome base URL, e.g. `https://navidrome.example.com` |
| `navidrome_user.txt` | An admin/service user for the bot |
| `navidrome_password.txt` | Password for that user |
| `telegram_bot_token.txt` | Your bot token from BotFather |
| `telegram_chat_id.txt` | Authorized group chat ID, e.g. `-100123456789` |
| `credentials_encryption_key.txt` | 64 hex characters (32 bytes) for AES-256-GCM |

### 2. Environment Variables (`.env`)

Copy `.env.example` to `.env` and adjust values as needed. This file is automatically loaded by Docker Compose.

```bash
cp .env.example .env
```

### 3. Telegram Topics (Subchannels) — Optional

If your group uses Telegram Topics, you can restrict commands and scheduled messages to specific subchannels.

**How to find a Thread ID**: Open a message in a topic, copy the link. In a link like `https://t.me/c/123456789/45/67`, the Thread ID is `45`.

Set these in `.env`:

| Variable | Subchannel |
|----------|-----------|
| `TOPIC_GENERAL` | General (usually `1`) |
| `TOPIC_ISSUES` | Issues & Improvements |
| `TOPIC_RECOMMENDATIONS` | Recommendations & Recents |

If all values are `0` or missing, the bot works normally in every channel.

**Routing rules:**

| Command | Allowed in |
|---------|-----------|
| `/help`, `/stats`, `/search` | All channels |
| `/year`, `/genres`, `/nowplaying` | All except Issues |
| `/recent`, `/random`, `/recommend` | Recommendations only |
| `/ticket`, `/tickets`, `/done` | Issues only |
| Scheduled new albums | → Recommendations |
| Scheduled anniversaries | → Recommendations |

## Commands

| Command | Description |
|---------|-------------|
| `/search <text>` | Search for an artist or album |
| `/year <year\|decade>` | Albums from a specific year or decade (e.g. `1994`, `90s`) |
| `/random` | Suggest a random album |
| `/recent` | Recently added albums (last 30 days) |
| `/nowplaying` | See who is currently listening |
| `/genres` | Browse albums by genre |
| `/recommend` | Get a random album/song/artist from another user's favorites |
| `/stats` | Server statistics |
| `/help` | Show command list |
| `/ticket` *(issues channel)* | Submit a new issue or improvement |
| `/tickets` *(issues channel)* | List all open tickets |
| `/done [id]` *(issues channel)* | Mark a ticket as resolved (shows picker if no ID given) |
| `/login` *(DM only)* | Store your Navidrome credentials for the bot to sync your favorites |

## Running Locally

```bash
make dev-up       # Build and start (hot-reload via air)
make dev-logs     # Follow container logs
make dev-down     # Stop containers
make test         # Run Go test suite
make lint         # Run golangci-lint
make help         # Show all available make targets
```

## Deployment

The bot is containerized and published to GHCR automatically by CI.

1. Pull the latest image and start it:
```bash
docker compose -f docker/docker-compose.prod.yml up -d
```
2. Ensure `secrets/` and `.env` are present alongside the compose file.

## CI/CD and Versioning

GitHub Actions runs on every push and PR:
- Tests, linting, and Trivy security scans.
- Versioning is **Git Tag based** (bumped automatically from commit messages).
  - Default: patch bump (`v1.0.0` → `v1.0.1`)
  - Add `#minor` or `#major` to the commit message to override.
- A GitHub Release and GHCR Docker image are created automatically on merge to `main`.
