# Deployment

`dash14` can be deployed as a three-service stack with:

- `app` running `dash14`
- `nginx` serving the generated overlay files
- `cloudflared` publishing the `nginx` endpoint on your domain

The stack definition lives in `docker-compose.yml`.

By default, the compose file builds the app image from the local checkout. Release images published by GitHub Actions are pushed to:

- `ghcr.io/smith3v/dash14:<git-tag>`

Examples:

- `ghcr.io/smith3v/dash14:v0.1.0`
- `ghcr.io/smith3v/dash14:v1.2.3`

## Runtime Contract

- `dash14` reads a mounted YAML config file.
- Template files are baked into the app image.
- SQLite, rendered overlay output files, copied logos, and logs live on mounted writable storage.
- `nginx` serves the generated overlay files.
- Cloudflare Tunnel proxies `nginx`, not the app process directly.

For image builds, Apple Silicon Docker hosts map to `linux/arm64`. Intel Synology NAS targets map to `linux/amd64`.

## Required Files

Create these local deployment files from the tracked examples:

```bash
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

## App Config

Edit `deploy/config/config.container.yaml` using `deploy/config/config.container.example.yaml` as the template for the container layout used by the compose stack.

These paths match the current image and compose wiring:

- `./runtime/data` -> `/data`
- `./runtime/out` -> `/out`
- `./runtime/logs` -> `/logs`
- templates are baked into the image at `/app/templates`

Set all three overlay template paths in `deploy/config/config.container.yaml`:

- `overlay.planned_template_path: /app/templates/planned.html.tmpl`
- `overlay.live_template_path: /app/templates/live.html.tmpl`
- `overlay.intermission_template_path: /app/templates/intermission.html.tmpl`

The app itself is not an HTTP server. It only writes files to `/out`, and `nginx` serves those files.

The generated pages are:

- `/out/overlay.html` for the main planned/live overlay
- `/out/intermission.html` for the pre-match and between-set scoreboard

## Cloudflare Config

Edit `deploy/cloudflared/config.yml`:

- set `tunnel` to your real tunnel ID
- keep `credentials-file` as `/etc/cloudflared/credentials.json`
- change `hostname` to the domain or subdomain that should serve the overlay

The tunnel should continue to point to:

```yaml
service: http://nginx:80
```

Create the Cloudflare DNS route for that hostname:

```bash
cloudflared tunnel route dns <tunnel-id> <overlay.example.com>
```

Replace `<tunnel-id>` and `<overlay.example.com>` with the values configured in `deploy/cloudflared/config.yml`.

## Start The Stack

Create the runtime directories once:

```bash
mkdir -p runtime/data runtime/out runtime/logs
```

If you want to deploy a published release image instead of building locally, edit `docker-compose.yml` and replace the `app` service `build:` section with an explicit image reference such as:

```yaml
image: ghcr.io/smith3v/dash14:v1.2.3
```

Then start the deployment:

```bash
docker compose up -d
```

`nginx` serves the generated overlay files on the local compose endpoint at:

- `http://127.0.0.1:8080/overlay.html`
- `http://127.0.0.1:8080/intermission.html`

Cloudflare Tunnel publishes those through the configured hostname.

## Verification

After bringing the stack up, run:

```bash
docker compose ps
docker compose logs app
docker compose logs nginx
docker compose logs cloudflared
curl http://127.0.0.1:8080/
curl http://127.0.0.1:8080/overlay.html
curl http://127.0.0.1:8080/intermission.html
```

For local validation of the deployment assets before rollout, run:

```bash
go test ./...
docker build -t dash14:dev .
docker compose config
docker run --rm -v "$PWD/deploy/nginx/default.conf:/etc/nginx/conf.d/default.conf:ro" nginx:stable nginx -t
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

Back up these paths on the NAS:

- `runtime/data/dash14.db` for SQLite state
- `runtime/data/logos/` for imported team logos
- `runtime/logs/` if you want to retain logs
- `deploy/cloudflared/credentials.json` and `deploy/cloudflared/config.yml` for tunnel access

If you move the deployment to another machine, those files are the state you need to bring with it.

## OBS Wiring

Use two OBS browser sources:

- `overlay.html` for normal match presentation
- `intermission.html` for pre-match and between-set scenes

Both pages are updated automatically when match state changes, including partial scores in the active set.
