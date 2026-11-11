# Deployment

`dash14` can be deployed as a two-service stack with:

- `app` running `dash14` and serving the overlays over HTTP
- `cloudflared` publishing the app endpoint on your domain

The canonical stack definition lives in `docker-compose.yml`. It is image-based so you can pin the deployed image tag in a local `.env` file.

Release images published by GitHub Actions are pushed to:

- `ghcr.io/smith3v/dash14:<git-tag>`

Examples:

- `ghcr.io/smith3v/dash14:v0.1.0`
- `ghcr.io/smith3v/dash14:v1.2.3`

## Runtime Contract

- `dash14` reads a mounted YAML config file.
- Template files are baked into the app image.
- SQLite, team logos, static overlay exports, and logs live on mounted writable storage.
- Go serves the overlay pages, logos, updater, and `GET /api/v1/game` on port `8081` in the app container.
- Compose publishes the app on host loopback at `127.0.0.1:8080`. Cloudflare Tunnel connects directly to `http://app:8081` over the Compose network.

Release images support `linux/arm64` and `linux/amd64`, including Apple Silicon Docker hosts and Intel Docker hosts.

## Required Files

Create these local deployment files from the tracked examples:

```bash
cp .env.example .env
cp deploy/config/config.container.example.yaml deploy/config/config.container.yaml
cp deploy/cloudflared/config.yml.example deploy/cloudflared/config.yml
```

You must also place your Cloudflare Tunnel credentials JSON at:

```bash
deploy/cloudflared/credentials.json
```

## Cloudflare Tunnel Credentials

To get the credentials file with the `cloudflared` CLI:

1. Authenticate with Cloudflare:

```bash
cloudflared tunnel login
```

2. Create the tunnel if you do not already have one:

```bash
cloudflared tunnel create dash14
```

3. List tunnels and note the tunnel ID:

```bash
cloudflared tunnel list
```

Cloudflare stores the generated credentials JSON under `~/.cloudflared/<tunnel-id>.json`. Copy that file into the deployment directory as:

```bash
cp ~/.cloudflared/<tunnel-id>.json deploy/cloudflared/credentials.json
```

If you already have an existing tunnel, you only need the matching credentials JSON file and tunnel ID.

## Compose Files

The repository now uses two compose entrypoints:

- `docker-compose.yml` is the tracked default for deployment. It expects `DASH14_IMAGE` to be set in `.env`.
- `docker-compose.dev.yml` is a standalone local-development stack that builds `dash14:local` from source.

The `.env` file is intentionally ignored so each machine can pin its own deployed tag without local edits to the tracked compose file.

Example `.env`:

```dotenv
DASH14_IMAGE=ghcr.io/smith3v/dash14:v1.2.3
```

On Synology NAS with Container Manager, point the project at the checked-out repository and use `docker-compose.yml` as the compose file it reads from disk. When you want to roll out a newer release, update `.env` instead of editing the compose file itself.

## App Config

Edit `deploy/config/config.container.yaml` using `deploy/config/config.container.example.yaml` as the template for the container layout used by the compose stack.

These paths match the current image and compose wiring:

- `./runtime/data` -> `/data`
- `./runtime/out` -> `/out`
- `./runtime/logs` -> `/logs`
- templates are baked into the image at `/app/templates`

Set `telegram.token` and keep `http.listen_address: "0.0.0.0:8081"` so Docker and Cloudflared can reach the app.

Set all four overlay template paths in `deploy/config/config.container.yaml`:

- `overlay.planned_template_path: /app/templates/planned.html.tmpl`
- `overlay.live_template_path: /app/templates/live.html.tmpl`
- `overlay.intermission_template_path: /app/templates/intermission.html.tmpl`
- `overlay.finished_template_path: /app/templates/finished.html.tmpl`

The generated files are static exports for inspection:

- `/out/overlay.html` for the main overlay
- `/out/intermission.html` for the standalone scoreboard

Use the HTTP URLs in OBS for live updates.

## Cloudflare Config

Edit `deploy/cloudflared/config.yml`:

- set `tunnel` to your real tunnel ID
- keep `credentials-file` as `/etc/cloudflared/credentials.json`
- change `hostname` to the domain or subdomain that should serve the overlay

The tunnel should point to the app:

```yaml
service: http://app:8081
```

Create the Cloudflare DNS route for that hostname:

```bash
cloudflared tunnel route dns <tunnel-id> <overlay.example.com>
```

Replace `<tunnel-id>` and `<overlay.example.com>` with the values configured in `deploy/cloudflared/config.yml`.

## Start The Stack

Create the runtime directories once and give the image's `dash14` user write access:

```bash
mkdir -p runtime/data runtime/out runtime/logs
```

Set the image tag in `.env`, then start the deployment:

```bash
docker compose up -d
```

For local development, see [Local testing with a Telegram test bot](#local-testing-with-a-telegram-test-bot).

The app serves the overlays on the local Compose endpoint at:

- `http://127.0.0.1:8080/overlay.html`
- `http://127.0.0.1:8080/intermission.html`

Cloudflare Tunnel publishes those through the configured hostname.

## Upgrade from the Nginx stack

1. Record the current checkout and image tag, and back up the runtime data and configs.
2. Run `docker compose stop nginx` from the previous checkout to free port `8080`.
3. Check out the new release, update the image tag in `.env`, add
   `http.listen_address: "0.0.0.0:8081"` to the mounted app config, and change the
   mounted tunnel origin to `http://app:8081`.
4. Run `docker compose pull app` and
   `docker compose up -d --remove-orphans --force-recreate app cloudflared`.
   Verify the deployment and refresh the OBS sources.

To roll back, stop the new stack, restore the previous checkout, image pin,
and both mounted configs, then run `docker compose up -d --remove-orphans`.

## Verification

After bringing the stack up, run:

```bash
docker compose ps
docker compose logs app
docker compose logs cloudflared
curl http://127.0.0.1:8080/
curl http://127.0.0.1:8080/overlay.html
curl http://127.0.0.1:8080/intermission.html
curl http://127.0.0.1:8080/api/v1/game
```

Repeat the HTTP checks on the public HTTPS hostname after starting the tunnel.

For local validation of the deployment assets before rollout, run:

```bash
go test ./...
docker compose config
docker compose -f docker-compose.dev.yml config
```

## Team Import In Containers

To import teams and logos with the containerized app, mount the source YAML and logo files into the app container and run the one-shot import mode:

```bash
docker compose run --rm \
  -w /workspace \
  -v "$PWD:/workspace:ro" \
  app \
  --config /config/config.yaml \
  --import /workspace/teams/teams.yaml
```

Imported logos are copied into `/data/logos`, which persists on the host under `runtime/data/logos`.

## Persistence And Backups

Back up these paths on the Docker host:

- `runtime/data/dash14.db` for SQLite state, using SQLite's backup command or with the app stopped
- `runtime/data/logos/` for imported team logos
- `runtime/logs/` if you want to retain logs
- `deploy/config/config.container.yaml` for the app configuration
- `deploy/cloudflared/credentials.json` and `deploy/cloudflared/config.yml` for tunnel access

If you move the deployment to another machine, those files are the state you need to bring with it.

## OBS Wiring

Use two OBS Browser Sources in URL mode:

- `overlay.html` for normal match presentation
- `intermission.html` for pre-match and between-set scenes

Both pages update scores in place, including partial scores in the active set. The main overlay reloads once when it switches layouts. The standalone intermission page keeps its document across match phases.

## Local testing with a Telegram test bot

From the repository root, prepare the ignored dev config and isolated runtime directories:

```bash
cp deploy/config/config.dev.example.yaml deploy/config/config.dev.yaml
mkdir -p runtime/dev/data runtime/dev/out
export DASH14_DEV_UID="$(id -u)" DASH14_DEV_GID="$(id -g)"
```

Set `telegram.token` in the dev config to your test bot's token. Free host
port `8080` if another local stack is using it, then start the app and import
the teams and logos:

```bash
make compose-dev-up
docker compose -f docker-compose.dev.yml run --rm app \
  --config /config/config.yaml --import /workspace/teams/teams.yaml
sqlite3 -readonly runtime/dev/data/dash14.db 'SELECT count(*) FROM teams;'
```

Send `/start` to the test bot, then grant your Telegram user admin rights in the dev database:

```bash
sqlite3 runtime/dev/data/dash14.db 'SELECT telegram_user_id FROM users;'
sqlite3 runtime/dev/data/dash14.db \
  'UPDATE users SET is_admin = 1 WHERE telegram_user_id = 123456789;'
```

Open both overlay URLs in a browser or OBS. Use `/plan` and `/game` to start
a match. Add and undo a point to check in-place updates, then finish a set
to check that only the main overlay reloads. The dev stack reloads edited
templates automatically. Add `?updates=off` to freeze an overlay for inspection.

`make compose-dev-logs` shows app logs. `make compose-dev-down` stops the stack
and preserves `runtime/dev`.
