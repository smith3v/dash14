# Docker deployment

The production Compose stack runs the Dash14 image from GHCR and a Cloudflare
Tunnel. Go serves the overlay pages, updater, team logos, and read-only game API.
Cloudflared connects to `http://app:8081` over the Compose network. The NAS also
has a loopback endpoint at `http://127.0.0.1:8080` for smoke checks.

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

## Synology rollout

The production stack uses `docker-compose.yml` and a tag in the ignored
`.env`. GitHub Actions publishes tagged `linux/amd64` and `linux/arm64` images
to `ghcr.io/smith3v/dash14:<tag>`.

1. Record the current checkout and `DASH14_IMAGE` tag. Back up the SQLite
   database using SQLite's backup command or a stopped app, the managed logos,
   and the mounted app and tunnel configs. Keep the tunnel credentials private.
2. Before switching the checkout, stop the legacy `nginx` service with
   `docker compose stop nginx` to free loopback port 8080.
3. Switch the deployment checkout to the release containing this change and pin
   its image tag in `.env`. In the mounted
   `deploy/config/config.container.yaml`, add `http.listen_address` with value
   `0.0.0.0:8081` as shown in the tracked example. Keep the existing mounted
   data, output, and log directories. A loopback bind inside the app container
   would make it unreachable from Cloudflared and Docker's host port mapping.
4. In the mounted `deploy/cloudflared/config.yml`, set the overlay hostname's
   service to `http://app:8081`. Keep the hostname, tunnel ID, credentials file,
   and catch-all rule. New installations can start from the tracked examples.
5. Run `docker compose config --quiet`, `docker compose pull app`, then
   `docker compose up -d --remove-orphans --force-recreate app cloudflared`.
6. Check `docker compose ps` and `docker compose logs app cloudflared`. Check
   `/`, `/overlay.html`, `/intermission.html`, and `/api/v1/game` on loopback
   port 8080 and on the public HTTPS hostname. Verify the HTML and API carry
   `Cache-Control: no-store` and bypass any custom Cloudflare cache rules that
   would cache those routes. Refresh the existing OBS sources after cutover.

To roll back, stop the new stack, restore the prior checkout and image pin, and
restore both mounted configs. The old Go image requires the prior app config
without the new HTTP key. Run `docker compose up -d --remove-orphans` from the
restored checkout, verify the two overlay URLs, and refresh both OBS sources.

## Fresh Synology setup

Copy `.env.example` to `.env` and pin the release image. Copy
`deploy/config/config.container.example.yaml` to
`deploy/config/config.container.yaml` and set the production Telegram token.
Create `runtime/data`, `runtime/out`, and `runtime/logs` with write access for
the image's `dash14` user.

Copy `deploy/cloudflared/config.yml.example` to `deploy/cloudflared/config.yml`.
Set the tunnel ID and hostname, and put that tunnel's credentials JSON at
`deploy/cloudflared/credentials.json`. For a new locally managed tunnel, use
`cloudflared tunnel login`, `cloudflared tunnel create <name>`, and
`cloudflared tunnel route dns <name> <overlay-hostname>`. In Synology Container
Manager, point the project at this checkout and select `docker-compose.yml`.
Start it and run the smoke checks above.

## Production team import

Run the existing one-shot import against the mounted production data.

```sh
docker compose run --rm -w /workspace -v "$PWD:/workspace:ro" app \
  --config /config/config.yaml --import /workspace/teams/teams.yaml
```

The SQLite database and imported logos are under `runtime/data`. Static HTML
exports are under `runtime/out`. Use the HTTP URLs for live updates.
