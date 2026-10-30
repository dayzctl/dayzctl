# DayZ Log Analyzer (web UI)

The browser front end of `dayzctl web`. It renders ADM/RPT analysis produced by
the Go backend — it does not parse anything itself.

```
browser ──GET /api/instances──▶ dayzctl web ──▶ <install>/profiles/<instance>/*.ADM|*.RPT
        ──POST /analyze───────▶            ──▶ internal/logs (parser + rules + aggregate)
        ◀──report JSON────────            ◀── cached by file signature
```

Parsing server-side means one implementation shared with the CLI, no multi-
hundred-MB transfer into the browser, and repeat visits served from cache.

## Run it

```bash
# development: foreground, loopback
sudo dayzctl web

# explicit address/port
dayzctl web --addr 0.0.0.0 --port 8081
```

The assets are embedded in the binary (`go:embed`), so nothing else has to be
copied to the server. For front-end work you can still serve the folder directly,
but the API must be reachable on the same origin:

```bash
cd web/log-analyzer && python3 -m http.server 8765
```

## Configuration

```yaml
web:
  enabled: true        # `dayzctl apply` writes and enables dayz-web.service
  address: 127.0.0.1   # loopback unless you have a reverse proxy
  port: 8081
```

`dayzctl apply` generates `/etc/systemd/system/dayz-web.service`, runs
`daemon-reload` and enables it. The unit runs the binary directly, restarts on
failure and applies hardening (`ProtectSystem=full`, `ProtectHome=read-only`,
`NoNewPrivileges`, `PrivateTmp`).

```bash
systemctl status dayz-web.service
journalctl -u dayz-web.service -f
curl -s localhost:8081/api/health
```

There is no built-in authentication. Keep the default loopback address, or
terminate TLS and auth in nginx/Caddy before binding to a public interface.

## API

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/health` | service status, version, instance count |
| `GET` | `/api/instances` | configured instances with log counts and latest mtime |
| `GET` | `/api/instances/{instance}/logs` | log files in that instance's profiles folder |
| `GET` | `/api/instances/{instance}/map-image` | the instance's map background, if one is configured (404 otherwise) |
| `POST` | `/api/instances/{instance}/analyze` | full report; body `{"files": [...]}`, omit to analyse all |

Errors are JSON: `{"error": "..."}` with 404 (unknown instance), 422 (nothing to
analyse / unreadable file) or 400 (bad body). Responses are gzipped when the
client accepts it.

File names are validated against `filepath.Base`, so a request cannot walk out
of the instance's profiles folder.

A server keeps months of rotated logs, so the file picker scrolls instead of
growing the page; the progress bar sits directly above the list it describes.
The **All / RPT / ADM** chips filter the list by type, and *Select all* acts on
whatever the filter is showing, so you can analyse every ADM of a week without
clicking through the RPT files.

## Tabs

- **Overview** — problem counts by severity, activity KPIs, log origin
  (server vs. game client), end-of-log state, mods, top players, weapons.
- **Problems** — every distinct RPT pattern with an explanation, a concrete fix,
  and an `also vanilla` tag for stock-server noise.
- **Players** — sessions, playtime, kills, deaths, K/D, chat, builds, flags.
  Tick several players to compare them: their details stack below the table, and
  *Show selected only* narrows the list to them. The Map tab can then draw just
  those players.
- **Kills** — PvP kills with weapon and distance; sort by time or longest shot.
- **Base building** — build/place actions with coordinates.
- **Suspicious** — combat logging, impossible movement, shared ids, short
  sessions, kicks and bans.
- **Timeline** — activity per hour of day, with a *From*/*to* window so a night
  shift or a single evening can be inspected on its own. An overturned range
  (22:00 → 04:00) is treated as a window across midnight.
- **Map** — positions with kills, trails, bases, heatmap and position jumps.
  *Players* is a multi-select list: tick any number of players and only their
  trails and highlights are drawn (the same set as the Players tab). The
  *Timeline* window under the toolbar narrows every layer to a from/until span,
  so a busy day can be cut down to a single fight.

## Coordinates

DayZ writes `pos=<x, y, z>` as **east, north, altitude**. The map plots `x`
horizontally and `y` vertically (north up), scaled by the map's world size.

The **Map** selector switches which map is drawn: the instance's own map (from
`instances[].map`, defaulting to the instance name), any other map installed on
the server, or no imagery at all. Picking a map loads its extent, tiles and
place names, so plotting Nasdara positions on the Chernarus map is one click.
The world size follows the selected map; typing a number overrides it, and
without map metadata the size is inferred from the ADM positions, rounded up to
whole kilometres.

### Map imagery and place names

The Map tab shows satellite tiles plus place names for the selected map, served
by `dayzctl` itself:

| Endpoint | Purpose |
| --- | --- |
| `GET /api/maps` | installed maps (id, name, extent, zoom range) |
| `GET /api/maps/{map}/meta` | world size, zoom limits, tile URL template, place names |
| `GET /api/maps/{map}/tiles/{z}/{x}/{y}` | one pyramid tile |

The map is chosen with `instances[].map` and **defaults to the instance name**,
so an instance called `nasdara` shows Nasdara with no configuration at all.
Only maps bundled with the binary are offered.

**Nothing is downloaded at runtime.** Map data is generated ahead of time and
committed under `web/log-analyzer/maps/<map>/`, then embedded into the binary —
so the service works with no internet access and never depends on a third-party
host being up. To refresh a map (or add another one), regenerate the files:

```sh
go run ./tools/mapfetch -map nasdara -zooms 3,4      # ~5.6 MB, 320 tiles
go run ./tools/mapfetch -map chernarusplus -zooms 3,4
```

The generator resolves the map's version, converts its place names to DayZ
world metres and writes `meta.json` plus the tiles. It skips tiles that already
exist, so an interrupted run resumes; pass `-force` to re-download. Commit the
result and rebuild — `go:embed` picks the files up automatically.

Map imagery © [dayz.xam.nu](https://dayz.xam.nu).

To use your own picture instead, set `instances[].map_image` (relative to
`paths.install_dir`, an absolute path, or an `http(s)://` URL the service
proxies) or drop `map.jpg|png|webp` into the instance's profiles folder as
`map.*`, `<instance>.*`, `<instance>-map.*` or `map_<instance>.*`. The **Custom
image** picker overrides both for the current session. Without any imagery the
tab draws a kilometre grid, which is still accurate for positions.

## Suspicious activity is a hint, not proof

| Signal | Rule | Weight |
| --- | --- | --- |
| Combat logging | Disconnect ≤ 120 s after being hit, killing, or dying | +2 |
| Impossible movement | > 100 m/s implied speed over ≥ 150 m | +3 |
| Shared player id | One `id=` seen with different names | +1 |
| Very short session | Session < 60 s | +1 |

Movement is only compared between two position samples with no death or
reconnect in between: a respawn legitimately relocates a player. The speed
threshold sits above helicopters, which reach roughly 250 km/h.

Crashes, admin teleports and harmless reconnects produce the same patterns, so
the list is a review queue. Thresholds live in `DEFAULTS` (`Internal logs.Options`,
`internal/logs/analyze.go`).

## Adding engine-message rules

Rules live in `internal/logs/rules.go` and are evaluated in order; the first
match wins:

```go
{
    ID:       "addon-requires-addon",
    Re:       regexp.MustCompile(`(?i)Addon '([^']+)' requires addon '([^']+)'`),
    Severity: SeverityError,
    Category: "mods",
    Title:    "A mod is missing one of its dependencies",
    Explain:  "{0} needs {1}, which is not loaded…",   // {n} = capture group n
    Fix:      "Add {1} to -mod= before {0}…",
    Vanilla:  false, // true => stock servers write this too
}
```

Unmatched messages are still shown (classified by their `Error:` / `Warning:`
prefix) and marked as unrecognised, so nothing is silently dropped.

## Layout

```
index.html          shell: instance picker, log picker, tabs
styles.css          dark theme
js/
  main.js           UI flow and state
  api.js            fetch wrappers + report normalisation
  ui.js             rendering for every tab
  mapview.js        canvas map renderer
  format.js         number/time helpers
  severity.js       severity vocabulary
```

Backend counterpart:

```
internal/logs/      ADM/RPT parsers, rules, aggregation, file discovery, cache
internal/webapp/    HTTP handlers, gzip, static asset serving
web/assets.go       go:embed of this folder
cmd/dayzctl/commands/web.go   `dayzctl web`
```

## What real server logs look like

Measured against a production server (1,039,290 RPT lines / 44 MB for one day,
185 ADM lines):

- About **97 % of an RPT is telemetry** — `Average server FPS`, `Used memory` and
  `Players: … in total` written once per second. They collapse into one group.
- A single day produced **7 distinct warning/error patterns** from 15,085
  occurrences, mostly `ENTITY (W)` model warnings that stock DayZ maps emit on
  every start (marked `also vanilla`).
- `[CE]` loot-economy lines repeat thousands of times; a genuine XML parse
  failure is separated out as critical.
- ADM positions can be the float sentinel `-3.4e38`; those are dropped instead
  of being plotted at the edge of the world.
- ADM writes `is connecting` (handshake, ignored) as well as `is connected`, and
  a respawn re-uses the `is connected` line.

Cold analysis of that day takes ~4 s; the result is cached until a file's size
or mtime changes, after which the dashboard reloads instantly.

## Known limits

- One analysis request at a time per instance selection; results are cached
  (last 8 selections) until a file's size or mtime changes.
- Messages beyond 4 000 distinct normalised patterns are collapsed to bound memory.
- Position samples are capped per player (1 500) — enough for trails, not for
  every heartbeat of a very long session.
- Binary crash dumps (`.mdmp`) are listed but not parsed.
- `id=` in the ADM is a per-session token in current builds, not a SteamID, so
  the shared-id signal only catches token reuse within the analysed files.

## Verifying against your own logs

`internal/logs/real_probe_test.go` prints a full summary for a folder of real
logs and is skipped unless you ask for it:

```bash
DAYZCTL_REAL_LOGS=~/profiles go test ./internal/logs -run TestProbeRealLogs -v
```
