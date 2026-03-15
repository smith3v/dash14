# Deployment

`dash14` can be deployed as a three-service stack with:

- `app` running `dash14`
- `nginx` serving the generated overlay files
- `cloudflared` publishing the `nginx` endpoint on your domain

The stack definition lives in [docker-compose.yml](/Users/neuron/dev/dash14/docker-compose.yml).

## Runtime Contract

- `dash14` reads a mounted YAML config file.
- Template files are baked into the app image.
- SQLite, rendered overlay output, copied logos, and logs live on mounted writable storage.
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

Edit `deploy/config/config.container.yaml` using [deploy/config/config.container.example.yaml](/Users/neuron/dev/dash14/deploy/config/config.container.example.yaml) as the template for the container layout used by the compose stack.

These paths match the current image and compose wiring:

- `./deploy/runtime/data` -> `/data`
- `./deploy/runtime/out` -> `/out`
- `./deploy/runtime/logs` -> `/logs`
- templates are baked into the image at `/app/templates`

The app itself is not an HTTP server. It only writes files to `/out`, and `nginx` serves those files.

## Cloudflare Config

Edit `deploy/cloudflared/config.yml`:

- set `tunnel` to your real tunnel ID
- keep `credentials-file` as `/etc/cloudflared/credentials.json`
- change `hostname` to the domain or subdomain that should serve the overlay

The tunnel should continue to point to:

```yaml
service: http://nginx:80
```

## Start The Stack

Create the runtime directories once:

```bash
mkdir -p deploy/runtime/data deploy/runtime/out deploy/runtime/logs
```

Then start the deployment:

```bash
docker compose up -d
```

`nginx` serves the generated overlay on the local compose endpoint at `http://127.0.0.1:8080/`, and Cloudflare Tunnel publishes that through the configured hostname.

## Team Import In Containers

To import teams and logos with the containerized app, mount the source YAML and logo files into the app container and run the one-shot import mode:

```bash
docker compose run --rm \
  -v "$PWD/teams:/import:ro" \
  app \
  --config /config/config.yaml \
  --import /import/teams.yaml
```

If the import YAML references logo files, keep those files inside the mounted `teams/` directory so the container can read them. Imported logos are copied into `/data/logos`, which persists on the host under `deploy/runtime/data/logos`.

## Persistence And Backups

Back up these paths on the NAS:

- `deploy/runtime/data/dash14.db` for SQLite state
- `deploy/runtime/data/logos/` for imported team logos
- `deploy/runtime/logs/` if you want to retain logs
- `deploy/cloudflared/credentials.json` and `deploy/cloudflared/config.yml` for tunnel access

If you move the deployment to another machine, those files are the state you need to bring with it.
