# DayZ Log Analyzer (web UI)

The browser front end of `dayzctl web`. It renders the ADM analysis produced by
the Go backend — it does not parse anything itself.

```
browser ──GET /api/instances──▶ dayzctl web ──▶ <install>/profiles/<instance>/*.ADM
        ──POST /analyze───────▶            ──▶ internal/logs (parser + aggregate)
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
  address: 127.0.0.1   # loopback unless auth is on or a reverse proxy fronts it
  port: 8081
  auth:                # login switch, off by default
    enabled: false
    session_hours: 12
    users:             # local accounts
      - username: "admin"
        # password: "change-me"
        # password_hash: "<sha256 hex>"
    steam:             # Steam SSO, allowlist required
      enabled: false
      # public_url: "https://dayz.example.com"
      allowed_ids: ["76561198000000000"]
      names: {"76561198000000000": "Alice"}
```

`dayzctl apply` generates `/etc/systemd/system/dayz-web.service`, runs
`daemon-reload` and enables it. Nothing is shipped as a ready-made unit, so the
file always matches the configuration: it is written when `web.enabled` is true
and removed (and disabled) when it is false, and an unchanged unit is left
alone — `daemon-reload` only runs when a unit's content actually changed. The
unit runs the binary directly, restarts on failure and applies hardening
(`ProtectSystem=full`, `ProtectHome=read-only`, `NoNewPrivileges`, `PrivateTmp`).

```bash
systemctl status dayz-web.service
journalctl -u dayz-web.service -f
curl -s localhost:8081/api/health
```

Access is open by default, so keep the loopback address unless you turn on the
login switch (`web.auth.enabled`) or terminate TLS and auth in nginx/Caddy in
front of it. With auth on, `/login` asks for a local username and password
and/or offers *Sign in with Steam*; every `/api/` call needs the session
cookie, the header shows who is signed in with a *Sign out* button, and
`/api/health` reports `user` and `via` (password/steam). Steam sign-ins are
verified server-side with Steam and then checked against
`web.auth.steam.allowed_ids`, so an authenticated Steam account that is not
listed gets a 403 page. Sessions are held in memory and die with the process.

## API

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/health` | service status, version, instance count, `auth: true` when a login is required, plus `user`/`via` once signed in |
| `GET` | `/api/instances` | configured instances with log counts and latest mtime |
| `GET` | `/api/instances/{instance}/logs` | log files in that instance's profiles folder |
| `GET` | `/api/instances/{instance}/map-image` | the instance's map background, if one is configured (404 otherwise) |
| `POST` | `/api/instances/{instance}/analyze` | full report; body `{"files": [...]}`, omit to analyse all |

Errors are JSON: `{"error": "..."}` with 404 (unknown instance), 422 (nothing to
analyse / unreadable file), 400 (bad body) or 401 (login required). Responses
are gzipped when the client accepts it.

File names are validated against `filepath.Base`, so a request cannot walk out
of the instance's profiles folder.

There is no file picker: opening an instance analyses **every ADM log** in its
profiles folder and goes straight to the report. A rotated folder of hundreds of
files is therefore one click, and repeat visits are served from the server's
signature-based cache.

Only the ADM admin log is read. RPT engine logs are larger, mostly per-second
FPS/memory telemetry, and are not parsed at all.

The results header names the instance, how many ADM logs were parsed and which
time window the map opens on. *Re-analyse* re-reads the folder and *Change
instance* goes back to the list.

## Tabs

- **Overview** — activity KPIs (period, players, deaths, suspicious events,
  chat, build actions), top players and weapons used.
- **Players** — sessions, playtime, kills, deaths, K/D, chat, builds, flags.
  Players whose last session was never closed were still connected when the log
  ends: their row is tinted green, marked *online* and counted in the card
  title. Tick several players to compare them: their details stack below the
  table, and *Show selected only* narrows the list to them. The Map tab can then
  draw just those players. Players with no activity inside the Timeline window
  are left out of the list, with a one-click *Show the whole log* to undo it.
- **Kills** — PvP kills with weapon and distance; sort by time or longest shot.
- **Base building** — build/place actions with coordinates, split by a *Type*
  column into structures and *traps* (bear traps, land mines, claymores,
  tripwires), which are deployables rather than base parts.
- **Suspicious** — combat logging, impossible movement, shared ids, short
  sessions, kicks and bans.
- **Timeline** — activity per hour of day, with a *From*/*to* window so a night
  shift or a single evening can be inspected on its own. An overturned range
  (22:00 → 04:00) is treated as a window across midnight.
- **Map** — positions with kills, trails, bases, heatmap and position jumps.
  Trails are drawn by default: per-player lines for the 12 busiest players on a
  normalised timeline, or for whichever players are ticked. A trail uses the
  same colour as its player's name everywhere else in the UI (Players, Kills,
  Base building, Suspicious and the *Players* picker), so a line can be matched
  to a name without a legend. Trails are painted under the event dots and are
  thick enough to trace at any zoom. Kills, build actions,
  position jumps and last positions leave a dot that is always larger than the
  trail it sits on; hovering one names the event (time, players,
  item), and a dot shared by several events lists the players involved.
  Every recorded position also leaves a dot: ADM only writes a position when
  something happened (a hit, a build, an emote, a logout), so those dots are the
  places a player really was and the line only joins them. Hovering one names
  what happened there — *hit by Infected into Torso for 7.2 damage*, *emote
  EmoteSitA*, *disconnected*. A trail is split at respawns and reconnects, so it
  never draws a line across the map between two unrelated lives.
  *Logins* adds a dot at every connect (green) and disconnect (grey ring), so a
  trail can be matched to the session that produced it; a disconnect that came
  with a kick/ban line is a red ring and reads *kicked*. A disconnect line with
  no usable position (DayZ often writes the `-3.4e38` sentinel) is pinned to the
  last place the player was seen, so the session still ends on the map. A missing
  logout emote proves nothing — logging off inside a vehicle writes no
  `EmoteSitA` — so such a disconnect is not called a crash. **A dot that holds
  only session events is summarised instead of listed**: `emote + disconnect` is
  a *logout* (or *last position* when it is the player's final spot), and a dot
  with a connect after a disconnect is a *login*.
  *Dead* marks every place a player died (triangle), from PvP kills and from
  environmental deaths. Hits from infected are grouped into one dot per attack
  and summarised on hover (*7 infected hits · 34 damage*) instead of piling up
  dozens of dots, and traps draw as magenta squares so they are not read as
  walls. *Heatmap* is a smooth density field — soft blobs added together and
  tinted from orange to crimson — scaled to the busiest 3 % of cells, so it
  shows where players spend their time without covering the roads underneath.
  *Players* is a multi-select list (limited to players with samples inside the
  map's own timeline window, so it only offers what can be drawn): tick any
  number of players and every layer is
  limited to them — heatmap, kills (either side of the fight), bases, last
  positions and jumps, not just the trails. The set is shared with the Players
  tab. The *Timeline* window under the toolbar narrows every layer to a
  from/until span, so a busy day can be cut down to a single fight. It opens on
  the **last 6 hours** of the analysed period — a rotated folder can span weeks,
  and what just happened is what an admin looks at — with *Last 1h*, *6h*,
  *24h* and *Whole period* presets to move it in one click.

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

The canvas is sized to the window — as wide as its card, and as tall as the
space left once the toolbar, legend and hint are accounted for, capped to the
width — so opening the tab brings the whole map into view instead of a long
page. On short windows the card tightens (hidden hint, smaller paddings) to keep
the map on screen.

The map fits that frame by **width**: at 1× one world width spans the whole
canvas, so a wide window gets a big map instead of a small square with empty
space on the sides. The surplus height is cropped, not letterboxed. The map
zooms and pans: the wheel (or a trackpad pinch) zooms about the pointer, a
double-click zooms in at the pointer, dragging moves the view, and **Fit**
returns to the width fit. That is also the lowest zoom — it can never be zoomed
out to show empty bands beside the map — while panning is clamped to the map
edges. Zooming in stops where the tile pyramid runs out, about 4× upscaling,
instead of turning into mush.

### Map imagery and place names

The Map tab shows satellite tiles plus place names for the selected map, served
by `dayzctl` itself:

| Endpoint | Purpose |
| --- | --- |
| `GET /api/maps` | installed maps (id, name, extent, zoom range) |
| `GET /api/maps/{map}/meta` | world size, zoom limits, tile URL template, place names |
| `GET /api/maps/{map}/tiles/{z}/{x}/{y}` | one pyramid tile |

The map is chosen with `instances[].map` and **defaults to the instance name**,
so an instance called `chernarusplus` shows Chernarus with no configuration at
all. Seven maps ship with the binary — Chernarus, Livonia, Sakhal, Namalsk, Deer
Isle, Banov and Nasdara — each with zoom levels 3 to 5. The tiles are the map
host's **topographic** rendering (the drawn road map, not the satellite
photograph), so roads and streets stay readable under the trails and dots, and
at 1.9 m per pixel they stay sharp several zoom steps past the full-map view.

**Nothing is downloaded at runtime.** Map data is generated ahead of time and
committed under `web/log-analyzer/maps/<map>/`, then embedded into the binary —
so the service works with no internet access and never depends on a third-party
host being up. To refresh a map (or add another one), regenerate the files:

```sh
go run ./tools/mapfetch -official                    # the bundled maps, z3–z5
go run ./tools/mapfetch -map chernarusplus -zooms 3,4,5
```

The generator converts each map's place names to DayZ world metres and writes
`meta.json` plus the tiles. It skips tiles that already exist, so an interrupted
run resumes; pass `-force` to re-download. Commit the result and rebuild —
`go:embed` picks the files up automatically.

Community maps can be added without touching the binary: generate them into the
maps directory the server reads (`-out /usr/local/share/dayzctl/maps`, or
`$DAYZCTL_MAPS_DIR`), restart the service, and they appear in the picker — the
on-disk copy wins over the embedded one.

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
index.html          shell: instance picker, tabs
styles.css          dark theme
js/
  main.js           UI flow and state
  api.js            fetch wrappers + report normalisation
  ui.js             rendering for every tab
  mapview.js        canvas map renderer
  format.js         number/time helpers
```

Backend counterpart:

```
internal/logs/      ADM parser, aggregation, file discovery, cache
internal/webapp/    HTTP handlers, gzip, static asset serving
web/assets.go       go:embed of this folder
cmd/dayzctl/commands/web.go   `dayzctl web`
```

## What real server logs look like

Measured against a production server (1,039,290 RPT lines / 44 MB and 185 ADM
lines for one day; only the ADM is parsed):

- ADM positions can be the float sentinel `-3.4e38`; those are dropped instead
  of being plotted at the edge of the world.
- ADM writes `is connecting` (handshake, ignored) as well as `is connected`, and
  a respawn re-uses the `is connected` line.
- One day of RPT is about 40× the size of the same day's ADM, and about 97 % of
  it is per-second telemetry — which is why RPT logs are not read at all.

Cold analysis of that day's ADM takes milliseconds; the result is cached until a
file's size or mtime changes, after which the dashboard reloads instantly.

## Known limits

- One analysis request at a time per instance; results are cached (last 8
  selections) until a file's size or mtime changes.
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
