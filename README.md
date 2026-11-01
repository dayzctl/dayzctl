# dayzctl

A modern, CLI-first DayZ server management tool.

Built in Go and designed to manage multiple DayZ server instances from a single configuration file.

## Key features

- Manage multiple DayZ instances (start/stop/restart/status)
- RCON integration: query players, send commands, kick/ban/say
- Mods management: list, add, remove, sync (instance-first UX)
- Configuration-driven: single YAML config at `/etc/dayzctl/config.yaml` (default)
- Embedded long-form help: all command help lives in `cmd/dayzctl/help/*.md` and is embedded at compile time
- CI: GitHub workflows run tests and lint; release jobs run tests before packaging

## Install

Install (one-liner):

This repository provides an installer script at `scripts/install.sh`. To install `dayzctl` from the `main` branch, run:

```bash
bash -c "$(curl -fsSL https://raw.githubusercontent.com/dayzctl/dayzctl/refs/heads/main/scripts/install.sh)"
```

The script also installs the log analyzer's map data:

- running from a checkout, it copies `web/log-analyzer/maps` to
  `/usr/local/share/dayzctl/maps`;
- running from a release, it downloads `dayzctl-maps.tar.gz` next to the binary
  (if that asset is missing it warns and the analyzer uses the maps compiled
  into the binary, or draws a grid).

Either way the data lands in a location `dayzctl` already checks, so there is
nothing to configure. Override the target with
`MAPS_DIR=/some/path bash scripts/install.sh` if you want the files elsewhere.
`scripts/uninstall.sh` offers to remove the data again.

Releases publish two assets:

| Asset | Contents |
| --- | --- |
| `dayzctl_<version>_<os>_<arch>.tar.gz` | the binary with the map data compiled in (~28 MB) |
| `dayzctl-maps.tar.gz` | the same map data as files, for every platform (25 MB) |

The binary works on its own — download it and the Map tab has terrain. The maps
archive exists so an installed server can keep the imagery as plain files and
update or add maps without rebuilding; it is also what `make maps-archive`
produces locally.

### Debian package (binary + map data)

`make deb` produces `build/dayzctl_<version>_amd64.deb`. Where the release
binary embeds the tiles, the package ships them as files so the system copy
stays lean:

| Path | Contents |
| --- | --- |
| `/usr/bin/dayzctl` | the binary, built with `-tags nomaps` (no tiles, 11 MB) |
| `/usr/share/dayzctl/maps/` | 46 maps (tiles + place names), ~33 MB |
| `/etc/dayzctl/config.yaml` | starter configuration (conffile) |

The package ships **no** systemd units. `dayzctl apply` is the only thing that
writes them (`dayz-web.service`, the instance units, the update/prune timers),
enables what the configuration asks for, and reloads systemd only when a unit's
content actually changed — so a repeated apply is a no-op and an upgrade can
never leave a stale service behind.

The server reads map data from disk; the copy embedded at build time is only a
fallback. Nothing is downloaded at runtime, and the installed binary is 11 MB
instead of 40 MB because the tiles live outside it.

#### Where map data is read from

The location is fixed and hardcoded — there is no config setting for it and
installing map data never modifies `config.yaml`. Lookup stops at the first
entry that exists, and each layer falls back to the tiles embedded in the
binary:

| Order | Location |
| --- | --- |
| 1 | `$DAYZCTL_MAPS_DIR` — environment override, handy for tests |
| 2 | `maps/` next to the `dayzctl` binary — portable/tarball installs |
| 3 | `/usr/local/share/dayzctl/maps` — where `install.sh` puts the data |
| 4 | `/usr/share/dayzctl/maps` — where the `.deb` puts the data |

The startup log of `dayzctl web` prints the resolved directory
(`mapsDir=/usr/share/dayzctl/maps`) or `mapsDir=embedded` when no data is found
on disk.

```bash
sudo dpkg -i build/dayzctl_1.0.0_amd64.deb
sudoedit /etc/dayzctl/config.yaml        # add your instance(s)
sudo dayzctl apply                       # writes and enables the systemd units
sudo systemctl start dayz-web            # optional: start the UI now
```

The package needs no dependencies beyond libc; `dpkg-deb` is not required to
build it, so `make deb` works on macOS too.

## Build (compile from source)

Prerequisites:

- Go 1.26+ is required to build from source.
- Root privileges to install system units (recommended when running the binary)

Build locally (from source):

```bash
# from repository root
go mod download
gofmt -w .
go test ./... -v
go build -o build/dayzctl ./cmd/dayzctl
```

Cross-build example:

```bash
GOOS=linux GOARCH=amd64 go build -o build/dayzctl-linux-amd64 ./cmd/dayzctl
```

## Configuration

Default config path: `/etc/dayzctl/config.yaml`. Pass `--config <path>` (a global
flag, so it comes before the subcommand: `dayzctl --config /tmp/test.yaml web`)
to use another file.

You can override in tests or custom invocations using `DAYZCTL_CONFIG` when `DAYZCTL_SKIP_ROOT=1` is set (test harnesses use this to avoid writing to `/etc`).

Environment variables:

- `DAYZCTL_SKIP_ROOT=1` — skip the root privilege check (useful in tests)
- `DAYZCTL_CONFIG` — path to an alternate config file when `DAYZCTL_SKIP_ROOT=1`

## Web UI

`dayzctl web` serves an ADM/RPT log analyzer from the server itself and exposes a
a small JSON API. Parsing happens in Go (`internal/logs`), so the browser only
renders the report and no log ever leaves the host.

```bash
sudo dayzctl web                        # http://127.0.0.1:8081
sudo dayzctl web --addr 0.0.0.0 --port 8081
```

The generated config enables it (`web.enabled: true`, loopback only), and
`dayzctl apply` writes and enables `dayz-web.service` from it. Set
`web.enabled: false` to have apply remove and disable the unit again; apply says
which of the two it did. See `web/log-analyzer/README.md` for the API, the
analysis tabs and how to extend the RPT rule set.

### Login switch

The UI is open by default, which is only safe while it listens on loopback. To
require a sign-in, enable `web.auth`:

```yaml
web:
  enabled: true
  address: 0.0.0.0        # safe now that a login is required
  port: 8081
  auth:
    enabled: true
    username: "admin"
    password: "change-me"         # or: password_hash: "<sha256 hex>"
    session_hours: 12             # default 12
```

`printf %s 'change-me' | sha256sum` produces the value for `password_hash`, so
the file does not have to hold a plain password. With the switch on, the UI
serves a sign-in page at `/login`, every `/api/` call answers `401` without a
session cookie, and the header grows a *Sign out* button. Sessions live in
memory, so restarting `dayzctl web` (or the service) signs everyone out.

## Commands

Top-level commands (run `dayzctl <command> --help` for extended embedded help):

- `rcon` — RCON commands for an instance (`dayzctl rcon <instance> <command>`)
- `list` — List all configured instances
- `start` — Start a server instance or all instances
- `stop` — Stop a server instance or all instances
- `restart` — Restart a server instance or all instances
- `status` — Show status of server instance(s)
- `logs` — Show (and follow) server logs from journald
- `apply` — Apply configuration and generate systemd units
- `update` — Update DayZ server and sync mods
- `validate-config` — Validate the configuration file
- `render-config` — Render server config for an instance (dry-run)
- `check-space` — Check available disk space for install directory
- `steam-login` — Interactive Steam login for workshop operations
- `version` — Print version information
- `mods` — Manage mods for an instance (`dayzctl mods <instance> <command>`)
- `web` — Serve the ADM/RPT log analyzer UI and its JSON API (`dayzctl web`)

Example: show embedded help for `mods`:

```bash
./build/dayzctl mods --help
# or
./build/dayzctl mods
```

## Help system

All extended, user-facing help is stored in `cmd/dayzctl/help/*.md` and embedded into the binary at compile-time using Go's `embed`.

This ensures a single source-of-truth for long-form command documentation and consistent formatting across platforms.

## Viewing logs

`dayzctl logs` wraps `journalctl -u dayz@<instance>.service` and accepts tail-style options:

```bash
dayzctl logs                     # print command help
dayzctl logs nasdara             # last 100 entries
dayzctl logs nasdara -f          # follow new entries
dayzctl logs nasdara -200f       # last 200 entries, then follow
dayzctl logs -n all nasdara      # entire journal for the unit
dayzctl logs -n all all          # entire journal for every enabled instance
dayzctl logs --since "1 hour ago" -f all
```

With no arguments the command prints its help; use `all` to show the logs of
every enabled instance.

## Gentle shutdowns and scheduled restarts

- Immediate graceful shutdown (on-demand): use RCON to request the server save state and exit cleanly:

```bash
dayzctl rcon <instance> shutdown
```

- `stop`/`restart` now try a graceful shutdown first when RCON is enabled. Use the flags to control behavior:

```bash
dayzctl stop <instance> --timeout 60      # wait up to 60s for graceful shutdown, then force
dayzctl stop <instance> --force           # skip graceful attempt and stop immediately
dayzctl restart <instance> --timeout 120  # wait up to 120s, then restart
```

- Scheduled warnings and self-shutdown: configure `shutdown_messages` **once at the top level** of the config to
	write the mission `db/messages.xml` of every map in use (broadcasts countdown warnings and can trigger the
	server's built-in graceful shutdown when a message has `shutdown: true`). DayZ reads the file from the
	mission folder, not per instance, so it is generated once per mission template - instances sharing a map
	share the file. These files are written to:

```
<installDir>/mpmissions/<template>/db/messages.xml
```

Configure examples are available in `configs/config.yaml.tmpl`.

## Testing & CI

Run unit and integration tests locally:

```bash
go test ./... -v
# integration tests
go test -v ./test/integration
```

GitHub Actions:
- `.github/workflows/build.yml` runs unit tests, linting, and integration tests on push and PRs.
- `.github/workflows/prerelease.yml` and `.github/workflows/release.yml` now run tests before packaging/releases.

## Contributing

- Fork the repo and create a branch for your change.
- Keep behavior backwards-compatible where possible.
- Add tests for new behavior or bug fixes.
- Run `gofmt -w .` before committing.
- Open a pull request describing the change and include test results.

## Troubleshooting

- If you see `No help available for '<cmd>'`, ensure a corresponding `cmd/dayzctl/help/<cmd>.md` file exists and is included at compile time.
- To run commands as non-root for development, set `DAYZCTL_SKIP_ROOT=1` and `DAYZCTL_CONFIG` to a local config.

## License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.

---

If you'd like, I can:

- Add example `config.yaml` templates under `scripts/` or `examples/`.
- Create a `CONTRIBUTING.md` and `CODE_OF_CONDUCT.md`.
- Add a CI job to fail the build when embedded help is missing for any command.

Which of these should I do next?
