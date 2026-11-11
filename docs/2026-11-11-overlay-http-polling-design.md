# Overlay HTTP Polling Design

## Goal

Keep the OBS browser document loaded while points and set history change. The
previous overlay reloaded the entire page every two seconds, which could flash
during a broadcast. The main overlay still needs to switch between the planned,
live, intermission, and finished layouts. This design addresses
[issue #30](https://github.com/smith3v/dash14/issues/30).

## Overlay behavior

The main overlay remains at `/overlay.html`, with `/` as an alias. The
standalone scoreboard remains at `/intermission.html`. Go renders both pages
from the existing templates. A shared updater polls the read-only
`GET /api/v1/game` endpoint on the same origin.

The response contains the match phase, current set, home side, points, sets
won, and set history. It carries a separate layout revision for each page.
A page updates the relevant counters and set rows in place when its revision
matches. When its revision changes, it reloads the current URL once to get
the new layout. The revision includes the template and updater contents, so
refreshing identical templates keeps the current document.

| Event | Main overlay | Standalone intermission |
| --- | --- | --- |
| Point, correction, or undo | Updates in place | Updates in place |
| Match starts, set finishes, next set starts, or match finishes | Reloads to show the new layout | Updates in place |
| Teams switch sides during a live set | Reloads with the new side assignment | Keeps the fixed home and guest positions |
| A new match or changed team details | Reloads | Reloads |
| Template edit | Reloads if the page uses that template | Reloads if the page uses that template |

The set history shows an active set after its first point. Undo back to
`0–0` hides the row for the unfinished set. The side control can set the
starting positions before the game begins. The live scoreboard uses that
assignment when it appears.

Polling waits two seconds between requests, with a five-second timeout and
one request in flight. HTTP or validation failures leave the last good
display visible. Pages, API responses, and the updater use
`Cache-Control: no-store` so a reload can pick up the published layout.
`?updates=off` freezes a page for local inspection.

## Publication and recovery

Telegram signals the overlay publisher after a successful database commit.
The signal is nonblocking, and rapid writes can share one pending signal.
One worker reads the latest committed match, teams, and sets in a SQLite
transaction. Template refresh uses the same publisher. This keeps rendering
off the Telegram response path and lets both overlay pages and their API
response come from one match snapshot.

The publisher renders both pages and builds the JSON response before
replacing the HTTP snapshot. A failed refresh or render leaves the previous
snapshot available. A failed publication is retried. The app can serve a
transparent polling page with `game: null` before it has a match to show.
At startup it restores the active match, or the most recent finished match
when no active match exists. Static HTML and logo exports remain available
for inspection, and export failures are logged separately from HTTP publication.

## Docker deployment and local debugging

The Go server defaults to `127.0.0.1:8081` for native runs. In Docker it binds
to `0.0.0.0:8081` inside the app container. Compose exposes the host loopback
endpoint at `127.0.0.1:8080`. Cloudflared connects directly to
`http://app:8081` over the Compose network, keeping the existing public hostname
and OBS URLs while removing the Nginx service.

The standalone development Compose stack uses a separate Telegram test bot,
isolated SQLite data and exports, and bind-mounted templates with a one-second
refresh interval. Setup, cutover, and rollback commands live in the
[deployment guide](../DEPLOYMENT.md).
