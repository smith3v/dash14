# dash14

## Project Overview

`dash14` is a local Go application for managing a volleyball match scoreboard overlay. It runs on the streaming laptop alongside OBS, persists state in SQLite, serves an HTML overlay over a local HTTP server, and exposes match control through a Telegram bot.

## Key Design Documents

- [docs/2026-03-09-dash14-design.md](fleet-file://29j7eoojmsah3h4rfre3/Users/gmogelashvili/projects/dash14/docs/2026-03-09-dash14-design.md?type=file&root=%252F) — Full design specification
- [AGENTS.md](fleet-file://29j7eoojmsah3h4rfre3/Users/gmogelashvili/projects/dash14/AGENTS.md?type=file&root=%252F) — Repository guidelines
- [overlay-example.html](fleet-file://29j7eoojmsah3h4rfre3/Users/gmogelashvili/projects/dash14/overlay-example.html?type=file&root=%252F) — OBS overlay UI reference

## Runtime

```
dash14 --config config.yaml              # normal runtime
dash14 --config config.yaml --import teams.yaml  # import teams and exit
```

## Package Layout

| Package     | Responsibility                                      |
|-------------|-----------------------------------------------------|
| `config`    | YAML loading and validation                         |
| `logging`   | Logger construction and shared logging setup        |
| `storage`   | GORM models, migrations, and persistence access     |
| `game`      | Match rules, state transitions, and scoring logic   |
| `telegram`  | Bot handlers, admin UI, authorization, broadcasts   |
| `overlay`   | Template loading, HTTP handlers, overlay behavior   |
| `importer`  | YAML team import and logo file management           |

## Core Entities (SQLite)

- `teams` — known teams available for match selection
- `games` — one match and its aggregate state
- `game_sets` — per-set score data and active set state
- `users` — Telegram users known to the bot
- `app_state` — singleton state (current_game_id)

## Match Rules

- Best-of-5 volleyball match; first to 3 sets wins
- Sets 1–4: first to 25 points, lead by ≥ 2
- Set 5: first to 15 points, lead by ≥ 2; sides switch when either team reaches 8 (applied at most once)
- App never auto-finishes a set or match — admin confirmation required

## Overlay Endpoints

- `GET /overlay` — current overlay page for OBS (planned or live template)
- `GET /api/overlay` — current overlay state as JSON

## Commands

| Command     | Who     | Effect                                         |
|-------------|---------|------------------------------------------------|
| `/start`    | anyone  | Subscribe to match updates                     |
| `/stop`     | anyone  | Unsubscribe from match updates                 |
| `/plan`     | admin   | Wizard to create a new planned game            |
| `/game`     | admin   | Open or refresh the inline game control thread |
| `/takeover` | admin   | Transfer game administration to yourself       |

## Development Notes

- Go 1.26+; use `go fmt` / `goimports` before committing
- Run `go test ./...` before pushing; use `-cover` for new features
- Table-driven tests in `_test.go` files colocated with the code
- Never commit real tokens or DB passwords; use `config.example.yaml` as template
- Config and logging patterns should follow `easy-recall-bot` (adapted to YAML)
