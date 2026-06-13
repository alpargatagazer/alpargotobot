# Alpargotobot: Navidrome Telegram Bot

Alpargotobot is a Telegram bot designed to interact with a Navidrome community server. It allows users to search for music, get random recommendations from other users' favorites, explore by genres and years, and see what others are currently listening to.

This is the Go port of the original Python bot, featuring significantly lower memory usage, faster execution, native concurrency, and robust Docker integration.

## Features

- **Search**: Find artists and albums instantly.
- **Discover**: Random albums, recent additions, and anniversary releases.
- **Recommendations**: Pull random albums/songs from other users' favorites.
- **Now Playing**: See who is currently listening to what.
- **Scheduler**: Automatically posts daily new releases and anniversaries, and purges inactive users.
- **Security**: AES-256-GCM encrypted local credential storage to keep Navidrome passwords safe, fully compatible with the legacy Python database.

## Prerequisites

- **Docker** and **Docker Compose**
- A **Navidrome** server
- A **Telegram Bot Token** (from BotFather)
- A **Telegram Group/Chat ID** (the authorized group)

## Setup and Configuration

### Telegram Topics (Subchannels)
If your Telegram group uses Topics (subchannels), you can restrict commands and scheduled messages to specific topics.

To enable this, create a `.env` file in the root directory (you can copy `.env.example`).
You will need to manually find the Thread ID for each topic (it's the middle number in a message link, e.g., `https://t.me/c/123456789/45/67` -> Thread ID is `45`) and add it to your `.env` file:
- `TOPIC_GENERAL` (normally "1")
- `TOPIC_ISSUES`
- `TOPIC_RECOMMENDATIONS`

If you don't use topics, simply leave these at `0` or empty, and the bot will work in all channels.

### Secrets Configuration

Create a `secrets/` directory containing the following files (no newlines):

- `navidrome_url.txt` (e.g., `https://navidrome.example.com`)
- `navidrome_user.txt` (An admin user for the bot)
- `navidrome_password.txt`
- `telegram_bot_token.txt` (Your bot token)
- `telegram_chat_id.txt` (Your group chat ID, e.g., `-100123456789`)
- `credentials_encryption_key.txt` (Exactly 64 hex characters / 32 bytes for AES-256-GCM)

## Running Locally

To build and run the development environment using Docker Compose:

```bash
make dev-up
```

Other useful development commands:
- `make dev-logs` - Follow container logs
- `make dev-down` - Stop the development container
- `make test` - Run the Go test suite
- `make lint` - Run `golangci-lint`

## Deployment

The bot is containerized and published to GHCR. To deploy in production, use the `docker-compose.prod.yml` file.

1. Ensure your `secrets/` directory is present alongside the compose file.
2. Run:
```bash
docker-compose -f docker-compose.prod.yml up -d
```

## CI/CD and Versioning

This repository uses GitHub Actions for continuous integration.
- Every push and PR runs tests, linting, and Trivy security scans.
- Versioning is strictly **Git Tag based**.
- Pushing to `main` auto-bumps the patch version (e.g., `v1.0.0` -> `v1.0.1`).
- You can override this by adding `#minor` or `#major` to your commit message.
- A GitHub Release and GHCR Docker image will be automatically created.

## Architecture

See `LEARNING.md` for a detailed breakdown of the Go architecture, memory optimizations, concurrency model, and a comparison with the original Python codebase.
