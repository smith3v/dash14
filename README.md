# dash14

`dash14` is a local Go application for volleyball match control during streams.
It stores match state in SQLite, renders an OBS-ready HTML overlay file, and exposes control through a Telegram bot.

## Features

- Single active match per device (`planned`, `in_progress`, `finished`)
- Team import from YAML with logo copy into a managed local directory
- OBS overlay rendering for planned and live states (atomic file writes)
- Telegram subscriber flow (`/start`, `/stop`)
- Admin-only game planning and control (`/plan`, `/game`, `/takeover`)
- Read-only broadcast updates to subscribed users
- Persistent state in SQLite

## Requirements

- Go `1.26`
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

So update overlay template paths accordingly.

### Minimal working config example

```yaml
telegram:
  token: "123456789:your-real-bot-token"

sqlite:
  path: "runtime/data/dash14.db"

overlay:
  planned_template_path: "templates/planned.html.tmpl"
  live_template_path: "templates/live.html.tmpl"
  output_path: "runtime/out/overlay.html"
  logo_dir: "runtime/data/logos"

logging:
  level: "info"
  file_path: "runtime/logs/dash14.log"
```

## Deployment

For Docker Compose, `nginx`, and Cloudflare Tunnel setup, see `DEPLOYMENT.md`.

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
./dash14 --config config.yaml --import teams.yaml
```

What it does:

- loads config
- initializes logging
- opens SQLite and runs migrations
- imports/upserts teams
- copies logos to `overlay.logo_dir`
- exits without starting Telegram polling

### 2. Runtime mode (normal operation)

```bash
./dash14 --config config.yaml
```

What it does:

- loads config
- initializes logging
- opens SQLite and runs migrations
- validates template paths
- renders current overlay (if a current game exists)
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
  - renders planned overlay
- `/game` opens/refreshes control message for current admin
- `/takeover` transfers admin ownership of current game

### Control message actions

- start planned game
- `+1` / `-1` home score
- `+1` / `-1` guest score
- confirm set finish when finishable
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

1. Start `dash14` runtime.
2. In OBS, add a **Browser Source**.
3. Point it to the local generated overlay file, e.g. `runtime/out/overlay.html`.
4. Re-rendering happens after state changes.

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
  - verify `overlay.planned_template_path` and `overlay.live_template_path`
  - ensure files exist and are readable.
- Bot does not respond:
  - verify `telegram.token`
  - check network connectivity and logs.
- `/plan` rejected with existing game message:
  - only one non-finished game is allowed at a time.
- Overlay not updating in OBS:
  - confirm `overlay.output_path`
  - ensure OBS is reading the same file path.

## Security Notes

- Do not commit real bot tokens or local secrets.
- Keep `config.yaml` local (it is git-ignored).
- Rotate credentials immediately if leaked.
