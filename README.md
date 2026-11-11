# dash14

`dash14` is a local Go application for volleyball match control during streams.
It stores match state in SQLite, serves OBS-ready overlays over HTTP, and exposes control through a Telegram bot.

## Features

- Single active match per device (`planned`, `set_in_progress`, `between_sets`, `finished`)
- Team import from YAML with logo copy into a managed local directory
- OBS overlay rendering for planned, live, between-set intermission, and finished states
- Separate intermission scoreboard page with per-set history for compatibility OBS setups
- Telegram subscriber flow (`/start`, `/stop`)
- Admin-only game planning and control (`/plan`, `/game`, `/takeover`)
- Read-only broadcast updates to subscribed users for game start, score changes, next-set start, and match finish
- Persistent state in SQLite

## Requirements

- Go `1.27.3`
- Telegram bot token (from BotFather)
- Local filesystem access for:
  - SQLite DB file
  - templates
  - generated overlay file
  - team logos directory

## Installation

### 1. Clone and enter the repository

```bash
git clone <your-repo-url>
cd dash14
```

### 2. Install dependencies

```bash
go mod tidy
```

### 3. Build the binary

```bash
go build -o dash14 ./cmd/dash14
```

## Configuration

Copy `config.example.yaml` to `config.yaml`:

```bash
cp config.example.yaml config.yaml
```

Then edit `config.yaml`.

Important: current template files in this repository are:

- `templates/planned.html.tmpl`
- `templates/live.html.tmpl`
- `templates/intermission.html.tmpl`
- `templates/finished.html.tmpl`

So update overlay template paths accordingly.

### Minimal working config example

```yaml
http:
  listen_address: "127.0.0.1:8081"

telegram:
  token: "123456789:your-real-bot-token"

sqlite:
  path: "runtime/data/dash14.db"

overlay:
  planned_template_path: "templates/planned.html.tmpl"
  live_template_path: "templates/live.html.tmpl"
  intermission_template_path: "templates/intermission.html.tmpl"
  finished_template_path: "templates/finished.html.tmpl"
  template_cache_refresh_interval_seconds: 0
  output_path: "runtime/out/overlay.html"
  logo_dir: "runtime/data/logos"

logging:
  level: "info"
  file_path: "runtime/logs/dash14.log"
```

`http.listen_address` controls where the overlay HTTP server binds. Existing
configs that omit it default to `127.0.0.1:8081`, reachable only from the machine
running Dash14. If you set a different port, use it in your native OBS URLs.
The container examples bind to `0.0.0.0:8081` inside Docker. Compose maps that
port to `127.0.0.1:8080` on the host.

## Deployment

See [local Docker testing](DEPLOYMENT.md#local-testing-with-a-telegram-test-bot)
for the test bot, database initialization, and OBS checks. See the
[Synology rollout](DEPLOYMENT.md#synology-rollout) for the image-based Compose
stack and Cloudflare Tunnel.

## Team Import File Format

Use a YAML list of team records.

Required field:

- `key`

Supported fields:

- `key`
- `name`
- `short_name`
- `logo`
- `aliases`

Unknown fields are ignored.

Example:

```yaml
- key: lokomotiv
  name: Lokomotiv Novosibirsk
  short_name: LOK
  logo: logos/lokomotiv.png
  aliases:
    - Loko
    - Lokomotiv

- key: zenit
  name: Zenit Saint Petersburg
  short_name: ZEN
  logo: logos/zenit.png
  aliases:
    - Zenit SPb
```

## Usage

`dash14` supports exactly two startup forms.

### 1. Import mode (teams + logos, then exit)

```bash
./dash14 --config config.yaml --import teams/teams.yaml
```

What it does:

- loads config
- initializes logging
- opens SQLite and runs migrations
- imports/upserts teams
- copies logos to `overlay.logo_dir`
- exits after import

### 2. Runtime mode (normal operation)

```bash
./dash14 --config config.yaml
```

What it does:

- loads config
- initializes logging
- opens SQLite and runs migrations
- validates template paths
- publishes the active match or most recent finished match, with a transparent overlay before the first match
- serves the overlay pages, team logos, updater, and read-only `GET /api/v1/game` on `http.listen_address`
- starts optional template refresh polling when `overlay.template_cache_refresh_interval_seconds > 0`
- starts Telegram update polling and blocks

## Telegram Workflow

### Subscriber commands

- `/start` subscribes user to broadcast updates
- `/stop` unsubscribes user from updates

### Admin commands

- `/plan` starts match planning wizard:
  - choose home team (search)
  - choose guest team (search)
  - creates `planned` game
  - sets current game admin
  - renders planned overlay and intermission page
- `/game` opens/refreshes control message for current admin
- `/takeover` transfers admin ownership of current game

### Control message actions

- start planned game
- `+1` / `-1` home score
- `+1` / `-1` guest score
- confirm set finish when finishable
- start next set after intermission
- confirm game finish when eligible
- reverse overlay sides

## Admin Setup

Admin rights are stored in SQLite (`users.is_admin`) and are currently managed manually.

Example SQL (run after user has appeared via `/start`):

```sql
UPDATE users
SET is_admin = 1
WHERE telegram_user_id = 123456789;
```

## OBS Setup

1. Start Dash14.
2. Add a **Browser Source** in URL mode for the main overlay. Use `http://127.0.0.1:8081/overlay.html` for a native run, port `8080` for local Docker, or your published HTTPS hostname.
3. For a separate break screen, use `/intermission.html` on the same origin.
4. Score updates appear in place within about two seconds on a healthy
   connection. The main source reloads when switching layouts. The standalone
   intermission scoreboard stays loaded across match phases.

## Development

### Run tests

```bash
go test ./...
```

### Run package-specific tests

```bash
go test ./pkg/telegram -run Plan
go test ./pkg/telegram -run GameControl
go test ./pkg/telegram -run Takeover
go test ./pkg/storage -run 'Game|AppState'
go test ./pkg/game
go test ./pkg/overlay
```

### Format

```bash
gofmt -w ./...
```

## Troubleshooting

- `--config is required`: provide `--config config.yaml`.
- Template validation fails at startup:
  - verify `overlay.planned_template_path`, `overlay.live_template_path`, `overlay.intermission_template_path`, and `overlay.finished_template_path`
  - ensure files exist and are readable.
- Template edits are not picked up:
  - by default templates are cached in memory for the lifetime of the process
  - set `overlay.template_cache_refresh_interval_seconds` to a positive number to enable periodic reloads
- Bot does not respond:
  - verify `telegram.token`
  - check network connectivity and logs.
- `/plan` rejected with existing game message:
  - only one non-finished game is allowed at a time.
- Overlay URL unreachable
  - check the app log for the bound address or a port conflict.
  - use port `8081` for a default native run and `8080` for Docker on the same host.
- Overlay not updating in OBS
  - use the overlay HTTP URL in OBS and inspect `/api/v1/game` on the same origin.
  - remove `?updates=off` from the URL to resume polling.
- Intermission screen not updating:
  - ensure OBS uses the `/intermission.html` URL
  - verify `overlay.intermission_template_path` exists and is readable.

## Security Notes

- Do not commit real bot tokens or local secrets.
- Keep `config.yaml` local (it is git-ignored).
- Rotate credentials immediately if leaked.
