# Deploy To NAS Implementation Plan

**Goal:** Add a containerized deployment for `dash14` with Docker Compose, `nginx`, and Cloudflare Tunnel, plus a tag-driven GitHub Actions workflow that builds multi-arch images for `linux/amd64` and `linux/arm64`.

**Architecture:** Keep the application process unchanged: `dash14` still renders the overlay file to disk and polls Telegram. Containerization wraps the existing runtime with a multi-stage Docker image, a shared writable volume for overlay/logos/database, an `nginx` sidecar that serves the generated overlay assets, and a `cloudflared` sidecar that publishes the `nginx` endpoint on the configured domain. Release automation should build and push OCI images on git tags, while the existing test workflow remains responsible for `go test ./...`.

**Tech Stack:** Go 1.26, Docker, Docker Compose, `nginx`, Cloudflare Tunnel (`cloudflared`), GitHub Actions, GHCR

---

## Container Runtime Contract

- `dash14` keeps its current startup model and reads a mounted YAML config file. This deployment does not add an environment-variable configuration layer.
- HTML templates are baked into the app image. The mounted config file must point at those in-image template paths.
- SQLite, generated overlay output, copied logos, and optional logs live on mounted writable storage, not inside the image layer.
- `nginx` serves the shared overlay directory read-only. The app container does not serve HTTP traffic itself.
- Cloudflare Tunnel proxies the `nginx` service on the internal Docker network. Public traffic should not target the app container directly.
- The issue text saying "M-based OSX" maps to Docker images for `linux/arm64`, which is the correct target for Apple Silicon Docker hosts. Intel Synology NAS support maps to `linux/amd64`.

---

### Task 1: Define the container runtime contract

**Prompt:**
1. Read `pkg/app/runtime.go`, `pkg/config/config.go`, `pkg/config/load.go`, and `README.md` to confirm which files and directories must exist at runtime: config YAML, SQLite DB path, overlay output path, logo directory, template files, and optional log path.
2. Write a short deployment contract section at the top of the implementation branch notes or PR description. State the exact container assumptions:
   - the app reads a mounted config file, not environment variables;
   - templates are baked into the image;
   - SQLite, generated overlay output, logos, and logs live on mounted volumes;
   - `nginx` serves the overlay directory read-only;
   - `cloudflared` proxies the `nginx` service, not the app container directly.
3. Explicitly document the platform assumption that issue text saying “M-based OSX” maps to Docker images for `linux/arm64`, which run on Apple Silicon Docker hosts.
4. No code changes in this task unless the current docs materially contradict the runtime contract.
5. Commit with subject `docs: define container deployment contract`.

---

### Task 2: Add the application container image

**Prompt:**
1. Create `Dockerfile` and `.dockerignore`.
2. Use a multi-stage build:
   - builder stage compiles `./cmd/dash14` to a static-ish Linux binary;
   - runtime stage contains the binary, `templates/`, and a non-root user;
   - default command runs `dash14 --config /config/config.yaml`.
3. Ensure the runtime image layout matches the app’s current path expectations. Do not rewrite the app to use environment variables unless you find a hard blocker in Task 1.
4. Keep writable paths outside the image layer, for example `/data`, `/out`, and `/logs`, with those paths referenced from the mounted config file.
5. Add a short smoke-check section to the task notes with exact commands:
   - `docker build -t dash14:dev .`
   - `docker run --rm dash14:dev --help` or equivalent command override to verify startup wiring.
6. Commit with subject `docker: add app container image`.

---

### Task 3: Add `nginx` static serving for the overlay

**Prompt:**
1. Create `deploy/nginx/default.conf`.
2. Serve the shared overlay output directory from `nginx`:
   - `/` should return the generated overlay HTML file or directory index behavior chosen explicitly;
   - logo assets copied next to the overlay output must be reachable by relative paths;
   - add conservative cache headers for logos and no-cache headers for the overlay HTML if needed so OBS/browser refresh behavior stays predictable.
3. Keep `nginx` configuration minimal. Do not add TLS termination; Cloudflare Tunnel handles public ingress.
4. Validate the config with:
   - `docker run --rm -v "$PWD/deploy/nginx/default.conf:/etc/nginx/conf.d/default.conf:ro" nginx:stable nginx -t`
5. Commit with subject `deploy: add nginx overlay config`.

---

### Task 4: Add Docker Compose deployment assets

**Prompt:**
1. Create:
   - `docker-compose.yml`
   - `deploy/cloudflared/config.yml.example`
   - `deploy/config/config.container.example.yaml`
   - optionally `deploy/.env.example` if variable substitution materially reduces duplication.
2. Compose must define exactly three services:
   - `app`: built from the local Dockerfile or image reference, mounts config and writable data dirs;
   - `nginx`: uses the shared overlay output volume and custom config;
   - `cloudflared`: uses the example config pattern and depends on `nginx`.
3. Use named volumes or explicit bind mounts for:
   - SQLite database and app state;
   - generated overlay output;
   - managed logos;
   - optional logs.
4. Ensure service wiring is concrete:
   - `app` writes overlay files into the shared output mount;
   - `nginx` serves that same mount read-only;
   - `cloudflared` points at `http://nginx:80`.
5. Validate with:
   - `docker compose config`
6. Commit with subject `deploy: add docker compose stack`.

---

### Task 5: Document deployment setup and operational flow

**Prompt:**
1. Update `README.md` with a deployment section for NAS/container usage.
2. Document:
   - required files to copy and edit;
   - how to prepare `config.yaml` for container paths;
   - how to prepare Cloudflare Tunnel credentials/config;
   - how to launch with `docker compose up -d`;
   - where overlay output is served from;
   - how team import works in containers, including an exact one-shot command such as `docker compose run --rm app --config ... --import ...` or the equivalent override for the image command.
3. Add a short section on persistence and backups covering the SQLite DB, logo directory, and tunnel credentials.
4. Keep the doc explicit that the app itself is not an HTTP server; `nginx` serves the generated files.
5. Commit with subject `docs: add nas deployment guide`.

---

### Task 6: Add release image build workflow for git tags

**Prompt:**
1. Create `.github/workflows/release-images.yml`.
2. Trigger the workflow on pushed tags matching a clear release pattern such as `v*`.
3. Build and push a multi-platform image for:
   - `linux/amd64`
   - `linux/arm64`
4. Use standard GitHub Actions components:
   - `actions/checkout`
   - `docker/setup-qemu-action`
   - `docker/setup-buildx-action`
   - `docker/login-action`
   - `docker/metadata-action`
   - `docker/build-push-action`
5. Publish to GHCR under the current repo namespace, with tags for the git tag and optionally `latest` only on stable tags if that choice is made explicitly.
6. Keep the existing `.github/workflows/tests.yml` unchanged unless a small refactor is necessary to avoid duplication.
7. Commit with subject `ci: build release images on tags`.

---

### Task 7: Add deployment-oriented verification steps

**Prompt:**
1. Add a short verification section to `README.md` or a dedicated deploy doc that lists the exact checks to run after bringing the stack up:
   - `docker compose ps`
   - `docker compose logs app`
   - `docker compose logs nginx`
   - `docker compose logs cloudflared`
   - local `curl` against the `nginx` service endpoint if applicable
2. Verify that the current test suite still passes locally:
   - `go test ./...`
3. Verify deployment assets are syntactically valid:
   - `docker build -t dash14:dev .`
   - `docker compose config`
   - `nginx -t` via the containerized check from Task 3
4. If any of these commands expose gaps in the deployment assets, fix them in the same task before committing.
5. Commit with subject `docs: add deployment verification steps`.

---

### Task 8: Tighten release and deployment polish

**Prompt:**
1. Do a final pass across:
   - `Dockerfile`
   - `docker-compose.yml`
   - `deploy/nginx/default.conf`
   - `deploy/cloudflared/config.yml.example`
   - `.github/workflows/release-images.yml`
   - `README.md`
2. Remove avoidable complexity:
   - no second app container;
   - no embedded reverse proxy in the app image;
   - no runtime shell scripts unless strictly necessary;
   - no environment-variable configuration layer unless the mounted YAML approach proved insufficient.
3. Confirm the plan outcome against issue `#14`:
   - three-service compose deployment;
   - domain served through Cloudflare Tunnel;
   - tag-driven multi-arch image builds for NAS and Apple Silicon Docker hosts.
4. Commit with subject `deploy: finalize nas container workflow`.
