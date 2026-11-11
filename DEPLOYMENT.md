# Docker deployment

The production stack in `docker-compose.yml` runs Dash14 and Cloudflare Tunnel.
Set `DASH14_IMAGE` in the ignored `.env` file to pin the Dash14 release.
GitHub Actions publishes `linux/amd64` and `linux/arm64` images as
`ghcr.io/smith3v/dash14:<tag>`. The standalone `docker-compose.dev.yml` builds
from source for local testing.

## Runtime Contract

- The app reads `./deploy/config/config.container.yaml` at `/config/config.yaml`.
  Its four overlay templates are baked into the image under `/app/templates`.
- The image runs as the `dash14` user. Give that user write access to the
  mounted runtime directories.

  | Host path | Container path | Contents |
  | --- | --- | --- |
  | `./runtime/data` | `/data` | SQLite database and managed team logos |
  | `./runtime/out` | `/out` | Static overlay HTML and logo exports |
  | `./runtime/logs` | `/logs` | App logs when file logging is configured |

- The Go server binds to `0.0.0.0:8081` inside the app container. It serves
  `/`, `/overlay.html`, `/intermission.html`, team logos, the updater, and
  `GET /api/v1/game`. Use the HTTP URLs in OBS for live updates.
- Compose publishes the app at `http://127.0.0.1:8080` on the Docker host.
  Cloudflared uses `http://app:8081` over the Compose network and exposes the
  same routes on the configured public HTTPS hostname.

## Production setup

From the repository root, create the local deployment files.

```sh
cp .env.example .env
cp deploy/config/config.container.example.yaml deploy/config/config.container.yaml
cp deploy/cloudflared/config.yml.example deploy/cloudflared/config.yml
mkdir -p runtime/data runtime/out runtime/logs
```

1. Pin a release image in `.env`. Set the production Telegram token in the
   mounted app config and keep `http.listen_address: "0.0.0.0:8081"`.
2. Set the tunnel ID and public hostname in `deploy/cloudflared/config.yml`.
   Place the tunnel credentials JSON at `deploy/cloudflared/credentials.json`.
   For a new locally managed tunnel, use `cloudflared tunnel login`,
   `cloudflared tunnel create <name>`, and
   `cloudflared tunnel route dns <name> <overlay-hostname>`.
3. Start the stack and inspect both services.

   ```sh
   docker compose config --quiet
   docker compose pull
   docker compose up -d
   docker compose ps
   docker compose logs app cloudflared
   ```

On Synology Container Manager, point the project at the repository checkout
and use the same `docker-compose.yml`.

## Verify the deployment

```sh
curl -fsS http://127.0.0.1:8080/overlay.html
curl -fsS http://127.0.0.1:8080/intermission.html
curl -fsS http://127.0.0.1:8080/api/v1/game
```

Repeat the checks on the public HTTPS hostname. Check that the pages and API
send `Cache-Control: no-store` and that custom Cloudflare cache rules honor it.
A fresh database serves transparent overlay pages and reports `game: null`
until a match is planned.

## Upgrade from the Nginx stack

1. Record the current checkout and image tag. Back up the database using
   SQLite's backup command or with the app stopped. Also back up the managed
   logos and the mounted app and tunnel configs.
2. Stop the legacy `nginx` service with `docker compose stop nginx` from the
   previous checkout to free host port `8080`.
3. Switch the checkout to the release containing the HTTP overlay and pin its
   image tag in `.env`. Add `http.listen_address: "0.0.0.0:8081"` to the mounted
   app config, keeping the existing runtime directories.
4. Set the overlay hostname's service in the mounted tunnel config to
   `http://app:8081`. Keep the existing hostname, tunnel ID, credentials path,
   and catch-all rule.
5. Run `docker compose config --quiet`, `docker compose pull app`, and
   `docker compose up -d --remove-orphans --force-recreate app cloudflared`.
   Verify the deployment and refresh the existing OBS sources.

To roll back, stop the new stack and restore the prior checkout, image pin,
and both mounted configs. The legacy image expects its original app config.
Run `docker compose up -d --remove-orphans` from the restored checkout, verify
both overlay URLs, and refresh the OBS sources.

## Production team import

Run the existing one-shot import against the mounted production data.

```sh
docker compose run --rm -w /workspace -v "$PWD:/workspace:ro" app \
  --config /config/config.yaml --import /workspace/teams/teams.yaml
```

The SQLite database and imported logos are under `runtime/data`. Static HTML
exports are under `runtime/out`. Use the HTTP URLs for live updates.

## Local testing with a Telegram test bot

From the repository root, create a local config and writable runtime paths.
If the example file is missing, check out the branch or release containing
the HTTP overlay update.

```sh
cp deploy/config/config.dev.example.yaml deploy/config/config.dev.yaml
mkdir -p runtime/dev/data runtime/dev/out
export DASH14_DEV_UID="$(id -u)" DASH14_DEV_GID="$(id -g)"
```

Set `telegram.token` in the ignored dev config to a dedicated test bot's token.
Use a fresh bot so only the test instance receives its updates. Keep
`http.listen_address: "0.0.0.0:8081"` in the dev config. The container listens
on port `8081` and Compose publishes it on host loopback port `8080`. Stop any
other local stack using host port `8080`.

```sh
make compose-dev-up
docker compose -f docker-compose.dev.yml run --rm app \
  --config /config/config.yaml --import /workspace/teams/teams.yaml
```

The one-shot import initializes the SQLite schema if needed and loads the
team catalog and logos into `runtime/dev/data`. Run it again when updating the
catalog. Verify the import before planning a match.

```sh
sqlite3 -readonly runtime/dev/data/dash14.db 'SELECT count(*) FROM teams;'
```

Send `/start` to the test bot in a private chat. List registered users and grant
your Telegram ID admin rights in the isolated database.

```sh
sqlite3 runtime/dev/data/dash14.db 'SELECT telegram_user_id, is_admin FROM users;'
sqlite3 runtime/dev/data/dash14.db \
  'UPDATE users SET is_admin = 1 WHERE telegram_user_id = 123456789;'
```

1. Open `http://127.0.0.1:8080/overlay.html` and
   `http://127.0.0.1:8080/intermission.html` in a browser and add the same URLs as
   OBS Browser Sources. On a fresh database, both pages are transparent before
   the first match and the API reports `game: null`.
2. Use `/plan` to select two teams, then `/game` to open the controls. Start the
   game, add a point to each side, and undo back to 0–0. Both pages should
   update within about two seconds and keep the same document. The standalone
   history row appears on the first point and disappears when both scores
   return to zero.
3. Finish a set, start the next one, and complete the match. In browser DevTools,
   preserve the Network log and filter for Document requests. The main overlay
   should navigate once on each layout change. The standalone intermission page
   should keep its document and display the latest set history. Check the same
   transitions visually in OBS.
4. Edit the active template. The dev stack checks templates every second, and
   the affected page adopts the change on its next poll. Add `?updates=off` to
   freeze a page for inspection. Rebuild with `make compose-dev-up` after editing
   Go or the embedded updater.

`make compose-dev-logs` follows the app logs. `make compose-dev-down` stops the
stack and preserves `runtime/dev`.
