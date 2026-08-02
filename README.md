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

Default config path: `/etc/dayzctl/config.yaml`.

You can override in tests or custom invocations using `DAYZCTL_CONFIG` when `DAYZCTL_SKIP_ROOT=1` is set (test harnesses use this to avoid writing to `/etc`).

Environment variables:

- `DAYZCTL_SKIP_ROOT=1` — skip the root privilege check (useful in tests)
- `DAYZCTL_CONFIG` — path to an alternate config file when `DAYZCTL_SKIP_ROOT=1`

## Commands

Top-level commands (run `dayzctl <command> --help` for extended embedded help):

- `rcon` — RCON commands for an instance (`dayzctl rcon <instance> <command>`)
- `list` — List all configured instances
- `start` — Start a server instance or all instances
- `stop` — Stop a server instance or all instances
- `restart` — Restart a server instance or all instances
- `status` — Show status of server instance(s)
- `apply` — Apply configuration and generate systemd units
- `update` — Update DayZ server and sync mods
- `validate-config` — Validate the configuration file
- `render-config` — Render server config for an instance (dry-run)
- `check-space` — Check available disk space for install directory
- `steam-login` — Interactive Steam login for workshop operations
- `version` — Print version information
- `mods` — Manage mods for an instance (`dayzctl mods <instance> <command>`)

Example: show embedded help for `mods`:

```bash
./build/dayzctl mods --help
# or
./build/dayzctl mods
```

## Help system

All extended, user-facing help is stored in `cmd/dayzctl/help/*.md` and embedded into the binary at compile-time using Go's `embed`.

This ensures a single source-of-truth for long-form command documentation and consistent formatting across platforms.

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

- Scheduled warnings and self-shutdown: configure `shutdown_messages` per instance in the config to write
	the mission `db/messages.xml` (broadcasts countdown warnings and can trigger the server's built-in
	graceful shutdown when a message has `shutdown: true`). These files are written to:

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
