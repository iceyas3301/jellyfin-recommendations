# Jellyfin Recommendations

A lightweight service that turns Jellyfin user favorites into server-wide recommendations by hijacking the Collections feature.

Each user with favorites appears as a named Collection containing their favorited items. Works on all native Jellyfin clients since it uses existing Collections UI.

## Guardrails & Improvements

This fork adds proper production guardrails over the upstream:

### Auth
- **Fixed auth header**: Uses `Authorization: MediaBrowser Token="..."` (Jellyfin 10.11+ compatible). Upstream used deprecated `X-Emby-Token`.

### Configuration
All behavior is configurable via environment variables:

| Variable | Default | Description |
|---|---|---|
| `JELLYFIN_URL` | *required* | Jellyfin server URL |
| `JELLYFIN_API_KEY` | *required* | API key (also accepts `API_KEY` for backward compat) |
| `SYNC_INTERVAL` | `20m` | How often to poll for changes |
| `COLLECTION_PREFIX` | *(none)* | Prefix for collection names |
| `EXCLUDE_USERS` | *(none)* | Comma-separated user names or IDs to skip |
| `DRY_RUN` | `false` | Log planned changes without applying |
| `HTTP_TIMEOUT` | `30s` | HTTP request timeout |
| `RETRY_MAX_ATTEMPTS` | `3` | Retry attempts for failed HTTP calls |
| `RETRY_BASE_DELAY` | `2s` | Base delay for retry backoff |
| `DEBUG` | `false` | Enable verbose debug logging |

### Safety Features

- **Dry run mode**: Set `DRY_RUN=true` to preview changes without touching anything
- **User exclusion**: `EXCLUDE_USERS=admin,serviceaccount` prevents collections for specific users
- **Collection namespacing**: `COLLECTION_PREFIX="★ "` prevents conflicts with real collections
- **Graceful shutdown**: Handles SIGINT/SIGTERM cleanly
- **HTTP timeouts**: All requests have configurable timeouts
- **Exponential backoff**: Failed HTTP calls retry with backoff
- **Thread safety**: Lock held only during in-memory state updates, not during HTTP calls
- **Poll-based reconcile**: one periodic pass per `SYNC_INTERVAL` (see below for why there is no WebSocket listener)
- **Context cancellation**: All operations respect context for clean shutdown

## Deployment

### Docker (recommended)

```bash
docker run -d \
  --name jellyfin-recommendations \
  --restart unless-stopped \
  -e JELLYFIN_URL="http://your-jellyfin:8096" \
  -e JELLYFIN_API_KEY="your-api-key" \
  -e COLLECTION_PREFIX="★ " \
  -e EXCLUDE_USERS="admin" \
  ghcr.io/amr-as90/jellyfin-recommendations:latest
```

### Docker Compose

`compose.yaml` reads machine-local values (including the API key) from an untracked
`.env` file in the same directory — never commit that file:

```bash
cat > .env <<'EOF'
JELLYFIN_URL=http://127.0.0.1:8096
JELLYFIN_API_KEY=your-api-key
SYNC_INTERVAL=4h
COLLECTION_PREFIX=Liked by
EXCLUDE_USERS=admin
EOF

docker compose up -d --build
docker compose config          # confirm the resolved environment before starting
```

`.env` is git-ignored; only `compose.yaml` (placeholders + safe defaults) is committed.

### First run

1. Set `DRY_RUN=true` and `DEBUG=true` to preview changes
2. Check logs: `docker logs jellyfin-recommendations`
3. Once satisfied, set `DRY_RUN=false` and restart

## How It Works

1. **Poll mode** (every `SYNC_INTERVAL`, default 20m): Fetches all users and their favorites, reconciles collections
   > `UserDataChanged` WebSocket events are deliberately **not** used on this fork: Jellyfin only routes them
   > to user-specific sessions, so a server-wide API-key listener never receives them. Polling is the reliable path.
3. **Collection sync**: For each user, creates/updates/deletes a Collection matching their current favorites
4. **Profile pictures**: Sets the user profile picture as the collection cover image
